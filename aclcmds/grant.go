// Copyright (c) 2026 Visvasity LLC

package aclcmds

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/visvasity/cli"
	"github.com/visvasity/zanzibar"
)

// Grant grants a relation tuple.
type Grant struct {
	flags ClientFlags

	notBefore string
	notAfter  string
	createdAt string
}

func (c *Grant) Purpose() string {
	return "Grant a relation tuple: <object> <relation> <subject>"
}

func (c *Grant) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.flags.SetFlags(fset)
	fset.StringVar(&c.notBefore, "not-before", "", "validity window start (RFC3339); empty means unbounded")
	fset.StringVar(&c.notAfter, "not-after", "", "validity window end (RFC3339); empty means unbounded")
	fset.StringVar(&c.createdAt, "created-at", "", "creation timestamp (RFC3339); default is now")
	return "grant", fset, c.run
}

func (c *Grant) run(ctx context.Context, args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("needs <object> <relation> <subject>")
	}
	nb, err := parseTime(c.notBefore)
	if err != nil {
		return fmt.Errorf("invalid -not-before: %w", err)
	}
	na, err := parseTime(c.notAfter)
	if err != nil {
		return fmt.Errorf("invalid -not-after: %w", err)
	}
	created := time.Now().UnixNano()
	if c.createdAt != "" {
		if created, err = parseTime(c.createdAt); err != nil {
			return fmt.Errorf("invalid -created-at: %w", err)
		}
	}

	client, err := c.flags.Client()
	if err != nil {
		return err
	}
	resp, err := client.Write(ctx, &zanzibar.WriteRequest{Mutations: []zanzibar.Mutation{{
		Op:                zanzibar.OpGrant,
		Tuple:             zanzibar.Tuple{Object: args[0], Relation: args[1], Subject: args[2]},
		CreatedAtUnixNano: created,
		NotBeforeUnixNano: nb,
		NotAfterUnixNano:  na,
	}}})
	if err != nil {
		return err
	}
	fmt.Fprintf(cli.Stdout(ctx), "granted %s#%s@%s (applied=%d)\n", args[0], args[1], args[2], resp.Applied)
	return nil
}
