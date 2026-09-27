// Copyright (c) 2026 Visvasity LLC

// Command aclcli is a standalone tool to manage, inspect, and investigate
// Zanzibar ACL data through a running zanzibar service's HTTP API (the
// -acl-api-url / -acl-config-api-url flags). It assembles the reusable commands
// from github.com/visvasity/zanzibar/aclcmds, which applications embedding
// github.com/visvasity/zanzibar can likewise construct and mount into their own
// CLI.
package main

import (
	"context"
	"log"
	"os"

	"github.com/visvasity/cli"
	"github.com/visvasity/zanzibar/aclcmds"
)

// commands assembles the full ACL command set: the data-plane commands plus a
// "config" group for namespace-config administration.
func commands() []cli.Command {
	return []cli.Command{
		new(aclcmds.Grant),
		new(aclcmds.Revoke),
		new(aclcmds.DeleteObject),
		new(aclcmds.Check),
		new(aclcmds.ListTuples),
		new(aclcmds.Expand),
		new(aclcmds.ListObjects),
		new(aclcmds.ListUsers),
		cli.NewGroup("config", "Manage namespace configs",
			new(aclcmds.ConfigWrite),
			new(aclcmds.ConfigRead),
			new(aclcmds.ConfigList),
			new(aclcmds.ConfigReadVersion),
			new(aclcmds.ConfigListVersions),
			new(aclcmds.ConfigCompile),
		),
	}
}

func main() {
	if err := cli.Run(context.Background(), commands(), os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}
