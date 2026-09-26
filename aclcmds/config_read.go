// Copyright (c) 2026 Visvasity LLC

package aclcmds

import (
	"context"
	"flag"
	"fmt"

	"github.com/visvasity/cli"
)

// ConfigRead prints a namespace's current config as JSON.
type ConfigRead struct {
	flags ConfigClientFlags
}

func (c *ConfigRead) Purpose() string {
	return "Print a namespace's current config as JSON: <namespace>"
}

func (c *ConfigRead) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.flags.SetFlags(fset)
	return "read", fset, c.run
}

func (c *ConfigRead) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("needs <namespace>")
	}
	client, err := c.flags.Client()
	if err != nil {
		return err
	}
	cfg, err := client.ReadConfig(ctx, args[0])
	if err != nil {
		return err
	}
	return printJSON(cli.Stdout(ctx), cfg)
}
