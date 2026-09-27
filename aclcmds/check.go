// Copyright (c) 2026 Visvasity LLC

package aclcmds

import (
	"context"
	"flag"
	"fmt"

	"github.com/visvasity/cli"
	"github.com/visvasity/zanzibar"
)

// Check reports whether a subject has a relation on an object.
type Check struct {
	flags ClientFlags
	sopts SubjectOptions

	asOf     string
	exitCode bool
}

func (c *Check) Purpose() string {
	return "Check access: <object> <relation> <subject>"
}

func (c *Check) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.flags.SetFlags(fset)
	fset.StringVar(&c.asOf, "as-of", "", "evaluation time for time-boxed grants (RFC3339)")
	fset.BoolVar(&c.exitCode, "exit-code", true, "exit non-zero when access is denied (use -exit-code=false to disable)")
	return "check", fset, c.run
}

func (c *Check) run(ctx context.Context, args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("needs <object> <relation> <subject>")
	}
	asOf, err := parseTime(c.asOf)
	if err != nil {
		return fmt.Errorf("invalid -as-of: %w", err)
	}
	subject, err := c.sopts.mapInput(ctx, args[2])
	if err != nil {
		return err
	}
	client, err := c.flags.Client()
	if err != nil {
		return err
	}
	resp, err := client.Check(ctx, &zanzibar.CheckRequest{
		Object: args[0], Relation: args[1], Subject: subject, AsOfUnixNano: asOf,
	})
	if err != nil {
		return err
	}
	out := cli.Stdout(ctx)
	if resp.Allowed {
		fmt.Fprintln(out, "allowed")
		return nil
	}
	fmt.Fprintln(out, "denied")
	if c.exitCode {
		return fmt.Errorf("access denied")
	}
	return nil
}

// SetSubjectOptions configures subject mapping for this command (see
// SubjectOptions).
func (c *Check) SetSubjectOptions(o SubjectOptions) {
	c.sopts = o
}
