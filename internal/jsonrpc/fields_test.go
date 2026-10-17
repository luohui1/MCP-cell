package jsonrpc

import (
	"encoding/json"
	"testing"
)

func TestProgressTokenRewriteKeepsOtherMeta(t *testing.T) {
	params := json.RawMessage(`{"name":"slow","arguments":{"a":1},"_meta":{"progressToken":"abc","vendor/x":true}}`)
	if got := string(ProgressToken(params)); got != `"abc"` {
		t.Fatalf("token = %s", got)
	}
	out, err := SetProgressToken(params, json.RawMessage(`7`))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(ProgressToken(out)); got != `7` {
		t.Fatalf("rewritten token = %s", got)
	}
	if string(Field(Field(out, "_meta"), "vendor/x")) != "true" || string(Field(out, "arguments")) != `{"a":1}` {
		t.Fatalf("lost fields: %s", out)
	}
}

func TestProgressTokenAbsent(t *testing.T) {
	for _, p := range []string{``, `{}`, `{"_meta":{}}`, `[1]`} {
		if tok := ProgressToken(json.RawMessage(p)); tok != nil {
			t.Errorf("ProgressToken(%q) = %s", p, tok)
		}
	}
}

func TestSetFieldRejectsNonObject(t *testing.T) {
	if _, err := SetField(json.RawMessage(`[1]`), "a", json.RawMessage(`1`)); err == nil {
		t.Fatal("expected error for array params")
	}
	out, err := SetField(nil, "requestId", json.RawMessage(`3`))
	if err != nil || string(out) != `{"requestId":3}` {
		t.Fatalf("SetField(nil) = %s, %v", out, err)
	}
}

func TestNewResultDefaultsToObject(t *testing.T) {
	m := NewResult(json.RawMessage(`1`), nil)
	if string(m.Result) != `{}` || m.Kind() != Response {
		t.Fatalf("got %s kind %v", m.Result, m.Kind())
	}
	if NewError(json.RawMessage(`1`), CodeInternalError, "x").ErrorCode() != CodeInternalError {
		t.Fatal("ErrorCode")
	}
}
