// Copyright (c) 2026 Visvasity LLC

package aclcmds

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/visvasity/cli"
)

// ConfigWrite creates or updates namespace configs from a JSON or schema-DSL
// document.
type ConfigWrite struct {
	flags ConfigClientFlags

	file          string
	format        string
	expectVersion int
}

func (c *ConfigWrite) Purpose() string {
	return "Create or update namespace config(s) from JSON or schema DSL (compare-and-set on Version)"
}

func (c *ConfigWrite) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.flags.SetFlags(fset)
	fset.StringVar(&c.file, "file", "", "path to the config file, or '-'/empty for stdin")
	fset.StringVar(&c.format, "format", "", "input format: json or dsl (default: inferred from file extension, else json)")
	fset.IntVar(&c.expectVersion, "expect-version", -1, "expected current version for compare-and-set; -1 uses the config's version field (single-namespace only)")
	return "write", fset, c.run
}

func (c *ConfigWrite) run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("command takes no positional arguments")
	}
	data, err := readInput(c.file)
	if err != nil {
		return err
	}
	cfgs, err := decodeConfigs(data, resolveFormat(c.format, c.file))
	if err != nil {
		return err
	}
	if c.expectVersion >= 0 {
		if len(cfgs) != 1 {
			return fmt.Errorf("-expect-version requires exactly one namespace, got %d", len(cfgs))
		}
		cfgs[0].Version = uint64(c.expectVersion)
	}

	client, err := c.flags.Client()
	if err != nil {
		return err
	}
	out := cli.Stdout(ctx)
	for i := range cfgs {
		stored, err := client.WriteConfig(ctx, &cfgs[i])
		if err != nil {
			return fmt.Errorf("writing %q: %w", cfgs[i].Namespace, err)
		}
		fmt.Fprintf(out, "wrote config for %q (version=%d)\n", stored.Namespace, stored.Version)
	}
	return nil
}

// readInput reads the named file, or stdin when path is empty or "-".
func readInput(path string) ([]byte, error) {
	if path == "" || path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}
