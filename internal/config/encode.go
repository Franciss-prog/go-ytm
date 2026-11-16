package config

import (
	"bytes"

	"github.com/pelletier/go-toml/v2"
)

// DefaultsTOML renders the default configuration as TOML.
func DefaultsTOML() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("# ytm-go default configuration. Every key is optional.\n")
	buf.WriteString("# Docs: docs/specs/04-config-keymap-theme.md\n\n")
	enc := toml.NewEncoder(&buf)
	enc.SetIndentTables(false)
	if err := enc.Encode(Default()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
