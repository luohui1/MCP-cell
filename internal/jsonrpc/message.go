// Package jsonrpc is the raw JSON-RPC 2.0 envelope MCPcell routes on.
//
// Only the fields a router needs are decoded. params, result and error stay
// raw so protocol fields MCPcell does not know about pass through unchanged,
// and ids keep their exact original spelling (string vs number, big integers).
package jsonrpc

import (
	"bytes"
	"encoding/json"
	"errors"
)

const Version = "2.0"

// Standard JSON-RPC error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// MCPcell-specific error codes, kept inside the implementation-defined range.
const (
	// CodeUpstreamUnavailable: the backing server could not be started or reached.
	CodeUpstreamUnavailable = -32001
	// CodeResultUnknown: the upstream died with the call in flight. The call is
	// never replayed, because it may already have had side effects.
	CodeResultUnknown = -32002
	// CodeNotShareable: the request needs a client the shared instance cannot attribute.
	CodeNotShareable = -32003
)

type Kind int

const (
	Invalid Kind = iota
	Request
	Notification
	Response
)

func (k Kind) String() string {
	switch k {
	case Request:
		return "request"
	case Notification:
		return "notification"
	case Response:
		return "response"
	}
	return "invalid"
}

// Message is one JSON-RPC message. A zero-length RawMessage means the member is
// absent; the literal null is kept as "null", so `"id": null` survives a round trip.
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

func (m Message) Kind() Kind {
	if m.JSONRPC != Version {
		return Invalid
	}
	hasID := len(m.ID) > 0
	switch {
	case m.Method != "" && m.Result == nil && m.Error == nil:
		if hasID {
			return Request
		}
		return Notification
	case m.Method == "" && hasID && (m.Result != nil) != (m.Error != nil):
		return Response
	}
	return Invalid
}

// IDKey returns a map key for m.ID that keeps 1 and "1" distinct.
func (m Message) IDKey() string { return IDKey(m.ID) }

// IDKey normalizes insignificant whitespace only; the id's type and digits are kept.
func IDKey(id json.RawMessage) string {
	var buf bytes.Buffer
	if json.Compact(&buf, id) != nil {
		return string(id)
	}
	return buf.String()
}

// WithID returns a shallow copy carrying a different id, used when remapping
// downstream ids onto a shared upstream and back.
func (m Message) WithID(id json.RawMessage) Message {
	m.ID = id
	return m
}

var ErrEmpty = errors.New("jsonrpc: empty message")

// Parse decodes one frame, which may be a single message or (legacy MCP
// 2025-03-26) a batch array. batch reports which form the sender used so a
// reply can mirror it.
func Parse(frame []byte) (msgs []Message, batch bool, err error) {
	frame = bytes.TrimSpace(frame)
	if len(frame) == 0 {
		return nil, false, ErrEmpty
	}
	if frame[0] == '[' {
		if err := json.Unmarshal(frame, &msgs); err != nil {
			return nil, true, err
		}
		if len(msgs) == 0 {
			return nil, true, ErrEmpty
		}
		return msgs, true, nil
	}
	var m Message
	if err := json.Unmarshal(frame, &m); err != nil {
		return nil, false, err
	}
	return []Message{m}, false, nil
}

type errorObject struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// NewError builds an error response. A missing id is sent as null, as JSON-RPC
// requires when the request id could not be determined.
func NewError(id json.RawMessage, code int, message string) Message {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	body, _ := json.Marshal(errorObject{Code: code, Message: message})
	return Message{JSONRPC: Version, ID: id, Error: body}
}
