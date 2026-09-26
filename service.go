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

// Write is implemented in write.go.

// Read is implemented in read.go.

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

// Config administration methods (WriteConfig, ReadConfig, ListConfigs,
// ReadConfigVersion, ListConfigVersions) are implemented in configstore.go.
