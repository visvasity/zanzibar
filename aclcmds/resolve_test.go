// Copyright (c) 2026 Visvasity LLC

package aclcmds_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/visvasity/cli"
	"github.com/visvasity/zanzibar"
	"github.com/visvasity/zanzibar/aclcmds"
)

// mappedCommands builds the command set with a resolver (human email ->
// synthetic), a classifier (suffix on the identity domain), and a formatter
// (synthetic -> human email). Unknown values pass through unchanged.
func mappedCommands() []cli.Command {
	synthByEmail := map[string]string{"alice@example.com": "acct1@id.test.invalid"}
	emailBySynth := map[string]string{"acct1@id.test.invalid": "alice@example.com"}
	opts := aclcmds.SubjectOptions{
		IsSynthetic: func(id string) bool {
			return strings.HasSuffix(id, "@id.test.invalid")
		},
		Resolve: func(_ context.Context, email string) (string, error) {
			if s, ok := synthByEmail[email]; ok {
				return s, nil
			}
			return email, nil // logical miss -> passthrough (a real app would log)
		},
		Format: func(_ context.Context, id string) (string, error) {
			if e, ok := emailBySynth[id]; ok {
				return e, nil
			}
			return id, nil
		},
	}
	cmds := allCommands()
	for _, c := range cmds {
		if s, ok := c.(interface{ SetSubjectOptions(aclcmds.SubjectOptions) }); ok {
			s.SetSubjectOptions(opts)
		}
	}
	return cmds
}

func runM(t *testing.T, cmds []cli.Command, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	ctx := cli.WithStdout(context.Background(), &buf)
	if err := cli.Run(ctx, cmds, args); err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return buf.String()
}

func TestSubjectMapping(t *testing.T) {
	dataURL, configURL := startServer(t)
	ctx := context.Background()

	cfgFile := filepath.Join(t.TempDir(), "doc.json")
	if err := os.WriteFile(cfgFile, []byte(docConfigJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, "config", "write", "-acl-config-api-url", configURL, "-file", cfgFile)

	cmds := mappedCommands()
	raw := zanzibar.NewClient(mustURL(t, dataURL), nil)

	// Grant with a human email: stored subject must be the resolved synthetic.
	runM(t, cmds, "grant", "-acl-api-url", dataURL, "doc:readme", "owner", "user:alice@example.com")
	resp, err := raw.Read(ctx, &zanzibar.ReadRequest{Object: "doc:readme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Records) != 1 || resp.Records[0].Tuple.Subject != "user:acct1@id.test.invalid" {
		t.Fatalf("stored subject = %+v, want user:acct1@id.test.invalid", resp.Records)
	}

	// Check with the human email resolves the same way and is allowed.
	if out := runM(t, cmds, "check", "-acl-api-url", dataURL, "doc:readme", "viewer", "user:alice@example.com"); strings.TrimSpace(out) != "allowed" {
		t.Errorf("check = %q, want allowed", out)
	}

	// list-users formats the synthetic back to the human email.
	if out := runM(t, cmds, "list-users", "-acl-api-url", dataURL, "doc:readme", "viewer"); strings.TrimSpace(out) != "user:alice@example.com" {
		t.Errorf("list-users = %q, want user:alice@example.com", out)
	}

	// An already-synthetic subject passes through unchanged (idempotent).
	runM(t, cmds, "grant", "-acl-api-url", dataURL, "doc:readme", "owner", "user:acct1@id.test.invalid")
	if resp, err = raw.Read(ctx, &zanzibar.ReadRequest{Object: "doc:readme", Relation: "owner"}); err != nil {
		t.Fatal(err)
	}
	if len(resp.Records) != 1 {
		t.Errorf("re-grant of synthetic changed tuple count: %+v", resp.Records)
	}

	// An unknown human email passes through as user:<email> (matches legacy
	// email-based grants during migration).
	runM(t, cmds, "grant", "-acl-api-url", dataURL, "doc:other", "owner", "user:ghost@example.com")
	resp, err = raw.Read(ctx, &zanzibar.ReadRequest{Object: "doc:other"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Records) != 1 || resp.Records[0].Tuple.Subject != "user:ghost@example.com" {
		t.Errorf("unknown email stored as %+v, want user:ghost@example.com", resp.Records)
	}
}

func TestNoMappingByDefault(t *testing.T) {
	// Without options, the human email is stored verbatim (no resolver).
	dataURL, configURL := startServer(t)
	ctx := context.Background()

	cfgFile := filepath.Join(t.TempDir(), "doc.json")
	if err := os.WriteFile(cfgFile, []byte(docConfigJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, "config", "write", "-acl-config-api-url", configURL, "-file", cfgFile)

	run(t, "grant", "-acl-api-url", dataURL, "doc:readme", "owner", "user:alice@example.com")
	raw := zanzibar.NewClient(mustURL(t, dataURL), nil)
	resp, err := raw.Read(ctx, &zanzibar.ReadRequest{Object: "doc:readme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Records) != 1 || resp.Records[0].Tuple.Subject != "user:alice@example.com" {
		t.Errorf("default stored subject = %+v, want unchanged email", resp.Records)
	}
}

// Without a classifier, mapping is disabled: the resolver is ignored and the
// subject is stored verbatim.
func TestResolverIgnoredWithoutClassifier(t *testing.T) {
	dataURL, configURL := startServer(t)
	ctx := context.Background()

	cfgFile := filepath.Join(t.TempDir(), "doc.json")
	if err := os.WriteFile(cfgFile, []byte(docConfigJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, "config", "write", "-acl-config-api-url", configURL, "-file", cfgFile)

	g := new(aclcmds.Grant)
	g.SetSubjectOptions(aclcmds.SubjectOptions{
		Resolve: func(_ context.Context, _ string) (string, error) { return "SHOULD_NOT_BE_USED", nil },
	})
	runM(t, []cli.Command{g}, "grant", "-acl-api-url", dataURL, "doc:readme", "owner", "user:alice@example.com")

	raw := zanzibar.NewClient(mustURL(t, dataURL), nil)
	resp, err := raw.Read(ctx, &zanzibar.ReadRequest{Object: "doc:readme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Records) != 1 || resp.Records[0].Tuple.Subject != "user:alice@example.com" {
		t.Errorf("stored subject = %+v, want unchanged user:alice@example.com", resp.Records)
	}
}
