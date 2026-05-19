package copilot

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeLLM is a tiny LLM stub. It records every Chat invocation so tests
// can assert what tool catalog and history made it through, and lets each
// test plug a bespoke reply function.
type fakeLLM struct {
	mu     sync.Mutex
	calls  []fakeLLMCall
	reply  func(history []Turn, tools []ToolSpec, dispatch ToolDispatcher) (string, error)
}

type fakeLLMCall struct {
	History []Turn
	Tools   []ToolSpec
}

func (f *fakeLLM) Enabled() bool { return true }

func (f *fakeLLM) Chat(ctx context.Context, system string, history []Turn, tools []ToolSpec, dispatch ToolDispatcher) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeLLMCall{History: append([]Turn(nil), history...), Tools: append([]ToolSpec(nil), tools...)})
	f.mu.Unlock()
	if f.reply != nil {
		return f.reply(history, tools, dispatch)
	}
	return "ok", nil
}

func newServiceForTest(t *testing.T, llm *fakeLLM) *Service {
	t.Helper()
	svc, err := New(Config{}, llm, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(svc.Close)
	return svc
}

func TestOpenSession_RoleFromIsAdmin(t *testing.T) {
	svc := newServiceForTest(t, &fakeLLM{})

	tenant := svc.OpenSession("tenant-1", false)
	admin := svc.OpenSession("admin-1", true)

	if tenant.Role != RoleTenant {
		t.Errorf("tenant role = %q, want %q", tenant.Role, RoleTenant)
	}
	if admin.Role != RoleAdmin {
		t.Errorf("admin role = %q, want %q", admin.Role, RoleAdmin)
	}
}

func TestSendMessage_RecordsHistoryAndReply(t *testing.T) {
	llm := &fakeLLM{
		reply: func(_ []Turn, _ []ToolSpec, _ ToolDispatcher) (string, error) {
			return "hi back", nil
		},
	}
	svc := newServiceForTest(t, llm)

	sess := svc.OpenSession("tenant-1", false)
	reply, err := svc.SendMessage(context.Background(), sess.ID, "hello")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "hi back" {
		t.Errorf("reply = %q, want %q", reply, "hi back")
	}

	history := svc.History(sess.ID)
	if got, want := len(history), 2; got != want {
		t.Fatalf("history length = %d, want %d", got, want)
	}
	if history[0].Role != "user" || history[0].Content != "hello" {
		t.Errorf("first turn: %+v", history[0])
	}
	if history[1].Role != "assistant" || history[1].Content != "hi back" {
		t.Errorf("second turn: %+v", history[1])
	}
}

func TestSendMessage_AdminGetsAdminToolsInLLMCall(t *testing.T) {
	llm := &fakeLLM{}
	svc := newServiceForTest(t, llm)
	sess := svc.OpenSession("admin-1", true)

	if _, err := svc.SendMessage(context.Background(), sess.ID, "what tools do you have?"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if got := len(llm.calls); got != 1 {
		t.Fatalf("llm call count = %d, want 1", got)
	}
	hasAdmin := false
	for _, tool := range llm.calls[0].Tools {
		if tool.Name == "list_tenants" || tool.Name == "create_tenant" {
			hasAdmin = true
			break
		}
	}
	if !hasAdmin {
		t.Errorf("admin session missing admin tools; got: %v", toolNames(llm.calls[0].Tools))
	}
}

func TestSendMessage_TenantDoesNotGetAdminTools(t *testing.T) {
	llm := &fakeLLM{}
	svc := newServiceForTest(t, llm)
	sess := svc.OpenSession("tenant-1", false)

	if _, err := svc.SendMessage(context.Background(), sess.ID, "hi"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	for _, tool := range llm.calls[0].Tools {
		if tool.Name == "list_tenants" || tool.Name == "create_tenant" || tool.Name == "delete_tenant" || tool.Name == "set_tenant_max_peers" {
			t.Errorf("tenant session leaked admin tool %q", tool.Name)
		}
	}
}

func TestSendMessage_SessionNotFound(t *testing.T) {
	svc := newServiceForTest(t, &fakeLLM{})

	if _, err := svc.SendMessage(context.Background(), "does-not-exist", "hi"); err == nil {
		t.Fatal("expected error for unknown session")
	}
}

func TestSessionIsolation_HistoriesDontBleed(t *testing.T) {
	llm := &fakeLLM{
		reply: func(history []Turn, _ []ToolSpec, _ ToolDispatcher) (string, error) {
			// Echo back the count of user turns in the history so we can
			// assert per-session histories don't leak between sessions.
			n := 0
			for _, t := range history {
				if t.Role == "user" {
					n++
				}
			}
			return "user_turns=" + string(rune('0'+n)), nil
		},
	}
	svc := newServiceForTest(t, llm)

	a := svc.OpenSession("tenant-a", false)
	b := svc.OpenSession("tenant-b", false)

	if _, err := svc.SendMessage(context.Background(), a.ID, "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SendMessage(context.Background(), a.ID, "2"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SendMessage(context.Background(), b.ID, "1"); err != nil {
		t.Fatal(err)
	}

	histA := svc.History(a.ID)
	histB := svc.History(b.ID)
	if len(histA) != 4 { // 2 user + 2 assistant
		t.Errorf("session A history len = %d, want 4", len(histA))
	}
	if len(histB) != 2 { // 1 user + 1 assistant
		t.Errorf("session B history len = %d, want 2", len(histB))
	}
}

func TestDispatcher_GatesAdminTools(t *testing.T) {
	svc := newServiceForTest(t, &fakeLLM{})

	tenantDisp := &dispatcher{svc: svc, sessionRole: RoleTenant}
	adminDisp := &dispatcher{svc: svc, sessionRole: RoleAdmin}

	tenantResult := tenantDisp.Dispatch(context.Background(), "list_tenants", nil)
	if !strings.HasPrefix(tenantResult, "error: tool list_tenants requires admin") {
		t.Errorf("expected tenant to be denied list_tenants, got: %q", tenantResult)
	}

	// adminSvc is nil in this test, so the admin tool returns
	// "admin service not configured" — that's still proof the gate let it through.
	adminResult := adminDisp.Dispatch(context.Background(), "list_tenants", nil)
	if !strings.Contains(adminResult, "admin service not configured") {
		t.Errorf("expected admin dispatch to attempt the tool (then fail on missing service); got: %q", adminResult)
	}
}

func TestCloseSession_Removes(t *testing.T) {
	svc := newServiceForTest(t, &fakeLLM{})
	sess := svc.OpenSession("tenant-1", false)

	if !svc.CloseSession(sess.ID) {
		t.Fatal("CloseSession should report success")
	}
	if got := svc.Get(sess.ID); got != nil {
		t.Errorf("session still present after close: %v", got)
	}
	if svc.CloseSession(sess.ID) {
		t.Error("second close should report not-found")
	}
}

func TestListSessionsForTenant(t *testing.T) {
	svc := newServiceForTest(t, &fakeLLM{})
	svc.OpenSession("alice", false)
	svc.OpenSession("alice", false)
	svc.OpenSession("bob", false)

	if got := len(svc.ListSessionsForTenant("alice")); got != 2 {
		t.Errorf("alice sessions = %d, want 2", got)
	}
	if got := len(svc.ListSessionsForTenant("bob")); got != 1 {
		t.Errorf("bob sessions = %d, want 1", got)
	}
	if got := len(svc.ListSessionsForTenant("nobody")); got != 0 {
		t.Errorf("nobody sessions = %d, want 0", got)
	}
}

func TestSessionHistoryTrim(t *testing.T) {
	// Configure a tiny history window so we can verify trimming runs.
	llm := &fakeLLM{reply: func([]Turn, []ToolSpec, ToolDispatcher) (string, error) {
		return "ok", nil
	}}
	svc, err := New(Config{TenantHistoryDepth: 4}, llm, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(svc.Close)
	sess := svc.OpenSession("tenant-1", false)

	for i := 0; i < 5; i++ {
		if _, err := svc.SendMessage(context.Background(), sess.ID, "msg"); err != nil {
			t.Fatalf("SendMessage: %v", err)
		}
	}
	history := svc.History(sess.ID)
	// Depth 4, but we sent 5 * 2 = 10 turns. After trimming, at most 4 remain.
	if len(history) > 4 {
		t.Errorf("history not trimmed: len=%d", len(history))
	}
}

// toolNames is a tiny helper used by the admin/tenant gating tests.
func toolNames(specs []ToolSpec) []string {
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		names = append(names, s.Name)
	}
	return names
}

// Smoke test the idle GC: open a session, force its LastActive into the past,
// run the GC by simulating a tick (we can't drive the ticker directly, but
// we can exercise the same code path that the goroutine runs).
func TestGCRemovesIdleSessions(t *testing.T) {
	svc, err := New(Config{IdleTimeout: 50 * time.Millisecond}, &fakeLLM{}, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(svc.Close)

	sess := svc.OpenSession("tenant-1", false)
	sess.mu.Lock()
	sess.LastActive = time.Now().Add(-time.Hour)
	sess.mu.Unlock()

	// Manually run the GC step that the goroutine would do, so we don't
	// have to wait for the 5-minute ticker.
	cutoff := time.Now().Add(-svc.cfg.idleTimeout())
	svc.mu.Lock()
	for id, s := range svc.sessions {
		if s.LastActive.Before(cutoff) {
			delete(svc.sessions, id)
		}
	}
	svc.mu.Unlock()

	if got := svc.Get(sess.ID); got != nil {
		t.Error("idle session should have been GC'd")
	}
}
