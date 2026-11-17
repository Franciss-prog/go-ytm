package keys

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/Franciss-Prog/go-ytm/internal/config"
)

// node is one trie level. A node with an action has no children: prefix
// conflicts are resolved at load time (the longer binding wins).
type node struct {
	action   string
	children map[string]*node
}

// Keymap maps key sequences to actions. Build it with Default or Load;
// it is immutable afterwards and safe to share.
type Keymap struct {
	root     *node
	bindings map[string][]string // action → canonical sequences, for Help
}

// Bindings returns the sequences bound to action, in binding order.
func (k *Keymap) Bindings(action string) []string { return slices.Clone(k.bindings[action]) }

// Default returns the built-in keymap.
func Default() *Keymap {
	// The default table is checked by TestDefaultKeymapClean.
	km, _ := build(nil, "")
	return km
}

// LoadFile reads keymap.toml. A missing file means defaults, silently.
func LoadFile(path string) (*Keymap, config.Diagnostics) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Default(), config.Diagnostics{{File: path, Severity: config.SevError, Message: fmt.Sprintf("cannot read file: %v; using default keys", err)}}
	}
	return Load(data, path)
}

// userBinding is one action entry from keymap.toml.
type userBinding struct {
	action string
	seqs   []string
	pos    config.Pos
}

// Load parses keymap.toml data. Each action listed under [keys] replaces
// that action's default bindings; an empty list unbinds it. Problems are
// reported as diagnostics and never stop loading.
func Load(data []byte, file string) (*Keymap, config.Diagnostics) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		d := config.Diagnostic{File: file, Severity: config.SevError, Message: err.Error() + "; using default keys"}
		var de *toml.DecodeError
		if errors.As(err, &de) {
			d.Line, d.Col = de.Position()
			d.Message = de.Error() + "; using default keys"
		}
		return Default(), config.Diagnostics{d}
	}
	pos := config.Positions(data)
	var diags config.Diagnostics
	add := func(sev config.Severity, key, format string, args ...any) {
		p := pos[key]
		diags = append(diags, config.Diagnostic{File: file, Line: p.Line, Col: p.Col, Key: key, Severity: sev, Message: fmt.Sprintf(format, args...)})
	}
	for sec := range raw {
		if sec != "keys" {
			add(config.SevWarning, sec, "unknown section%s", config.Suggest(sec, []string{"keys"}))
		}
	}
	table, _ := raw["keys"].(map[string]any)
	var user []userBinding
	for action, v := range table {
		key := "keys." + action
		if _, ok := Lookup(action); !ok {
			add(config.SevWarning, key, "unknown action%s", config.Suggest(action, ActionNames()))
			continue
		}
		var seqs []string
		switch t := v.(type) {
		case string:
			seqs = []string{t}
		case []any:
			for _, e := range t {
				s, ok := e.(string)
				if !ok {
					add(config.SevWarning, key, "binding %v is not a string; skipped", e)
					continue
				}
				seqs = append(seqs, s)
			}
		default:
			add(config.SevWarning, key, "expected a string or a list of strings; keeping defaults")
			continue
		}
		user = append(user, userBinding{action: action, seqs: seqs, pos: pos[key]})
	}
	// File order decides "later wins" for duplicates.
	sort.SliceStable(user, func(i, j int) bool {
		a, b := user[i].pos, user[j].pos
		return a.Line < b.Line || a.Line == b.Line && a.Col < b.Col
	})
	km, more := build(user, file)
	return km, append(diags, more...)
}

// entry is one binding being placed in the trie.
type entry struct {
	action string
	seq    []string
	user   bool
	pos    config.Pos
}

func build(user []userBinding, file string) (*Keymap, config.Diagnostics) {
	var diags config.Diagnostics
	add := func(sev config.Severity, e entry, format string, args ...any) {
		diags = append(diags, config.Diagnostic{File: file, Line: e.pos.Line, Col: e.pos.Col, Key: "keys." + e.action, Severity: sev, Message: fmt.Sprintf(format, args...)})
	}
	overridden := map[string]bool{}
	for _, u := range user {
		overridden[u.action] = true
	}
	var entries []entry
	for _, a := range Actions {
		if overridden[a.Name] {
			continue
		}
		for _, d := range a.Defaults {
			seq, err := ParseSequence(d)
			if err != nil {
				add(config.SevError, entry{action: a.Name}, "default binding %q: %v", d, err)
				continue
			}
			entries = append(entries, entry{action: a.Name, seq: seq})
		}
	}
	for _, u := range user {
		for _, s := range u.seqs {
			seq, err := ParseSequence(s)
			if err != nil {
				add(config.SevWarning, entry{action: u.action, pos: u.pos}, "invalid binding %q: %v; skipped", s, err)
				continue
			}
			entries = append(entries, entry{action: u.action, seq: seq, user: true, pos: u.pos})
		}
	}

	// Exact duplicates: the later entry wins (user entries come after
	// defaults, and among themselves in file order).
	owner := map[string]int{}
	for i, e := range entries {
		k := strings.Join(e.seq, " ")
		if j, ok := owner[k]; ok && entries[j].action != e.action {
			prev := entries[j]
			switch {
			case prev.user:
				add(config.SevWarning, e, "%q is also bound to %s; %s wins (later in file)", k, prev.action, e.action)
			case e.user:
				add(config.SevInfo, e, "%q replaces the default binding of %s", k, prev.action)
			}
		}
		owner[k] = i
	}
	keep := make([]bool, len(entries))
	for _, i := range owner {
		keep[i] = true
	}
	// Prefix conflicts: a binding that is a strict prefix of another is
	// dropped (spec 04: the longer wins).
	for i, e := range entries {
		if !keep[i] {
			continue
		}
		for j, o := range entries {
			if keep[j] && len(o.seq) > len(e.seq) && slices.Equal(o.seq[:len(e.seq)], e.seq) {
				add(config.SevWarning, e, "%q is a prefix of %q (%s); the longer binding wins", strings.Join(e.seq, " "), strings.Join(o.seq, " "), o.action)
				keep[i] = false
				break
			}
		}
	}

	km := &Keymap{root: &node{}, bindings: map[string][]string{}}
	for i, e := range entries {
		if !keep[i] {
			continue
		}
		n := km.root
		for _, k := range e.seq {
			if n.children == nil {
				n.children = map[string]*node{}
			}
			c, ok := n.children[k]
			if !ok {
				c = &node{}
				n.children[k] = c
			}
			n = c
		}
		n.action = e.action
		km.bindings[e.action] = append(km.bindings[e.action], strings.Join(e.seq, " "))
	}
	return km, diags
}
