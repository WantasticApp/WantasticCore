package adminbot

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"WantasticCore/internal/tenant"

	"github.com/go-pg/pg/v10"
	"github.com/nyaruka/phonenumbers"
)

type TelemetryService struct {
	db       *pg.DB
	registry tenantLookup
}

type tenantLookup interface {
	ListTenants() ([]*tenant.Tenant, error)
	GetTenant(string) (*tenant.Tenant, error)
	GetTenantByEmail(string) (*tenant.Tenant, error)
}

type AnalyticsRequest struct {
	Category string
	Period   string
	Filters  AnalyticsFilters
}

type AnalyticsFilters struct {
	Country     string
	UserAgent   string
	PeerCountEQ *int
	PeerCountGE *int
	PeerCountLE *int
}

type TenantCard struct {
	ID                 string
	FullName           string
	Email              string
	Phone              string
	Country            string
	Status             string
	OverlayAccountID   string
	Networks           []string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	LastLogin          time.Time
	LastActivityAt     time.Time
	PeerOldestAt       time.Time // oldest peer.created_at for this tenant
	PeerNewestAt       time.Time // newest peer.created_at for this tenant
	PeerLastSeenAt     time.Time // most recent peer.last_seen_at for this tenant
	PeerCount          int
	OnlinePeers        int
	OfflinePeers       int
	WantasticdPeers    int
	SSHSessions        int
	WinboxSessions     int
	TenantSessionCount int
	BytesSent          uint64
	BytesRecv          uint64
	UserAgentMatched   bool
	MaxPeers           int // Per-account device cap (0 = default)
}

type TenantActivityView struct {
	Card          *TenantCard
	RecentSSH     []SSHActivityRow
	RecentWinbox  []WinboxActivityRow
	RecentSession []TenantSessionRow
}

type PeerAggregateRow struct {
	AccountID       string    `pg:"account_id"`
	PeerCount       int       `pg:"peer_count"`
	OnlinePeers     int       `pg:"online_peers"`
	OfflinePeers    int       `pg:"offline_peers"`
	WantasticdPeers int       `pg:"wantasticd_peers"`
	OldestCreated   time.Time `pg:"oldest_created"`
	NewestCreated   time.Time `pg:"newest_created"`
	LastSeenAt      time.Time `pg:"last_seen_at"`
}

type SSHAggregateRow struct {
	AccountID    string    `pg:"account_id"`
	SSHSessions  int       `pg:"ssh_sessions"`
	BytesSent    uint64    `pg:"bytes_sent"`
	BytesRecv    uint64    `pg:"bytes_recv"`
	LastActivity time.Time `pg:"last_activity"`
}

type WinboxAggregateRow struct {
	AccountID       string    `pg:"account_id"`
	WinboxSessions  int       `pg:"winbox_sessions"`
	LastActivity    time.Time `pg:"last_activity"`
	AvgDurationMsec float64   `pg:"avg_duration_msec"`
}

type TenantSessionAggregateRow struct {
	TenantID           string    `pg:"tenant_id"`
	TenantSessionCount int       `pg:"tenant_session_count"`
	LastActivity       time.Time `pg:"last_activity"`
}

type UserAgentMatchRow struct {
	TenantID string `pg:"tenant_id"`
}

type SSHActivityRow struct {
	PeerID    string    `pg:"peer_id"`
	Username  string    `pg:"username"`
	UserAgent string    `pg:"user_agent"`
	ClientIP  string    `pg:"client_ip"`
	Timestamp time.Time `pg:"timestamp"`
	EndTime   time.Time `pg:"end_time"`
	BytesSent uint64    `pg:"bytes_sent"`
	BytesRecv uint64    `pg:"bytes_recv"`
}

type WinboxActivityRow struct {
	PeerID       string    `pg:"peer_id"`
	Username     string    `pg:"username"`
	ClientIP     string    `pg:"client_ip"`
	Timestamp    time.Time `pg:"timestamp"`
	EndTime      time.Time `pg:"end_time"`
	DurationMsec int64     `pg:"duration_ms"`
}

type TenantSessionRow struct {
	SessionID    string    `pg:"session_id"`
	IPAddress    string    `pg:"ip_address"`
	UserAgent    string    `pg:"user_agent"`
	LastActivity time.Time `pg:"last_activity"`
	ExpiresAt    time.Time `pg:"expires_at"`
}

func NewTelemetryService(db *pg.DB, registry tenantLookup) *TelemetryService {
	return &TelemetryService{db: db, registry: registry}
}

func (t *TelemetryService) ResolveTenant(ctx context.Context, selector string) (*TenantCard, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return nil, fmt.Errorf("tenant selector is required")
	}

	if tenantByID, err := t.registry.GetTenant(selector); err == nil && tenantByID != nil {
		return t.BuildTenantCard(ctx, tenantByID.ID)
	}
	if tenantByEmail, err := t.registry.GetTenantByEmail(selector); err == nil && tenantByEmail != nil {
		return t.BuildTenantCard(ctx, tenantByEmail.ID)
	}

	cards, err := t.LoadTenantCards(ctx, "all", AnalyticsFilters{})
	if err != nil {
		return nil, err
	}

	lower := strings.ToLower(selector)
	var matches []*TenantCard
	for _, card := range cards {
		if strings.Contains(strings.ToLower(card.FullName), lower) ||
			strings.Contains(strings.ToLower(card.Email), lower) ||
			strings.Contains(strings.ToLower(card.Phone), lower) ||
			strings.Contains(strings.ToLower(card.ID), lower) {
			matches = append(matches, card)
		}
	}

	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no tenant matched %q", selector)
	case 1:
		return matches[0], nil
	default:
		choices := make([]string, 0, minInt(5, len(matches)))
		for _, card := range matches[:minInt(5, len(matches))] {
			choices = append(choices, fmt.Sprintf("%s (%s)", card.FullName, card.Email))
		}
		return nil, fmt.Errorf("multiple tenants matched %q: %s", selector, strings.Join(choices, ", "))
	}
}

func (t *TelemetryService) BuildTenantCard(ctx context.Context, tenantID string) (*TenantCard, error) {
	cards, err := t.LoadTenantCards(ctx, "all", AnalyticsFilters{})
	if err != nil {
		return nil, err
	}
	for _, card := range cards {
		if card.ID == tenantID {
			return card, nil
		}
	}
	return nil, fmt.Errorf("tenant %s not found", tenantID)
}

func (t *TelemetryService) LoadTenantCards(ctx context.Context, period string, filters AnalyticsFilters) ([]*TenantCard, error) {
	allTenants, err := t.registry.ListTenants()
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}

	startAt := analyticsStartAt(period)
	peerAgg, err := t.loadPeerAggregates(ctx)
	if err != nil {
		return nil, err
	}
	sshAgg, err := t.loadSSHAggregates(ctx, startAt, filters.UserAgent)
	if err != nil {
		return nil, err
	}
	winboxAgg, err := t.loadWinboxAggregates(ctx, startAt)
	if err != nil {
		return nil, err
	}
	sessionAgg, err := t.loadTenantSessionAggregates(ctx, startAt, filters.UserAgent)
	if err != nil {
		return nil, err
	}
	userAgentMatches, err := t.loadUserAgentMatches(ctx, filters.UserAgent)
	if err != nil {
		return nil, err
	}
	peerOverrides, err := t.loadPeerLimitOverrides(ctx)
	if err != nil {
		return nil, err
	}

	cards := make([]*TenantCard, 0, len(allTenants))
	for _, item := range allTenants {
		card := &TenantCard{
			ID:               item.ID,
			FullName:         item.FullName,
			Email:            item.Email,
			Status:           item.Status,
			OverlayAccountID: item.OverlayAccountID,
			Networks:         append([]string(nil), item.Networks...),
			CreatedAt:        item.CreatedAt,
			UpdatedAt:        item.UpdatedAt,
			LastLogin:        item.LastLogin,
		}

		if agg, ok := peerAgg[item.OverlayAccountID]; ok {
			card.PeerCount = agg.PeerCount
			card.OnlinePeers = agg.OnlinePeers
			card.OfflinePeers = agg.OfflinePeers
			card.WantasticdPeers = agg.WantasticdPeers
			card.PeerOldestAt = agg.OldestCreated
			card.PeerNewestAt = agg.NewestCreated
			card.PeerLastSeenAt = agg.LastSeenAt
			card.LastActivityAt = latestTime(card.LastActivityAt, agg.LastSeenAt)
		}
		if agg, ok := sshAgg[item.OverlayAccountID]; ok {
			card.SSHSessions = agg.SSHSessions
			card.BytesSent = agg.BytesSent
			card.BytesRecv = agg.BytesRecv
			card.LastActivityAt = latestTime(card.LastActivityAt, agg.LastActivity)
		}
		if agg, ok := winboxAgg[item.OverlayAccountID]; ok {
			card.WinboxSessions = agg.WinboxSessions
			card.LastActivityAt = latestTime(card.LastActivityAt, agg.LastActivity)
		}
		if agg, ok := sessionAgg[item.ID]; ok {
			card.TenantSessionCount = agg.TenantSessionCount
			card.LastActivityAt = latestTime(card.LastActivityAt, agg.LastActivity)
		}
		card.UserAgentMatched = userAgentMatches[item.ID] || (filters.UserAgent == "")
		card.MaxPeers = peerOverrides[item.OverlayAccountID]

		if matchesTenantFilters(card, filters) {
			cards = append(cards, card)
		}
	}

	sort.Slice(cards, func(i, j int) bool {
		return cards[i].CreatedAt.After(cards[j].CreatedAt)
	})
	return cards, nil
}

func (t *TelemetryService) BuildTenantActivity(ctx context.Context, tenantID string) (*TenantActivityView, error) {
	card, err := t.BuildTenantCard(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	view := &TenantActivityView{Card: card}

	if _, err := t.db.QueryContext(ctx, &view.RecentSSH, `
		SELECT peer_id, username, user_agent, client_ip, timestamp, end_time, bytes_sent, bytes_recv
		FROM ssh_activities
		WHERE account_id = ?
		ORDER BY timestamp DESC
		LIMIT 5
	`, card.OverlayAccountID); err != nil {
		return nil, fmt.Errorf("list ssh activities: %w", err)
	}

	if _, err := t.db.QueryContext(ctx, &view.RecentWinbox, `
		SELECT peer_id, username, client_ip, timestamp, end_time, duration_ms
		FROM winbox_activities
		WHERE account_id = ?
		ORDER BY timestamp DESC
		LIMIT 5
	`, card.OverlayAccountID); err != nil {
		return nil, fmt.Errorf("list winbox activities: %w", err)
	}

	if _, err := t.db.QueryContext(ctx, &view.RecentSession, `
		SELECT session_id, ip_address, user_agent, last_activity, expires_at
		FROM tenant_sessions
		WHERE tenant_id = ?
		ORDER BY last_activity DESC
		LIMIT 5
	`, tenantID); err != nil {
		return nil, fmt.Errorf("list tenant sessions: %w", err)
	}

	return view, nil
}

func (t *TelemetryService) RenderAnalytics(ctx context.Context, req AnalyticsRequest) (string, error) {
	cards, err := t.LoadTenantCards(ctx, req.Period, req.Filters)
	if err != nil {
		return "", err
	}
	if len(cards) == 0 {
		return "No tenants matched those filters.", nil
	}

	switch normalizeCategory(req.Category) {
	case "peers":
		var totalPeers, onlinePeers, offlinePeers, wantasticdPeers int
		top := make([]*TenantCard, len(cards))
		copy(top, cards)
		sort.Slice(top, func(i, j int) bool {
			if top[i].PeerCount == top[j].PeerCount {
				return top[i].FullName < top[j].FullName
			}
			return top[i].PeerCount > top[j].PeerCount
		})
		for _, card := range cards {
			totalPeers += card.PeerCount
			onlinePeers += card.OnlinePeers
			offlinePeers += card.OfflinePeers
			wantasticdPeers += card.WantasticdPeers
		}
		lines := []string{
			fmt.Sprintf("Peer analytics (%s)", normalizePeriod(req.Period)),
			fmt.Sprintf("Tenants in scope: %d", len(cards)),
			fmt.Sprintf("Total peers: %d", totalPeers),
			fmt.Sprintf("Online peers: %d", onlinePeers),
			fmt.Sprintf("Offline peers: %d", offlinePeers),
			fmt.Sprintf("Wantasticd peers: %d", wantasticdPeers),
			fmt.Sprintf("Average peers per tenant: %.1f", float64(totalPeers)/float64(len(cards))),
		}
		lines = append(lines, formatTopTenantLines("Top peer counts", top)...)
		return strings.Join(lines, "\n"), nil

	case "winbox":
		var totalWinbox int
		top := make([]*TenantCard, len(cards))
		copy(top, cards)
		sort.Slice(top, func(i, j int) bool {
			if top[i].WinboxSessions == top[j].WinboxSessions {
				return top[i].FullName < top[j].FullName
			}
			return top[i].WinboxSessions > top[j].WinboxSessions
		})
		for _, card := range cards {
			totalWinbox += card.WinboxSessions
		}
		lines := []string{
			fmt.Sprintf("Winbox analytics (%s)", normalizePeriod(req.Period)),
			fmt.Sprintf("Tenants in scope: %d", len(cards)),
			fmt.Sprintf("Total Winbox sessions: %d", totalWinbox),
			fmt.Sprintf("Tenants with Winbox activity: %d", countMatching(cards, func(card *TenantCard) bool {
				return card.WinboxSessions > 0
			})),
		}
		lines = append(lines, formatTopTenantLines("Top Winbox activity", top)...)
		return strings.Join(lines, "\n"), nil

	default:
		statuses := map[string]int{}
		var totalPeers, totalSSH int
		for _, card := range cards {
			statuses[emptyAs(card.Status, "unknown")]++
			totalPeers += card.PeerCount
			totalSSH += card.SSHSessions
		}
		lines := []string{
			fmt.Sprintf("Tenant analytics (%s)", normalizePeriod(req.Period)),
			fmt.Sprintf("Tenants in scope: %d", len(cards)),
			fmt.Sprintf("Total peers: %d", totalPeers),
			fmt.Sprintf("Total SSH sessions: %d", totalSSH),
			fmt.Sprintf("Active tenants: %d", statuses["active"]),
		}
		top := make([]*TenantCard, len(cards))
		copy(top, cards)
		sort.Slice(top, func(i, j int) bool {
			if top[i].PeerCount == top[j].PeerCount {
				return top[i].FullName < top[j].FullName
			}
			return top[i].PeerCount > top[j].PeerCount
		})
		lines = append(lines, formatTopTenantLines("Top tenant cards", top)...)
		return strings.Join(lines, "\n"), nil
	}
}

func (t *TelemetryService) RenderTenantCard(ctx context.Context, tenantID string) (string, error) {
	card, err := t.BuildTenantCard(ctx, tenantID)
	if err != nil {
		return "", err
	}
	lines := []string{
		fmt.Sprintf("Tenant card: %s", emptyAs(card.FullName, card.Email)),
		fmt.Sprintf("Tenant ID: %s", card.ID),
		fmt.Sprintf("Email: %s", emptyAs(card.Email, "-")),
		fmt.Sprintf("Phone: %s", emptyAs(card.Phone, "-")),
		fmt.Sprintf("Country: %s", emptyAs(card.Country, "-")),
		fmt.Sprintf("Status: %s | MaxPeers: %d", emptyAs(card.Status, "unknown"), card.MaxPeers),
		fmt.Sprintf("Overlay account: %s", emptyAs(card.OverlayAccountID, "-")),
		fmt.Sprintf("Networks: %s", emptyAs(strings.Join(card.Networks, ", "), "-")),
		fmt.Sprintf("Peers total / online / offline: %d / %d / %d", card.PeerCount, card.OnlinePeers, card.OfflinePeers),
		fmt.Sprintf("SSH sessions / Winbox sessions: %d / %d", card.SSHSessions, card.WinboxSessions),
		fmt.Sprintf("Traffic sent / recv: %d / %d bytes", card.BytesSent, card.BytesRecv),
		fmt.Sprintf("Last activity: %s", formatTime(card.LastActivityAt)),
		fmt.Sprintf("Last login: %s", formatTime(card.LastLogin)),
	}
	return strings.Join(lines, "\n"), nil
}

func (t *TelemetryService) RenderTenantActivity(ctx context.Context, tenantID string) (string, error) {
	view, err := t.BuildTenantActivity(ctx, tenantID)
	if err != nil {
		return "", err
	}
	lines := []string{
		fmt.Sprintf("Tenant activity: %s", emptyAs(view.Card.FullName, view.Card.Email)),
		fmt.Sprintf("Peers by status: online=%d offline=%d wantasticd=%d", view.Card.OnlinePeers, view.Card.OfflinePeers, view.Card.WantasticdPeers),
		fmt.Sprintf("SSH sessions in scope: %d", view.Card.SSHSessions),
		fmt.Sprintf("Winbox sessions in scope: %d", view.Card.WinboxSessions),
		"Recent SSH:",
	}
	if len(view.RecentSSH) == 0 {
		lines = append(lines, "- none")
	} else {
		for _, row := range view.RecentSSH {
			lines = append(lines, fmt.Sprintf("- %s from %s at %s", emptyAs(row.Username, "unknown"), emptyAs(row.ClientIP, "-"), formatTime(row.Timestamp)))
		}
	}
	lines = append(lines, "Recent Winbox:")
	if len(view.RecentWinbox) == 0 {
		lines = append(lines, "- none")
	} else {
		for _, row := range view.RecentWinbox {
			lines = append(lines, fmt.Sprintf("- %s from %s at %s", emptyAs(row.Username, "unknown"), emptyAs(row.ClientIP, "-"), formatTime(row.Timestamp)))
		}
	}
	return strings.Join(lines, "\n"), nil
}

// BuildClaudeContext emits a compact, PII-free telemetry snapshot for Claude.
// Format design:
//   - single-letter keys, no spaces around `=`, ISO-short dates (YYYY-MM-DD)
//     and relative ages ("2h", "5d", "3mo") to keep tokens low.
//   - excludes email, phone, networks, account UUIDs, IPs — only the tenant
//     display name (or "(anon)" when blank) goes out. Country is the
//     E.164-derived two-letter code, treated as non-sensitive.
//   - includes ALL relevant timestamps: tenant created/updated/last-login/
//     last-activity, plus per-tenant peer-fleet ages (oldest/newest peer
//     and most recent peer.last_seen_at).
func (t *TelemetryService) BuildClaudeContext(ctx context.Context, question string, filters AnalyticsFilters) (string, error) {
	cards, err := t.LoadTenantCards(ctx, "month", filters)
	if err != nil {
		return "", err
	}
	sort.Slice(cards, func(i, j int) bool {
		if cards[i].PeerCount == cards[j].PeerCount {
			return cards[i].FullName < cards[j].FullName
		}
		return cards[i].PeerCount > cards[j].PeerCount
	})

	now := time.Now()
	totalPeers, totalSSH, totalWinbox := 0, 0, 0
	active24h, active7d, active30d := 0, 0, 0
	var oldestTenant, newestTenant time.Time
	var oldestPeer, newestPeer, freshestPeerLastSeen time.Time
	for _, c := range cards {
		totalPeers += c.PeerCount
		totalSSH += c.SSHSessions
		totalWinbox += c.WinboxSessions
		if !c.LastActivityAt.IsZero() {
			age := now.Sub(c.LastActivityAt)
			if age <= 24*time.Hour {
				active24h++
			}
			if age <= 7*24*time.Hour {
				active7d++
			}
			if age <= 30*24*time.Hour {
				active30d++
			}
		}
		oldestTenant = earliestTime(oldestTenant, c.CreatedAt)
		newestTenant = latestTime(newestTenant, c.CreatedAt)
		oldestPeer = earliestTime(oldestPeer, c.PeerOldestAt)
		newestPeer = latestTime(newestPeer, c.PeerNewestAt)
		freshestPeerLastSeen = latestTime(freshestPeerLastSeen, c.PeerLastSeenAt)
	}

	lines := []string{
		"ctx=adminbot-telemetry; answer-only-from-this-context; if-missing-say-so",
		fmt.Sprintf("now=%s", now.UTC().Format(time.RFC3339)),
		fmt.Sprintf("q=%s", strings.TrimSpace(question)),
		fmt.Sprintf("scope: tenants=%d peers=%d ssh-30d=%d wb-30d=%d", len(cards), totalPeers, totalSSH, totalWinbox),
		fmt.Sprintf("active: 24h=%d 7d=%d 30d=%d", active24h, active7d, active30d),
		fmt.Sprintf("tenant-window: oldest=%s newest=%s", shortDate(oldestTenant), shortDate(newestTenant)),
		fmt.Sprintf("peer-window: oldest=%s newest=%s last-seen=%s", shortDate(oldestPeer), shortDate(newestPeer), relAge(now, freshestPeerLastSeen)),
		"top:",
	}
	for _, c := range cards[:minInt(5, len(cards))] {
		// `n` = display name only — never falls back to email (was leaking PII).
		// `c/u/login/last` = tenant lifecycle dates.
		// `pold/pnew/plast` = peer fleet ages.
		lines = append(lines, fmt.Sprintf(
			"- n=%s mp=%d st=%s ctry=%s p=%d(on=%d/off=%d/wd=%d) ssh=%d wb=%d c=%s u=%s login=%s last=%s pold=%s pnew=%s plast=%s",
			emptyAs(c.FullName, "(anon)"),
			c.MaxPeers,
			emptyAs(c.Status, "?"),
			emptyAs(c.Country, "-"),
			c.PeerCount, c.OnlinePeers, c.OfflinePeers, c.WantasticdPeers,
			c.SSHSessions, c.WinboxSessions,
			shortDate(c.CreatedAt),
			shortDate(c.UpdatedAt),
			relAge(now, c.LastLogin),
			relAge(now, c.LastActivityAt),
			shortDate(c.PeerOldestAt),
			shortDate(c.PeerNewestAt),
			relAge(now, c.PeerLastSeenAt),
		))
	}
	return strings.Join(lines, "\n"), nil
}

// shortDate returns "YYYY-MM-DD" or "-" for the zero value.
func shortDate(v time.Time) string {
	if v.IsZero() {
		return "-"
	}
	return v.UTC().Format("2006-01-02")
}

// relAge returns a 1-3 char relative age like "2h", "5d", "3w", "8mo", "2y".
// "-" for the zero value, "now" for under a minute. Designed to be readable
// to Claude while being far cheaper than a full timestamp.
func relAge(now, v time.Time) string {
	if v.IsZero() {
		return "-"
	}
	d := now.Sub(v)
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

func earliestTime(a, b time.Time) time.Time {
	if a.IsZero() {
		return b
	}
	if b.IsZero() {
		return a
	}
	if b.Before(a) {
		return b
	}
	return a
}

func (t *TelemetryService) loadPeerAggregates(ctx context.Context) (map[string]PeerAggregateRow, error) {
	var rows []PeerAggregateRow
	_, err := t.db.QueryContext(ctx, &rows, `
		SELECT
			account_id,
			COUNT(*)::int AS peer_count,
			COUNT(*) FILTER (WHERE is_online)::int AS online_peers,
			COUNT(*) FILTER (WHERE NOT is_online)::int AS offline_peers,
			COUNT(*) FILTER (WHERE is_wantasticd)::int AS wantasticd_peers,
			MIN(created_at)            AS oldest_created,
			MAX(created_at)            AS newest_created,
			MAX(last_seen_at)          AS last_seen_at
		FROM peers
		GROUP BY account_id
	`)
	if err != nil {
		return nil, fmt.Errorf("load peer aggregates: %w", err)
	}
	result := make(map[string]PeerAggregateRow, len(rows))
	for _, row := range rows {
		result[row.AccountID] = row
	}
	return result, nil
}

func (t *TelemetryService) loadSSHAggregates(ctx context.Context, startAt *time.Time, userAgent string) (map[string]SSHAggregateRow, error) {
	conditions := []string{"1=1"}
	args := make([]any, 0, 2)
	if startAt != nil {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, *startAt)
	}
	if strings.TrimSpace(userAgent) != "" {
		conditions = append(conditions, "user_agent ILIKE ?")
		args = append(args, "%"+strings.TrimSpace(userAgent)+"%")
	}

	query := fmt.Sprintf(`
		SELECT
			account_id,
			COUNT(*)::int AS ssh_sessions,
			COALESCE(SUM(bytes_sent), 0)::bigint AS bytes_sent,
			COALESCE(SUM(bytes_recv), 0)::bigint AS bytes_recv,
			MAX(COALESCE(end_time, timestamp)) AS last_activity
		FROM ssh_activities
		WHERE %s
		GROUP BY account_id
	`, strings.Join(conditions, " AND "))

	var rows []SSHAggregateRow
	if _, err := t.db.QueryContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("load ssh aggregates: %w", err)
	}
	result := make(map[string]SSHAggregateRow, len(rows))
	for _, row := range rows {
		result[row.AccountID] = row
	}
	return result, nil
}

func (t *TelemetryService) loadWinboxAggregates(ctx context.Context, startAt *time.Time) (map[string]WinboxAggregateRow, error) {
	conditions := []string{"1=1"}
	args := make([]any, 0, 1)
	if startAt != nil {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, *startAt)
	}

	query := fmt.Sprintf(`
		SELECT
			account_id,
			COUNT(*)::int AS winbox_sessions,
			MAX(COALESCE(end_time, timestamp)) AS last_activity,
			COALESCE(AVG(duration_ms), 0)::float8 AS avg_duration_msec
		FROM winbox_activities
		WHERE %s
		GROUP BY account_id
	`, strings.Join(conditions, " AND "))

	var rows []WinboxAggregateRow
	if _, err := t.db.QueryContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("load winbox aggregates: %w", err)
	}
	result := make(map[string]WinboxAggregateRow, len(rows))
	for _, row := range rows {
		result[row.AccountID] = row
	}
	return result, nil
}

func (t *TelemetryService) loadTenantSessionAggregates(ctx context.Context, startAt *time.Time, userAgent string) (map[string]TenantSessionAggregateRow, error) {
	conditions := []string{"1=1"}
	args := make([]any, 0, 2)
	if startAt != nil {
		conditions = append(conditions, "last_activity >= ?")
		args = append(args, *startAt)
	}
	if strings.TrimSpace(userAgent) != "" {
		conditions = append(conditions, "user_agent ILIKE ?")
		args = append(args, "%"+strings.TrimSpace(userAgent)+"%")
	}

	query := fmt.Sprintf(`
		SELECT
			tenant_id,
			COUNT(*)::int AS tenant_session_count,
			MAX(last_activity) AS last_activity
		FROM tenant_sessions
		WHERE %s
		GROUP BY tenant_id
	`, strings.Join(conditions, " AND "))

	var rows []TenantSessionAggregateRow
	if _, err := t.db.QueryContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("load tenant session aggregates: %w", err)
	}
	result := make(map[string]TenantSessionAggregateRow, len(rows))
	for _, row := range rows {
		result[row.TenantID] = row
	}
	return result, nil
}

func (t *TelemetryService) loadUserAgentMatches(ctx context.Context, userAgent string) (map[string]bool, error) {
	matches := map[string]bool{}
	if strings.TrimSpace(userAgent) == "" {
		return matches, nil
	}

	pattern := "%" + strings.TrimSpace(userAgent) + "%"
	var tenantRows []UserAgentMatchRow
	if _, err := t.db.QueryContext(ctx, &tenantRows, `
		SELECT DISTINCT tenant_id
		FROM tenant_sessions
		WHERE user_agent ILIKE ?
	`, pattern); err != nil {
		return nil, fmt.Errorf("load tenant session user-agent matches: %w", err)
	}
	for _, row := range tenantRows {
		matches[row.TenantID] = true
	}

	type sshMatchRow struct {
		TenantID string `pg:"tenant_id"`
	}
	var sshRows []sshMatchRow
	if _, err := t.db.QueryContext(ctx, &sshRows, `
		SELECT DISTINCT t.id AS tenant_id
		FROM tenants t
		JOIN ssh_activities sa ON sa.account_id = t.overlay_account_id
		WHERE sa.user_agent ILIKE ?
	`, pattern); err != nil {
		return nil, fmt.Errorf("load ssh user-agent matches: %w", err)
	}
	for _, row := range sshRows {
		matches[row.TenantID] = true
	}

	return matches, nil
}

func analyticsStartAt(period string) *time.Time {
	now := time.Now().UTC()
	switch normalizePeriod(period) {
	case "day":
		start := now.Add(-24 * time.Hour)
		return &start
	case "month":
		start := now.AddDate(0, -1, 0)
		return &start
	default:
		return nil
	}
}

func matchesTenantFilters(card *TenantCard, filters AnalyticsFilters) bool {
	if filters.Country != "" && !strings.EqualFold(strings.TrimSpace(card.Country), strings.TrimSpace(filters.Country)) {
		return false
	}
	if filters.UserAgent != "" && !card.UserAgentMatched {
		return false
	}
	if filters.PeerCountEQ != nil && card.PeerCount != *filters.PeerCountEQ {
		return false
	}
	if filters.PeerCountGE != nil && card.PeerCount < *filters.PeerCountGE {
		return false
	}
	if filters.PeerCountLE != nil && card.PeerCount > *filters.PeerCountLE {
		return false
	}
	return true
}

func phoneCountry(phone string) string {
	trimmed := strings.TrimSpace(phone)
	if trimmed == "" {
		return ""
	}
	number, err := phonenumbers.Parse(trimmed, "")
	if err != nil {
		return ""
	}
	return strings.ToUpper(phonenumbers.GetRegionCodeForNumber(number))
}

func formatTopTenantLines(title string, cards []*TenantCard) []string {
	lines := []string{title + ":"}
	if len(cards) == 0 {
		return append(lines, "- none")
	}
	for _, card := range cards[:minInt(5, len(cards))] {
		lines = append(lines, fmt.Sprintf("- %s | peers=%d | ssh=%d | winbox=%d | max=%d",
			emptyAs(card.FullName, card.Email),
			card.PeerCount,
			card.SSHSessions,
			card.WinboxSessions,
			card.MaxPeers,
		))
	}
	return lines
}

func latestTime(current, candidate time.Time) time.Time {
	if candidate.After(current) {
		return candidate
	}
	return current
}

func normalizeCategory(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "peer", "peers":
		return "peers"
	case "winbox":
		return "winbox"
	case "tenant", "tenants", "tennant", "tennants":
		return "tenants"
	default:
		return "tenants"
	}
}

func normalizePeriod(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "day", "daily":
		return "day"
	case "2", "month", "monthly":
		return "month"
	default:
		return "all"
	}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.UTC().Format(time.RFC3339)
}

func emptyAs(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func countMatching(cards []*TenantCard, fn func(*TenantCard) bool) int {
	total := 0
	for _, card := range cards {
		if fn(card) {
			total++
		}
	}
	return total
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// loadPeerLimitOverrides returns a map of account_id → max_peers
// for all accounts that have a non-zero cap set.
func (t *TelemetryService) loadPeerLimitOverrides(ctx context.Context) (map[string]int, error) {
	var rows []struct {
		ID       string `pg:"id"`
		MaxPeers int    `pg:"max_peers"`
	}
	_, err := t.db.QueryContext(ctx, &rows, `SELECT id, max_peers FROM accounts WHERE max_peers > 0`)
	if err != nil {
		return nil, fmt.Errorf("load max-peers caps: %w", err)
	}
	out := make(map[string]int, len(rows))
	for _, r := range rows {
		out[r.ID] = r.MaxPeers
	}
	return out, nil
}

