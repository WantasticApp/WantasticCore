package copilot

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
	claudeModel        = "claude-haiku-4-5"
	claudeMaxTokens    = 1024
	claudeToolLoopRnds = 6
)

// ClaudeLLM is a Copilot-side wrapper around the Anthropic SDK. It mirrors
// the adminbot's ClaudeClient but adds per-call tool catalogs (each session
// can have a different tool set, depending on role).
type ClaudeLLM struct {
	client anthropic.Client
	apiKey string
}

// NewClaudeLLM builds a ClaudeLLM. Pass the Anthropic API key (the same one
// the adminbot uses — config.AdminBot.Claude.APIKey).
func NewClaudeLLM(apiKey string) *ClaudeLLM {
	return &ClaudeLLM{
		client: anthropic.NewClient(option.WithAPIKey(strings.TrimSpace(apiKey))),
		apiKey: strings.TrimSpace(apiKey),
	}
}

// Enabled reports whether the API key is non-empty.
func (l *ClaudeLLM) Enabled() bool { return l != nil && l.apiKey != "" }

// Chat runs one Claude turn (with optional tool loop) against the provided
// history. Returns the final assistant text. Implements the LLM interface.
func (l *ClaudeLLM) Chat(ctx context.Context, system string, history []Turn, tools []ToolSpec, dispatch ToolDispatcher) (string, error) {
	if !l.Enabled() {
		return "", fmt.Errorf("claude not configured")
	}

	msgs := make([]anthropic.MessageParam, 0, len(history))
	for _, t := range history {
		if t.Role == "user" {
			msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(t.Content)))
		} else {
			msgs = append(msgs, anthropic.NewAssistantMessage(anthropic.NewTextBlock(t.Content)))
		}
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(claudeModel),
		MaxTokens: claudeMaxTokens,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Messages:  msgs,
	}
	if len(tools) > 0 {
		params.Tools = toAnthropicTools(tools)
	}

	for round := 0; round < claudeToolLoopRnds; round++ {
		resp, err := l.client.Messages.New(ctx, params)
		if err != nil {
			return "", fmt.Errorf("call claude: %w", err)
		}

		var toolUses []anthropic.ToolUseBlock
		for _, block := range resp.Content {
			if tu, ok := block.AsAny().(anthropic.ToolUseBlock); ok {
				toolUses = append(toolUses, tu)
			}
		}

		if len(toolUses) == 0 || dispatch == nil {
			return claudeTextFrom(resp)
		}

		// Append the assistant turn (text + tool_use blocks) and then a user
		// turn with one tool_result block per tool_use.
		params.Messages = append(params.Messages, resp.ToParam())
		results := make([]anthropic.ContentBlockParamUnion, 0, len(toolUses))
		for _, tu := range toolUses {
			out := dispatch.Dispatch(ctx, tu.Name, tu.Input)
			log.Debug().Str("tool", tu.Name).Int("input_bytes", len(tu.Input)).Int("result_bytes", len(out)).Msg("copilot: tool dispatch")
			results = append(results, anthropic.NewToolResultBlock(tu.ID, out, false))
		}
		params.Messages = append(params.Messages, anthropic.NewUserMessage(results...))
	}
	return "", fmt.Errorf("copilot: tool loop exceeded %d rounds", claudeToolLoopRnds)
}

func toAnthropicTools(specs []ToolSpec) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(specs))
	for _, s := range specs {
		var schema anthropic.ToolInputSchemaParam
		_ = json.Unmarshal(s.InputSchema, &schema)
		out = append(out, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        s.Name,
				Description: anthropic.String(s.Description),
				InputSchema: schema,
			},
		})
	}
	return out
}

func claudeTextFrom(msg *anthropic.Message) (string, error) {
	var parts []string
	for _, block := range msg.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			if s := strings.TrimSpace(t.Text); s != "" {
				parts = append(parts, s)
			}
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("claude returned no text content")
	}
	return strings.Join(parts, "\n\n"), nil
}
