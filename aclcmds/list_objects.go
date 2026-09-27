// Copyright (c) 2026 Visvasity LLC

package aclcmds

import (
	"context"
	"flag"
	"fmt"

	"github.com/visvasity/cli"
	"github.com/visvasity/zanzibar"
)

// ListObjects prints the objects in a namespace on which a subject holds a
// relation.
type ListObjects struct {
	flags ClientFlags
	sopts SubjectOptions

	asOf     string
	pageSize int
}

func (c *ListObjects) Purpose() string {
	return "List objects a subject can access: <namespace> <relation> <subject>"
}

func (c *ListObjects) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.flags.SetFlags(fset)
	fset.StringVar(&c.asOf, "as-of", "", "evaluation time for time-boxed grants (RFC3339)")
	fset.IntVar(&c.pageSize, "page-size", 0, "page size per underlying request (0 = server default)")
	return "list-objects", fset, c.run
}

func (c *ListObjects) run(ctx context.Context, args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("needs <namespace> <relation> <subject>")
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
	out := cli.Stdout(ctx)
	token := ""
	for {
		resp, err := client.ListObjects(ctx, &zanzibar.ListObjectsRequest{
			Namespace: args[0], Relation: args[1], Subject: subject,
			AsOfUnixNano: asOf, PageSize: c.pageSize, PageToken: token,
		})
		if err != nil {
			return err
		}
		for _, id := range resp.ObjectIDs {
			fmt.Fprintln(out, id)
		}
		if resp.NextPageToken == "" {
			return nil
		}
		token = resp.NextPageToken
	}
}

// SetSubjectOptions configures subject mapping for this command (see
// SubjectOptions).
func (c *ListObjects) SetSubjectOptions(o SubjectOptions) {
	c.sopts = o
}
