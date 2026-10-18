package clients

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

func leadingSpace(b []byte) string {
	n := 0
	for n < len(b) && (b[n] == ' ' || b[n] == '\t') {
		n++
	}
	return string(b[:n])
}

// valueSpan returns the byte range of the value at keys in a JSON or JSONC
// document (comments and trailing commas allowed), so one value can be
// replaced without reformatting the rest of the file.
func valueSpan(src []byte, keys ...string) (start, end int, err error) {
	s := &scanner{src: src}
	s.ws()
	return s.find(keys)
}

type scanner struct {
	src []byte
	pos int
}

var errSyntax = errors.New("配置文件不是有效的 JSON")

func (s *scanner) ws() {
	for s.pos < len(s.src) {
		switch c := s.src[s.pos]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == 0xEF || c == 0xBB || c == 0xBF:
			s.pos++ // whitespace, or a UTF-8 BOM
		case c == '/' && s.pos+1 < len(s.src) && s.src[s.pos+1] == '/':
			for s.pos < len(s.src) && s.src[s.pos] != '\n' {
				s.pos++
			}
		case c == '/' && s.pos+1 < len(s.src) && s.src[s.pos+1] == '*':
			s.pos += 2
			for s.pos+1 < len(s.src) && !(s.src[s.pos] == '*' && s.src[s.pos+1] == '/') {
				s.pos++
			}
			s.pos += 2
		default:
			return
		}
	}
}

func (s *scanner) find(keys []string) (int, int, error) {
	if len(keys) == 0 {
		start := s.pos
		if err := s.skipValue(); err != nil {
			return 0, 0, err
		}
		return start, s.pos, nil
	}
	if s.pos >= len(s.src) || s.src[s.pos] != '{' {
		return 0, 0, ErrNotFound
	}
	s.pos++
	for {
		s.ws()
		if s.pos >= len(s.src) {
			return 0, 0, errSyntax
		}
		if s.src[s.pos] == '}' {
			return 0, 0, ErrNotFound
		}
		key, err := s.str()
		if err != nil {
			return 0, 0, err
		}
		s.ws()
		if s.pos >= len(s.src) || s.src[s.pos] != ':' {
			return 0, 0, errSyntax
		}
		s.pos++
		s.ws()
		if key == keys[0] {
			return s.find(keys[1:])
		}
		if err := s.skipValue(); err != nil {
			return 0, 0, err
		}
		s.ws()
		if s.pos < len(s.src) && s.src[s.pos] == ',' {
			s.pos++
		}
	}
}

func (s *scanner) str() (string, error) {
	if s.pos >= len(s.src) || s.src[s.pos] != '"' {
		return "", errSyntax
	}
	start := s.pos
	s.pos++
	for s.pos < len(s.src) {
		switch s.src[s.pos] {
		case '\\':
			s.pos += 2
			continue
		case '"':
			s.pos++
			var out string
			if err := json.Unmarshal(s.src[start:s.pos], &out); err != nil {
				return "", errSyntax
			}
			return out, nil
		}
		s.pos++
	}
	return "", errSyntax
}

func (s *scanner) skipValue() error {
	if s.pos >= len(s.src) {
		return errSyntax
	}
	switch s.src[s.pos] {
	case '"':
		_, err := s.str()
		return err
	case '{', '[':
		closer := byte('}')
		if s.src[s.pos] == '[' {
			closer = ']'
		}
		s.pos++
		for {
			s.ws()
			if s.pos >= len(s.src) {
				return errSyntax
			}
			if s.src[s.pos] == closer {
				s.pos++
				return nil
			}
			if closer == '}' {
				if _, err := s.str(); err != nil {
					return err
				}
				s.ws()
				if s.pos >= len(s.src) || s.src[s.pos] != ':' {
					return errSyntax
				}
				s.pos++
				s.ws()
			}
			if err := s.skipValue(); err != nil {
				return err
			}
			s.ws()
			if s.pos < len(s.src) && s.src[s.pos] == ',' {
				s.pos++
			}
		}
	default: // number, true, false, null
		start := s.pos
		for s.pos < len(s.src) {
			c := s.src[s.pos]
			if c == ',' || c == '}' || c == ']' || c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '/' {
				break
			}
			s.pos++
		}
		if s.pos == start {
			return errSyntax
		}
		return nil
	}
}

// stripJSONC removes comments and trailing commas so encoding/json can parse
// a JSONC file. String contents are left alone.
func stripJSONC(src []byte) []byte {
	out := make([]byte, 0, len(src))
	inStr, esc := false, false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inStr {
			out = append(out, c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			out = append(out, c)
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			out = append(out, '\n')
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i++
		case c == ',':
			j := i + 1
			for j < len(src) && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r') {
				j++
			}
			if j < len(src) && (src[j] == '}' || src[j] == ']') {
				continue // trailing comma
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}

// sameJSON compares two JSON texts by value.
func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(stripJSONC(a), &x) != nil || json.Unmarshal(stripJSONC(b), &y) != nil {
		return false
	}
	xa, _ := json.Marshal(x)
	ya, _ := json.Marshal(y)
	return string(xa) == string(ya)
}

// memberSpan locates member name of the object at parent: from its key's
// opening quote to the end of its value.
func memberSpan(src []byte, parent []string, name string) (keyStart, valueEnd int, err error) {
	start, _, err := valueSpan(src, parent...)
	if err != nil {
		return 0, 0, err
	}
	if src[start] != '{' {
		return 0, 0, errSyntax
	}
	s := &scanner{src: src, pos: start + 1}
	for {
		s.ws()
		if s.pos >= len(src) {
			return 0, 0, errSyntax
		}
		if src[s.pos] == '}' {
			return 0, 0, ErrNotFound
		}
		keyStart = s.pos
		key, err := s.str()
		if err != nil {
			return 0, 0, err
		}
		s.ws()
		if s.pos >= len(src) || src[s.pos] != ':' {
			return 0, 0, errSyntax
		}
		s.pos++
		s.ws()
		if err := s.skipValue(); err != nil {
			return 0, 0, err
		}
		if key == name {
			return keyStart, s.pos, nil
		}
		s.ws()
		if s.pos < len(src) && src[s.pos] == ',' {
			s.pos++
		}
	}
}

// removeMember deletes member name from the object at parent, taking one
// separating comma with it, so that removing a member added by insertMember
// restores the original bytes.
func removeMember(src []byte, parent []string, name string) ([]byte, error) {
	keyStart, valueEnd, err := memberSpan(src, parent, name)
	if err != nil {
		return nil, err
	}
	s := &scanner{src: src, pos: valueEnd}
	s.ws()
	if s.pos < len(src) && src[s.pos] == ',' {
		// Not the last member: drop it up to the next member.
		s.pos++
		s.ws()
		return splice(src, keyStart, s.pos, ""), nil
	}
	// Last member: drop the comma before it, or everything inside the braces
	// when it is the only one.
	i := keyStart - 1
	for i >= 0 && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
		i--
	}
	if i >= 0 && src[i] == ',' {
		return splice(src, i, valueEnd, ""), nil
	}
	close := s.pos // the closing brace
	return splice(src, i+1, close, ""), nil
}

// insertMember adds "name": value as the first member of the object at
// parent, indented like its siblings.
func insertMember(src []byte, parent []string, name string, value any) ([]byte, string, error) {
	start, end, err := valueSpan(src, parent...)
	if err != nil {
		return nil, "", err
	}
	nl := newline(src)
	lineStart := bytes.LastIndexByte(src[:start], '\n') + 1
	parentIndent := leadingSpace(src[lineStart:start])
	childIndent := parentIndent + "  "
	inner := stripJSONC(src[start+1 : end-1])
	empty := len(bytes.TrimSpace(inner)) == 0
	if !empty {
		// Match the first existing member's indentation.
		s := &scanner{src: src, pos: start + 1}
		s.ws()
		ls := bytes.LastIndexByte(src[:s.pos], '\n') + 1
		if ls > start {
			childIndent = leadingSpace(src[ls:s.pos])
		}
	}
	key, _ := json.Marshal(name)
	unit := "  "
	if len(childIndent) > 0 && childIndent[0] == '\t' {
		unit = "\t"
	}
	body, _ := json.MarshalIndent(value, childIndent, unit)
	written := strings.ReplaceAll(string(body), "\n", nl)
	member := string(key) + ": " + written
	if empty {
		return splice(src, start+1, end-1, nl+childIndent+member+nl+parentIndent), written, nil
	}
	return splice(src, start+1, start+1, nl+childIndent+member+","), written, nil
}
