package adminbot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/rs/zerolog/log"
)

const (
	defaultClaudeMaxTokens = 768
	// Cap how many tool-use rounds we'll let Claude take per question.
	// Each round = "model emits tool_use, we run the tool, send the result
	// back". Real questions resolve in 1-2 rounds; the cap is a safety belt
	// against a runaway loop (e.g. the model repeatedly asking for the
	// same data because it's not parsing the tool output).
	maxClaudeToolRounds = 4
)

// ToolDispatcher is implemented by *Bot. It executes a tool call by name and
// returns the textual result that gets fed back to Claude as a tool_result
// content block. The Claude client uses this so AskWithMemory doesn't need
// to import the rest of the bot.
type ToolDispatcher interface {
	DispatchClaudeTool(ctx context.Context, name string, input json.RawMessage) string
}

type ClaudeClient struct {
	client anthropic.Client
	cfg    ClaudeConfig
}

func NewClaudeClient(cfg ClaudeConfig) *ClaudeClient {
	return &ClaudeClient{
		client: anthropic.NewClient(option.WithAPIKey(strings.TrimSpace(cfg.APIKey))),
		cfg:    cfg,
	}
}

func (c *ClaudeClient) Enabled() bool {
	return c != nil && strings.TrimSpace(c.cfg.APIKey) != ""
}

// Ask sends a single-shot question to Claude (no conversation history).
// Used for telemetry-context queries and internal memory compression calls.
func (c *ClaudeClient) Ask(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("claude api key not configured")
	}

	message, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(defaultClaudeModel),
		MaxTokens: defaultClaudeMaxTokens,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("call claude: %w", err)
	}

	return extractClaudeText(message)
}

// AskWithMemory sends a question to Claude including the sender's compressed
// conversation history. Implements:
//
//  1. Anchored Iterative Summarization (memory.Summarize) for old turns.
//  2. A tool-use loop: when Claude needs more context than the pre-baked
//     telemetry snapshot provides (e.g. "who's the oldest tenant?"), it
//     emits tool_use blocks; we dispatch them via `tools` and feed the
//     results back as tool_result blocks. Repeats until end_turn or the
//     tool-round cap is hit (safety against runaway loops).
//
// The `tools` parameter may be nil — in that case the call is a plain
// single-shot like before. Pass *Bot (which implements ToolDispatcher) to
// enable the on-demand context tools.
func (c *ClaudeClient) AskWithMemory(
	ctx context.Context,
	mem *MemoryStore,
	senderID string,
	systemPrompt string,
	telemetryCtx string,
	question string,
	tools ToolDispatcher,
) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("claude api key not configured")
	}

	if mem.NeedsSummarization(senderID) {
		if err := mem.Summarize(ctx, senderID, c); err != nil {
			_ = err
		}
	}

	summary, history := mem.Snapshot(senderID)

	var messages []anthropic.MessageParam
	var preamble strings.Builder
	if telemetryCtx != "" {
		preamble.WriteString("Telemetry context:\n")
		preamble.WriteString(telemetryCtx)
	}
	if summary != "" {
		if preamble.Len() > 0 {
			preamble.WriteString("\n\n")
		}
		preamble.WriteString("Compressed conversation history:\n")
		preamble.WriteString(summary)
	}
	if preamble.Len() > 0 {
		messages = append(messages,
			anthropic.NewUserMessage(anthropic.NewTextBlock(preamble.String())),
			anthropic.NewAssistantMessage(anthropic.NewTextBlock("Understood. I have the context.")),
		)
	}
	for _, turn := range history {
		if turn.Role == "user" {
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(turn.Content)))
		} else {
			messages = append(messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(turn.Content)))
		}
	}
	messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(question)))

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(defaultClaudeModel),
		MaxTokens: defaultClaudeMaxTokens,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages:  messages,
	}
	if tools != nil {
		params.Tools = claudeTools()
	}

	answer, err := c.runToolLoop(ctx, params, tools)
	if err != nil {
		return "", err
	}

	mem.AddExchange(senderID, question, answer)
	return answer, nil
}

// runToolLoop drives the tool-use cycle. Each iteration sends the current
// message list to Claude; if the response is a plain end_turn we return its
// text. If the response asks for tools, we dispatch each tool_use block,
// append the assistant turn + a user turn carrying the tool_result blocks,
// and loop. Capped at maxClaudeToolRounds rounds.
func (c *ClaudeClient) runToolLoop(ctx context.Context, params anthropic.MessageNewParams, tools ToolDispatcher) (string, error) {
	for round := 0; round < maxClaudeToolRounds; round++ {
		resp, err := c.client.Messages.New(ctx, params)
		if err != nil {
			return "", fmt.Errorf("call claude: %w", err)
		}

		// Collect any tool-use requests in this turn. If there are none,
		// we're done — return the text and let the caller persist memory.
		var toolUses []anthropic.ToolUseBlock
		for _, block := range resp.Content {
			if tu, ok := block.AsAny().(anthropic.ToolUseBlock); ok {
				toolUses = append(toolUses, tu)
			}
		}

		if len(toolUses) == 0 || tools == nil {
			return extractClaudeText(resp)
		}

		// Echo the assistant turn (text + tool_use blocks) back into the
		// conversation, then attach a single user message containing one
		// tool_result block per tool_use, in the same order.
		params.Messages = append(params.Messages, resp.ToParam())

		toolResults := make([]anthropic.ContentBlockParamUnion, 0, len(toolUses))
		for _, tu := range toolUses {
			result := tools.DispatchClaudeTool(ctx, tu.Name, tu.Input)
			log.Debug().Str("tool", tu.Name).Int("input_bytes", len(tu.Input)).Int("result_bytes", len(result)).Msg("claude tool dispatch")
			toolResults = append(toolResults, anthropic.NewToolResultBlock(tu.ID, result, false))
		}
		params.Messages = append(params.Messages, anthropic.NewUserMessage(toolResults...))
	}

	return "", fmt.Errorf("claude tool loop exceeded %d rounds without finishing", maxClaudeToolRounds)
}

// extractClaudeText joins the text blocks from a Claude API response.
func extractClaudeText(msg *anthropic.Message) (string, error) {
	var parts []string
	for _, block := range msg.Content {
		switch variant := block.AsAny().(type) {
		case anthropic.TextBlock:
			if text := strings.TrimSpace(variant.Text); text != "" {
				parts = append(parts, text)
			}
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("claude api returned no text content")
	}
	return strings.Join(parts, "\n\n"), nil
}
