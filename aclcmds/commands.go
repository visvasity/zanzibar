// Copyright (c) 2026 Visvasity LLC

package aclcmds

import "github.com/visvasity/cli"

// Commands returns the full set of ACL commands (the data-plane commands plus
// the "config" group), for an application to mount into its CLI — typically
// inside its own group:
//
//	acl := cli.NewGroup("acl", "Zanzibar ACL operations", aclcmds.Commands()...)
func Commands() []cli.Command {
	return []cli.Command{
		new(Grant),
		new(Revoke),
		new(DeleteObject),
		new(Check),
		new(ListTuples),
		new(Expand),
		new(ListObjects),
		new(ListUsers),
		ConfigGroup(),
	}
}

// Group returns all ACL commands wrapped in a single named cli group.
func Group(name, purpose string) cli.Command {
	return cli.NewGroup(name, purpose, Commands()...)
}
