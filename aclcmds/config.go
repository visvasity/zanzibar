// Copyright (c) 2026 Visvasity LLC

package aclcmds

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/visvasity/cli"
)

// ConfigGroup returns the "config" command group for namespace-config
// administration (it talks to the config-plane API).
func ConfigGroup() cli.Command {
	return cli.NewGroup("config", "Manage namespace configs",
		new(ConfigWrite),
		new(ConfigRead),
		new(ConfigList),
		new(ConfigReadVersion),
		new(ConfigListVersions),
	)
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
