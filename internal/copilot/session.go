// Package copilot exposes a role-aware Claude assistant to the portal.
// Tenants get a sandboxed chat with self-service tools; super-admins get the
// same chat plus admin-scoped tools (CreateTenant, SetTenantMaxPeers, etc.).
// Each session keeps its own in-memory history so contexts don't bleed.
package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	pb "WantasticCore/internal/types"
	"WantasticCore/internal/admin"
	core "WantasticCore/internal/core"
)

// Defaults — overridable via Config.
const (
	defaultTenantHistoryDepth = 40   // turns kept verbatim per tenant session
	defaultAdminHistoryDepth  = 80   // admins get a bigger window
	defaultIdleTimeout        = time.Hour
	defaultMaxToolRounds      = 6
)

// Role determines which tool catalog and history depth a session gets.
type Role string

const (
	RoleTenant Role = "tenant"
	RoleAdmin  Role = "admin"
)

// Config tunes per-role budgets. Zero values fall back to the defaults above.
type Config struct {
	TenantHistoryDepth int
	AdminHistoryDepth  int
	IdleTimeout        time.Duration
	MaxToolRounds      int
}

func (c Config) tenantDepth() int {
	if c.TenantHistoryDepth > 0 {
		return c.TenantHistoryDepth
	}
	return defaultTenantHistoryDepth
}

func (c Config) adminDepth() int {
	if c.AdminHistoryDepth > 0 {
		return c.AdminHistoryDepth
	}
	return defaultAdminHistoryDepth
}

func (c Config) idleTimeout() time.Duration {
	if c.IdleTimeout > 0 {
		return c.IdleTimeout
	}
	return defaultIdleTimeout
}

func (c Config) maxToolRounds() int {
	if c.MaxToolRounds > 0 {
		return c.MaxToolRounds
	}
	return defaultMaxToolRounds
}

// LLM is the minimal interface the copilot needs from a Claude (or compatible)
// client. We accept any implementation so the adminbot's existing client and
// future test doubles both work.
type LLM interface {
	Enabled() bool
	Chat(ctx context.Context, system string, history []Turn, tools []ToolSpec, dispatch ToolDispatcher) (string, error)
}

// Turn is one entry in a session's conversation history.
type Turn struct {
	Role    string    `json:"role"` // "user" | "assistant"
	Content string    `json:"content"`
	At      time.Time `json:"at"`
}

// ToolSpec is the JSON-schema description handed to the LLM so it knows
// what tools exist and how to call them.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ToolDispatcher executes a tool by name and returns the textual result that
// gets fed back to the LLM as the tool_result content block.
type ToolDispatcher interface {
	Dispatch(ctx context.Context, name string, input json.RawMessage) string
}

// Session holds one tenant's chat thread. Mutated only under sess.mu.
type Session struct {
	ID         string
	TenantID   string
	Role       Role
	CreatedAt  time.Time
	LastActive time.Time

	mu      sync.Mutex
	history []Turn
}

// Service is the per-process copilot manager. Construct it once at startup
// and pass it into the WebSocket dispatcher. The LLM can be swapped at
// runtime via SetLLM — used by the in-app API-key setup flow so admins
// can enable Copilot without restarting the process.
type Service struct {
	cfg      Config
	adminSvc *admin.Service
	services *core.Services

	llmMu sync.RWMutex
	llm   LLM

	mu       sync.Mutex
	sessions map[string]*Session

	stop chan struct{}
}

// New creates a Service. llm may be nil or unconfigured; in that case the
// service exists but Enabled() returns false and SendMessage fails until
// SetLLM is called with a working client. The admin service is optional;
// without it, admin-only tools fail closed.
func New(cfg Config, llm LLM, adminSvc *admin.Service, services *core.Services) (*Service, error) {
	s := &Service{
		cfg:      cfg,
		llm:      llm,
		adminSvc: adminSvc,
		services: services,
		sessions: make(map[string]*Session),
		stop:     make(chan struct{}),
	}
	go s.gcLoop()
	return s, nil
}

// Enabled reports whether the underlying LLM is configured (e.g. an API
// key is set). Handlers should check this before invoking SendMessage so
// they can surface a "configure me" affordance to the UI.
func (s *Service) Enabled() bool {
	if s == nil {
		return false
	}
	s.llmMu.RLock()
	defer s.llmMu.RUnlock()
	return s.llm != nil && s.llm.Enabled()
}

// SetLLM atomically swaps the underlying language-model client. Pass a
// freshly-constructed ClaudeLLM after the admin sets a new API key.
func (s *Service) SetLLM(llm LLM) {
	s.llmMu.Lock()
	s.llm = llm
	s.llmMu.Unlock()
}

func (s *Service) getLLM() LLM {
	s.llmMu.RLock()
	defer s.llmMu.RUnlock()
	return s.llm
}

// Close stops the idle-session GC goroutine.
func (s *Service) Close() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
}

// OpenSession creates a fresh session for the caller. role is "tenant" or
// "admin"; non-admins requesting "admin" are silently downgraded to "tenant".
func (s *Service) OpenSession(tenantID string, isAdmin bool) *Session {
	role := RoleTenant
	if isAdmin {
		role = RoleAdmin
	}
	now := time.Now().UTC()
	sess := &Session{
		ID:         uuid.New().String(),
		TenantID:   tenantID,
		Role:       role,
		CreatedAt:  now,
		LastActive: now,
	}
	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.mu.Unlock()
	log.Debug().Str("session_id", sess.ID).Str("tenant_id", tenantID).Str("role", string(role)).Msg("copilot: session opened")
	return sess
}

// CloseSession releases a session. Returns true if the session existed.
func (s *Service) CloseSession(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[id]; !ok {
		return false
	}
	delete(s.sessions, id)
	return true
}

// Get returns the session by ID; nil if missing.
func (s *Service) Get(id string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

// ListSessionsForTenant returns sessions belonging to the given tenant.
func (s *Service) ListSessionsForTenant(tenantID string) []*Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Session, 0)
	for _, sess := range s.sessions {
		if sess.TenantID == tenantID {
			out = append(out, sess)
		}
	}
	return out
}

// SendMessage runs one user turn through the LLM with the session's history
// and the role-appropriate tool catalog. The reply (final assistant text) is
// appended to the session and returned. Tool calls are dispatched in-loop
// against the in-process services; an admin tool invoked from a tenant session
// returns "forbidden" without escalating.
func (s *Service) SendMessage(ctx context.Context, sessionID, userText string) (string, error) {
	sess := s.Get(sessionID)
	if sess == nil {
		return "", errors.New("copilot: session not found")
	}

	sess.mu.Lock()
	sess.history = append(sess.history, Turn{Role: "user", Content: userText, At: time.Now().UTC()})
	histCopy := append([]Turn(nil), sess.history...)
	role := sess.Role
	sess.mu.Unlock()

	tools := s.toolsFor(role)
	dispatch := &dispatcher{svc: s, sessionRole: role}

	llm := s.getLLM()
	if llm == nil || !llm.Enabled() {
		return "", errors.New("copilot: not configured (no API key)")
	}
	systemPrompt := systemPromptFor(role)
	reply, err := llm.Chat(ctx, systemPrompt, histCopy, tools, dispatch)
	if err != nil {
		return "", err
	}

	sess.mu.Lock()
	sess.history = append(sess.history, Turn{Role: "assistant", Content: reply, At: time.Now().UTC()})
	sess.LastActive = time.Now().UTC()
	depth := s.cfg.tenantDepth()
	if role == RoleAdmin {
		depth = s.cfg.adminDepth()
	}
	if len(sess.history) > depth {
		// Drop the oldest non-system turns. The model will lose context on
		// dropped turns; for v1 that's acceptable. Future iteration could
		// summarize instead of truncate.
		sess.history = sess.history[len(sess.history)-depth:]
	}
	sess.mu.Unlock()

	return reply, nil
}

// History returns a copy of the session's conversation history.
func (s *Service) History(sessionID string) []Turn {
	sess := s.Get(sessionID)
	if sess == nil {
		return nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return append([]Turn(nil), sess.history...)
}

// dispatcher routes tool calls to in-process services, gating admin-only tools.
type dispatcher struct {
	svc         *Service
	sessionRole Role
}

func (d *dispatcher) Dispatch(ctx context.Context, name string, input json.RawMessage) string {
	tool, ok := allTools()[name]
	if !ok {
		return "error: unknown tool " + name
	}
	if tool.AdminOnly && d.sessionRole != RoleAdmin {
		return "error: tool " + name + " requires admin role"
	}
	return tool.Run(ctx, d.svc, input)
}

func (s *Service) gcLoop() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			cutoff := time.Now().Add(-s.cfg.idleTimeout())
			s.mu.Lock()
			for id, sess := range s.sessions {
				if sess.LastActive.Before(cutoff) {
					delete(s.sessions, id)
					log.Debug().Str("session_id", id).Msg("copilot: session GC'd (idle)")
				}
			}
			s.mu.Unlock()
		}
	}
}

// Ensure pb is referenced so we can plumb in callerTenantID metadata for
// tools that dispatch through the gRPC service impls.
var _ = pb.AddTenantPeerRequest{}
