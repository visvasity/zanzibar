// Copyright (c) 2026 Visvasity LLC

package aclcmds

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/visvasity/cli"
	"github.com/visvasity/zanzibar"
)

// ConfigWrite creates or updates a namespace config from a JSON document.
type ConfigWrite struct {
	flags ConfigClientFlags

	file          string
	expectVersion int
}

func (c *ConfigWrite) Purpose() string {
	return "Create or update a namespace config from JSON (compare-and-set on Version)"
}

func (c *ConfigWrite) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.flags.SetFlags(fset)
	fset.StringVar(&c.file, "file", "", "path to a JSON NamespaceConfig, or '-'/empty for stdin")
	fset.IntVar(&c.expectVersion, "expect-version", -1, "expected current version for compare-and-set; -1 uses the JSON's version field")
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
	var cfg zanzibar.NamespaceConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("could not parse config JSON: %w", err)
	}
	if c.expectVersion >= 0 {
		cfg.Version = uint64(c.expectVersion)
	}

	client, err := c.flags.Client()
	if err != nil {
		return err
	}
	stored, err := client.WriteConfig(ctx, &cfg)
	if err != nil {
		return err
	}
	fmt.Fprintf(cli.Stdout(ctx), "wrote config for %q (version=%d)\n", stored.Namespace, stored.Version)
	return nil
}

// readInput reads the named file, or stdin when path is empty or "-".
func readInput(path string) ([]byte, error) {
	if path == "" || path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}
