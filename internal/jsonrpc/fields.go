package jsonrpc

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Encode marshals m on one line (raw members are compacted, so it is safe as
// an SSE data field or a stdio frame). HTML characters are not escaped, to
// keep passthrough bytes close to what the sender wrote. It can only fail if a
// RawMessage member holds invalid JSON.
func Encode(m Message) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func NewRequest(id json.RawMessage, method string, params any) (Message, error) {
	raw, err := marshalParams(params)
	return Message{JSONRPC: Version, ID: id, Method: method, Params: raw}, err
}

func NewNotification(method string, params any) (Message, error) {
	raw, err := marshalParams(params)
	return Message{JSONRPC: Version, Method: method, Params: raw}, err
}

// NewResult builds a success response. A nil result is sent as {} because MCP
// results are always objects.
func NewResult(id json.RawMessage, result any) Message {
	raw, ok := result.(json.RawMessage)
	if !ok {
		var err error
		if raw, err = json.Marshal(result); err != nil || string(raw) == "null" {
			raw = json.RawMessage("{}")
		}
	}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	return Message{JSONRPC: Version, ID: id, Result: raw}
}

func marshalParams(params any) (json.RawMessage, error) {
	switch p := params.(type) {
	case nil:
		return nil, nil
	case json.RawMessage:
		return p, nil
	}
	return json.Marshal(params)
}

// ErrorCode returns the code of an error response, or 0.
func (m Message) ErrorCode() int {
	var e errorObject
	if len(m.Error) == 0 || json.Unmarshal(m.Error, &e) != nil {
		return 0
	}
	return e.Code
}

var errNotObject = errors.New("jsonrpc: params is not an object")

// Field returns the raw value of a top-level member of a params/result object,
// or nil when the object or member is absent.
func Field(obj json.RawMessage, key string) json.RawMessage {
	var m map[string]json.RawMessage
	if len(obj) == 0 || json.Unmarshal(obj, &m) != nil {
		return nil
	}
	return m[key]
}

// SetField returns obj with one top-level member replaced. Other members are
// carried over as raw JSON, so unknown fields survive; member order may change.
func SetField(obj json.RawMessage, key string, value json.RawMessage) (json.RawMessage, error) {
	m := map[string]json.RawMessage{}
	if len(obj) > 0 && string(obj) != "null" {
		if err := json.Unmarshal(obj, &m); err != nil {
			return nil, errNotObject
		}
	}
	m[key] = value
	return json.Marshal(m)
}

// ProgressToken returns params._meta.progressToken, or nil.
func ProgressToken(params json.RawMessage) json.RawMessage {
	return Field(Field(params, "_meta"), "progressToken")
}

// SetProgressToken returns params with _meta.progressToken replaced, keeping
// every other _meta member.
func SetProgressToken(params json.RawMessage, token json.RawMessage) (json.RawMessage, error) {
	meta, err := SetField(Field(params, "_meta"), "progressToken", token)
	if err != nil {
		return nil, err
	}
	return SetField(params, "_meta", meta)
}
