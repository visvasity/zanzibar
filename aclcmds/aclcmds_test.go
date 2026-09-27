// Copyright (c) 2026 Visvasity LLC

package aclcmds_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/visvasity/cli"
	"github.com/visvasity/kv"
	"github.com/visvasity/kvmemdb"
	"github.com/visvasity/zanzibar"
	"github.com/visvasity/zanzibar/aclcmds"
)

const docConfigJSON = `{
  "namespace": "doc",
  "relations": {
    "owner": {"this": {}},
    "editor": {"union": {"children": [{"this": {}}, {"computedUserset": {"relation": "owner"}}]}},
    "viewer": {"union": {"children": [{"this": {}}, {"computedUserset": {"relation": "editor"}}]}}
  }
}`

// startServer mounts a Service's data and config handlers over loopback and
// returns their base URLs.
func startServer(t *testing.T) (dataURL, configURL string) {
	t.Helper()
	svc, err := zanzibar.New(kv.DatabaseFrom(kvmemdb.New()))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/authz/", http.StripPrefix("/authz", svc.Handler()))
	mux.Handle("/cfg/", http.StripPrefix("/cfg", svc.ConfigHandler()))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts.URL + "/authz/", ts.URL + "/cfg/"
}

// allCommands assembles the full ACL command set (mirroring aclcli's main).
func allCommands() []cli.Command {
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

// run executes one CLI command, capturing stdout, and fails on error.
func run(t *testing.T, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	ctx := cli.WithStdout(context.Background(), &buf)
	if err := cli.Run(ctx, allCommands(), args); err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return buf.String()
}

// runErr executes one CLI command and returns its error (stdout discarded).
func runErr(args ...string) error {
	var buf bytes.Buffer
	ctx := cli.WithStdout(context.Background(), &buf)
	return cli.Run(ctx, allCommands(), args)
}

func TestCLIEndToEnd(t *testing.T) {
	dataURL, configURL := startServer(t)

	// config write from a file.
	cfgFile := filepath.Join(t.TempDir(), "doc.json")
	if err := os.WriteFile(cfgFile, []byte(docConfigJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := run(t, "config", "write", "-acl-config-api-url", configURL, "-file", cfgFile); !strings.Contains(out, "version=1") {
		t.Errorf("config write out = %q, want version=1", out)
	}

	// grant + check (owner implies viewer).
	run(t, "grant", "-acl-api-url", dataURL, "doc:readme", "owner", "user:alice@example.com")
	if out := run(t, "check", "-acl-api-url", dataURL, "doc:readme", "viewer", "user:alice@example.com"); strings.TrimSpace(out) != "allowed" {
		t.Errorf("check alice = %q, want allowed", out)
	}
	if out := run(t, "check", "-acl-api-url", dataURL, "-exit-code=false", "doc:readme", "viewer", "user:bob@example.com"); strings.TrimSpace(out) != "denied" {
		t.Errorf("check bob = %q, want denied", out)
	}

	// list-tuples.
	if out := run(t, "list-tuples", "-acl-api-url", dataURL, "-object", "doc:readme"); !strings.Contains(out, "doc:readme#owner@user:alice@example.com") {
		t.Errorf("list-tuples = %q", out)
	}

	// list-objects / list-users.
	if out := run(t, "list-objects", "-acl-api-url", dataURL, "doc", "viewer", "user:alice@example.com"); strings.TrimSpace(out) != "readme" {
		t.Errorf("list-objects = %q, want readme", out)
	}
	if out := run(t, "list-users", "-acl-api-url", dataURL, "doc:readme", "viewer"); strings.TrimSpace(out) != "user:alice@example.com" {
		t.Errorf("list-users = %q, want alice", out)
	}

	// expand shows the rewrite structure.
	if out := run(t, "expand", "-acl-api-url", dataURL, "doc:readme", "viewer"); !strings.Contains(out, "union") || !strings.Contains(out, "computedUserset") {
		t.Errorf("expand = %q", out)
	}

	// revoke.
	if out := run(t, "revoke", "-acl-api-url", dataURL, "doc:readme", "owner", "user:alice@example.com"); !strings.Contains(out, "applied=1") {
		t.Errorf("revoke = %q", out)
	}
	if out := run(t, "check", "-acl-api-url", dataURL, "-exit-code=false", "doc:readme", "viewer", "user:alice@example.com"); strings.TrimSpace(out) != "denied" {
		t.Errorf("check after revoke = %q, want denied", out)
	}
	// By default a denied check exits non-zero.
	if err := runErr("check", "-acl-api-url", dataURL, "doc:readme", "viewer", "user:alice@example.com"); err == nil {
		t.Error("denied check with default -exit-code should return an error")
	}

	// config read / list.
	if out := run(t, "config", "read", "-acl-config-api-url", configURL, "doc"); !strings.Contains(out, "\"namespace\": \"doc\"") {
		t.Errorf("config read = %q", out)
	}
	if out := run(t, "config", "list", "-acl-config-api-url", configURL); !strings.Contains(out, "doc\tversion=1") {
		t.Errorf("config list = %q", out)
	}
}

func TestCLIDeleteObject(t *testing.T) {
	dataURL, configURL := startServer(t)
	// Seed a config and a few tuples on one object, then delete the object.
	cc := zanzibar.NewConfigClient(configURL, nil)
	if _, err := cc.WriteConfig(context.Background(), &zanzibar.NamespaceConfig{
		Namespace: "doc", Relations: map[string]zanzibar.Rewrite{
			"owner":  {This: &zanzibar.This{}},
			"viewer": {This: &zanzibar.This{}},
		}}); err != nil {
		t.Fatal(err)
	}
	run(t, "grant", "-acl-api-url", dataURL, "doc:d1", "owner", "user:a@x.com")
	run(t, "grant", "-acl-api-url", dataURL, "doc:d1", "viewer", "user:b@x.com")
	run(t, "grant", "-acl-api-url", dataURL, "doc:d2", "viewer", "user:c@x.com")

	if out := run(t, "delete-object", "-acl-api-url", dataURL, "doc:d1"); !strings.Contains(out, "deleted 2 grant") {
		t.Errorf("delete-object = %q, want 2 grants", out)
	}
	if out := run(t, "list-tuples", "-acl-api-url", dataURL, "-object", "doc:d1"); strings.TrimSpace(out) != "" {
		t.Errorf("doc:d1 still has tuples: %q", out)
	}
	if out := run(t, "list-tuples", "-acl-api-url", dataURL, "-object", "doc:d2"); !strings.Contains(out, "doc:d2#viewer@user:c@x.com") {
		t.Errorf("doc:d2 should be untouched: %q", out)
	}
}

func TestCLIErrors(t *testing.T) {
	dataURL, _ := startServer(t)

	// Missing -url.
	if err := runErr("check", "doc:x", "viewer", "user:a@b.com"); err == nil || !strings.Contains(err.Error(), "-acl-api-url is required") {
		t.Errorf("missing url err = %v, want -url required", err)
	}
	// Server-side validation error surfaces (InvalidArgument) across HTTP + CLI.
	if err := runErr("check", "-acl-api-url", dataURL, "nocolon", "viewer", "user:a@b.com"); err == nil || !strings.Contains(err.Error(), "InvalidArgument") {
		t.Errorf("bad object err = %v, want InvalidArgument", err)
	}
	// Wrong arg count.
	if err := runErr("grant", "-acl-api-url", dataURL, "doc:x", "viewer"); err == nil {
		t.Error("grant with 2 args should error")
	}
}
