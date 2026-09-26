// Copyright (c) 2026 Visvasity LLC

package aclcmds

import (
	"context"
	"flag"
	"fmt"

	"github.com/visvasity/cli"
	"github.com/visvasity/zanzibar"
)

// ListUsers prints the user subjects that are members of object#relation.
type ListUsers struct {
	flags ClientFlags

	asOf     string
	pageSize int
}

func (c *ListUsers) Purpose() string {
	return "List users who are members of: <object> <relation>"
}

func (c *ListUsers) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.flags.SetFlags(fset)
	fset.StringVar(&c.asOf, "as-of", "", "evaluation time for time-boxed grants (RFC3339)")
	fset.IntVar(&c.pageSize, "page-size", 0, "page size per underlying request (0 = server default)")
	return "list-users", fset, c.run
}

func (c *ListUsers) run(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("needs <object> <relation>")
	}
	asOf, err := parseTime(c.asOf)
	if err != nil {
		return fmt.Errorf("invalid -as-of: %w", err)
	}
	client, err := c.flags.Client()
	if err != nil {
		return err
	}
	out := cli.Stdout(ctx)
	token := ""
	for {
		resp, err := client.ListUsers(ctx, &zanzibar.ListUsersRequest{
			Object: args[0], Relation: args[1],
			AsOfUnixNano: asOf, PageSize: c.pageSize, PageToken: token,
		})
		if err != nil {
			return err
		}
		for _, u := range resp.Users {
			fmt.Fprintln(out, u)
		}
		if resp.NextPageToken == "" {
			return nil
		}
		token = resp.NextPageToken
	}
}
