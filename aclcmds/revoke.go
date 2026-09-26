// Copyright (c) 2026 Visvasity LLC

package aclcmds

import (
	"context"
	"flag"
	"fmt"

	"github.com/visvasity/cli"
	"github.com/visvasity/zanzibar"
)

// Revoke removes a relation tuple.
type Revoke struct {
	flags ClientFlags
}

func (c *Revoke) Purpose() string {
	return "Revoke a relation tuple: <object> <relation> <subject>"
}

func (c *Revoke) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.flags.SetFlags(fset)
	return "revoke", fset, c.run
}

func (c *Revoke) run(ctx context.Context, args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("needs <object> <relation> <subject>")
	}
	client, err := c.flags.Client()
	if err != nil {
		return err
	}
	resp, err := client.Write(ctx, &zanzibar.WriteRequest{Mutations: []zanzibar.Mutation{{
		Op:    zanzibar.OpRevoke,
		Tuple: zanzibar.Tuple{Object: args[0], Relation: args[1], Subject: args[2]},
	}}})
	if err != nil {
		return err
	}
	fmt.Fprintf(cli.Stdout(ctx), "revoked %s#%s@%s (applied=%d)\n", args[0], args[1], args[2], resp.Applied)
	return nil
}
