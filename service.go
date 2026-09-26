// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"

	"github.com/visvasity/kv"
)

// Default construction settings (§5.5, §6.4, §9.3, §11.1).
const (
	// DefaultKeyPrefix is the key prefix used when [WithKeyPrefix] is not given.
	// The "1" is a layout-version marker reserved for future migrations.
	DefaultKeyPrefix = "zz1/"

	// DefaultMaxDepth is the recursion-depth limit used when [WithMaxDepth] is
	// not given.
	DefaultMaxDepth = 100

	// DefaultCommitRetries is the number of transaction-commit-conflict retries
	// used when [WithCommitRetries] is not given.
	DefaultCommitRetries = 3
)

// Service is the access-control engine. It reads and writes all state through a
// kv.Database and is safe for concurrent use by multiple goroutines. Construct
// one with [New]; the zero value is not usable.
type Service struct {
	db   kv.Database
	opts options
}

// Option customizes a [Service] at construction time.
type Option func(*options) error

// options holds resolved construction settings.
type options struct {
	keyPrefix     string
	maxDepth      int
	commitRetries int
	emailCaseFold bool
}

func defaultOptions() options {
	return options{
		keyPrefix:     DefaultKeyPrefix,
		maxDepth:      DefaultMaxDepth,
		commitRetries: DefaultCommitRetries,
		emailCaseFold: false,
	}
}

// New constructs a Service backed by db. Namespace configuration lives only in
// db and is created/evolved via [Service.WriteConfig]; New registers no schema
// and a fresh database has no namespaces until one is written (§5.5).
func New(db kv.Database, opts ...Option) (*Service, error) {
	if db == nil {
		return nil, &Error{Code: CodeInvalidArgument, Message: "nil kv.Database"}
	}
	o := defaultOptions()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&o); err != nil {
			return nil, err
		}
	}
	if o.keyPrefix == "" {
		return nil, &Error{Code: CodeInvalidArgument, Message: "empty key prefix"}
	}
	if o.maxDepth <= 0 {
		return nil, &Error{Code: CodeInvalidArgument, Message: "max depth must be positive"}
	}
	if o.commitRetries < 0 {
		return nil, &Error{Code: CodeInvalidArgument, Message: "commit retries must not be negative"}
	}
	return &Service{db: db, opts: o}, nil
}

// WithKeyPrefix overrides the key prefix under which all records are stored.
// The default is "zz1/". All keys used by the Service live under this prefix.
// The prefix must be non-empty.
func WithKeyPrefix(prefix string) Option {
	return func(o *options) error {
		if prefix == "" {
			return &Error{Code: CodeInvalidArgument, Message: "empty key prefix"}
		}
		o.keyPrefix = prefix
		return nil
	}
}

// WithMaxDepth sets the maximum recursion depth for Check and Expand. Exceeding
// it fails the operation (fail-closed) rather than returning a decision. The
// default is 100. n must be positive.
func WithMaxDepth(n int) Option {
	return func(o *options) error {
		if n <= 0 {
			return &Error{Code: CodeInvalidArgument, Message: "max depth must be positive"}
		}
		o.maxDepth = n
		return nil
	}
}

// WithCommitRetries bounds the number of times a mutating operation retries on
// a transaction-commit conflict before returning a conflict error. n must not
// be negative (0 disables retries).
func WithCommitRetries(n int) Option {
	return func(o *options) error {
		if n < 0 {
			return &Error{Code: CodeInvalidArgument, Message: "commit retries must not be negative"}
		}
		o.commitRetries = n
		return nil
	}
}

// WithEmailCaseFold enables case-folding of user emails, applied consistently
// to writes and checks. It is off by default; the library otherwise treats
// emails as opaque identifiers.
func WithEmailCaseFold(fold bool) Option {
	return func(o *options) error {
		o.emailCaseFold = fold
		return nil
	}
}

// Check reports whether the request's subject is a member of the userset
// Object#Relation. It evaluates against a single point-in-time snapshot and is
// fail-closed: any error or exceeded depth denies rather than allows.
func (s *Service) Check(ctx context.Context, req *CheckRequest) (*CheckResponse, error) {
	panic("not implemented")
}

// Write applies an ordered batch of grants and revokes atomically in one
// transaction. If any mutation is invalid or any precondition fails, the whole
// batch is aborted and no change is made.
func (s *Service) Write(ctx context.Context, req *WriteRequest) (*WriteResponse, error) {
	panic("not implemented")
}

// Read returns stored tuples (not computed membership) matching the request's
// filter, in deterministic key order, with cursor-based pagination.
func (s *Service) Read(ctx context.Context, req *ReadRequest) (*ReadResponse, error) {
	panic("not implemented")
}

// Expand returns the userset tree for Object#Relation without flattening it to
// leaf users. Nodes cut off by the cycle guard or the depth limit are marked
// truncated.
func (s *Service) Expand(ctx context.Context, req *ExpandRequest) (*ExpandResponse, error) {
	panic("not implemented")
}

// ListObjects returns the objects in the request's namespace on which the
// subject holds the relation. The result is sound and complete relative to
// Check for the snapshot taken, subject to the depth limit, and is paginated.
func (s *Service) ListObjects(ctx context.Context, req *ListObjectsRequest) (*ListObjectsResponse, error) {
	panic("not implemented")
}

// ListUsers returns the user subjects that are members of Object#Relation,
// flattening usersets and inheritance but not expanding a "user:*" wildcard.
// The result is paginated.
func (s *Service) ListUsers(ctx context.Context, req *ListUsersRequest) (*ListUsersResponse, error) {
	panic("not implemented")
}

// WriteConfig creates or updates the stored [NamespaceConfig] for its
// namespace, validating it before commit. It performs a compare-and-set on
// cfg.Version (§5.5): 0 creates the namespace and fails if one already exists;
// a non-zero value updates only if it equals the currently stored version. A
// version mismatch returns a conflict error ([CodeConflict]) and changes
// nothing; the caller re-reads and retries. On success it returns the stored
// config carrying its newly assigned Version. This is a privileged
// administrative operation; the caller is responsible for authorizing it.
func (s *Service) WriteConfig(ctx context.Context, cfg *NamespaceConfig) (*NamespaceConfig, error) {
	panic("not implemented")
}

// ReadConfig returns the stored [NamespaceConfig] for the namespace, including
// its current Version. It reports an error if no config is stored for it.
func (s *Service) ReadConfig(ctx context.Context, namespace string) (*NamespaceConfig, error) {
	panic("not implemented")
}

// ListConfigs returns all configs stored in the database, each with its Version.
func (s *Service) ListConfigs(ctx context.Context) ([]NamespaceConfig, error) {
	panic("not implemented")
}

// ReadConfigVersion returns the immutable [NamespaceConfig] stored at a specific
// past version of the namespace (§5.5, §11.2). It reports an error if that
// version was never written.
func (s *Service) ReadConfigVersion(ctx context.Context, namespace string, version uint64) (*NamespaceConfig, error) {
	panic("not implemented")
}

// ListConfigVersions returns the stored versions for the namespace in ascending
// order.
func (s *Service) ListConfigVersions(ctx context.Context, namespace string) ([]uint64, error) {
	panic("not implemented")
}
