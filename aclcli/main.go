// Copyright (c) 2026 Visvasity LLC

// Command aclcli is a standalone tool to manage, inspect, and investigate
// Zanzibar ACL data through a running zanzibar service's HTTP API (the -url
// flag). It is the same command set that applications embedding
// github.com/visvasity/zanzibar can import from
// github.com/visvasity/zanzibar/aclcmds and mount into their own CLI.
package main

import (
	"context"
	"log"
	"os"

	"github.com/visvasity/cli"
	"github.com/visvasity/zanzibar/aclcmds"
)

func main() {
	if err := cli.Run(context.Background(), aclcmds.Commands(), os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}
