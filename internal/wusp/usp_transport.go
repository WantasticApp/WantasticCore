package wusp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

const uspTransportVersion = 1

const (
	uspTransportKindRequest  = 1
	uspTransportKindResponse = 2
)

const (
	MetadataKeyResponseMaxControlPayload = "wusp.response_max_control_payload"
	TransferMetadataSource               = "source"
	TransferMetadataDestination          = "destination"
	TransferMetadataSessionID            = "session_id"
	TransferMetadataChunkSize            = "chunk_size"
	TransferMetadataTransport            = "transport"
)

func CloneMetadata(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func WithResponseMaxControlPayload(metadata map[string]string, maxPayload int) map[string]string {
	out := CloneMetadata(metadata)
	if maxPayload <= 0 {
		return out
	}
	if out == nil {
		out = make(map[string]string, 1)
	}
	out[MetadataKeyResponseMaxControlPayload] = strconv.Itoa(maxPayload)
	return out
}

func RequestedResponseMaxControlPayload(metadata map[string]string, fallback int) int {
	if fallback <= 0 {
		fallback = WUSPMaxDatagramPayload
	}
	if len(metadata) == 0 {
		return fallback
	}
	value := strings.TrimSpace(metadata[MetadataKeyResponseMaxControlPayload])
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < uspControlFragmentHeaderSize+1 {
		return fallback
	}
	if parsed > WUSPMaxDatagramPayload {
		return WUSPMaxDatagramPayload
	}
	return parsed
}

// USPAgentMethod identifies one USP operation carried over the WUSP control transport.
type USPAgentMethod uint8

const (
	USPAgentMethodGet USPAgentMethod = iota + 1
	USPAgentMethodSet
	USPAgentMethodAdd
	USPAgentMethodDelete
	USPAgentMethodGetInstances
	USPAgentMethodOperate
	USPAgentMethodNotify
	USPAgentMethodGetSupportedDM
	USPAgentMethodGetSupportedProtocol
	USPAgentMethodUpload
	USPAgentMethodDownload
)

// String returns the canonical name of the USP method, used in logs and
// dashboards. Falls back to the numeric value for unknown codes.
func (m USPAgentMethod) String() string {
	switch m {
	case USPAgentMethodGet:
		return "Get"
	case USPAgentMethodSet:
		return "Set"
	case USPAgentMethodAdd:
		return "Add"
	case USPAgentMethodDelete:
		return "Delete"
	case USPAgentMethodGetInstances:
		return "GetInstances"
	case USPAgentMethodOperate:
		return "Operate"
	case USPAgentMethodNotify:
		return "Notify"
	case USPAgentMethodGetSupportedDM:
		return "GetSupportedDM"
	case USPAgentMethodGetSupportedProtocol:
		return "GetSupportedProtocol"
	case USPAgentMethodUpload:
		return "Upload"
	case USPAgentMethodDownload:
		return "Download"
	default:
		return fmt.Sprintf("Method(%d)", uint8(m))
	}
}

type uspTransportHeader struct {
	Version uint8
	Kind    uint8
	Method  USPAgentMethod
	Flags   uint8
	ID      uint64
}

// USPAgentRequest is the on-the-wire request shape used over MessageWUSPType.
type USPAgentRequest struct {
	ID              uint64
	Method          USPAgentMethod
	Paths           []string
	PathCodes       []uint64
	PathInstances   [][]uint64
	ObjectPath      string
	ObjectCode      uint64
	ObjectInstances []uint64
	Message         *Message
	Transfer        *USPTransferRequest
	Metadata        map[string]string
}

// USPAgentResponse is the on-the-wire response shape used over MessageWUSPType.
type USPAgentResponse struct {
	ID                 uint64
	Method             USPAgentMethod
	Paths              []string
	PathCodes          []uint64
	PathInstances      [][]uint64
	ObjectPath         string
	ObjectCode         uint64
	ObjectInstances    []uint64
	Message            *Message
	Transfer           *USPTransferResult
	Metadata           map[string]string
	SupportedDataModel *USPSupportedDataModel
	Protocol           *USPProtocolInfo
	Error              string
}

var (
	ErrUSPTransportMalformed      = errors.New("wusp: malformed transport payload")
	ErrUSPTransportUnsupported    = errors.New("wusp: unsupported transport payload")
	ErrUSPTransportMethodRequired = errors.New("wusp: transport method required")
)

// HandleRequest executes a transport request against the agent and returns the response payload.
func (a *USPAgent) HandleRequest(ctx context.Context, req USPAgentRequest) (USPAgentResponse, error) {
	req = resolveFastRequestPaths(req)
	resp := USPAgentResponse{
		ID:     req.ID,
		Method: req.Method,
	}

	switch req.Method {
	case USPAgentMethodGet:
		msg, err := a.Get(req.Paths...)
		if err != nil {
			resp.Error = err.Error()
			return resp, nil
		}
		resp.Message = msg
		return resp, nil
	case USPAgentMethodSet:
		if req.Message == nil || len(req.Message.Fields) == 0 {
			resp.Error = "set request missing fields"
			return resp, nil
		}
		if err := a.storeFields(req.Message.Fields, true); err != nil {
			resp.Error = err.Error()
			return resp, nil
		}
		return resp, nil
	case USPAgentMethodAdd:
		instances, err := a.Add(req.ObjectPath, req.Message)
		if err != nil {
			resp.Error = err.Error()
			return resp, nil
		}
		resp.ObjectPath = req.ObjectPath
		resp.Paths = instances
		return resp, nil
	case USPAgentMethodDelete:
		if len(req.Paths) == 0 {
			resp.Error = "delete request missing paths"
			return resp, nil
		}
		if err := a.Delete(req.Paths...); err != nil {
			resp.Error = err.Error()
			return resp, nil
		}
		return resp, nil
	case USPAgentMethodGetInstances:
		instances, err := a.GetInstances(req.Paths...)
		if err != nil {
			resp.Error = err.Error()
			return resp, nil
		}
		resp.Paths = instances
		return resp, nil
	case USPAgentMethodOperate:
		msg, err := a.Operate(ctx, req.ObjectPath, req.Message, req.Metadata)
		if err != nil {
			resp.Error = err.Error()
			return resp, nil
		}
		resp.ObjectPath = req.ObjectPath
		resp.Message = msg
		return resp, nil
	case USPAgentMethodNotify:
		if err := a.Notify(ctx, req.ObjectPath, req.Message, req.Metadata); err != nil {
			resp.Error = err.Error()
			return resp, nil
		}
		resp.ObjectPath = req.ObjectPath
		return resp, nil
	case USPAgentMethodGetSupportedDM:
		resp.SupportedDataModel = a.GetSupportedDM(req.Paths...)
		return resp, nil
	case USPAgentMethodGetSupportedProtocol:
		resp.Protocol = a.GetSupportedProtocol()
		return resp, nil
	case USPAgentMethodUpload:
		if req.Transfer == nil {
			resp.Error = "upload request missing transfer"
			return resp, nil
		}
		result, err := a.upload(ctx, *req.Transfer, false)
		if err != nil {
			resp.Error = err.Error()
			return resp, nil
		}
		resp.Transfer = &result
		return resp, nil
	case USPAgentMethodDownload:
		if req.Transfer == nil {
			resp.Error = "download request missing transfer"
			return resp, nil
		}
		result, err := a.download(ctx, *req.Transfer, false)
		if err != nil {
			resp.Error = err.Error()
			return resp, nil
		}
		resp.Transfer = &result
		return resp, nil
	default:
		return USPAgentResponse{}, fmt.Errorf("%w: method %d", ErrUSPTransportUnsupported, req.Method)
	}
}

func EncodeUSPAgentRequest(req USPAgentRequest) ([]byte, error) {
	req = compactRequestSelectors(req)
	if err := validateUSPAgentRequest(req); err != nil {
		return nil, err
	}

	// Use string paths instead of binary codes — the controller and agent ship
	// independent wusp packages whose path-code registries drift, so encoding
	// fields by string path is the only durable cross-boundary wire format.
	// Mirrors the symmetric reset on the agent's EncodeUSPAgentResponse.
	if req.Message != nil {
		req.Message.ResetAllFieldIDs()
	}

	var nestedFrame []byte
	var err error
	header := uspTransportHeader{
		Version: uspTransportVersion,
		Kind:    uspTransportKindRequest,
		Method:  req.Method,
		ID:      req.ID,
	}
	if req.Message != nil {
		header.Flags |= 1 << 0
		nestedFrame, err = encodeNestedValidatedMessage(req.Message)
		if err != nil {
			return nil, err
		}
	}
	if req.Transfer != nil {
		header.Flags |= 1 << 1
	}
	if strings.TrimSpace(req.ObjectPath) != "" {
		header.Flags |= 1 << 2
	}
	if len(req.Metadata) > 0 {
		header.Flags |= 1 << 3
	}
	if len(req.PathCodes) > 0 {
		header.Flags |= 1 << 4
	}
	if req.ObjectCode != 0 {
		header.Flags |= 1 << 5
	}
	transport := newUSPTransportBuffer(estimateUSPAgentRequestSize(req, nestedFrame))
	transport.writeHeader(header)
	transport.writeStringList(req.Paths)
	if header.Flags&(1<<2) != 0 {
		transport.writeString(req.ObjectPath)
	}
	if header.Flags&(1<<4) != 0 {
		transport.writePathSelectors(req.PathCodes, req.PathInstances)
	}
	if header.Flags&(1<<5) != 0 {
		transport.writePathSelector(req.ObjectCode, req.ObjectInstances)
	}
	if len(nestedFrame) > 0 {
		transport.writeBytes(nestedFrame)
	}
	if req.Transfer != nil {
		transport.writeTransferRequest(*req.Transfer)
	}
	if header.Flags&(1<<3) != 0 {
		transport.writeMetadata(req.Metadata)
	}
	return transport.bytes(), nil
}

func DecodeUSPAgentRequest(data []byte) (USPAgentRequest, error) {
	reader := newUSPTransportReader(data)
	header, err := reader.readHeader()
	if err != nil {
		return USPAgentRequest{}, err
	}
	if header.Kind != uspTransportKindRequest {
		return USPAgentRequest{}, fmt.Errorf("%w: unexpected request kind %d", ErrUSPTransportMalformed, header.Kind)
	}

	req := USPAgentRequest{
		ID:     header.ID,
		Method: header.Method,
	}
	req.Paths, err = reader.readStringList()
	if err != nil {
		return USPAgentRequest{}, err
	}
	if header.Flags&(1<<2) != 0 {
		req.ObjectPath, err = reader.readString()
		if err != nil {
			return USPAgentRequest{}, err
		}
	}
	if header.Flags&(1<<4) != 0 {
		req.PathCodes, req.PathInstances, err = reader.readPathSelectors()
		if err != nil {
			return USPAgentRequest{}, err
		}
	}
	if header.Flags&(1<<5) != 0 {
		req.ObjectCode, req.ObjectInstances, err = reader.readPathSelector()
		if err != nil {
			return USPAgentRequest{}, err
		}
	}
	if header.Flags&(1<<0) != 0 {
		msgFrame, err := reader.readBytes()
		if err != nil {
			return USPAgentRequest{}, err
		}
		req.Message, err = DecodeMessage(msgFrame)
		if err != nil {
			return USPAgentRequest{}, err
		}
	}
	if header.Flags&(1<<1) != 0 {
		transfer, err := reader.readTransferRequest()
		if err != nil {
			return USPAgentRequest{}, err
		}
		req.Transfer = &transfer
	}
	if header.Flags&(1<<3) != 0 {
		req.Metadata, err = reader.readMetadata()
		if err != nil {
			return USPAgentRequest{}, err
		}
	}
	if !reader.done() && !reader.remainingAllZero() {
		return USPAgentRequest{}, fmt.Errorf("%w: trailing request data", ErrUSPTransportMalformed)
	}
	req = resolveFastRequestPaths(req)
	return req, validateUSPAgentRequest(req)
}

func EncodeUSPAgentResponse(resp USPAgentResponse) ([]byte, error) {
	resp = compactResponseSelectors(resp)
	if err := validateUSPAgentResponse(resp); err != nil {
		return nil, err
	}

	var nestedFrame []byte
	var err error
	header := uspTransportHeader{
		Version: uspTransportVersion,
		Kind:    uspTransportKindResponse,
		Method:  resp.Method,
		ID:      resp.ID,
	}
	if resp.Message != nil {
		header.Flags |= 1 << 0
		nestedFrame, err = encodeNestedValidatedMessage(resp.Message)
		if err != nil {
			return nil, err
		}
	}
	if resp.Transfer != nil {
		header.Flags |= 1 << 1
	}
	if len(resp.Paths) > 0 {
		header.Flags |= 1 << 2
	}
	if strings.TrimSpace(resp.ObjectPath) != "" {
		header.Flags |= 1 << 3
	}
	if len(resp.Metadata) > 0 {
		header.Flags |= 1 << 4
	}
	if resp.SupportedDataModel != nil {
		header.Flags |= 1 << 5
	}
	if resp.Protocol != nil {
		header.Flags |= 1 << 6
	}
	if len(resp.PathCodes) > 0 || resp.ObjectCode != 0 || len(resp.ObjectInstances) > 0 {
		header.Flags |= 1 << 7
	}
	transport := newUSPTransportBuffer(estimateUSPAgentResponseSize(resp, nestedFrame))
	transport.writeHeader(header)
	transport.writeString(resp.Error)
	if header.Flags&(1<<2) != 0 {
		transport.writeStringList(resp.Paths)
	}
	if header.Flags&(1<<3) != 0 {
		transport.writeString(resp.ObjectPath)
	}
	if len(nestedFrame) > 0 {
		transport.writeBytes(nestedFrame)
	}
	if resp.Transfer != nil {
		transport.writeTransferResult(*resp.Transfer)
	}
	if header.Flags&(1<<4) != 0 {
		transport.writeMetadata(resp.Metadata)
	}
	if header.Flags&(1<<7) != 0 {
		transport.writePathSelectors(resp.PathCodes, resp.PathInstances)
		transport.writePathSelector(resp.ObjectCode, resp.ObjectInstances)
	}
	if header.Flags&(1<<5) != 0 {
		transport.writeSupportedDataModel(*resp.SupportedDataModel)
	}
	if header.Flags&(1<<6) != 0 {
		transport.writeProtocolInfo(*resp.Protocol)
	}
	return transport.bytes(), nil
}

func DecodeUSPAgentResponse(data []byte) (USPAgentResponse, error) {
	reader := newUSPTransportReader(data)
	header, err := reader.readHeader()
	if err != nil {
		return USPAgentResponse{}, err
	}
	if header.Kind != uspTransportKindResponse {
		return USPAgentResponse{}, fmt.Errorf("%w: unexpected response kind %d", ErrUSPTransportMalformed, header.Kind)
	}

	resp := USPAgentResponse{
		ID:     header.ID,
		Method: header.Method,
	}
	resp.Error, err = reader.readString()
	if err != nil {
		return USPAgentResponse{}, err
	}
	if header.Flags&(1<<2) != 0 {
		resp.Paths, err = reader.readStringList()
		if err != nil {
			return USPAgentResponse{}, err
		}
	}
	if header.Flags&(1<<3) != 0 {
		resp.ObjectPath, err = reader.readString()
		if err != nil {
			return USPAgentResponse{}, err
		}
	}
	if header.Flags&(1<<0) != 0 {
		msgFrame, err := reader.readBytes()
		if err != nil {
			return USPAgentResponse{}, err
		}
		resp.Message, err = DecodeMessage(msgFrame)
		if err != nil {
			return USPAgentResponse{}, err
		}
	}
	if header.Flags&(1<<1) != 0 {
		transfer, err := reader.readTransferResult()
		if err != nil {
			return USPAgentResponse{}, err
		}
		resp.Transfer = &transfer
	}
	if header.Flags&(1<<4) != 0 {
		resp.Metadata, err = reader.readMetadata()
		if err != nil {
			return USPAgentResponse{}, err
		}
	}
	if header.Flags&(1<<7) != 0 {
		resp.PathCodes, resp.PathInstances, err = reader.readPathSelectors()
		if err != nil {
			return USPAgentResponse{}, err
		}
		resp.ObjectCode, resp.ObjectInstances, err = reader.readPathSelector()
		if err != nil {
			return USPAgentResponse{}, err
		}
	}
	if header.Flags&(1<<5) != 0 {
		resp.SupportedDataModel, err = reader.readSupportedDataModel()
		if err != nil {
			return USPAgentResponse{}, err
		}
	}
	if header.Flags&(1<<6) != 0 {
		resp.Protocol, err = reader.readProtocolInfo()
		if err != nil {
			return USPAgentResponse{}, err
		}
	}
	if !reader.done() && !reader.remainingAllZero() {
		return USPAgentResponse{}, fmt.Errorf("%w: trailing response data", ErrUSPTransportMalformed)
	}
	resp = resolveFastResponsePaths(resp)
	return resp, validateUSPAgentResponse(resp)
}

func validateUSPAgentRequest(req USPAgentRequest) error {
	if req.Method == 0 {
		return ErrUSPTransportMethodRequired
	}
	for _, path := range req.Paths {
		if strings.TrimSpace(path) == "" {
			return &ValidationError{Reason: "request contains empty path"}
		}
	}
	for _, code := range req.PathCodes {
		if code == 0 {
			return &ValidationError{Reason: "request contains zero path code"}
		}
	}
	if len(req.PathInstances) > 0 && len(req.PathInstances) != len(req.PathCodes) {
		return &ValidationError{Reason: "request path selector metadata length mismatch"}
	}
	if strings.TrimSpace(req.ObjectPath) != "" && !isObjectPath(req.ObjectPath) {
		return &ValidationError{Path: req.ObjectPath, Reason: "request object path must end with '.'"}
	}
	if req.ObjectCode == 0 && len(req.ObjectInstances) > 0 {
		return &ValidationError{Reason: "request object selector instances without object code"}
	}
	if req.ObjectCode == 0 && len(req.PathCodes) == 0 && len(req.Paths) == 0 {
		switch req.Method {
		// USPAgentMethodGet with no paths is valid — it means "get all parameters"
		// (full device snapshot). Only Delete and GetInstances require at least one path.
		case USPAgentMethodDelete, USPAgentMethodGetInstances:
			return &ValidationError{Reason: "request missing path selector"}
		}
	}
	if req.Message != nil {
		if err := ValidateMessageFast(req.Message); err != nil {
			return err
		}
	}
	if req.Transfer != nil {
		if err := validateTransferRequest(*req.Transfer); err != nil {
			return err
		}
	}
	switch req.Method {
	case USPAgentMethodSet:
		if req.Message == nil || len(req.Message.Fields) == 0 {
			return &ValidationError{Reason: "set request missing fields"}
		}
	case USPAgentMethodAdd:
		if strings.TrimSpace(req.ObjectPath) == "" && req.ObjectCode == 0 {
			return &ValidationError{Reason: "add request missing object path"}
		}
	case USPAgentMethodDelete:
		if len(req.Paths) == 0 && len(req.PathCodes) == 0 {
			return &ValidationError{Reason: "delete request missing paths"}
		}
	case USPAgentMethodOperate, USPAgentMethodNotify:
		if strings.TrimSpace(req.ObjectPath) == "" && req.ObjectCode == 0 {
			return &ValidationError{Reason: "request missing object path"}
		}
	case USPAgentMethodUpload, USPAgentMethodDownload:
		if req.Transfer == nil {
			return &ValidationError{Reason: "transfer request missing transfer block"}
		}
	}
	return nil
}

func validateUSPAgentResponse(resp USPAgentResponse) error {
	if resp.Method == 0 {
		return ErrUSPTransportMethodRequired
	}
	for _, path := range resp.Paths {
		if strings.TrimSpace(path) == "" {
			return &ValidationError{Reason: "response contains empty path"}
		}
	}
	for _, code := range resp.PathCodes {
		if code == 0 {
			return &ValidationError{Reason: "response contains zero path code"}
		}
	}
	if len(resp.PathInstances) > 0 && len(resp.PathInstances) != len(resp.PathCodes) {
		return &ValidationError{Reason: "response path selector metadata length mismatch"}
	}
	if strings.TrimSpace(resp.ObjectPath) != "" && !isObjectPath(resp.ObjectPath) {
		return &ValidationError{Path: resp.ObjectPath, Reason: "response object path must end with '.'"}
	}
	if resp.ObjectCode == 0 && len(resp.ObjectInstances) > 0 {
		return &ValidationError{Reason: "response object selector instances without object code"}
	}
	// Note: resp.Message is NOT validated here. The controller trusts the
	// agent's wire data — schema validation is a sender-side concern. Strict
	// validation here breaks inbound responses when the agent's data model
	// has minor type mismatches (e.g. string instead of unsignedInt for a
	// counter field), which would cause SyncDeviceState to fail entirely.
	return nil
}

func encodeNestedValidatedMessage(msg *Message) ([]byte, error) {
	return encodeMessageValidated(msg, shouldCompressNestedMessage(msg))
}

func shouldCompressNestedMessage(msg *Message) bool {
	if msg == nil {
		return false
	}
	if len(msg.Fields) >= 8 {
		return true
	}
	size := len(msg.DeviceID) + 16
	for _, field := range msg.Fields {
		size += len(field.Path) + estimateValueEncodedSize(field.Val) + 3
		if size > 512 {
			return true
		}
	}
	return false
}

type uspTransportBuffer struct {
	buf []byte
}

func newUSPTransportBuffer(capacity int) uspTransportBuffer {
	if capacity < 16 {
		capacity = 16
	}
	return uspTransportBuffer{buf: make([]byte, 0, capacity)}
}

func (b *uspTransportBuffer) bytes() []byte {
	return b.buf
}

func (b *uspTransportBuffer) writeHeader(header uspTransportHeader) {
	b.buf = append(b.buf, header.Version, header.Kind, byte(header.Method), header.Flags)
	var id [8]byte
	binary.LittleEndian.PutUint64(id[:], header.ID)
	b.buf = append(b.buf, id[:]...)
}

func (b *uspTransportBuffer) writeUvarint(value uint64) {
	var tmp [10]byte
	n := binary.PutUvarint(tmp[:], value)
	b.buf = append(b.buf, tmp[:n]...)
}

func (b *uspTransportBuffer) writeString(value string) {
	b.writeUvarint(uint64(len(value)))
	b.buf = append(b.buf, value...)
}

func (b *uspTransportBuffer) writeBytes(value []byte) {
	b.writeUvarint(uint64(len(value)))
	b.buf = append(b.buf, value...)
}

func (b *uspTransportBuffer) writeStringList(values []string) {
	b.writeUvarint(uint64(len(values)))
	for _, value := range values {
		b.writeString(value)
	}
}

func (b *uspTransportBuffer) writeUint64List(values []uint64) {
	b.writeUvarint(uint64(len(values)))
	for _, value := range values {
		b.writeUvarint(value)
	}
}

func (b *uspTransportBuffer) writePathSelectors(pathCodes []uint64, pathInstances [][]uint64) {
	b.writeUvarint(uint64(len(pathCodes)))
	for i, code := range pathCodes {
		b.writeUvarint(code)
		var instances []uint64
		if len(pathInstances) > i {
			instances = pathInstances[i]
		}
		b.writeUint64List(instances)
	}
}

func (b *uspTransportBuffer) writePathSelector(code uint64, instances []uint64) {
	b.writeUvarint(code)
	b.writeUint64List(instances)
}

func (b *uspTransportBuffer) writeTransferRequest(req USPTransferRequest) {
	b.writeString(req.Path)
	b.writeString(req.URI)
	b.writeString(req.Filename)
	b.writeString(req.ContentType)
	b.writeBytes(req.Payload)
	b.writeMetadata(req.Metadata)
}

func (b *uspTransportBuffer) writeTransferResult(result USPTransferResult) {
	b.writeString(result.Path)
	b.writeString(result.URI)
	b.writeUvarint(uint64(result.Bytes))
	b.writeMetadata(result.Metadata)
}

func (b *uspTransportBuffer) writeMetadata(metadata map[string]string) {
	if len(metadata) == 0 {
		b.writeUvarint(0)
		return
	}
	if len(metadata) == 1 {
		b.writeUvarint(1)
		for key, value := range metadata {
			b.writeString(key)
			b.writeString(value)
		}
		return
	}
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	b.writeUvarint(uint64(len(keys)))
	for _, key := range keys {
		b.writeString(key)
		b.writeString(metadata[key])
	}
}

func (b *uspTransportBuffer) writeBool(value bool) {
	if value {
		b.writeUvarint(1)
		return
	}
	b.writeUvarint(0)
}

func (b *uspTransportBuffer) writeBroadbandDataModel(model BroadbandDataModel) {
	b.writeString(model.ID)
	b.writeString(model.Name)
	b.writeString(model.ModelVersion)
	b.writeString(model.FirstVersion)
	b.writeString(model.LatestVersion)
	b.writeString(model.Source)
	b.writeString(model.SourceURL)
	b.writeUvarint(uint64(model.ObjectCount))
	b.writeUvarint(uint64(model.ParamCount))
}

func (b *uspTransportBuffer) writeSupportedDataModel(model USPSupportedDataModel) {
	b.writeString(model.RootDataModelVersion)
	b.writeUvarint(uint64(len(model.Models)))
	for _, item := range model.Models {
		b.writeBroadbandDataModel(item)
	}
	b.writeUvarint(uint64(len(model.Objects)))
	for _, object := range model.Objects {
		b.writeString(object.Path)
		b.writeBool(object.MultiInstance)
		b.writeString(object.SinceVersion)
	}
	b.writeUvarint(uint64(len(model.Params)))
	for _, param := range model.Params {
		b.writeString(param.Path)
		b.writeString(string(param.Type))
		b.writeString(string(param.Access))
		b.writeString(param.SinceVersion)
	}
}

func (b *uspTransportBuffer) writeProtocolInfo(info USPProtocolInfo) {
	b.writeString(info.Name)
	b.writeUvarint(info.Version)
	b.writeStringList(info.Methods)
	b.writeStringList(info.Compression)
	b.writeString(info.ControlTransport)
	b.writeString(info.TransferTransport)
	b.writeUvarint(info.MaxControlPayload)
	b.writeUvarint(info.RecommendedChunkSize)
	b.writeBool(info.TunnelOnly)
	b.writeBool(info.ReliableTransfer)
}

type uspTransportReader struct {
	data []byte
	pos  int
}

func newUSPTransportReader(data []byte) *uspTransportReader {
	return &uspTransportReader{data: data}
}

func (r *uspTransportReader) done() bool {
	return r.pos == len(r.data)
}

func (r *uspTransportReader) remainingAllZero() bool {
	for _, b := range r.data[r.pos:] {
		if b != 0 {
			return false
		}
	}
	r.pos = len(r.data)
	return true
}

func (r *uspTransportReader) readHeader() (uspTransportHeader, error) {
	if len(r.data) < 12 {
		return uspTransportHeader{}, fmt.Errorf("%w: payload too short", ErrUSPTransportMalformed)
	}
	header := uspTransportHeader{
		Version: r.data[0],
		Kind:    r.data[1],
		Method:  USPAgentMethod(r.data[2]),
		Flags:   r.data[3],
		ID:      binary.LittleEndian.Uint64(r.data[4:12]),
	}
	if header.Version != uspTransportVersion {
		return uspTransportHeader{}, fmt.Errorf("%w: version %d", ErrUSPTransportUnsupported, header.Version)
	}
	r.pos = 12
	return header, nil
}

func (r *uspTransportReader) readUvarint() (uint64, error) {
	value, n := binary.Uvarint(r.data[r.pos:])
	if n <= 0 {
		return 0, fmt.Errorf("%w: bad varint", ErrUSPTransportMalformed)
	}
	r.pos += n
	return value, nil
}

func (r *uspTransportReader) readString() (string, error) {
	data, err := r.readBytes()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (r *uspTransportReader) readBytes() ([]byte, error) {
	length, err := r.readUvarint()
	if err != nil {
		return nil, err
	}
	if length > uint64(len(r.data)-r.pos) {
		return nil, fmt.Errorf("%w: short blob", ErrUSPTransportMalformed)
	}
	value := r.data[r.pos : r.pos+int(length)]
	r.pos += int(length)
	return value, nil
}

func (r *uspTransportReader) readStringList() ([]string, error) {
	count, err := r.readUvarint()
	if err != nil {
		return nil, err
	}
	values := make([]string, 0, count)
	for i := uint64(0); i < count; i++ {
		value, err := r.readString()
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func (r *uspTransportReader) readUint64List() ([]uint64, error) {
	count, err := r.readUvarint()
	if err != nil {
		return nil, err
	}
	values := make([]uint64, 0, count)
	for i := uint64(0); i < count; i++ {
		value, err := r.readUvarint()
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func (r *uspTransportReader) readPathSelectors() ([]uint64, [][]uint64, error) {
	count, err := r.readUvarint()
	if err != nil {
		return nil, nil, err
	}
	pathCodes := make([]uint64, 0, count)
	pathInstances := make([][]uint64, 0, count)
	for i := uint64(0); i < count; i++ {
		code, err := r.readUvarint()
		if err != nil {
			return nil, nil, err
		}
		instances, err := r.readUint64List()
		if err != nil {
			return nil, nil, err
		}
		pathCodes = append(pathCodes, code)
		pathInstances = append(pathInstances, instances)
	}
	return pathCodes, pathInstances, nil
}

func (r *uspTransportReader) readPathSelector() (uint64, []uint64, error) {
	code, err := r.readUvarint()
	if err != nil {
		return 0, nil, err
	}
	instances, err := r.readUint64List()
	if err != nil {
		return 0, nil, err
	}
	return code, instances, nil
}

func (r *uspTransportReader) readTransferRequest() (USPTransferRequest, error) {
	var req USPTransferRequest
	var err error
	if req.Path, err = r.readString(); err != nil {
		return USPTransferRequest{}, err
	}
	if req.URI, err = r.readString(); err != nil {
		return USPTransferRequest{}, err
	}
	if req.Filename, err = r.readString(); err != nil {
		return USPTransferRequest{}, err
	}
	if req.ContentType, err = r.readString(); err != nil {
		return USPTransferRequest{}, err
	}
	if req.Payload, err = r.readBytes(); err != nil {
		return USPTransferRequest{}, err
	}
	if req.Metadata, err = r.readMetadata(); err != nil {
		return USPTransferRequest{}, err
	}
	return req, nil
}

func (r *uspTransportReader) readTransferResult() (USPTransferResult, error) {
	var result USPTransferResult
	var err error
	if result.Path, err = r.readString(); err != nil {
		return USPTransferResult{}, err
	}
	if result.URI, err = r.readString(); err != nil {
		return USPTransferResult{}, err
	}
	if result.Bytes, err = r.readInt64AsUvarint(); err != nil {
		return USPTransferResult{}, err
	}
	if result.Metadata, err = r.readMetadata(); err != nil {
		return USPTransferResult{}, err
	}
	return result, nil
}

func (r *uspTransportReader) readInt64AsUvarint() (int64, error) {
	value, err := r.readUvarint()
	if err != nil {
		return 0, err
	}
	if value > uint64(^uint64(0)>>1) {
		return 0, io.ErrUnexpectedEOF
	}
	return int64(value), nil
}

func estimateUSPAgentRequestSize(req USPAgentRequest, nestedFrame []byte) int {
	size := 12 + estimateStringListSize(req.Paths)
	if req.ObjectPath != "" {
		size += estimateStringSize(req.ObjectPath)
	}
	if len(req.PathCodes) > 0 {
		size += estimatePathSelectorsSize(req.PathCodes, req.PathInstances)
	}
	if req.ObjectCode != 0 {
		size += estimatePathSelectorSize(req.ObjectCode, req.ObjectInstances)
	}
	if len(nestedFrame) > 0 {
		size += estimateBlobSize(nestedFrame)
	}
	if req.Transfer != nil {
		size += estimateTransferRequestSize(*req.Transfer)
	}
	size += estimateMetadataSize(req.Metadata)
	return size
}

func estimateUSPAgentResponseSize(resp USPAgentResponse, nestedFrame []byte) int {
	size := 12 + estimateStringSize(resp.Error)
	if len(resp.Paths) > 0 {
		size += estimateStringListSize(resp.Paths)
	}
	if resp.ObjectPath != "" {
		size += estimateStringSize(resp.ObjectPath)
	}
	if len(nestedFrame) > 0 {
		size += estimateBlobSize(nestedFrame)
	}
	if resp.Transfer != nil {
		size += estimateTransferResultSize(*resp.Transfer)
	}
	size += estimateMetadataSize(resp.Metadata)
	size += estimatePathSelectorsSize(resp.PathCodes, resp.PathInstances)
	size += estimatePathSelectorSize(resp.ObjectCode, resp.ObjectInstances)
	if resp.SupportedDataModel != nil {
		size += estimateSupportedDataModelSize(*resp.SupportedDataModel)
	}
	if resp.Protocol != nil {
		size += estimateProtocolInfoSize(*resp.Protocol)
	}
	return size
}

func estimateTransferRequestSize(req USPTransferRequest) int {
	return estimateStringSize(req.Path) +
		estimateStringSize(req.URI) +
		estimateStringSize(req.Filename) +
		estimateStringSize(req.ContentType) +
		estimateBlobSize(req.Payload) +
		estimateMetadataSize(req.Metadata)
}

func estimateTransferResultSize(result USPTransferResult) int {
	return estimateStringSize(result.Path) +
		estimateStringSize(result.URI) +
		estimateUvarintSize(uint64(result.Bytes)) +
		estimateMetadataSize(result.Metadata)
}

func estimateStringListSize(values []string) int {
	size := estimateUvarintSize(uint64(len(values)))
	for _, value := range values {
		size += estimateStringSize(value)
	}
	return size
}

func estimateUint64ListSize(values []uint64) int {
	size := estimateUvarintSize(uint64(len(values)))
	for _, value := range values {
		size += estimateUvarintSize(value)
	}
	return size
}

func estimatePathSelectorsSize(pathCodes []uint64, pathInstances [][]uint64) int {
	if len(pathCodes) == 0 {
		return 0
	}
	size := estimateUvarintSize(uint64(len(pathCodes)))
	for i, code := range pathCodes {
		size += estimateUvarintSize(code)
		if len(pathInstances) > i {
			size += estimateUint64ListSize(pathInstances[i])
			continue
		}
		size += estimateUvarintSize(0)
	}
	return size
}

func estimatePathSelectorSize(code uint64, instances []uint64) int {
	if code == 0 && len(instances) == 0 {
		return 0
	}
	return estimateUvarintSize(code) + estimateUint64ListSize(instances)
}

func estimateMetadataSize(metadata map[string]string) int {
	size := estimateUvarintSize(uint64(len(metadata)))
	for key, value := range metadata {
		size += estimateStringSize(key) + estimateStringSize(value)
	}
	return size
}

func estimateSupportedDataModelSize(model USPSupportedDataModel) int {
	size := estimateStringSize(model.RootDataModelVersion) + estimateUvarintSize(uint64(len(model.Models)))
	for _, item := range model.Models {
		size += estimateBroadbandDataModelSize(item)
	}
	size += estimateUvarintSize(uint64(len(model.Objects)))
	for _, object := range model.Objects {
		size += estimateStringSize(object.Path) + 1 + estimateStringSize(object.SinceVersion)
	}
	size += estimateUvarintSize(uint64(len(model.Params)))
	for _, param := range model.Params {
		size += estimateStringSize(param.Path) +
			estimateStringSize(string(param.Type)) +
			estimateStringSize(string(param.Access)) +
			estimateStringSize(param.SinceVersion)
	}
	return size
}

func estimateBroadbandDataModelSize(model BroadbandDataModel) int {
	return estimateStringSize(model.ID) +
		estimateStringSize(model.Name) +
		estimateStringSize(model.ModelVersion) +
		estimateStringSize(model.FirstVersion) +
		estimateStringSize(model.LatestVersion) +
		estimateStringSize(model.Source) +
		estimateStringSize(model.SourceURL) +
		estimateUvarintSize(uint64(model.ObjectCount)) +
		estimateUvarintSize(uint64(model.ParamCount))
}

func estimateProtocolInfoSize(info USPProtocolInfo) int {
	return estimateStringSize(info.Name) +
		estimateUvarintSize(info.Version) +
		estimateStringListSize(info.Methods) +
		estimateStringListSize(info.Compression) +
		estimateStringSize(info.ControlTransport) +
		estimateStringSize(info.TransferTransport) +
		estimateUvarintSize(info.MaxControlPayload) +
		estimateUvarintSize(info.RecommendedChunkSize) +
		2
}

func estimateStringSize(value string) int {
	return estimateUvarintSize(uint64(len(value))) + len(value)
}

func estimateBlobSize(value []byte) int {
	return estimateUvarintSize(uint64(len(value))) + len(value)
}

func estimateUvarintSize(value uint64) int {
	size := 1
	for value >= 0x80 {
		value >>= 7
		size++
	}
	return size
}

func estimateValueEncodedSize(value Value) int {
	size := 1
	switch value.Tag {
	case TagNull, TagFalse, TagTrue:
		return size
	case TagUint:
		return size + estimateUvarintSize(uint64(value.ival))
	case TagInt, TagTime:
		zigzag := uint64((value.ival << 1) ^ (value.ival >> 63))
		return size + estimateUvarintSize(zigzag)
	case TagFloat:
		return size + 8
	case TagString, TagBytes:
		return size + estimateBlobSize(value.blob)
	case TagIP4:
		return size + 4
	case TagIP6:
		return size + 16
	case TagIP4Pfx:
		return size + 5
	case TagIP6Pfx:
		return size + 17
	case TagMAC:
		return size + 6
	case TagList:
		size += estimateUvarintSize(uint64(len(value.list)))
		for _, item := range value.list {
			size += estimateValueEncodedSize(item)
		}
		return size
	default:
		return size
	}
}

func (r *uspTransportReader) readMetadata() (map[string]string, error) {
	count, err := r.readUvarint()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	metadata := make(map[string]string, count)
	for i := uint64(0); i < count; i++ {
		key, err := r.readString()
		if err != nil {
			return nil, err
		}
		value, err := r.readString()
		if err != nil {
			return nil, err
		}
		metadata[key] = value
	}
	return metadata, nil
}

func (r *uspTransportReader) readBool() (bool, error) {
	value, err := r.readUvarint()
	if err != nil {
		return false, err
	}
	return value != 0, nil
}

func (r *uspTransportReader) readBroadbandDataModel() (BroadbandDataModel, error) {
	var model BroadbandDataModel
	var err error
	if model.ID, err = r.readString(); err != nil {
		return BroadbandDataModel{}, err
	}
	if model.Name, err = r.readString(); err != nil {
		return BroadbandDataModel{}, err
	}
	if model.ModelVersion, err = r.readString(); err != nil {
		return BroadbandDataModel{}, err
	}
	if model.FirstVersion, err = r.readString(); err != nil {
		return BroadbandDataModel{}, err
	}
	if model.LatestVersion, err = r.readString(); err != nil {
		return BroadbandDataModel{}, err
	}
	if model.Source, err = r.readString(); err != nil {
		return BroadbandDataModel{}, err
	}
	if model.SourceURL, err = r.readString(); err != nil {
		return BroadbandDataModel{}, err
	}
	objectCount, err := r.readUvarint()
	if err != nil {
		return BroadbandDataModel{}, err
	}
	paramCount, err := r.readUvarint()
	if err != nil {
		return BroadbandDataModel{}, err
	}
	model.ObjectCount = int(objectCount)
	model.ParamCount = int(paramCount)
	return model, nil
}

func (r *uspTransportReader) readSupportedDataModel() (*USPSupportedDataModel, error) {
	model := &USPSupportedDataModel{}
	var err error
	if model.RootDataModelVersion, err = r.readString(); err != nil {
		return nil, err
	}
	modelCount, err := r.readUvarint()
	if err != nil {
		return nil, err
	}
	model.Models = make([]BroadbandDataModel, 0, modelCount)
	for i := uint64(0); i < modelCount; i++ {
		item, err := r.readBroadbandDataModel()
		if err != nil {
			return nil, err
		}
		model.Models = append(model.Models, item)
	}
	objectCount, err := r.readUvarint()
	if err != nil {
		return nil, err
	}
	model.Objects = make([]USPSupportedObject, 0, objectCount)
	for i := uint64(0); i < objectCount; i++ {
		path, err := r.readString()
		if err != nil {
			return nil, err
		}
		multi, err := r.readBool()
		if err != nil {
			return nil, err
		}
		sinceVersion, err := r.readString()
		if err != nil {
			return nil, err
		}
		model.Objects = append(model.Objects, USPSupportedObject{
			Path:          path,
			MultiInstance: multi,
			SinceVersion:  sinceVersion,
		})
	}
	paramCount, err := r.readUvarint()
	if err != nil {
		return nil, err
	}
	model.Params = make([]USPSupportedParam, 0, paramCount)
	for i := uint64(0); i < paramCount; i++ {
		path, err := r.readString()
		if err != nil {
			return nil, err
		}
		paramType, err := r.readString()
		if err != nil {
			return nil, err
		}
		access, err := r.readString()
		if err != nil {
			return nil, err
		}
		sinceVersion, err := r.readString()
		if err != nil {
			return nil, err
		}
		model.Params = append(model.Params, USPSupportedParam{
			Path:         path,
			Type:         ParamType(paramType),
			Access:       Access(access),
			SinceVersion: sinceVersion,
		})
	}
	return model, nil
}

func (r *uspTransportReader) readProtocolInfo() (*USPProtocolInfo, error) {
	info := &USPProtocolInfo{}
	var err error
	if info.Name, err = r.readString(); err != nil {
		return nil, err
	}
	if info.Version, err = r.readUvarint(); err != nil {
		return nil, err
	}
	if info.Methods, err = r.readStringList(); err != nil {
		return nil, err
	}
	if info.Compression, err = r.readStringList(); err != nil {
		return nil, err
	}
	if info.ControlTransport, err = r.readString(); err != nil {
		return nil, err
	}
	if info.TransferTransport, err = r.readString(); err != nil {
		return nil, err
	}
	if info.MaxControlPayload, err = r.readUvarint(); err != nil {
		return nil, err
	}
	if info.RecommendedChunkSize, err = r.readUvarint(); err != nil {
		return nil, err
	}
	if info.TunnelOnly, err = r.readBool(); err != nil {
		return nil, err
	}
	if info.ReliableTransfer, err = r.readBool(); err != nil {
		return nil, err
	}
	return info, nil
}

func resolveFastRequestPaths(req USPAgentRequest) USPAgentRequest {
	model := runtimeDeviceFast()
	if model == nil {
		return req
	}
	if len(req.Paths) == 0 && len(req.PathCodes) > 0 {
		req.Paths = make([]string, 0, len(req.PathCodes))
		for i, code := range req.PathCodes {
			selector := PathSelector{Code: code}
			if len(req.PathInstances) > i {
				selector.Instances = req.PathInstances[i]
			}
			if path, ok := model.PathForSelector(selector); ok {
				req.Paths = append(req.Paths, path)
			}
		}
	}
	if req.ObjectPath == "" && req.ObjectCode != 0 {
		if path, ok := model.PathForSelector(PathSelector{Code: req.ObjectCode, Instances: req.ObjectInstances}); ok {
			req.ObjectPath = path
		}
	}
	return req
}

func resolveFastResponsePaths(resp USPAgentResponse) USPAgentResponse {
	model := runtimeDeviceFast()
	if model == nil {
		return resp
	}
	if len(resp.Paths) == 0 && len(resp.PathCodes) > 0 {
		resp.Paths = make([]string, 0, len(resp.PathCodes))
		for i, code := range resp.PathCodes {
			selector := PathSelector{Code: code}
			if len(resp.PathInstances) > i {
				selector.Instances = resp.PathInstances[i]
			}
			if path, ok := model.PathForSelector(selector); ok {
				resp.Paths = append(resp.Paths, path)
			}
		}
	}
	if resp.ObjectPath == "" && resp.ObjectCode != 0 {
		if path, ok := model.PathForSelector(PathSelector{Code: resp.ObjectCode, Instances: resp.ObjectInstances}); ok {
			resp.ObjectPath = path
		}
	}
	return resp
}

func compactRequestSelectors(req USPAgentRequest) USPAgentRequest {
	model := runtimeDeviceFast()
	if model == nil {
		return req
	}
	if len(req.PathCodes) == 0 && len(req.Paths) > 0 {
		if codes, instances, ok := strictResolvedPathSelectors(model, req.Paths); ok {
			req.PathCodes = codes
			req.PathInstances = instances
			req.Paths = nil
		}
	}
	if req.ObjectCode == 0 && strings.TrimSpace(req.ObjectPath) != "" {
		if code, instances, ok := strictResolvedObjectCode(model, req.ObjectPath); ok {
			req.ObjectCode = code
			req.ObjectInstances = instances
			req.ObjectPath = ""
		}
	}
	return req
}

func compactResponseSelectors(resp USPAgentResponse) USPAgentResponse {
	model := runtimeDeviceFast()
	if model == nil {
		return resp
	}
	if len(resp.PathCodes) == 0 && len(resp.Paths) > 0 {
		if codes, instances, ok := strictResolvedPathSelectors(model, resp.Paths); ok {
			resp.PathCodes = codes
			resp.PathInstances = instances
			resp.Paths = nil
		}
	}
	if resp.ObjectCode == 0 && strings.TrimSpace(resp.ObjectPath) != "" {
		if code, instances, ok := strictResolvedObjectCode(model, resp.ObjectPath); ok {
			resp.ObjectCode = code
			resp.ObjectInstances = instances
			resp.ObjectPath = ""
		}
	}
	return resp
}

func strictResolvedPathSelectors(model *Device, paths []string) ([]uint64, [][]uint64, bool) {
	if model == nil || len(paths) == 0 {
		return nil, nil, false
	}
	codes := make([]uint64, 0, len(paths))
	instances := make([][]uint64, 0, len(paths))
	for _, path := range paths {
		selector, ok := model.SelectorForPath(path)
		if !ok || selector.Code == 0 {
			return nil, nil, false
		}
		codes = append(codes, selector.Code)
		instances = append(instances, append([]uint64(nil), selector.Instances...))
	}
	return codes, instances, true
}

func strictResolvedObjectCode(model *Device, path string) (uint64, []uint64, bool) {
	if model == nil {
		return 0, nil, false
	}
	selector, ok := model.SelectorForPath(path)
	if !ok || selector.Code == 0 {
		return 0, nil, false
	}
	return selector.Code, append([]uint64(nil), selector.Instances...), true
}
