package wusp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	ErrUSPPathUnsupported     = errors.New("wusp: path unsupported")
	ErrUSPPathNotFound        = errors.New("wusp: path not found")
	ErrUSPTransferUnsupported = errors.New("wusp: transfer handler not configured")
)

// DataCollector returns live device values for a subset of WUSP paths.
// Unsupported paths are ignored rather than treated as fatal errors so the
// caller can merge live fields with cached or synthetic values.
type DataCollector interface {
	Collect(context.Context, ...string) (*Message, error)
}

// DataSetter applies WUSP parameter changes onto a device backend.
// Unsupported paths should return ErrUSPPathUnsupported.
type DataSetter interface {
	Set(context.Context, string, Value) error
	Delete(context.Context, ...string) error
}

// DataBackend combines collection and mutation for a concrete platform.
type DataBackend interface {
	DataCollector
	DataSetter
}

// USPTransferRequest describes a transport-neutral upload/download request.
// The future WireGuard control-plane can use this for command metadata while
// leaving large/bulk transfer policy to a pluggable handler.
type USPTransferRequest struct {
	Path        string
	URI         string
	Filename    string
	ContentType string
	Payload     []byte
	Metadata    map[string]string
}

// USPTransferResult reports the outcome of a delegated upload/download action.
type USPTransferResult struct {
	Path     string
	URI      string
	Bytes    int64
	Metadata map[string]string
}

type USPTransferHandler func(context.Context, USPTransferRequest) (USPTransferResult, error)
type USPOperationHandler func(context.Context, string, *Message, map[string]string) (*Message, error)
type USPNotifyHandler func(context.Context, string, *Message, map[string]string) error

type USPSupportedObject struct {
	Path          string
	MultiInstance bool
	SinceVersion  string
}

type USPSupportedParam struct {
	Path         string
	Type         ParamType
	Access       Access
	SinceVersion string
}

type USPSupportedDataModel struct {
	RootDataModelVersion string
	Models               []BroadbandDataModel
	Objects              []USPSupportedObject
	Params               []USPSupportedParam
}

type USPProtocolInfo struct {
	Name                 string
	Version              uint64
	Methods              []string
	Compression          []string
	ControlTransport     string
	TransferTransport    string
	MaxControlPayload    uint64
	RecommendedChunkSize uint64
	TunnelOnly           bool
	ReliableTransfer     bool
}

// USPAgentOptions configures a transport-agnostic USPAgent state machine.
type USPAgentOptions struct {
	FillProfile     FillProfile
	UploadHandler   USPTransferHandler
	DownloadHandler USPTransferHandler
	OperateHandler  USPOperationHandler
	NotifyHandler   USPNotifyHandler
	Collector       DataCollector
	Setter          DataSetter
}

// USPAgent is a transport-agnostic in-process USP state surface.
// It intentionally does not assume stream semantics. Control operations are
// modelled as discrete requests over parameter paths, which maps cleanly onto
// both USP Records and WireGuard's packet-oriented transport.
type USPAgent struct {
	mu              sync.RWMutex
	fillProfile     FillProfile
	uploadHandler   USPTransferHandler
	downloadHandler USPTransferHandler
	operateHandler  USPOperationHandler
	notifyHandler   USPNotifyHandler
	collector       DataCollector
	setter          DataSetter
	values          map[string]Field
}

func NewUSPAgent(opts USPAgentOptions) *USPAgent {
	return &USPAgent{
		fillProfile:     normalizeFillProfile(opts.FillProfile),
		uploadHandler:   opts.UploadHandler,
		downloadHandler: opts.DownloadHandler,
		operateHandler:  opts.OperateHandler,
		notifyHandler:   opts.NotifyHandler,
		collector:       opts.Collector,
		setter:          opts.Setter,
		values:          make(map[string]Field),
	}
}

// Bootstrap seeds the agent with schema-derived values for every known param.
func (a *USPAgent) Bootstrap(opts FillOptions) error {
	msg, err := BuildFilledMessage(FillOptions{
		Profile:   coalesceFillProfile(opts.Profile, a.fillProfile),
		DeviceID:  opts.DeviceID,
		Timestamp: opts.Timestamp,
	})
	if err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if opts.Overwrite {
		a.values = make(map[string]Field, len(msg.Fields))
	}

	for _, field := range msg.Fields {
		a.values[field.Path] = cloneField(field)
	}

	return nil
}

// Snapshot returns the stored state as a stable, path-sorted message.
func (a *USPAgent) Snapshot() *Message {
	if a.collector == nil {
		return a.snapshotFromStore()
	}
	values := a.snapshotValues()
	if a.collector != nil {
		if msg, err := a.collector.Collect(context.Background()); err == nil {
			mergeMessageFields(values, msg)
		}
	}
	return buildSnapshotMessage(values)
}

// Get returns stored values for the requested parameter or object paths.
// Param paths return a single field, while object paths ending with "." return
// all stored descendant params beneath that object instance/pattern.
func (a *USPAgent) Get(paths ...string) (*Message, error) {
	if a.collector == nil {
		return a.getStored(paths...)
	}

	values := a.snapshotValues()
	if a.collector != nil {
		msg, err := a.collector.Collect(context.Background(), paths...)
		if err != nil {
			return nil, err
		}
		mergeMessageFields(values, msg)
	}

	if len(paths) == 0 {
		return buildSnapshotMessage(values), nil
	}

	return filterValuesForPaths(values, paths...)
}

// GetByCode resolves one or more stable uint64 path codes and returns the
// matching parameter values or object subtrees.
func (a *USPAgent) GetByCode(codes ...uint64) (*Message, error) {
	paths, err := resolvePathsForCodes(codes...)
	if err != nil {
		return nil, err
	}
	return a.Get(paths...)
}

// Set validates and stores one parameter value.
func (a *USPAgent) Set(path string, value Value) error {
	path = strings.TrimSpace(path)
	return a.storeFields([]Field{{Path: path, Val: value}}, false)
}

// SetBatch validates and stores a full field batch in one pass.
func (a *USPAgent) SetBatch(fields ...Field) error {
	return a.storeFields(fields, false)
}

// Delete removes stored params or object subtrees from the agent state.
func (a *USPAgent) Delete(paths ...string) error {
	if len(paths) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			return &ValidationError{Reason: "empty delete path"}
		}
		normalized = append(normalized, path)
	}

	handledBySetter := false
	if a.setter != nil {
		err := a.setter.Delete(context.Background(), normalized...)
		switch {
		case err == nil:
			handledBySetter = true
		case errors.Is(err, ErrUSPPathUnsupported):
		default:
			return err
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	for _, path := range normalized {
		removed := deletePathLocked(a.values, path)
		if !removed && !handledBySetter {
			return fmt.Errorf("%w: %s", ErrUSPPathNotFound, path)
		}
	}
	return nil
}

// DeleteByCode removes stored params or object subtrees selected by stable
// uint64 path codes.
func (a *USPAgent) DeleteByCode(codes ...uint64) error {
	paths, err := resolvePathsForCodes(codes...)
	if err != nil {
		return err
	}
	return a.Delete(paths...)
}

func (a *USPAgent) Add(objectPath string, initial *Message) ([]string, error) {
	objectPath = strings.TrimSpace(objectPath)
	if objectPath == "" || !isObjectPath(objectPath) {
		return nil, &ValidationError{Path: objectPath, Reason: "add requires an object path"}
	}

	object, canonical, ok := lookupObjectDefinition(objectPath)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUSPPathNotFound, objectPath)
	}
	if !object.MultiInstance {
		return nil, &ValidationError{Path: objectPath, Reason: "object is not multi-instance"}
	}
	if strings.Count(canonical, "{i}") == 0 {
		return nil, &ValidationError{Path: objectPath, Reason: "object path does not expose an instance token"}
	}
	if strings.Count(objectPath, "{i}") > 1 {
		return nil, &ValidationError{Path: objectPath, Reason: "add requires parent instances to be concrete"}
	}

	values := a.snapshotValues()
	instanceValues, actualObjectPath, err := nextObjectInstance(values, objectPath, canonical)
	if err != nil {
		return nil, err
	}

	fields := buildObjectFieldsForAdd(actualObjectPath, canonical, instanceValues)
	if initial != nil {
		for _, field := range initial.Fields {
			path := strings.TrimSpace(field.Path)
			if path == "" {
				return nil, &ValidationError{Reason: "add payload contains empty field path"}
			}
			path = materializePathForInstance(canonicalParamPath(path), canonical, instanceValues)
			if !strings.HasPrefix(path, strings.TrimSuffix(actualObjectPath, ".")) {
				return nil, &ValidationError{Path: field.Path, Reason: "field does not belong to added object"}
			}
			fields[path] = Field{Path: path, Val: cloneValue(field.Val)}
		}
	}

	toStore := make([]Field, 0, len(fields))
	for _, field := range fields {
		toStore = append(toStore, field)
	}
	sort.Slice(toStore, func(i, j int) bool {
		return toStore[i].Path < toStore[j].Path
	})
	if err := a.storeFields(toStore, false); err != nil {
		return nil, err
	}
	a.refreshEntryCount(canonical)
	return []string{actualObjectPath}, nil
}

// AddByCode resolves one stable uint64 object code and creates a new instance.
func (a *USPAgent) AddByCode(code uint64, initial *Message) ([]string, error) {
	path, err := resolveObjectPathForCode(code)
	if err != nil {
		return nil, err
	}
	return a.Add(path, initial)
}

func (a *USPAgent) GetInstances(paths ...string) ([]string, error) {
	values := a.snapshotValues()
	if a.collector != nil {
		msg, err := a.collector.Collect(context.Background(), paths...)
		if err != nil {
			return nil, err
		}
		mergeMessageFields(values, msg)
	}

	if len(paths) == 0 {
		paths = collectAllMultiInstanceObjectPaths()
	}

	out := make([]string, 0)
	seen := make(map[string]struct{})
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			return nil, &ValidationError{Reason: "empty get-instances path"}
		}

		instances, err := instancePathsForRequest(values, path)
		if err != nil {
			return nil, err
		}
		for _, instance := range instances {
			if _, ok := seen[instance]; ok {
				continue
			}
			seen[instance] = struct{}{}
			out = append(out, instance)
		}
	}
	sort.Strings(out)
	return out, nil
}

// GetInstancesByCode resolves one or more stable uint64 path codes and
// returns the matching concrete instance object paths.
func (a *USPAgent) GetInstancesByCode(codes ...uint64) ([]string, error) {
	paths, err := resolvePathsForCodes(codes...)
	if err != nil {
		return nil, err
	}
	return a.GetInstances(paths...)
}

func (a *USPAgent) Operate(ctx context.Context, commandPath string, input *Message, metadata map[string]string) (*Message, error) {
	commandPath = strings.TrimSpace(commandPath)
	if commandPath == "" {
		return nil, &ValidationError{Reason: "operate requires a command path"}
	}
	if a.operateHandler == nil {
		return nil, ErrUSPPathUnsupported
	}
	return a.operateHandler(ctx, commandPath, cloneMessage(input), cloneStringMap(metadata))
}

func (a *USPAgent) Notify(ctx context.Context, eventPath string, payload *Message, metadata map[string]string) error {
	eventPath = strings.TrimSpace(eventPath)
	if eventPath == "" {
		return &ValidationError{Reason: "notify requires an event path"}
	}
	if a.notifyHandler == nil {
		return ErrUSPPathUnsupported
	}
	return a.notifyHandler(ctx, eventPath, cloneMessage(payload), cloneStringMap(metadata))
}

func (a *USPAgent) GetSupportedDM(paths ...string) *USPSupportedDataModel {
	objects := make([]USPSupportedObject, 0)
	params := make([]USPSupportedParam, 0)
	filters := normalizeSupportedDMFilters(paths)

	for _, object := range AllDeviceObjects {
		if !supportedPathMatches(filters, object.Path) {
			continue
		}
		objects = append(objects, USPSupportedObject{
			Path:          object.Path,
			MultiInstance: object.MultiInstance,
			SinceVersion:  normalizeModelVersion(object.SinceVersion),
		})
	}

	for _, param := range AllDeviceParams {
		if !supportedPathMatches(filters, param.Path) {
			continue
		}
		params = append(params, USPSupportedParam{
			Path:         param.Path,
			Type:         param.Type,
			Access:       param.Access,
			SinceVersion: normalizeModelVersion(param.SinceVersion),
		})
	}

	return &USPSupportedDataModel{
		RootDataModelVersion: BroadbandRootDataModelVersion,
		Models:               append([]BroadbandDataModel(nil), BroadbandDataModels...),
		Objects:              objects,
		Params:               params,
	}
}

func (a *USPAgent) GetSupportedProtocol() *USPProtocolInfo {
	return &USPProtocolInfo{
		Name:                 "wantastic-wusp-over-wireguard",
		Version:              1,
		Methods:              []string{"Get", "Set", "Add", "Delete", "GetInstances", "Operate", "Notify", "GetSupportedDM", "GetSupportedProtocol", "Upload", "Download"},
		Compression:          []string{"nested-message-lz4", "stream-chunk-lz4"},
		ControlTransport:     "wireguard-noise-fragmented-datagram",
		TransferTransport:    "wireguard-noise-stream-packets",
		MaxControlPayload:    WUSPMaxDatagramPayload,
		RecommendedChunkSize: USPRecommendedChunkSize,
		TunnelOnly:           true,
		ReliableTransfer:     true,
	}
}

func (a *USPAgent) snapshotValues() map[string]Field {
	a.mu.RLock()
	defer a.mu.RUnlock()

	values := make(map[string]Field, len(a.values))
	for path, field := range a.values {
		values[path] = cloneField(field)
	}
	return values
}

func (a *USPAgent) snapshotFromStore() *Message {
	a.mu.RLock()
	defer a.mu.RUnlock()

	fields := make([]Field, 0, len(a.values))
	for _, field := range a.values {
		fields = append(fields, cloneField(field))
	}
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].Path < fields[j].Path
	})
	return &Message{Fields: fields}
}

func (a *USPAgent) getStored(paths ...string) (*Message, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if len(paths) == 0 {
		fields := make([]Field, 0, len(a.values))
		for _, field := range a.values {
			fields = append(fields, cloneField(field))
		}
		sort.Slice(fields, func(i, j int) bool {
			return fields[i].Path < fields[j].Path
		})
		return &Message{Fields: fields}, nil
	}

	out := &Message{Fields: make([]Field, 0, len(paths))}
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			return nil, &ValidationError{Reason: "empty get path"}
		}

		if isObjectPath(path) {
			matched := false
			for key, field := range a.values {
				if !strings.HasPrefix(key, path) {
					continue
				}
				if _, ok := seen[key]; ok {
					continue
				}
				out.Fields = append(out.Fields, cloneField(field))
				seen[key] = struct{}{}
				matched = true
			}
			if !matched {
				return nil, fmt.Errorf("%w: %s", ErrUSPPathNotFound, path)
			}
			continue
		}

		field, ok := a.values[path]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUSPPathNotFound, path)
		}
		if _, ok := seen[path]; ok {
			continue
		}
		out.Fields = append(out.Fields, cloneField(field))
		seen[path] = struct{}{}
	}

	sort.Slice(out.Fields, func(i, j int) bool {
		return out.Fields[i].Path < out.Fields[j].Path
	})
	return out, nil
}

// Upload delegates transfer execution to the configured upload handler.
func (a *USPAgent) Upload(ctx context.Context, req USPTransferRequest) (USPTransferResult, error) {
	return a.upload(ctx, req, true)
}

func (a *USPAgent) upload(ctx context.Context, req USPTransferRequest, clone bool) (USPTransferResult, error) {
	if err := validateTransferRequest(req); err != nil {
		return USPTransferResult{}, err
	}
	if a.uploadHandler == nil {
		return USPTransferResult{}, ErrUSPTransferUnsupported
	}
	if clone {
		req = cloneTransferRequest(req)
	}
	return a.uploadHandler(ctx, req)
}

// Download delegates transfer execution to the configured download handler.
func (a *USPAgent) Download(ctx context.Context, req USPTransferRequest) (USPTransferResult, error) {
	return a.download(ctx, req, true)
}

func (a *USPAgent) download(ctx context.Context, req USPTransferRequest, clone bool) (USPTransferResult, error) {
	if err := validateTransferRequest(req); err != nil {
		return USPTransferResult{}, err
	}
	if a.downloadHandler == nil {
		return USPTransferResult{}, ErrUSPTransferUnsupported
	}
	if clone {
		req = cloneTransferRequest(req)
	}
	return a.downloadHandler(ctx, req)
}

func coalesceFillProfile(primary, fallback FillProfile) FillProfile {
	if normalizeFillProfile(primary) != FillProfileRealistic || primary == FillProfileMaxCompressible {
		return normalizeFillProfile(primary)
	}
	if primary == "" {
		return normalizeFillProfile(fallback)
	}
	return normalizeFillProfile(primary)
}

func buildSnapshotMessage(values map[string]Field) *Message {
	fields := sortedStoredFields(values)
	msg := &Message{
		Fields: make([]Field, 0, len(fields)),
	}
	for _, field := range fields {
		msg.Fields = append(msg.Fields, cloneField(field))
	}
	return msg
}

func filterValuesForPaths(values map[string]Field, paths ...string) (*Message, error) {
	out := &Message{Fields: make([]Field, 0)}
	seen := make(map[string]struct{})

	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			return nil, &ValidationError{Reason: "empty get path"}
		}

		if isObjectPath(path) {
			matched := false
			for _, field := range sortedStoredFields(values) {
				if strings.HasPrefix(field.Path, path) {
					if _, ok := seen[field.Path]; ok {
						continue
					}
					out.Fields = append(out.Fields, cloneField(field))
					seen[field.Path] = struct{}{}
					matched = true
				}
			}
			if !matched {
				return nil, fmt.Errorf("%w: %s", ErrUSPPathNotFound, path)
			}
			continue
		}

		field, ok := values[path]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUSPPathNotFound, path)
		}
		if _, ok := seen[path]; ok {
			continue
		}
		out.Fields = append(out.Fields, cloneField(field))
		seen[path] = struct{}{}
	}

	sort.Slice(out.Fields, func(i, j int) bool {
		return out.Fields[i].Path < out.Fields[j].Path
	})
	return out, nil
}

func mergeMessageFields(values map[string]Field, msg *Message) {
	if msg == nil {
		return
	}
	for _, field := range msg.Fields {
		values[field.Path] = cloneField(field)
	}
}

func deletePathLocked(values map[string]Field, path string) bool {
	if isObjectPath(path) {
		removed := false
		for key := range values {
			if strings.HasPrefix(key, path) {
				delete(values, key)
				removed = true
			}
		}
		return removed
	}

	if _, ok := values[path]; !ok {
		return false
	}
	delete(values, path)
	return true
}

func sortedStoredFields(values map[string]Field) []Field {
	fields := make([]Field, 0, len(values))
	for _, field := range values {
		fields = append(fields, field)
	}
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].Path < fields[j].Path
	})
	return fields
}

func (a *USPAgent) storeFields(fields []Field, alreadyValidated bool) error {
	if len(fields) == 0 {
		return nil
	}

	if !alreadyValidated {
		normalized := make([]Field, 0, len(fields))
		for _, field := range fields {
			field.Path = strings.TrimSpace(field.Path)
			normalized = append(normalized, field)
		}
		msg := &Message{Fields: normalized}
		if err := ValidateMessageFast(msg); err != nil {
			return err
		}
		fields = normalized
	}

	if a.setter != nil {
		for _, field := range fields {
			field.Path = strings.TrimSpace(field.Path)
			if err := a.setter.Set(context.Background(), field.Path, cloneValue(field.Val)); err != nil && !errors.Is(err, ErrUSPPathUnsupported) {
				return err
			}
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	for _, field := range fields {
		field.Path = strings.TrimSpace(field.Path)
		if id, ok := globalRegistry.IDFor(field.Path); ok {
			field.id = id
		}
		a.values[field.Path] = cloneField(field)
	}
	return nil
}

func cloneField(field Field) Field {
	cloned := Field{
		id:   field.id,
		Path: field.Path,
		Val:  cloneValue(field.Val),
	}
	return cloned
}

func cloneValue(value Value) Value {
	cloned := Value{
		Tag:  value.Tag,
		ival: value.ival,
		fval: value.fval,
	}
	if len(value.blob) > 0 {
		cloned.blob = append([]byte(nil), value.blob...)
	}
	if len(value.list) > 0 {
		cloned.list = make([]Value, len(value.list))
		for i := range value.list {
			cloned.list[i] = cloneValue(value.list[i])
		}
	}
	return cloned
}

func cloneTransferRequest(req USPTransferRequest) USPTransferRequest {
	cloned := req
	if len(req.Payload) > 0 {
		cloned.Payload = append([]byte(nil), req.Payload...)
	}
	if len(req.Metadata) > 0 {
		cloned.Metadata = make(map[string]string, len(req.Metadata))
		for key, value := range req.Metadata {
			cloned.Metadata[key] = value
		}
	}
	return cloned
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneMessage(msg *Message) *Message {
	if msg == nil {
		return nil
	}
	cloned := &Message{
		DeviceID:  msg.DeviceID,
		Timestamp: msg.Timestamp,
		Fields:    make([]Field, 0, len(msg.Fields)),
	}
	for _, field := range msg.Fields {
		cloned.Fields = append(cloned.Fields, cloneField(field))
	}
	return cloned
}

func resolvePathsForCodes(codes ...uint64) ([]string, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	model := runtimeDeviceFast()
	if model == nil {
		return nil, ErrUSPPathUnsupported
	}
	paths := make([]string, 0, len(codes))
	for _, code := range codes {
		path, ok := model.PathByCode(code)
		if !ok {
			return nil, fmt.Errorf("%w: unknown path code %d", ErrUSPPathNotFound, code)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func resolveObjectPathForCode(code uint64) (string, error) {
	if code == 0 {
		return "", &ValidationError{Reason: "missing object code"}
	}
	model := runtimeDeviceFast()
	if model == nil {
		return "", ErrUSPPathUnsupported
	}
	path, ok := model.PathByCode(code)
	if !ok {
		return "", fmt.Errorf("%w: unknown path code %d", ErrUSPPathNotFound, code)
	}
	if !isObjectPath(path) {
		return "", &ValidationError{Path: path, Reason: "path code does not resolve to an object"}
	}
	return path, nil
}

func subsetMessageByPaths(msg *Message, paths ...string) *Message {
	if msg == nil {
		return &Message{}
	}

	out := &Message{
		DeviceID:  msg.DeviceID,
		Timestamp: msg.Timestamp,
		Fields:    make([]Field, 0, len(msg.Fields)),
	}
	if len(paths) == 0 {
		for _, field := range msg.Fields {
			out.Fields = append(out.Fields, cloneField(field))
		}
		return out
	}

	seen := make(map[string]struct{})
	for _, requested := range paths {
		requested = strings.TrimSpace(requested)
		if requested == "" {
			continue
		}
		for _, field := range msg.Fields {
			if requested != field.Path && !(strings.HasSuffix(requested, ".") && strings.HasPrefix(field.Path, requested)) {
				continue
			}
			if _, ok := seen[field.Path]; ok {
				continue
			}
			seen[field.Path] = struct{}{}
			out.Fields = append(out.Fields, cloneField(field))
		}
	}
	sort.Slice(out.Fields, func(i, j int) bool {
		return out.Fields[i].Path < out.Fields[j].Path
	})
	return out
}

func validateTransferRequest(req USPTransferRequest) error {
	if strings.TrimSpace(req.Path) == "" {
		return &ValidationError{Reason: "transfer path is required"}
	}

	if !isObjectPath(req.Path) {
		if _, _, ok := lookupSafeParam(req.Path); !ok {
			return &ValidationError{Path: req.Path, Reason: "unknown transfer path"}
		}
	}

	return nil
}

func isObjectPath(path string) bool {
	return strings.HasSuffix(strings.TrimSpace(path), ".")
}

func lookupObjectDefinition(path string) (Object, string, bool) {
	canonical := canonicalParamPath(strings.TrimSpace(path))
	for _, object := range AllDeviceObjects {
		if object.Path == canonical {
			return object, canonical, true
		}
	}
	return Object{}, canonical, false
}

func nextObjectInstance(values map[string]Field, requestedPath, canonical string) ([]string, string, error) {
	templateParts := splitObjectParts(canonical)
	requestParts := splitObjectParts(strings.TrimSpace(requestedPath))
	if len(templateParts) != len(requestParts) {
		return nil, "", &ValidationError{Path: requestedPath, Reason: "object path shape does not match schema"}
	}

	instanceValues := make([]string, 0, strings.Count(canonical, "{i}"))
	unresolved := -1
	for i := range templateParts {
		switch templateParts[i] {
		case "{i}":
			switch {
			case requestParts[i] == "{i}":
				if unresolved != -1 {
					return nil, "", &ValidationError{Path: requestedPath, Reason: "only the final instance token may be unresolved"}
				}
				unresolved = len(instanceValues)
				instanceValues = append(instanceValues, "")
			case isNumericPathSegment(requestParts[i]):
				instanceValues = append(instanceValues, requestParts[i])
			default:
				return nil, "", &ValidationError{Path: requestedPath, Reason: "instance segments must be numeric or {i}"}
			}
		default:
			if templateParts[i] != requestParts[i] {
				return nil, "", &ValidationError{Path: requestedPath, Reason: "object path does not match schema"}
			}
		}
	}
	if unresolved == -1 {
		return nil, "", &ValidationError{Path: requestedPath, Reason: "add requires an unresolved {i} token"}
	}

	maxInstance := 0
	for path := range values {
		candidate, ok := extractActualObjectPath(canonical, path)
		if !ok {
			continue
		}
		parts := splitObjectParts(candidate)
		valueIdx := 0
		match := true
		for i := range templateParts {
			if templateParts[i] != "{i}" {
				continue
			}
			if valueIdx == unresolved {
				valueIdx++
				continue
			}
			if instanceValues[valueIdx] != parts[i] {
				match = false
				break
			}
			valueIdx++
		}
		if !match {
			continue
		}
		instanceValue, err := parseObjectInstanceAt(candidate, unresolved)
		if err != nil {
			continue
		}
		if instanceValue > maxInstance {
			maxInstance = instanceValue
		}
	}
	instanceValues[unresolved] = fmt.Sprintf("%d", maxInstance+1)

	actualParts := append([]string(nil), templateParts...)
	valueIdx := 0
	for i := range actualParts {
		if actualParts[i] != "{i}" {
			continue
		}
		actualParts[i] = instanceValues[valueIdx]
		valueIdx++
	}
	return instanceValues, strings.Join(actualParts, ".") + ".", nil
}

func buildObjectFieldsForAdd(actualObjectPath, canonicalObjectPath string, instanceValues []string) map[string]Field {
	fields := make(map[string]Field)
	for i, param := range AllDeviceParams {
		if !strings.HasPrefix(param.Path, canonicalObjectPath) {
			continue
		}
		path := materializePathForInstance(param.Path, canonicalObjectPath, instanceValues)
		fields[path] = Field{
			Path: path,
			Val:  safeFilledValueForParam(param, i, FillProfileRealistic),
		}
	}
	return fields
}

func materializePathForInstance(path, canonicalObjectPath string, instanceValues []string) string {
	templateParts := strings.Split(canonicalObjectPath, ".")
	pathParts := strings.Split(path, ".")
	actual := make([]string, len(pathParts))
	copy(actual, pathParts)
	valueIdx := 0
	for i := 0; i < len(templateParts) && i < len(actual); i++ {
		if templateParts[i] != "{i}" || valueIdx >= len(instanceValues) {
			continue
		}
		actual[i] = instanceValues[valueIdx]
		valueIdx++
	}
	return strings.Join(actual, ".")
}

func instancePathsForRequest(values map[string]Field, path string) ([]string, error) {
	object, canonical, ok := lookupObjectDefinition(path)
	if ok {
		if object.MultiInstance {
			return instancePathsForObject(values, canonical), nil
		}
		if hasFieldsWithPrefix(values, canonical) {
			return []string{canonical}, nil
		}
		return nil, fmt.Errorf("%w: %s", ErrUSPPathNotFound, path)
	}
	if !isObjectPath(path) {
		return nil, &ValidationError{Path: path, Reason: "get-instances requires object paths"}
	}
	canonical = canonicalParamPath(path)
	instances := instancePathsForObject(values, canonical)
	if len(instances) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrUSPPathNotFound, path)
	}
	return instances, nil
}

func instancePathsForObject(values map[string]Field, canonicalObjectPath string) []string {
	instances := make([]string, 0)
	seen := make(map[string]struct{})
	for fieldPath := range values {
		instancePath, ok := extractActualObjectPath(canonicalObjectPath, fieldPath)
		if !ok {
			continue
		}
		if _, exists := seen[instancePath]; exists {
			continue
		}
		seen[instancePath] = struct{}{}
		instances = append(instances, instancePath)
	}
	sort.Strings(instances)
	return instances
}

func extractActualObjectPath(canonicalObjectPath, fieldPath string) (string, bool) {
	objectParts := splitObjectParts(canonicalObjectPath)
	fieldParts := strings.Split(strings.TrimSpace(fieldPath), ".")
	if len(fieldParts) < len(objectParts)+1 {
		return "", false
	}
	actualParts := make([]string, 0, len(objectParts))
	for i, objectPart := range objectParts {
		fieldPart := fieldParts[i]
		switch {
		case objectPart == "{i}" && isNumericPathSegment(fieldPart):
			actualParts = append(actualParts, fieldPart)
		case objectPart == fieldPart:
			actualParts = append(actualParts, fieldPart)
		default:
			return "", false
		}
	}
	return strings.Join(actualParts, ".") + ".", true
}

func splitObjectParts(path string) []string {
	return strings.Split(strings.TrimSuffix(strings.TrimSpace(path), "."), ".")
}

func parseObjectInstanceAt(objectPath string, instanceOrdinal int) (int, error) {
	parts := splitObjectParts(objectPath)
	count := 0
	for _, part := range parts {
		if !isNumericPathSegment(part) {
			continue
		}
		if count == instanceOrdinal {
			var value int
			_, err := fmt.Sscanf(part, "%d", &value)
			return value, err
		}
		count++
	}
	return 0, fmt.Errorf("instance ordinal %d not found in %s", instanceOrdinal, objectPath)
}

func hasFieldsWithPrefix(values map[string]Field, prefix string) bool {
	for path := range values {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func collectAllMultiInstanceObjectPaths() []string {
	out := make([]string, 0)
	for _, object := range AllDeviceObjects {
		if object.MultiInstance {
			out = append(out, object.Path)
		}
	}
	return out
}

func normalizeSupportedDMFilters(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		path = canonicalParamPath(path)
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func supportedPathMatches(filters []string, candidate string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, filter := range filters {
		if candidate == filter || strings.HasPrefix(candidate, filter) || strings.HasPrefix(filter, candidate) {
			return true
		}
	}
	return false
}

func (a *USPAgent) refreshEntryCount(objectPath string) {
	countPath, ok := objectCountParamPath(objectPath)
	if !ok {
		return
	}
	count := uint64(len(a.instancePaths(objectPath)))
	a.mu.Lock()
	a.values[countPath] = Field{Path: countPath, Val: Uint(count)}
	a.mu.Unlock()
}

func (a *USPAgent) instancePaths(objectPath string) []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return instancePathsForObject(a.values, objectPath)
}

func objectCountParamPath(objectPath string) (string, bool) {
	parts := splitObjectParts(objectPath)
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "{i}" {
			continue
		}
		if i == 0 {
			return "", false
		}
		child := parts[i-1]
		prefix := ""
		if i > 1 {
			prefix = strings.Join(parts[:i-1], ".") + "."
		}
		countPath := prefix + child + "NumberOfEntries"
		if _, _, ok := lookupSafeParam(countPath); ok {
			return countPath, true
		}
		return "", false
	}
	return "", false
}
