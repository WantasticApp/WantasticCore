package routerosapi

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"path"
	"sort"
	"strings"
	"time"

	routeros "github.com/swoga/go-routeros"
)

// DialContextFunc dials a backend connection through the caller's network path.
type DialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

// Resource identifies a RouterOS resource area exposed in the dashboard.
type Resource int32

const (
	ResourceUnknown Resource = iota
	ResourceIPAddresses
	ResourceRoutes
	ResourceFirewall
	ResourcePackages
	ResourceFiles
	ResourceWireless
	ResourceTR069Client
	ResourceBridge
)

// ConnectParams contains the transport and credential parameters for a RouterOS session.
type ConnectParams struct {
	Address            string
	Username           string
	Password           string
	UseTLS             bool
	InsecureSkipVerify bool
}

// DeviceIdentity is a lightweight device overview derived from RouterOS API print data.
type DeviceIdentity struct {
	Identity     string
	Version      string
	BoardName    string
	Model        string
	Platform     string
	Architecture string
	CPU          string
}

// Overview is the high-level dashboard state returned for a verified RouterOS device.
type Overview struct {
	Identity       DeviceIdentity
	SystemResource map[string]string
	Routerboard    map[string]string
}

// ProbeResult captures the result of an authentication + resource sanity check.
type ProbeResult struct {
	Identity       DeviceIdentity
	SystemResource map[string]string
}

// Record is a generic RouterOS row with the raw print fields preserved.
type Record struct {
	ID     string
	Fields map[string]string
}

// Session owns a live RouterOS API login and is intended to back one
// higher-level dashboard session. Calls should be serialized by the caller.
type Session struct {
	client *routeros.Client
}

// Manager provides typed RouterOS API helpers while keeping the third-party
// client isolated to this package.
type Manager struct{}

func NewManager() *Manager {
	return &Manager{}
}

const (
	metaKindField         = "__kind"
	metaGroupField        = "__group"
	metaPathField         = "__path"
	metaParentField       = "__parent"
	metaLabelField        = "__label"
	metaBridgeField       = "__bridge"
	metaDirectoryField    = "__directory"
	metaNameOnlyField     = "__name_only"
	metaRouterOSIDField   = "__routeros_id"
	defaultCommandTimeout = 15 * time.Second
)

// OpenSession logs in once and returns a reusable API session.
func (m *Manager) OpenSession(ctx context.Context, dial DialContextFunc, params ConnectParams) (*Session, error) {
	client, err := connect(ctx, dial, params)
	if err != nil {
		return nil, err
	}
	return &Session{client: client}, nil
}

// Probe verifies connectivity and credentials by logging in and loading the
// system resource + identity summary.
func (m *Manager) Probe(ctx context.Context, dial DialContextFunc, params ConnectParams) (*ProbeResult, error) {
	overview, err := m.GetOverview(ctx, dial, params)
	if err != nil {
		return nil, err
	}
	return &ProbeResult{
		Identity:       overview.Identity,
		SystemResource: overview.SystemResource,
	}, nil
}

// GetOverview returns a small identity snapshot for the dashboard header.
func (m *Manager) GetOverview(ctx context.Context, dial DialContextFunc, params ConnectParams) (*Overview, error) {
	session, err := m.OpenSession(ctx, dial, params)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	return session.GetOverview(ctx)
}

// ListRecords prints the requested RouterOS resource and returns the raw rows.
func (m *Manager) ListRecords(ctx context.Context, dial DialContextFunc, params ConnectParams, resource Resource) ([]Record, error) {
	session, err := m.OpenSession(ctx, dial, params)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	return session.ListRecords(ctx, resource)
}

// AddRecord creates a new record inside the selected resource.
func (m *Manager) AddRecord(ctx context.Context, dial DialContextFunc, params ConnectParams, resource Resource, fields map[string]string) error {
	session, err := m.OpenSession(ctx, dial, params)
	if err != nil {
		return err
	}
	defer session.Close()
	return session.AddRecord(ctx, resource, fields)
}

// UpdateRecord applies a set command to an existing resource row by .id.
func (m *Manager) UpdateRecord(ctx context.Context, dial DialContextFunc, params ConnectParams, resource Resource, id string, fields map[string]string) error {
	session, err := m.OpenSession(ctx, dial, params)
	if err != nil {
		return err
	}
	defer session.Close()
	return session.UpdateRecord(ctx, resource, id, fields)
}

// DeleteRecord removes a resource row by .id.
func (m *Manager) DeleteRecord(ctx context.Context, dial DialContextFunc, params ConnectParams, resource Resource, id string) error {
	session, err := m.OpenSession(ctx, dial, params)
	if err != nil {
		return err
	}
	defer session.Close()
	return session.DeleteRecord(ctx, resource, id)
}

func (s *Session) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	s.client.Close()
	return nil
}

// GetOverview returns a small identity snapshot for the dashboard header.
func (s *Session) GetOverview(ctx context.Context) (*Overview, error) {
	systemResource, err := runSingleRecord(ctx, s.client, []string{"/system/resource/print"})
	if err != nil {
		return nil, err
	}

	identityFields, _ := runSingleRecord(ctx, s.client, []string{"/system/identity/print"})
	routerboardFields, _ := runSingleRecord(ctx, s.client, []string{"/system/routerboard/print"})

	identity := DeviceIdentity{
		Identity:     identityFields["name"],
		Version:      systemResource["version"],
		BoardName:    routerboardFields["board-name"],
		Model:        firstNonEmpty(routerboardFields["model"], systemResource["board-name"]),
		Platform:     systemResource["platform"],
		Architecture: systemResource["architecture-name"],
		CPU:          systemResource["cpu"],
	}

	return &Overview{
		Identity:       identity,
		SystemResource: systemResource,
		Routerboard:    routerboardFields,
	}, nil
}

// ListRecords prints the requested RouterOS resource and returns the raw rows.
func (s *Session) ListRecords(ctx context.Context, resource Resource) ([]Record, error) {
	switch resource {
	case ResourceBridge:
		return s.listBridgeRecords(ctx)
	case ResourceWireless:
		return s.listWirelessRecords(ctx)
	case ResourceFiles:
		return s.listFileRecords(ctx)
	}

	commands, err := printCommands(resource)
	if err != nil {
		return nil, err
	}

	records, _, err := runFirstSuccessfulRecords(ctx, s.client, commands)
	if err != nil {
		return nil, err
	}
	return records, nil
}

// AddRecord creates a new record inside the selected resource.
func (s *Session) AddRecord(ctx context.Context, resource Resource, fields map[string]string) error {
	command, err := addCommand(resource, fields)
	if err != nil {
		return err
	}
	return runMutation(ctx, s.client, command, "", stripMetadataFields(fields))
}

// UpdateRecord applies a set command to an existing resource row by .id.
func (s *Session) UpdateRecord(ctx context.Context, resource Resource, id string, fields map[string]string) error {
	command, rawID, err := setCommand(resource, id, fields)
	if err != nil {
		return err
	}
	return runMutation(ctx, s.client, command, rawID, stripMetadataFields(fields))
}

// DeleteRecord removes a resource row by .id.
func (s *Session) DeleteRecord(ctx context.Context, resource Resource, id string) error {
	command, rawID, err := removeCommand(resource, id)
	if err != nil {
		return err
	}
	return runMutation(ctx, s.client, command, rawID, nil)
}

func connect(ctx context.Context, dial DialContextFunc, params ConnectParams) (*routeros.Client, error) {
	conn, err := dial(ctx, "tcp", params.Address)
	if err != nil {
		return nil, fmt.Errorf("routeros dial failed: %w", err)
	}

	if params.UseTLS {
		host := params.Address
		if h, _, splitErr := net.SplitHostPort(params.Address); splitErr == nil && h != "" {
			host = h
		}
		tlsConn := tls.Client(conn, &tls.Config{
			ServerName:         host,
			InsecureSkipVerify: params.InsecureSkipVerify,
			MinVersion:         tls.VersionTLS12,
		})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("routeros tls handshake failed: %w", err)
		}
		conn = tlsConn
	}

	timeout := commandTimeout(ctx)

	client, err := routeros.NewClient(conn, timeout)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("routeros client init failed: %w", err)
	}

	if err := client.Login(params.Username, params.Password); err != nil {
		client.Close()
		return nil, fmt.Errorf("routeros login failed: %w", err)
	}
	return client, nil
}

func runSingleRecord(ctx context.Context, client *routeros.Client, command []string) (map[string]string, error) {
	reply, err := runArgs(ctx, client, command)
	if err != nil {
		return nil, err
	}
	records := replyToRecords(reply)
	if len(records) == 0 {
		return map[string]string{}, nil
	}
	return records[0].Fields, nil
}

func runFirstSuccessfulRecords(ctx context.Context, client *routeros.Client, commands [][]string) ([]Record, string, error) {
	var lastErr error
	for _, command := range commands {
		reply, runErr := runArgs(ctx, client, command)
		if runErr != nil {
			lastErr = runErr
			continue
		}
		return replyToRecords(reply), strings.TrimSuffix(command[0], "/print"), nil
	}

	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", fmt.Errorf("routeros resource is not available")
}

func replyToRecords(reply *routeros.Reply) []Record {
	if reply == nil || len(reply.Re) == 0 {
		return []Record{}
	}

	out := make([]Record, 0, len(reply.Re))
	for _, sentence := range reply.Re {
		if sentence == nil {
			continue
		}
		fields := make(map[string]string, len(sentence.Map))
		for key, value := range sentence.Map {
			fields[key] = value
		}
		id := fields[".id"]
		if id == "" {
			id = fields["id"]
		}
		out = append(out, Record{
			ID:     id,
			Fields: fields,
		})
	}
	return out
}

func (s *Session) listBridgeRecords(ctx context.Context) ([]Record, error) {
	specs := []struct {
		kind     string
		group    string
		commands [][]string
		annotate func(*Record)
	}{
		{
			kind:     "bridge",
			group:    "bridges",
			commands: [][]string{{"/interface/bridge/print"}},
			annotate: func(record *Record) {
				bridgeName := firstNonEmpty(record.Fields["name"], record.Fields[".id"])
				record.Fields[metaParentField] = "bridges"
				record.Fields[metaBridgeField] = bridgeName
				record.Fields[metaLabelField] = bridgeName
			},
		},
		{
			kind:     "port",
			group:    "ports",
			commands: [][]string{{"/interface/bridge/port/print"}},
			annotate: func(record *Record) {
				bridgeName := firstNonEmpty(record.Fields["bridge"], "unassigned")
				record.Fields[metaParentField] = "bridge:" + bridgeName
				record.Fields[metaBridgeField] = bridgeName
				record.Fields[metaLabelField] = firstNonEmpty(record.Fields["interface"], record.Fields[".id"])
			},
		},
		{
			kind:     "vlan",
			group:    "vlans",
			commands: [][]string{{"/interface/bridge/vlan/print"}},
			annotate: func(record *Record) {
				bridgeName := firstNonEmpty(record.Fields["bridge"], "unassigned")
				record.Fields[metaParentField] = "bridge:" + bridgeName
				record.Fields[metaBridgeField] = bridgeName
				record.Fields[metaLabelField] = firstNonEmpty(record.Fields["vlan-ids"], record.Fields["comment"], record.Fields[".id"])
			},
		},
	}

	return listStructuredRecords(ctx, s.client, specs)
}

func (s *Session) listWirelessRecords(ctx context.Context) ([]Record, error) {
	specs := []struct {
		kind     string
		group    string
		commands [][]string
		annotate func(*Record)
	}{
		{
			kind:  "interface",
			group: "interfaces",
			commands: [][]string{
				{"/interface/wifi/print"},
				{"/interface/wireless/print"},
			},
			annotate: func(record *Record) {
				record.Fields[metaParentField] = "interfaces"
				record.Fields[metaLabelField] = firstNonEmpty(record.Fields["name"], record.Fields["default-name"], record.Fields["interface"], record.Fields["ssid"], record.Fields[".id"])
			},
		},
		{
			kind:  "security_profile",
			group: "profiles",
			commands: [][]string{
				{"/interface/wifi/security/print"},
				{"/interface/wireless/security-profiles/print"},
			},
			annotate: func(record *Record) {
				record.Fields[metaParentField] = "profiles"
				record.Fields[metaLabelField] = firstNonEmpty(record.Fields["name"], record.Fields[".id"])
			},
		},
		{
			kind:  "configuration",
			group: "profiles",
			commands: [][]string{
				{"/interface/wifi/configuration/print"},
			},
			annotate: func(record *Record) {
				record.Fields[metaParentField] = "profiles"
				record.Fields[metaLabelField] = firstNonEmpty(record.Fields["name"], record.Fields["ssid"], record.Fields[".id"])
			},
		},
	}

	return listStructuredRecords(ctx, s.client, specs)
}

func (s *Session) listFileRecords(ctx context.Context) ([]Record, error) {
	records, pathName, err := runFirstSuccessfulRecords(ctx, s.client, [][]string{{"/file/print"}})
	if err != nil {
		return nil, err
	}

	for i := range records {
		record := &records[i]
		name := strings.Trim(record.Fields["name"], "/")
		dir := path.Dir(name)
		if dir == "." || dir == "/" {
			dir = ""
		}
		base := path.Base(name)
		if base == "." || base == "/" || base == "" {
			base = name
		}
		kind := "file"
		if strings.Contains(strings.ToLower(record.Fields["type"]), "dir") {
			kind = "directory"
		}
		record.Fields[metaKindField] = kind
		record.Fields[metaGroupField] = "files"
		record.Fields[metaPathField] = pathName
		record.Fields[metaParentField] = dir
		record.Fields[metaDirectoryField] = dir
		record.Fields[metaNameOnlyField] = base
		record.Fields[metaLabelField] = firstNonEmpty(base, record.Fields["name"], record.Fields[".id"])
		annotateRecordIdentity(record, pathName)
	}

	return records, nil
}

func listStructuredRecords(ctx context.Context, client *routeros.Client, specs []struct {
	kind     string
	group    string
	commands [][]string
	annotate func(*Record)
}) ([]Record, error) {
	out := make([]Record, 0, 32)
	var firstErr error
	successCount := 0

	for _, spec := range specs {
		records, pathName, err := runFirstSuccessfulRecords(ctx, client, spec.commands)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		successCount++
		for i := range records {
			record := &records[i]
			record.Fields[metaKindField] = spec.kind
			record.Fields[metaGroupField] = spec.group
			record.Fields[metaPathField] = pathName
			if spec.annotate != nil {
				spec.annotate(record)
			}
			if strings.TrimSpace(record.Fields[metaParentField]) == "" {
				record.Fields[metaParentField] = spec.group
			}
			if strings.TrimSpace(record.Fields[metaLabelField]) == "" {
				record.Fields[metaLabelField] = firstNonEmpty(record.Fields["name"], record.Fields["interface"], record.Fields[".id"])
			}
			annotateRecordIdentity(record, pathName)
			out = append(out, *record)
		}
	}

	if successCount == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func annotateRecordIdentity(record *Record, pathName string) {
	rawID := strings.TrimSpace(record.ID)
	if rawID == "" {
		rawID = strings.TrimSpace(record.Fields[".id"])
	}
	if rawID != "" {
		record.Fields[metaRouterOSIDField] = rawID
		record.ID = encodeRecordID(pathName, rawID)
		return
	}

	record.ID = firstNonEmpty(
		record.Fields[metaLabelField],
		record.Fields["name"],
		record.Fields["interface"],
		record.Fields["address"],
		record.Fields["dst-address"],
	)
}

func encodeRecordID(pathName, rawID string) string {
	pathName = strings.TrimSpace(pathName)
	rawID = strings.TrimSpace(rawID)
	if pathName == "" || rawID == "" {
		return rawID
	}
	return pathName + "|" + rawID
}

func decodeRecordID(id string) (string, string) {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, "/") {
		return "", id
	}
	index := strings.LastIndex(id, "|")
	if index <= 0 || index >= len(id)-1 {
		return "", id
	}
	return id[:index], id[index+1:]
}

func stripMetadataFields(fields map[string]string) map[string]string {
	if len(fields) == 0 {
		return nil
	}

	clean := make(map[string]string, len(fields))
	for key, value := range fields {
		if key == "" || key == ".id" || strings.HasPrefix(key, "__") {
			continue
		}
		clean[key] = value
	}
	return clean
}

func runMutation(ctx context.Context, client *routeros.Client, command []string, id string, fields map[string]string) error {
	args := append([]string{}, command...)
	if id != "" {
		args = append(args, "=.id="+id)
	}

	if len(fields) > 0 {
		keys := make([]string, 0, len(fields))
		for key := range fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if key == "" {
				continue
			}
			args = append(args, fmt.Sprintf("=%s=%s", key, fields[key]))
		}
	}

	_, err := runArgs(ctx, client, args)
	return err
}

func runArgs(ctx context.Context, client *routeros.Client, args []string) (*routeros.Reply, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return client.RunArgs(args)
}

func commandTimeout(ctx context.Context) time.Duration {
	if ctx == nil {
		return defaultCommandTimeout
	}

	if deadline, ok := ctx.Deadline(); ok {
		until := time.Until(deadline)
		switch {
		case until <= 0:
			return time.Second
		case until < defaultCommandTimeout:
			return until
		}
	}

	return defaultCommandTimeout
}

func printCommands(resource Resource) ([][]string, error) {
	switch resource {
	case ResourceIPAddresses:
		return [][]string{{"/ip/address/print"}}, nil
	case ResourceRoutes:
		return [][]string{{"/ip/route/print"}}, nil
	case ResourceFirewall:
		return [][]string{{"/ip/firewall/filter/print"}}, nil
	case ResourcePackages:
		return [][]string{{"/system/package/print"}}, nil
	case ResourceFiles:
		return [][]string{{"/file/print"}}, nil
	case ResourceWireless:
		return [][]string{
			{"/interface/wifi/print"},
			{"/interface/wireless/print"},
		}, nil
	case ResourceTR069Client:
		return [][]string{
			{"/tr069-client/print"},
			{"/tr069-client/client/print"},
		}, nil
	case ResourceBridge:
		return [][]string{{"/interface/bridge/print"}}, nil
	default:
		return nil, fmt.Errorf("unsupported RouterOS resource")
	}
}

func addCommand(resource Resource, fields map[string]string) ([]string, error) {
	if pathName := strings.TrimSpace(fields[metaPathField]); pathName != "" {
		return []string{pathName + "/add"}, nil
	}
	switch resource {
	case ResourceIPAddresses:
		return []string{"/ip/address/add"}, nil
	case ResourceRoutes:
		return []string{"/ip/route/add"}, nil
	case ResourceFirewall:
		return []string{"/ip/firewall/filter/add"}, nil
	case ResourceWireless:
		return []string{"/interface/wireless/add"}, nil
	case ResourceTR069Client:
		return []string{"/tr069-client/add"}, nil
	case ResourceBridge:
		return []string{"/interface/bridge/add"}, nil
	default:
		return nil, fmt.Errorf("RouterOS add is not supported for this resource")
	}
}

func setCommand(resource Resource, id string, fields map[string]string) ([]string, string, error) {
	if pathName, rawID := resolveMutationTarget(resource, id, fields); pathName != "" {
		return []string{pathName + "/set"}, rawID, nil
	}
	switch resource {
	case ResourceIPAddresses:
		return []string{"/ip/address/set"}, id, nil
	case ResourceRoutes:
		return []string{"/ip/route/set"}, id, nil
	case ResourceFirewall:
		return []string{"/ip/firewall/filter/set"}, id, nil
	case ResourcePackages:
		return []string{"/system/package/set"}, id, nil
	case ResourceFiles:
		return []string{"/file/set"}, id, nil
	case ResourceWireless:
		return []string{"/interface/wireless/set"}, id, nil
	case ResourceTR069Client:
		return []string{"/tr069-client/set"}, id, nil
	case ResourceBridge:
		return []string{"/interface/bridge/set"}, id, nil
	default:
		return nil, "", fmt.Errorf("RouterOS set is not supported for this resource")
	}
}

func removeCommand(resource Resource, id string) ([]string, string, error) {
	if pathName, rawID := resolveMutationTarget(resource, id, nil); pathName != "" {
		return []string{pathName + "/remove"}, rawID, nil
	}
	switch resource {
	case ResourceIPAddresses:
		return []string{"/ip/address/remove"}, id, nil
	case ResourceRoutes:
		return []string{"/ip/route/remove"}, id, nil
	case ResourceFirewall:
		return []string{"/ip/firewall/filter/remove"}, id, nil
	case ResourceFiles:
		return []string{"/file/remove"}, id, nil
	case ResourceWireless:
		return []string{"/interface/wireless/remove"}, id, nil
	case ResourceTR069Client:
		return []string{"/tr069-client/remove"}, id, nil
	case ResourceBridge:
		return []string{"/interface/bridge/remove"}, id, nil
	default:
		return nil, "", fmt.Errorf("RouterOS remove is not supported for this resource")
	}
}

func resolveMutationTarget(resource Resource, id string, fields map[string]string) (string, string) {
	if fields != nil {
		if pathName := strings.TrimSpace(fields[metaPathField]); pathName != "" {
			rawID := strings.TrimSpace(fields[metaRouterOSIDField])
			if rawID == "" {
				rawID = strings.TrimSpace(fields[".id"])
			}
			if rawID == "" {
				_, rawID = decodeRecordID(id)
			}
			return pathName, rawID
		}
	}
	if pathName, rawID := decodeRecordID(id); pathName != "" {
		return pathName, rawID
	}
	return "", id
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
