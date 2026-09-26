// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/visvasity/httphelp"
)

// startHTTP mounts a fresh Service's data and config handlers at disjoint
// prefixes on an httphelp server over plaintext loopback TCP, returning typed
// clients for each.
func startHTTP(t *testing.T) (*Client, *ConfigClient) {
	t.Helper()
	svc := newTestService(t)
	server, err := httphelp.NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { server.Close() })

	server.Handle("/authz/", http.StripPrefix("/authz", svc.Handler()))
	server.Handle("/cfg/", http.StripPrefix("/cfg", svc.ConfigHandler()))
	addr, err := server.StartTCP("127.0.0.1:0", false)
	if err != nil {
		t.Fatalf("StartTCP: %v", err)
	}
	base := "http://" + addr.String()
	return NewClient(base+"/authz/", nil), NewConfigClient(base+"/cfg/", nil)
}

func TestHTTPRoundTrip(t *testing.T) {
	ctx := context.Background()
	c, cc := startHTTP(t)

	// Config administration over the config plane.
	stored, err := cc.WriteConfig(ctx, sampleDocConfig())
	if err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	if stored.Version != 1 {
		t.Errorf("created version = %d, want 1", stored.Version)
	}
	for _, cfg := range []*NamespaceConfig{groupConfig(), folderConfig()} {
		if _, err := cc.WriteConfig(ctx, cfg); err != nil {
			t.Fatalf("WriteConfig(%s): %v", cfg.Namespace, err)
		}
	}

	// Grant + check on the data plane.
	if _, err := c.Write(ctx, grantReq("doc:readme", "viewer", "user:alice@example.com")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if resp, err := c.Check(ctx, &CheckRequest{Object: "doc:readme", Relation: "viewer", Subject: "user:alice@example.com"}); err != nil || !resp.Allowed {
		t.Errorf("Check(alice) = (%v, %v), want (Allowed true, nil)", resp, err)
	}
	// Allowed:false is a normal decision, not an error.
	if resp, err := c.Check(ctx, &CheckRequest{Object: "doc:readme", Relation: "viewer", Subject: "user:bob@example.com"}); err != nil || resp.Allowed {
		t.Errorf("Check(bob) = (%v, %v), want (Allowed false, nil)", resp, err)
	}

	// Read.
	rd, err := c.Read(ctx, &ReadRequest{Object: "doc:readme"})
	if err != nil || len(rd.Records) != 1 {
		t.Errorf("Read = (%v, %v), want 1 record", rd, err)
	}

	// Expand.
	ex, err := c.Expand(ctx, &ExpandRequest{Object: "doc:readme", Relation: "viewer"})
	if err != nil || ex.Tree.Kind != NodeUnion {
		t.Errorf("Expand kind = %v (err %v), want union", ex.Tree.Kind, err)
	}

	// ListObjects / ListUsers.
	lo, err := c.ListObjects(ctx, &ListObjectsRequest{Namespace: "doc", Relation: "viewer", Subject: "user:alice@example.com"})
	if err != nil || len(lo.ObjectIDs) != 1 || lo.ObjectIDs[0] != "readme" {
		t.Errorf("ListObjects = (%v, %v), want [readme]", lo, err)
	}
	lu, err := c.ListUsers(ctx, &ListUsersRequest{Object: "doc:readme", Relation: "viewer"})
	if err != nil || len(lu.Users) != 1 || lu.Users[0] != "user:alice@example.com" {
		t.Errorf("ListUsers = (%v, %v), want [alice]", lu, err)
	}

	// Config reads on the config plane.
	if got, err := cc.ReadConfig(ctx, "doc"); err != nil || got.Version != 1 {
		t.Errorf("ReadConfig = (%v, %v), want version 1", got, err)
	}
	if cfgs, err := cc.ListConfigs(ctx); err != nil || len(cfgs) != 3 {
		t.Errorf("ListConfigs = (%d configs, %v), want 3", len(cfgs), err)
	}
	if got, err := cc.ReadConfigVersion(ctx, "doc", 1); err != nil || got.Version != 1 {
		t.Errorf("ReadConfigVersion = (%v, %v), want version 1", got, err)
	}
	if vs, err := cc.ListConfigVersions(ctx, "doc"); err != nil || len(vs) != 1 || vs[0] != 1 {
		t.Errorf("ListConfigVersions = (%v, %v), want [1]", vs, err)
	}
}

func TestHTTPErrorPropagation(t *testing.T) {
	ctx := context.Background()
	c, cc := startHTTP(t)

	// Malformed request -> InvalidArgument reconstructed on the client side.
	if _, err := c.Check(ctx, &CheckRequest{Object: "nocolon", Relation: "viewer", Subject: "user:a@b.com"}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("bad object over HTTP = %v, want ErrInvalidArgument", err)
	}

	// Unregistered namespace read -> NamespaceUnregistered.
	if _, err := cc.ReadConfig(ctx, "ghost"); !errors.Is(err, ErrNamespaceUnregistered) {
		t.Errorf("ReadConfig(ghost) = %v, want ErrNamespaceUnregistered", err)
	}

	// CAS create conflict -> Conflict.
	if _, err := cc.WriteConfig(ctx, sampleDocConfig()); err != nil {
		t.Fatal(err)
	}
	if _, err := cc.WriteConfig(ctx, sampleDocConfig()); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate create over HTTP = %v, want ErrConflict", err)
	}
}

func TestHTTPErrorPreservesContext(t *testing.T) {
	ctx := context.Background()
	c, cc := startHTTP(t)
	if _, err := cc.WriteConfig(ctx, sampleDocConfig()); err != nil {
		t.Fatal(err)
	}
	// A write against an undeclared relation should surface as *Error with the
	// relation context intact across the wire.
	_, err := c.Write(ctx, grantReq("doc:readme", "ghostrel", "user:a@b.com"))
	var ze *Error
	if !errors.As(err, &ze) {
		t.Fatalf("err = %v, want *Error", err)
	}
	if ze.Code != CodeRelationUndeclared || ze.Relation != "ghostrel" {
		t.Errorf("wire error = %+v, want RelationUndeclared with relation ghostrel", ze)
	}
}
