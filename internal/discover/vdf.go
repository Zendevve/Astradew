package discover

import (
	"sort"
	"strings"
)

// LibraryRootsFromText extracts Steam library roots from libraryfolders.vdf
// text. It accepts both shapes from the research notes: the modern
// "libraryfolders" { "<n>" { "path" "<root>" ... } } form and the legacy
// form mapping "<n>" directly to a path string. Numeric keys need not be
// contiguous; entries without a usable path are skipped. Malformed or empty
// input yields no roots, never an error: a missing or unmounted library is
// tolerated, not fatal.
func LibraryRootsFromText(text string) []string {
	root := parseVDF(text)
	var libsObj map[string]any
	for key, value := range root {
		if strings.EqualFold(key, "libraryfolders") {
			if obj, ok := value.(map[string]any); ok {
				libsObj = obj
			}
			break
		}
	}
	if libsObj == nil {
		return nil
	}
	keys := make([]string, 0, len(libsObj))
	for key := range libsObj {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out []string
	for _, key := range keys {
		switch value := libsObj[key].(type) {
		case string:
			if path := strings.TrimSpace(value); path != "" {
				out = append(out, path)
			}
		case map[string]any:
			for field, fieldValue := range value {
				if strings.EqualFold(field, "path") {
					if text, ok := fieldValue.(string); ok {
						if path := strings.TrimSpace(text); path != "" {
							out = append(out, path)
						}
					}
					break
				}
			}
		}
	}
	return out
}

// vdfParser is a small recursive-descent reader over Valve KeyValues text:
// quoted strings, { } nesting, and // line comments. Anything unexpected
// ends the current object; callers treat a short parse as fewer entries,
// never a failure.
type vdfParser struct {
	text string
	pos  int
}

// parseVDF reads top-level key/value pairs into a map. Values are strings
// or nested maps.
func parseVDF(text string) map[string]any {
	parser := &vdfParser{text: text}
	out := map[string]any{}
	for {
		key, ok := parser.nextString()
		if !ok {
			return out
		}
		parser.skipSpace()
		if parser.peek() == '{' {
			parser.pos++
			out[key] = parser.parseObject()
			continue
		}
		value, ok := parser.nextString()
		if !ok {
			return out
		}
		out[key] = value
	}
}

// parseObject reads pairs until the closing brace or the end of input.
func (p *vdfParser) parseObject() map[string]any {
	out := map[string]any{}
	for {
		p.skipSpace()
		if p.peek() == '}' {
			p.pos++
			return out
		}
		key, ok := p.nextString()
		if !ok {
			return out
		}
		p.skipSpace()
		if p.peek() == '{' {
			p.pos++
			out[key] = p.parseObject()
			continue
		}
		value, ok := p.nextString()
		if !ok {
			return out
		}
		out[key] = value
	}
}

// peek reports the current byte, or 0 past the end.
func (p *vdfParser) peek() byte {
	if p.pos >= len(p.text) {
		return 0
	}
	return p.text[p.pos]
}

// skipSpace moves past whitespace and // line comments.
func (p *vdfParser) skipSpace() {
	for p.pos < len(p.text) {
		c := p.text[p.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			p.pos++
			continue
		}
		if c == '/' && p.pos+1 < len(p.text) && p.text[p.pos+1] == '/' {
			for p.pos < len(p.text) && p.text[p.pos] != '\n' {
				p.pos++
			}
			continue
		}
		return
	}
}

// nextString reads one quoted string, unescaping \\ to \ and \" to ".
// Any other backslash sequence keeps the backslash literally so Windows
// paths never corrupt. It reports false when no quoted string follows.
func (p *vdfParser) nextString() (string, bool) {
	p.skipSpace()
	if p.peek() != '"' {
		return "", false
	}
	p.pos++
	var out strings.Builder
	for p.pos < len(p.text) {
		c := p.text[p.pos]
		if c == '"' {
			p.pos++
			return out.String(), true
		}
		if c == '\\' && p.pos+1 < len(p.text) {
			next := p.text[p.pos+1]
			if next == '\\' || next == '"' {
				out.WriteByte(next)
				p.pos += 2
				continue
			}
		}
		out.WriteByte(c)
		p.pos++
	}
	return "", false
}
