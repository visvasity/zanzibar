// Copyright (c) 2026 Visvasity LLC

package aclcmds

import (
	"context"
	"flag"
	"fmt"

	"github.com/visvasity/cli"
	"github.com/visvasity/zanzibar"
)

// ListTuples prints stored relation tuples matching a filter.
type ListTuples struct {
	flags ClientFlags

	object    string
	namespace string
	relation  string
	subject   string
	pageSize  int
}

func (c *ListTuples) Purpose() string {
	return "List stored relation tuples matching a filter"
}

func (c *ListTuples) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.flags.SetFlags(fset)
	fset.StringVar(&c.object, "object", "", "filter by exact object (namespace:id)")
	fset.StringVar(&c.namespace, "namespace", "", "filter by object namespace")
	fset.StringVar(&c.relation, "relation", "", "filter by exact relation")
	fset.StringVar(&c.subject, "subject", "", "filter by exact subject")
	fset.IntVar(&c.pageSize, "page-size", 0, "page size per underlying request (0 = server default)")
	return "list-tuples", fset, c.run
}

func (c *ListTuples) run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("command takes no positional arguments")
	}
	client, err := c.flags.Client()
	if err != nil {
		return err
	}
	out := cli.Stdout(ctx)
	token := ""
	for {
		resp, err := client.Read(ctx, &zanzibar.ReadRequest{
			Object:    c.object,
			Namespace: c.namespace,
			Relation:  c.relation,
			Subject:   c.subject,
			PageSize:  c.pageSize,
			PageToken: token,
		})
		if err != nil {
			return err
		}
		for _, r := range resp.Records {
			fmt.Fprintf(out, "%s#%s@%s%s\n", r.Tuple.Object, r.Tuple.Relation, r.Tuple.Subject, intervalSuffix(r))
		}
		if resp.NextPageToken == "" {
			return nil
		}
		token = resp.NextPageToken
	}
}

// intervalSuffix renders a tuple's metadata annotations, if any.
func intervalSuffix(r zanzibar.TupleRecord) string {
	if r.NotBeforeUnixNano == 0 && r.NotAfterUnixNano == 0 {
		return ""
	}
	return fmt.Sprintf(" [not-before=%d not-after=%d]", r.NotBeforeUnixNano, r.NotAfterUnixNano)
}
