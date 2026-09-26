// Copyright (c) 2026 Visvasity LLC

package aclcmds

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/visvasity/cli"
	"github.com/visvasity/zanzibar"
	"github.com/visvasity/zanzibar/schema"
)

// ConfigGroup returns the "config" command group for namespace-config
// administration (it talks to the config-plane API), plus the local "compile"
// converter.
func ConfigGroup() cli.Command {
	return cli.NewGroup("config", "Manage namespace configs",
		new(ConfigWrite),
		new(ConfigRead),
		new(ConfigList),
		new(ConfigReadVersion),
		new(ConfigListVersions),
		new(ConfigCompile),
	)
}

// resolveFormat determines the input format from an explicit flag or, failing
// that, the file extension (".acl"/".zml"/".dsl" → dsl; otherwise json).
func resolveFormat(explicit, file string) string {
	if explicit != "" {
		return strings.ToLower(explicit)
	}
	switch strings.ToLower(filepath.Ext(file)) {
	case ".acl", ".zml", ".dsl":
		return "dsl"
	default:
		return "json"
	}
}

// decodeConfigs parses one or more NamespaceConfigs from data. The DSL always
// yields a list; JSON accepts either a single object or an array.
func decodeConfigs(data []byte, format string) ([]zanzibar.NamespaceConfig, error) {
	switch format {
	case "dsl":
		return schema.Parse(data)
	case "json":
		if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && trimmed[0] == '[' {
			var arr []zanzibar.NamespaceConfig
			if err := json.Unmarshal(data, &arr); err != nil {
				return nil, fmt.Errorf("could not parse config JSON array: %w", err)
			}
			return arr, nil
		}
		var one zanzibar.NamespaceConfig
		if err := json.Unmarshal(data, &one); err != nil {
			return nil, fmt.Errorf("could not parse config JSON: %w", err)
		}
		return []zanzibar.NamespaceConfig{one}, nil
	default:
		return nil, fmt.Errorf("unknown format %q (want json or dsl)", format)
	}
}

// printJSON writes v as indented JSON followed by a newline.
func printJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}
