// Copyright (c) 2026 Visvasity LLC

package aclcmds

import (
	"context"
	"flag"
	"fmt"

	"github.com/visvasity/cli"
)

// ConfigListVersions prints a namespace's stored version numbers.
type ConfigListVersions struct {
	flags ConfigClientFlags
}

func (c *ConfigListVersions) Purpose() string {
	return "List a namespace's stored config versions: <namespace>"
}

func (c *ConfigListVersions) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.flags.SetFlags(fset)
	return "list-versions", fset, c.run
}

func (c *ConfigListVersions) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("needs <namespace>")
	}
	client, err := c.flags.Client()
	if err != nil {
		return err
	}
	versions, err := client.ListConfigVersions(ctx, args[0])
	if err != nil {
		return err
	}
	out := cli.Stdout(ctx)
	for _, v := range versions {
		fmt.Fprintln(out, v)
	}
	return nil
}
