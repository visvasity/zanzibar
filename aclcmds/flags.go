// Copyright (c) 2026 Visvasity LLC

// Package aclcmds provides reusable github.com/visvasity/cli commands that
// manage, inspect, and investigate Zanzibar ACL data. The commands never touch
// a database directly; they operate entirely through a running zanzibar service's
// HTTP API (see zanzibar.Service.Handler and Service.ConfigHandler), whose base
// URL is supplied with the -acl-api-url (data plane) and -acl-config-api-url
// (config plane) flags. This keeps the package light (only the core zanzibar
// client and the cli framework) and lets operators manage a live service without
// direct database access.
//
// The commands use http.DefaultClient; an importing application can tune
// transport parameters (timeouts, TLS, proxies) by configuring http.DefaultClient
// or its default transport before running the commands. The package deliberately
// defines no transport flags of its own, to avoid colliding with application-level
// flags.
//
// Mount them into an application's CLI, for example:
//
//	acl := cli.NewGroup("acl", "Zanzibar ACL operations", aclcmds.Commands()...)
//	cli.Run(ctx, []cli.Command{ ..., acl }, os.Args[1:])
package aclcmds

import (
	"errors"
	"flag"
	"time"

	"github.com/visvasity/zanzibar"
)

// ClientFlags supplies the base URL of the zanzibar data-plane API
// (zanzibar.Service.Handler) via the -acl-api-url flag.
type ClientFlags struct {
	url string
}

// SetFlags registers the -acl-api-url flag on fset.
func (v *ClientFlags) SetFlags(fset *flag.FlagSet) {
	fset.StringVar(&v.url, "acl-api-url", "", "base URL of the zanzibar data API, e.g. https://host/api/authz/")
}

// Client builds a data-plane client from the flags, using http.DefaultClient.
func (v *ClientFlags) Client() (*zanzibar.Client, error) {
	if v.url == "" {
		return nil, errors.New("-acl-api-url is required")
	}
	return zanzibar.NewClient(v.url, nil), nil
}

// ConfigClientFlags supplies the base URL of the zanzibar config-plane API
// (zanzibar.Service.ConfigHandler) via the -acl-config-api-url flag.
type ConfigClientFlags struct {
	url string
}

// SetFlags registers the -acl-config-api-url flag on fset.
func (v *ConfigClientFlags) SetFlags(fset *flag.FlagSet) {
	fset.StringVar(&v.url, "acl-config-api-url", "", "base URL of the zanzibar config API, e.g. https://host/api/authz-admin/")
}

// Client builds a config-plane client from the flags, using http.DefaultClient.
func (v *ConfigClientFlags) Client() (*zanzibar.ConfigClient, error) {
	if v.url == "" {
		return nil, errors.New("-acl-config-api-url is required")
	}
	return zanzibar.NewConfigClient(v.url, nil), nil
}

// parseTime converts an optional RFC3339 timestamp into a Unix-nanosecond value,
// returning 0 (unbounded) for the empty string.
func parseTime(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0, err
	}
	return t.UnixNano(), nil
}
