package jsonrpc

import (
	"encoding/json"
	"testing"
)

func parseOne(t *testing.T, s string) Message {
	t.Helper()
	msgs, batch, err := Parse([]byte(s))
	if err != nil || batch || len(msgs) != 1 {
		t.Fatalf("Parse(%s) = %v, batch=%v, err=%v", s, msgs, batch, err)
	}
	return msgs[0]
}

func TestKind(t *testing.T) {
	cases := []struct {
		in   string
		want Kind
	}{
		{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{}}`, Request},
		{`{"jsonrpc":"2.0","id":"a","method":"ping"}`, Request},
		{`{"jsonrpc":"2.0","method":"notifications/initialized"}`, Notification},
		{`{"jsonrpc":"2.0","id":1,"result":{}}`, Response},
		{`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"x"}}`, Response},
		{`{"jsonrpc":"2.0","id":1,"result":{},"error":{}}`, Invalid},
		{`{"jsonrpc":"1.0","id":1,"method":"ping"}`, Invalid},
		{`{"jsonrpc":"2.0","id":1}`, Invalid},
	}
	for _, c := range cases {
		m := parseOne(t, c.in)
		if got := m.Kind(); got != c.want {
			t.Errorf("Kind(%s) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestIDsSurviveRoundTrip(t *testing.T) {
	// Big integers and string/number distinction must not be lost by remapping.
	for _, id := range []string{`12345678901234567890`, `"12345678901234567890"`, `0`, `"x y"`, `null`} {
		m := parseOne(t, `{"jsonrpc":"2.0","id":`+id+`,"result":{"ok":true}}`)
		out, err := json.Marshal(m.WithID(json.RawMessage(`7`)).WithID(m.ID))
		if err != nil {
			t.Fatal(err)
		}
		back := parseOne(t, string(out))
		if string(back.ID) != id {
			t.Errorf("id %s came back as %s", id, back.ID)
		}
	}
}

func TestIDKeyKeepsTypes(t *testing.T) {
	if IDKey(json.RawMessage(`1`)) == IDKey(json.RawMessage(`"1"`)) {
		t.Fatal(`1 and "1" must be different request ids`)
	}
	if IDKey(json.RawMessage(` "a" `)) != IDKey(json.RawMessage(`"a"`)) {
		t.Fatal("insignificant whitespace should not change the key")
	}
}

func TestUnknownFieldsInParamsPassThrough(t *testing.T) {
	in := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x","_meta":{"future":[1,2]}}}`
	m := parseOne(t, in)
	out, _ := json.Marshal(m)
	if string(out) != in {
		t.Fatalf("re-encoded as %s", out)
	}
}

func TestBatch(t *testing.T) {
	msgs, batch, err := Parse([]byte(` [{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"n"}] `))
	if err != nil || !batch || len(msgs) != 2 || msgs[1].Kind() != Notification {
		t.Fatalf("batch parse: %v %v %v", msgs, batch, err)
	}
	if _, _, err := Parse([]byte(`[]`)); err != ErrEmpty {
		t.Fatalf("empty batch: %v", err)
	}
}

func TestNewErrorDefaultsToNullID(t *testing.T) {
	m := NewError(nil, CodeParseError, "bad frame")
	out, _ := json.Marshal(m)
	want := `{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"bad frame"}}`
	if string(out) != want {
		t.Fatalf("got %s", out)
	}
}
