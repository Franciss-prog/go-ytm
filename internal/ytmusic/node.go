package ytmusic

import (
	"strconv"
	"strings"
)

// node is a read-only view over decoded JSON (map[string]any / []any).
// Navigation never panics: a missing key, an out-of-range index or a type
// mismatch yields the zero node, whose accessors return zero values.
type node struct{ v any }

// get walks a path of string keys and int indexes.
func (n node) get(path ...any) node {
	cur := n.v
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := cur.(map[string]any)
			if !ok {
				return node{}
			}
			cur = m[k]
		case int:
			a, ok := cur.([]any)
			if !ok {
				return node{}
			}
			if k < 0 {
				k += len(a)
			}
			if k < 0 || k >= len(a) {
				return node{}
			}
			cur = a[k]
		default:
			return node{}
		}
		if cur == nil {
			return node{}
		}
	}
	return node{cur}
}

func (n node) exists() bool { return n.v != nil }

func (n node) has(key string) bool {
	m, ok := n.v.(map[string]any)
	if !ok {
		return false
	}
	_, ok = m[key]
	return ok
}

func (n node) str() string {
	s, _ := n.v.(string)
	return s
}

func (n node) boolean() bool {
	b, _ := n.v.(bool)
	return b
}

func (n node) list() []node {
	a, ok := n.v.([]any)
	if !ok {
		return nil
	}
	out := make([]node, len(a))
	for i, v := range a {
		out[i] = node{v}
	}
	return out
}

// firstKey returns the single key of a one-key renderer wrapper such as
// {"musicShelfRenderer": {...}}; with several keys it returns "".
func (n node) firstKey() string {
	m, ok := n.v.(map[string]any)
	if !ok || len(m) != 1 {
		return ""
	}
	for k := range m {
		return k
	}
	return ""
}

// runsText joins the text of a {"runs":[{"text":...}]} object. Objects with
// "simpleText" are supported too.
func (n node) runsText() string {
	if s := n.get("simpleText").str(); s != "" {
		return s
	}
	var b strings.Builder
	for _, r := range n.get("runs").list() {
		b.WriteString(r.get("text").str())
	}
	return b.String()
}

// parseDuration parses "3:09" or "1:02:03" into seconds; ok is false for
// anything else.
func parseDuration(s string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	total := 0
	for _, p := range parts {
		if p == "" {
			return 0, false
		}
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return 0, false
		}
		total = total*60 + v
	}
	return total, true
}
