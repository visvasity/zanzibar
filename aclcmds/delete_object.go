// Copyright (c) 2026 Visvasity LLC

package aclcmds

import (
	"context"
	"flag"
	"fmt"

	"github.com/visvasity/cli"
	"github.com/visvasity/zanzibar"
)

// DeleteObject removes every tuple stored on an object (object-deletion cleanup).
type DeleteObject struct {
	flags ClientFlags
}

func (c *DeleteObject) Purpose() string {
	return "Delete all grants stored on an object: <object>"
}

func (c *DeleteObject) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.flags.SetFlags(fset)
	return "delete-object", fset, c.run
}

func (c *DeleteObject) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("needs <object>")
	}
	client, err := c.flags.Client()
	if err != nil {
		return err
	}
	resp, err := client.Write(ctx, &zanzibar.WriteRequest{Mutations: []zanzibar.Mutation{{
		Op:    zanzibar.OpDelete,
		Tuple: zanzibar.Tuple{Object: args[0]},
	}}})
	if err != nil {
		return err
	}
	fmt.Fprintf(cli.Stdout(ctx), "deleted %d grant(s) on %s\n", resp.Applied, args[0])
	return nil
}
