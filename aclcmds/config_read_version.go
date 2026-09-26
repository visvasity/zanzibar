// Copyright (c) 2026 Visvasity LLC

package aclcmds

import (
	"context"
	"flag"
	"fmt"
	"strconv"

	"github.com/visvasity/cli"
)

// ConfigReadVersion prints a specific historical config version as JSON.
type ConfigReadVersion struct {
	flags ConfigClientFlags
}

func (c *ConfigReadVersion) Purpose() string {
	return "Print a historical config version as JSON: <namespace> <version>"
}

func (c *ConfigReadVersion) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.flags.SetFlags(fset)
	return "read-version", fset, c.run
}

func (c *ConfigReadVersion) run(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("needs <namespace> <version>")
	}
	version, err := strconv.ParseUint(args[1], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid version %q: %w", args[1], err)
	}
	client, err := c.flags.Client()
	if err != nil {
		return err
	}
	cfg, err := client.ReadConfigVersion(ctx, args[0], version)
	if err != nil {
		return err
	}
	return printJSON(cli.Stdout(ctx), cfg)
}
