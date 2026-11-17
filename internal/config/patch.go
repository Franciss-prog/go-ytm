package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// Change sets one dotted key ("theme.transparent") to a value (bool, int,
// string or []string).
type Change struct {
	Key   string
	Value any
}

// Patch applies changes to the TOML file at path, preserving comments,
// ordering, blank lines, line endings and unrelated keys: an existing value
// is replaced in place (its byte range is spliced), a missing key is
// appended at the end of its table, and a missing table is created at EOF.
// A missing file is created. The write is atomic (temp file + rename,
// keeping the file mode). The result is re-parsed before writing, so a
// patch can never leave an invalid file.
func Patch(path string, changes []Change) error {
	data, err := os.ReadFile(path)
	mode := fs.FileMode(0o600)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		data = nil
	case err != nil:
		return fmt.Errorf("patch config: %w", err)
	default:
		if fi, err := os.Stat(path); err == nil {
			mode = fi.Mode().Perm()
		}
	}
	out, err := PatchBytes(data, changes)
	if err != nil {
		return fmt.Errorf("patch config %s: %w", path, err)
	}
	if err := writeAtomic(path, out, mode); err != nil {
		return fmt.Errorf("patch config: %w", err)
	}
	return nil
}

// PatchBytes is Patch on file content.
func PatchBytes(data []byte, changes []Change) ([]byte, error) {
	for _, c := range changes {
		lit, err := literal(c.Value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.Key, err)
		}
		if data, err = patchOne(data, c.Key, lit); err != nil {
			return nil, fmt.Errorf("%s: %w", c.Key, err)
		}
	}
	var v map[string]any
	if err := toml.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("patched file would be invalid: %w", err)
	}
	return data, nil
}

// literal renders v as a TOML value (strings as basic "…" strings).
func literal(v any) (string, error) {
	switch x := v.(type) {
	case bool:
		return strconv.FormatBool(x), nil
	case int:
		return strconv.Itoa(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case string:
		return quote(x), nil
	case []string:
		q := make([]string, len(x))
		for i, s := range x {
			q[i] = quote(s)
		}
		return "[" + strings.Join(q, ", ") + "]", nil
	}
	return "", fmt.Errorf("unsupported value type %T", v)
}

// quote is a TOML basic string: only \, \" and control characters are
// escaped (TOML has no \x or \a escapes, so strconv.Quote won't do).
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// patchOne replaces or inserts one key.
func patchOne(data []byte, key, lit string) ([]byte, error) {
	want := strings.Split(key, ".")
	if len(want) < 2 {
		return nil, errors.New("key must be table.key")
	}
	table, leaf := want[:len(want)-1], want[len(want)-1]
	nl := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		nl = "\r\n"
	}

	var p unstable.Parser
	p.KeepComments = true
	p.Reset(data)
	var prefix []string
	tableEnd := -1 // insertion offset at the end of the wanted [table]
	for p.NextExpression() {
		e := p.Expression()
		switch e.Kind {
		case unstable.Table, unstable.ArrayTable:
			prefix = keyParts(e.Key())
			if e.Kind == unstable.Table && slices.Equal(prefix, table) {
				it := e.Key()
				var last *unstable.Node
				for it.Next() {
					last = it.Node()
				}
				// The header's closing "]" follows the last key part.
				tableEnd = lineEnd(data, int(last.Raw.Offset+last.Raw.Length))
			}
		case unstable.KeyValue:
			parts := keyParts(e.Key())
			full := append(slices.Clone(prefix), parts...)
			it := e.Key()
			var lastKey *unstable.Node
			for it.Next() {
				lastKey = it.Node()
			}
			end := int(e.Raw.Offset + e.Raw.Length)
			if slices.Equal(prefix, table) {
				tableEnd = lineEnd(data, end)
			}
			if slices.Equal(full, want) {
				eq := bytes.IndexByte(data[lastKey.Raw.Offset+lastKey.Raw.Length:end], '=')
				if eq < 0 {
					return nil, errors.New("malformed key/value")
				}
				start := int(lastKey.Raw.Offset+lastKey.Raw.Length) + eq + 1
				for start < end && (data[start] == ' ' || data[start] == '\t') {
					start++
				}
				return splice(data, start, end, lit), nil
			}
			if len(full) <= len(want) && slices.Equal(full, want[:len(full)]) && e.Value().Kind == unstable.InlineTable {
				return nil, fmt.Errorf("%s is an inline table; edit it by hand", strings.Join(full, "."))
			}
		}
	}
	if err := p.Error(); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	line := leaf + " = " + lit
	if tableEnd >= 0 {
		ins := line + nl
		if tableEnd > 0 && data[tableEnd-1] != '\n' {
			ins = nl + ins
		}
		return splice(data, tableEnd, tableEnd, ins), nil
	}
	var b bytes.Buffer
	b.Write(data)
	if len(data) > 0 {
		if !bytes.HasSuffix(data, []byte("\n")) {
			b.WriteString(nl)
		}
		b.WriteString(nl)
	}
	b.WriteString("[" + strings.Join(table, ".") + "]" + nl + line + nl)
	return b.Bytes(), nil
}

// lineEnd returns the offset just past the newline ending the line that
// contains off (len(data) on the last line).
func lineEnd(data []byte, off int) int {
	if i := bytes.IndexByte(data[off:], '\n'); i >= 0 {
		return off + i + 1
	}
	return len(data)
}

func splice(data []byte, start, end int, s string) []byte {
	out := make([]byte, 0, len(data)-(end-start)+len(s))
	out = append(out, data[:start]...)
	out = append(out, s...)
	return append(out, data[end:]...)
}

// writeAtomic writes data to a temp file next to path and renames it over
// path.
func writeAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op after a successful rename
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
