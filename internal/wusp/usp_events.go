package wusp

import (
	"context"
	"strconv"
	"strings"
)

// USPEventType classifies an agent-originated notification.
// Values mirror the Notify.notification oneof in TR-369 usp-msg-1-4.proto
// without using Protocol Buffers — WUSP carries the same semantics over the
// Wantastic WireGuard tunnel using the existing binary codec.
type USPEventType uint8

const (
	// USPEventTypeEvent is a custom named event with arbitrary string params.
	// Equivalent to Notify.Event in TR-369.
	USPEventTypeEvent USPEventType = 1

	// USPEventTypeValueChange notifies the controller that a parameter value
	// changed. Equivalent to Notify.ValueChange in TR-369.
	USPEventTypeValueChange USPEventType = 2

	// USPEventTypeObjectCreation notifies the controller that a new
	// multi-instance object was instantiated. Equivalent to
	// Notify.ObjectCreation in TR-369.
	USPEventTypeObjectCreation USPEventType = 3

	// USPEventTypeObjectDeletion notifies the controller that an object
	// instance was removed. Equivalent to Notify.ObjectDeletion in TR-369.
	USPEventTypeObjectDeletion USPEventType = 4

	// USPEventTypeOperationComplete reports the outcome of an asynchronous
	// Operate request. Equivalent to Notify.OperationComplete in TR-369.
	USPEventTypeOperationComplete USPEventType = 5

	// USPEventTypeOnBoardRequest is sent by the agent during initial
	// onboarding to announce its identity to the controller. Equivalent to
	// Notify.OnBoardRequest in TR-369.
	USPEventTypeOnBoardRequest USPEventType = 6
)

// USPEvent is an agent-originated notification delivered to the controller.
//
// On the agent side, construct events with the USPAgent.Emit* helpers. On the
// controller side, call DecodeEventFromRequest after receiving an inbound
// USPAgentRequest with Method == USPAgentMethodNotify.
//
// Wire encoding: USPEvent is carried as a USPAgentRequest (Method=Notify) with
// type-specific fields packed into Metadata and a flat Message for param maps.
// The encoding/decoding layer is not touched — only this decoded representation
// travels through application code.
type USPEvent struct {
	// Type identifies the notification category.
	Type USPEventType

	// SubscriptionID ties the event to a subscription previously registered
	// via USPAgent.Subscribe or the wire subscription protocol.
	// Empty for spontaneous agent-initiated events with no matching subscription.
	SubscriptionID string

	// ObjPath is the data-model object or parameter path the event originates
	// from.
	//   - USPEventTypeEvent:            event source object path (ends with ".")
	//   - USPEventTypeValueChange:      the changed parameter path
	//   - USPEventTypeObjectCreation:   the newly instantiated object path
	//   - USPEventTypeObjectDeletion:   the deleted object path
	//   - USPEventTypeOperationComplete: the command object path (ends with ".")
	//   - USPEventTypeOnBoardRequest:   empty (agent-level event)
	ObjPath string

	// EventName is the event or command name within ObjPath.
	//   - USPEventTypeEvent:            the event name (e.g. "Boot!")
	//   - USPEventTypeOperationComplete: the command name (e.g. "Reboot()")
	//   - other types:                  unused
	EventName string

	// CommandKey is the controller-assigned key from the original Operate
	// request. Set only for USPEventTypeOperationComplete.
	CommandKey string

	// Params carries type-specific string key/value data:
	//   - USPEventTypeEvent:            arbitrary event arguments
	//   - USPEventTypeObjectCreation:   unique key name → value pairs
	//   - USPEventTypeOperationComplete: output argument name → value pairs
	//   - other types:                  nil
	Params map[string]string

	// ParamValue is the new value of ObjPath. Set only for
	// USPEventTypeValueChange.
	ParamValue string

	// Err carries the failure detail for USPEventTypeOperationComplete when
	// the operation failed. Nil means the operation succeeded.
	Err *USPEventError

	// OnBoard is set for USPEventTypeOnBoardRequest and carries the agent
	// identity presented during the onboarding exchange.
	OnBoard *USPOnBoardInfo
}

// USPEventError is a structured failure payload for OperationComplete events.
type USPEventError struct {
	// Code is the USP error code (TR-369 Table 2).
	Code uint32
	// Message is a human-readable description of the failure.
	Message string
}

// USPOnBoardInfo carries agent identity in an OnBoardRequest event.
type USPOnBoardInfo struct {
	OUI                            string
	ProductClass                   string
	SerialNumber                   string
	Manufacturer                   string
	SoftwareVersion                string
	AgentSupportedProtocolVersions string
}

// USPSubscription is a controller-registered interest in agent events.
// Register with USPAgent.Subscribe; remove with USPAgent.Unsubscribe.
//
// Wire-based subscription (controller sends a Notify request with subscription
// metadata) uses the same struct — call DecodeSubscriptionFromRequest to parse
// the inbound request and then pass the result to USPAgent.Subscribe.
type USPSubscription struct {
	// ID is the unique controller-assigned subscription identifier. Required.
	// The agent echoes this ID in every matching outbound notification.
	ID string

	// Types is the set of event types this subscription accepts.
	// An empty or nil slice means all event types are accepted.
	Types []USPEventType

	// PathFilter restricts notifications to events whose ObjPath starts with
	// at least one of the listed prefixes.
	// An empty or nil slice means no path restriction (all paths match).
	PathFilter []string

	// EventFilter restricts USPEventTypeEvent notifications to specific event
	// names. Ignored for all other event types.
	// An empty or nil slice means all event names match.
	EventFilter []string

	// Handler is called in-process for every matching event. Optional.
	// When nil, the subscription still participates in wire-based delivery
	// via USPAgent's configured USPEventSender.
	Handler USPEventHandler
}

// USPEventHandler processes a single agent-originated event.
//
// On the agent side, install per-subscription handlers in USPSubscription.Handler.
// On the controller side, install a global handler via USPAgentOptions.EventHandler
// or call DecodeEventFromRequest in your receive loop.
type USPEventHandler func(ctx context.Context, event USPEvent) error

// USPEventSender delivers encoded WUSP Notify payloads from the agent to the
// controller over the WireGuard tunnel. Implement this on the transport layer.
//
// data is the output of EncodeUSPAgentRequest. The implementation is
// responsible for fragmenting it with FragmentUSPControlPayload before writing
// to the tunnel, since it owns the MTU constraints.
type USPEventSender interface {
	SendUSPNotify(ctx context.Context, data []byte) error
}

// Stable metadata key names used in USPAgentRequest.Metadata for event and
// subscription exchange. Do not change these — they are part of the wire protocol.
const (
	// Event payload keys
	eventMetaType       = "event_type"
	eventMetaSubID      = "subscription_id"
	eventMetaName       = "event_name"
	eventMetaCmdKey     = "command_key"
	eventMetaParamValue = "param_value"
	eventMetaErrCode    = "error_code"
	eventMetaErrMsg     = "error_message"
	eventMetaOUI        = "oui"
	eventMetaPC         = "product_class"
	eventMetaSN         = "serial_number"
	eventMetaMfg        = "manufacturer"
	eventMetaSWVer      = "software_version"
	eventMetaPV         = "protocol_versions"

	// Wire subscription management keys (sent from controller → agent via Notify)
	subMetaAction   = "wusp_action"
	subMetaID       = "wusp_sub_id"
	subMetaTypes    = "wusp_sub_types"
	subMetaPaths    = "wusp_sub_paths"
	subMetaEvents   = "wusp_sub_events"
	subMetaStatus   = "wusp_sub_status"
	subActionAdd    = "subscribe"
	subActionRemove = "unsubscribe"
)

// EncodeEventToRequest builds a USPAgentRequest carrying the given event.
// id must be a non-zero unique value for request/response correlation.
//
// Call EncodeUSPAgentRequest on the result, then FragmentUSPControlPayload,
// and send all fragments over Message Type 8 on the WireGuard tunnel.
func EncodeEventToRequest(event USPEvent, id uint64) USPAgentRequest {
	meta := make(map[string]string, 6)
	meta[eventMetaType] = strconv.Itoa(int(event.Type))
	if event.SubscriptionID != "" {
		meta[eventMetaSubID] = event.SubscriptionID
	}

	var msg *Message

	switch event.Type {
	case USPEventTypeEvent:
		if event.EventName != "" {
			meta[eventMetaName] = event.EventName
		}
		msg = eventParamsToMessage(event.Params)

	case USPEventTypeValueChange:
		meta[eventMetaParamValue] = event.ParamValue

	case USPEventTypeObjectCreation:
		msg = eventParamsToMessage(event.Params) // unique keys as flat fields

	case USPEventTypeObjectDeletion:
		// ObjPath is sufficient — no extra payload

	case USPEventTypeOperationComplete:
		if event.EventName != "" {
			meta[eventMetaName] = event.EventName
		}
		if event.CommandKey != "" {
			meta[eventMetaCmdKey] = event.CommandKey
		}
		if event.Err != nil {
			meta[eventMetaErrCode] = strconv.FormatUint(uint64(event.Err.Code), 10)
			if event.Err.Message != "" {
				meta[eventMetaErrMsg] = event.Err.Message
			}
		} else {
			msg = eventParamsToMessage(event.Params) // output args
		}

	case USPEventTypeOnBoardRequest:
		if ob := event.OnBoard; ob != nil {
			if ob.OUI != "" {
				meta[eventMetaOUI] = ob.OUI
			}
			if ob.ProductClass != "" {
				meta[eventMetaPC] = ob.ProductClass
			}
			if ob.SerialNumber != "" {
				meta[eventMetaSN] = ob.SerialNumber
			}
			if ob.Manufacturer != "" {
				meta[eventMetaMfg] = ob.Manufacturer
			}
			if ob.SoftwareVersion != "" {
				meta[eventMetaSWVer] = ob.SoftwareVersion
			}
			if ob.AgentSupportedProtocolVersions != "" {
				meta[eventMetaPV] = ob.AgentSupportedProtocolVersions
			}
		}
	}

	objPath := strings.TrimSpace(event.ObjPath)
	if isObjectPath(objPath) {
		return USPAgentRequest{
			ID:         id,
			Method:     USPAgentMethodNotify,
			ObjectPath: objPath,
			Message:    msg,
			Metadata:   meta,
		}
	}
	// Non-object paths (parameters, empty) go in Paths
	paths := []string{}
	if objPath != "" {
		paths = []string{objPath}
	}
	return USPAgentRequest{
		ID:       id,
		Method:   USPAgentMethodNotify,
		Paths:    paths,
		Message:  msg,
		Metadata: meta,
	}
}

// DecodeEventFromRequest extracts a USPEvent from an inbound USPAgentRequest.
//
// Call this on the controller side when it receives a Notify request (Method ==
// USPAgentMethodNotify) that passes IsEventNotifyRequest. Returns
// ErrUSPTransportMalformed if the metadata is missing or the event type is
// unknown.
func DecodeEventFromRequest(req USPAgentRequest) (USPEvent, error) {
	if req.Method != USPAgentMethodNotify {
		return USPEvent{}, ErrUSPTransportMalformed
	}
	typeStr, ok := req.Metadata[eventMetaType]
	if !ok {
		return USPEvent{}, ErrUSPTransportMalformed
	}
	typeVal, err := strconv.Atoi(typeStr)
	if err != nil || typeVal < 1 || typeVal > 6 {
		return USPEvent{}, ErrUSPTransportMalformed
	}

	objPath := req.ObjectPath
	if objPath == "" && len(req.Paths) > 0 {
		objPath = req.Paths[0]
	}

	event := USPEvent{
		Type:           USPEventType(typeVal),
		SubscriptionID: req.Metadata[eventMetaSubID],
		ObjPath:        objPath,
	}

	switch event.Type {
	case USPEventTypeEvent:
		event.EventName = req.Metadata[eventMetaName]
		event.Params = eventMessageToParams(req.Message)

	case USPEventTypeValueChange:
		event.ParamValue = req.Metadata[eventMetaParamValue]

	case USPEventTypeObjectCreation:
		event.Params = eventMessageToParams(req.Message)

	case USPEventTypeObjectDeletion:
		// nothing extra beyond ObjPath

	case USPEventTypeOperationComplete:
		event.EventName = req.Metadata[eventMetaName]
		event.CommandKey = req.Metadata[eventMetaCmdKey]
		if codeStr, ok := req.Metadata[eventMetaErrCode]; ok {
			code, _ := strconv.ParseUint(codeStr, 10, 32)
			event.Err = &USPEventError{
				Code:    uint32(code),
				Message: req.Metadata[eventMetaErrMsg],
			}
		} else {
			event.Params = eventMessageToParams(req.Message)
		}

	case USPEventTypeOnBoardRequest:
		event.OnBoard = &USPOnBoardInfo{
			OUI:                            req.Metadata[eventMetaOUI],
			ProductClass:                   req.Metadata[eventMetaPC],
			SerialNumber:                   req.Metadata[eventMetaSN],
			Manufacturer:                   req.Metadata[eventMetaMfg],
			SoftwareVersion:                req.Metadata[eventMetaSWVer],
			AgentSupportedProtocolVersions: req.Metadata[eventMetaPV],
		}
	}

	return event, nil
}

// IsEventNotifyRequest reports whether an inbound USPAgentRequest is an
// agent-to-controller event push (as opposed to a subscription management
// request or a controller-originated Notify).
func IsEventNotifyRequest(req USPAgentRequest) bool {
	if req.Method != USPAgentMethodNotify {
		return false
	}
	_, ok := req.Metadata[eventMetaType]
	return ok
}

// IsSubscriptionRequest reports whether an inbound USPAgentRequest is a
// controller subscription management request (subscribe or unsubscribe).
func IsSubscriptionRequest(req USPAgentRequest) bool {
	if req.Method != USPAgentMethodNotify {
		return false
	}
	action := strings.TrimSpace(req.Metadata[subMetaAction])
	return action == subActionAdd || action == subActionRemove
}

// EncodeSubscribeRequest builds the USPAgentRequest a controller sends to the
// agent to register a subscription.
//
//   - id:          unique non-zero request ID
//   - subID:       controller-assigned subscription identifier
//   - types:       event types to accept (nil = all)
//   - pathFilter:  ObjPath prefixes to watch (nil = all)
//   - eventFilter: event names to watch for USPEventTypeEvent (nil = all)
func EncodeSubscribeRequest(id uint64, subID string, types []USPEventType, pathFilter, eventFilter []string) USPAgentRequest {
	meta := map[string]string{
		subMetaAction: subActionAdd,
		subMetaID:     subID,
	}
	if len(types) > 0 {
		parts := make([]string, len(types))
		for i, t := range types {
			parts[i] = strconv.Itoa(int(t))
		}
		meta[subMetaTypes] = strings.Join(parts, ",")
	}
	if len(pathFilter) > 0 {
		meta[subMetaPaths] = strings.Join(pathFilter, ",")
	}
	if len(eventFilter) > 0 {
		meta[subMetaEvents] = strings.Join(eventFilter, ",")
	}
	return USPAgentRequest{
		ID:       id,
		Method:   USPAgentMethodNotify,
		Paths:    []string{},
		Metadata: meta,
	}
}

// EncodeUnsubscribeRequest builds the USPAgentRequest a controller sends to
// cancel a previously registered subscription.
func EncodeUnsubscribeRequest(id uint64, subID string) USPAgentRequest {
	return USPAgentRequest{
		ID:     id,
		Method: USPAgentMethodNotify,
		Paths:  []string{},
		Metadata: map[string]string{
			subMetaAction: subActionRemove,
			subMetaID:     subID,
		},
	}
}

// DecodeSubscriptionFromRequest parses the subscription parameters from an
// inbound subscription management request. Call IsSubscriptionRequest first.
// Returns the action ("subscribe"/"unsubscribe") and the parsed subscription.
func DecodeSubscriptionFromRequest(req USPAgentRequest) (action string, sub USPSubscription) {
	action = strings.TrimSpace(req.Metadata[subMetaAction])
	sub.ID = strings.TrimSpace(req.Metadata[subMetaID])

	if typesStr := req.Metadata[subMetaTypes]; typesStr != "" {
		for _, t := range strings.Split(typesStr, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil && n >= 1 && n <= 6 {
				sub.Types = append(sub.Types, USPEventType(n))
			}
		}
	}
	if pathsStr := req.Metadata[subMetaPaths]; pathsStr != "" {
		for _, p := range strings.Split(pathsStr, ",") {
			if p = strings.TrimSpace(p); p != "" {
				sub.PathFilter = append(sub.PathFilter, p)
			}
		}
	}
	if eventsStr := req.Metadata[subMetaEvents]; eventsStr != "" {
		for _, e := range strings.Split(eventsStr, ",") {
			if e = strings.TrimSpace(e); e != "" {
				sub.EventFilter = append(sub.EventFilter, e)
			}
		}
	}
	return action, sub
}

// subscriptionMatches reports whether a subscription accepts the given event.
func subscriptionMatches(sub USPSubscription, event USPEvent) bool {
	if len(sub.Types) > 0 {
		matched := false
		for _, t := range sub.Types {
			if t == event.Type {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(sub.PathFilter) > 0 {
		matched := false
		for _, prefix := range sub.PathFilter {
			if strings.HasPrefix(event.ObjPath, prefix) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if event.Type == USPEventTypeEvent && len(sub.EventFilter) > 0 {
		matched := false
		for _, name := range sub.EventFilter {
			if name == event.EventName {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// eventParamsToMessage encodes a string→string map as a flat WUSP Message.
// Keys become Field paths; values become string Field values.
func eventParamsToMessage(params map[string]string) *Message {
	if len(params) == 0 {
		return nil
	}
	fields := make([]Field, 0, len(params))
	for k, v := range params {
		fields = append(fields, Field{Path: k, Val: String(v)})
	}
	return &Message{Fields: fields}
}

// eventMessageToParams converts a flat WUSP Message back into a string→string map.
func eventMessageToParams(msg *Message) map[string]string {
	if msg == nil || len(msg.Fields) == 0 {
		return nil
	}
	out := make(map[string]string, len(msg.Fields))
	for _, field := range msg.Fields {
		out[field.Path] = field.Val.AsString()
	}
	return out
}
