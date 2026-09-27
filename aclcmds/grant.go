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
	sopts SubjectOptions

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

	subject, err := c.sopts.mapInput(ctx, args[2])
	if err != nil {
		return err
	}
	client, err := c.flags.Client()
	if err != nil {
		return err
	}
	resp, err := client.Write(ctx, &zanzibar.WriteRequest{Mutations: []zanzibar.Mutation{{
		Op:                zanzibar.OpGrant,
		Tuple:             zanzibar.Tuple{Object: args[0], Relation: args[1], Subject: subject},
		CreatedAtUnixNano: created,
		NotBeforeUnixNano: nb,
		NotAfterUnixNano:  na,
	}}})
	if err != nil {
		return err
	}
	fmt.Fprintf(cli.Stdout(ctx), "granted %s#%s@%s (applied=%d)\n", args[0], args[1], subject, resp.Applied)
	return nil
}

// SetSubjectOptions configures subject mapping for this command (see
// SubjectOptions).
func (c *Grant) SetSubjectOptions(o SubjectOptions) {
	c.sopts = o
}
