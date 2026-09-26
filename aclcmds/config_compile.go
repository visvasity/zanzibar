// Copyright (c) 2026 Visvasity LLC

package aclcmds

import (
	"context"
	"flag"
	"fmt"

	"github.com/visvasity/cli"
	"github.com/visvasity/zanzibar/schema"
)

// ConfigCompile converts a config between the schema DSL and JSON locally, with
// no server. It is useful to author in the DSL, inspect the generated JSON, or
// pretty-print an existing schema.
type ConfigCompile struct {
	file string
	from string
	to   string
}

func (c *ConfigCompile) Purpose() string {
	return "Convert a config between schema DSL and JSON (offline; no server needed)"
}

func (c *ConfigCompile) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	fset.StringVar(&c.file, "file", "", "input file, or '-'/empty for stdin")
	fset.StringVar(&c.from, "from", "", "input format: json or dsl (default: inferred from extension, else dsl)")
	fset.StringVar(&c.to, "to", "json", "output format: json or dsl")
	return "compile", fset, c.run
}

func (c *ConfigCompile) run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("command takes no positional arguments")
	}
	data, err := readInput(c.file)
	if err != nil {
		return err
	}
	from := c.from
	if from == "" {
		// Default input to dsl unless the extension says json.
		if resolveFormat("", c.file) == "json" && c.file != "" {
			from = "json"
		} else {
			from = "dsl"
		}
	}
	cfgs, err := decodeConfigs(data, from)
	if err != nil {
		return err
	}

	out := cli.Stdout(ctx)
	switch c.to {
	case "dsl":
		_, err = out.Write(schema.Format(cfgs))
		return err
	case "json":
		return printJSON(out, cfgs)
	default:
		return fmt.Errorf("unknown -to format %q (want json or dsl)", c.to)
	}
}
