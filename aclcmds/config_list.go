// Copyright (c) 2026 Visvasity LLC

package aclcmds

import (
	"context"
	"flag"
	"fmt"

	"github.com/visvasity/cli"
)

// ConfigList prints all stored namespaces and their current versions.
type ConfigList struct {
	flags ConfigClientFlags
}

func (c *ConfigList) Purpose() string { return "List stored namespaces and their current versions" }

func (c *ConfigList) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.flags.SetFlags(fset)
	return "list", fset, c.run
}

func (c *ConfigList) run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("command takes no positional arguments")
	}
	client, err := c.flags.Client()
	if err != nil {
		return err
	}
	cfgs, err := client.ListConfigs(ctx)
	if err != nil {
		return err
	}
	out := cli.Stdout(ctx)
	for _, cfg := range cfgs {
		fmt.Fprintf(out, "%s\tversion=%d\trelations=%d\n", cfg.Namespace, cfg.Version, len(cfg.Relations))
	}
	return nil
}
