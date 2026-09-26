// Copyright (c) 2026 Visvasity LLC

package aclcmds

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/visvasity/cli"
	"github.com/visvasity/zanzibar"
)

// Expand prints the userset tree for object#relation.
type Expand struct {
	flags ClientFlags

	asOf string
}

func (c *Expand) Purpose() string {
	return "Expand the userset tree: <object> <relation>"
}

func (c *Expand) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.flags.SetFlags(fset)
	fset.StringVar(&c.asOf, "as-of", "", "evaluation time for time-boxed grants (RFC3339)")
	return "expand", fset, c.run
}

func (c *Expand) run(ctx context.Context, args []string) error {
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
	resp, err := client.Expand(ctx, &zanzibar.ExpandRequest{Object: args[0], Relation: args[1], AsOfUnixNano: asOf})
	if err != nil {
		return err
	}
	printNode(cli.Stdout(ctx), resp.Tree, 0)
	return nil
}

func printNode(w io.Writer, n zanzibar.UsersetNode, depth int) {
	indent := strings.Repeat("  ", depth)
	label := string(n.Kind)
	if label == "" {
		label = "(unexpanded)"
	}
	var extra []string
	if n.Object != "" {
		extra = append(extra, "object="+n.Object)
	}
	if n.Relation != "" {
		extra = append(extra, "relation="+n.Relation)
	}
	if n.Tupleset != "" {
		extra = append(extra, "tupleset="+n.Tupleset)
	}
	if n.Truncated {
		extra = append(extra, "TRUNCATED")
	}
	suffix := ""
	if len(extra) > 0 {
		suffix = " (" + strings.Join(extra, " ") + ")"
	}
	fmt.Fprintf(w, "%s- %s%s\n", indent, label, suffix)
	for _, s := range n.Subjects {
		fmt.Fprintf(w, "%s    %s\n", indent, s)
	}
	for _, child := range n.Children {
		printNode(w, child, depth+1)
	}
}
