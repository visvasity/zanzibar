// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// sampleDocConfig is the canonical doc namespace from SPEC §5.4.
func sampleDocConfig() *NamespaceConfig {
	return &NamespaceConfig{
		Namespace: "doc",
		Relations: map[string]Rewrite{
			"parent": {This: &This{}},
			"owner":  {This: &This{}},
			"editor": {Union: &SetOperation{Children: []Rewrite{
				{This: &This{}},
				{ComputedUserset: &ComputedUserset{Relation: "owner"}},
			}}},
			"viewer": {Union: &SetOperation{Children: []Rewrite{
				{This: &This{}},
				{ComputedUserset: &ComputedUserset{Relation: "editor"}},
				{TupleToUserset: &TupleToUserset{Tupleset: "parent", ComputedUserset: "viewer"}},
			}}},
		},
	}
}

func TestValidateConfigValid(t *testing.T) {
	if err := validateConfig(sampleDocConfig()); err != nil {
		t.Fatalf("validateConfig(sampleDoc) = %v, want nil", err)
	}
	// Empty top-level rewrite defaults to _this and is valid.
	cfg := &NamespaceConfig{Namespace: "n", Relations: map[string]Rewrite{"r": {}}}
	if err := validateConfig(cfg); err != nil {
		t.Errorf("validateConfig(empty rewrite) = %v, want nil", err)
	}
}

func TestValidateConfigRejections(t *testing.T) {
	tests := []struct {
		name string
		cfg  *NamespaceConfig
		code ErrorCode
	}{
		{
			"reserved namespace user",
			&NamespaceConfig{Namespace: "user", Relations: map[string]Rewrite{"member": {This: &This{}}}},
			CodeInvalidArgument,
		},
		{
			"bad namespace",
			&NamespaceConfig{Namespace: "bad ns", Relations: map[string]Rewrite{"r": {This: &This{}}}},
			CodeInvalidArgument,
		},
		{
			"bad relation name",
			&NamespaceConfig{Namespace: "n", Relations: map[string]Rewrite{"bad rel": {This: &This{}}}},
			CodeInvalidArgument,
		},
		{
			"multiple node kinds",
			&NamespaceConfig{Namespace: "n", Relations: map[string]Rewrite{
				"r": {This: &This{}, Union: &SetOperation{Children: []Rewrite{{This: &This{}}}}},
			}},
			CodeInvalidArgument,
		},
		{
			"empty union children",
			&NamespaceConfig{Namespace: "n", Relations: map[string]Rewrite{
				"r": {Union: &SetOperation{}},
			}},
			CodeInvalidArgument,
		},
		{
			"nested empty rewrite",
			&NamespaceConfig{Namespace: "n", Relations: map[string]Rewrite{
				"r": {Union: &SetOperation{Children: []Rewrite{{}}}},
			}},
			CodeInvalidArgument,
		},
		{
			"undeclared computed_userset",
			&NamespaceConfig{Namespace: "n", Relations: map[string]Rewrite{
				"r": {ComputedUserset: &ComputedUserset{Relation: "ghost"}},
			}},
			CodeRelationUndeclared,
		},
		{
			"undeclared tupleset",
			&NamespaceConfig{Namespace: "n", Relations: map[string]Rewrite{
				"r": {TupleToUserset: &TupleToUserset{Tupleset: "ghost", ComputedUserset: "viewer"}},
			}},
			CodeRelationUndeclared,
		},
		{
			"computed cycle a->b->a",
			&NamespaceConfig{Namespace: "n", Relations: map[string]Rewrite{
				"a": {ComputedUserset: &ComputedUserset{Relation: "b"}},
				"b": {ComputedUserset: &ComputedUserset{Relation: "a"}},
			}},
			CodeInvalidArgument,
		},
		{
			"computed self-cycle",
			&NamespaceConfig{Namespace: "n", Relations: map[string]Rewrite{
				"a": {ComputedUserset: &ComputedUserset{Relation: "a"}},
			}},
			CodeInvalidArgument,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.cfg)
			if err == nil {
				t.Fatalf("validateConfig = nil, want error")
			}
			if !errors.Is(err, sentinelForCode(tt.code)) {
				t.Errorf("error = %v, want code %s", err, tt.code)
			}
		})
	}
}

// tupleToUserset with an undeclared *computed_userset* target (evaluated on the
// parent namespace) is allowed at config time; only the tupleset must be local.
func TestValidateConfigCrossNamespaceComputedAllowed(t *testing.T) {
	cfg := &NamespaceConfig{Namespace: "doc", Relations: map[string]Rewrite{
		"parent": {This: &This{}},
		"viewer": {TupleToUserset: &TupleToUserset{Tupleset: "parent", ComputedUserset: "viewer_on_folder"}},
	}}
	if err := validateConfig(cfg); err != nil {
		t.Errorf("validateConfig = %v, want nil (cross-namespace computed target ok)", err)
	}
}

func TestWriteConfigCreateAndRead(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)

	stored, err := svc.WriteConfig(ctx, sampleDocConfig())
	if err != nil {
		t.Fatalf("WriteConfig create: %v", err)
	}
	if stored.Version != 1 {
		t.Errorf("created version = %d, want 1", stored.Version)
	}

	got, err := svc.ReadConfig(ctx, "doc")
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if got.Version != 1 || len(got.Relations) != 4 {
		t.Errorf("ReadConfig = version %d, %d relations; want 1, 4", got.Version, len(got.Relations))
	}
}

func TestReadConfigUnregistered(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	_, err := svc.ReadConfig(ctx, "nope")
	if !errors.Is(err, ErrNamespaceUnregistered) {
		t.Errorf("ReadConfig(unregistered) = %v, want ErrNamespaceUnregistered", err)
	}
}

func TestWriteConfigCASCreateConflict(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	if _, err := svc.WriteConfig(ctx, sampleDocConfig()); err != nil {
		t.Fatal(err)
	}
	// A second create (Version 0) for an existing namespace must conflict.
	_, err := svc.WriteConfig(ctx, sampleDocConfig())
	if !errors.Is(err, ErrConflict) {
		t.Errorf("second create = %v, want ErrConflict", err)
	}
}

func TestWriteConfigCASUpdate(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	v1, err := svc.WriteConfig(ctx, sampleDocConfig())
	if err != nil {
		t.Fatal(err)
	}

	// Stale version -> conflict.
	stale := sampleDocConfig()
	stale.Version = 0
	if _, err := svc.WriteConfig(ctx, stale); !errors.Is(err, ErrConflict) {
		t.Errorf("stale update = %v, want ErrConflict", err)
	}

	// Correct version -> v2.
	upd := sampleDocConfig()
	upd.Version = v1.Version // 1
	upd.Relations["auditor"] = Rewrite{This: &This{}}
	v2, err := svc.WriteConfig(ctx, upd)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if v2.Version != 2 {
		t.Errorf("updated version = %d, want 2", v2.Version)
	}
}

func TestConfigHistory(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)

	v1, _ := svc.WriteConfig(ctx, sampleDocConfig())
	upd := sampleDocConfig()
	upd.Version = v1.Version
	upd.Relations["auditor"] = Rewrite{This: &This{}}
	if _, err := svc.WriteConfig(ctx, upd); err != nil {
		t.Fatal(err)
	}

	// Versions listed ascending.
	versions, err := svc.ListConfigVersions(ctx, "doc")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0] != 1 || versions[1] != 2 {
		t.Errorf("ListConfigVersions = %v, want [1 2]", versions)
	}

	// Old version content is immutable: v1 has no auditor relation.
	old, err := svc.ReadConfigVersion(ctx, "doc", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := old.Relations["auditor"]; ok {
		t.Error("history v1 unexpectedly contains 'auditor' (history not immutable)")
	}
	// Current has it.
	cur, _ := svc.ReadConfig(ctx, "doc")
	if _, ok := cur.Relations["auditor"]; !ok {
		t.Error("current config missing 'auditor'")
	}
}

func TestReadConfigVersionMissing(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	svc.WriteConfig(ctx, sampleDocConfig())
	if _, err := svc.ReadConfigVersion(ctx, "doc", 99); err == nil {
		t.Error("ReadConfigVersion(99) = nil error, want error")
	}
}

func TestListConfigsOrder(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	for _, ns := range []string{"folder", "doc", "group"} {
		cfg := &NamespaceConfig{Namespace: ns, Relations: map[string]Rewrite{"r": {This: &This{}}}}
		if _, err := svc.WriteConfig(ctx, cfg); err != nil {
			t.Fatal(err)
		}
	}
	got, err := svc.ListConfigs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"doc", "folder", "group"} // key (namespace) order
	if len(got) != 3 {
		t.Fatalf("ListConfigs len = %d, want 3", len(got))
	}
	for i := range want {
		if got[i].Namespace != want[i] {
			t.Errorf("ListConfigs[%d] = %q, want %q", i, got[i].Namespace, want[i])
		}
	}
}

// TestConcurrentUpdatesOneWins exercises SSI on the update path (which reads an
// existing head record): exactly one concurrent CAS update succeeds; the rest
// get a conflict.
func TestConcurrentUpdatesOneWins(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	if _, err := svc.WriteConfig(ctx, sampleDocConfig()); err != nil {
		t.Fatal(err)
	}

	const n = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	var mu sync.Mutex
	success, conflict := 0, 0

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			upd := sampleDocConfig()
			upd.Version = 1 // all race to move v1 -> v2
			<-start
			_, err := svc.WriteConfig(ctx, upd)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				success++
			case errors.Is(err, ErrConflict):
				conflict++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if success != 1 {
		t.Errorf("successful updates = %d, want exactly 1", success)
	}
	if conflict != n-1 {
		t.Errorf("conflicts = %d, want %d", conflict, n-1)
	}
	cur, _ := svc.ReadConfig(ctx, "doc")
	if cur.Version != 2 {
		t.Errorf("final version = %d, want 2", cur.Version)
	}
}
