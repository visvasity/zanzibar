// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"net/http"

	"github.com/visvasity/httphelp"
)

// Endpoint path suffixes registered by [Service.RegisterHandlers], relative to
// the mount prefix. Each is served as an httphelp POST handler accepting and
// returning application/json or application/gob.
const (
	PathCheck              = "check"
	PathWrite              = "write"
	PathRead               = "read"
	PathExpand             = "expand"
	PathListObjects        = "list-objects"
	PathListUsers          = "list-users"
	PathConfigWrite        = "config/write"
	PathConfigRead         = "config/read"
	PathConfigList         = "config/list"
	PathConfigReadVersion  = "config/read-version"
	PathConfigListVersions = "config/list-versions"
)

// ConfigReadRequest is the body of the config/read endpoint.
type ConfigReadRequest struct {
	Namespace string `json:"namespace"`
}

// ConfigListResponse is the body returned by the config/list endpoint.
type ConfigListResponse struct {
	Configs []NamespaceConfig `json:"configs"`
}

// ConfigReadVersionRequest is the body of the config/read-version endpoint.
type ConfigReadVersionRequest struct {
	Namespace string `json:"namespace"`
	Version   uint64 `json:"version"`
}

// ConfigListVersionsRequest is the body of the config/list-versions endpoint.
type ConfigListVersionsRequest struct {
	Namespace string `json:"namespace"`
}

// ConfigListVersionsResponse is the body returned by the config/list-versions
// endpoint: the stored versions in ascending order.
type ConfigListVersionsResponse struct {
	Versions []uint64 `json:"versions"`
}

// RegisterHandlers mounts the HTTP endpoints onto server under prefix (for
// example "/api/authz/"), which must end with "/". Handlers are registered on
// the secure (TLS and unix) endpoints.
//
// The library performs no authentication and makes no decision about who may
// call these endpoints. The caller MUST gate the mutating endpoints
// (PathWrite, PathConfigWrite) — and any read endpoints it considers sensitive
// — with its own authn/authz middleware before exposing the server. Any
// authenticated caller identity that should influence a decision is passed
// explicitly in request bodies, never inferred by the library from transport
// state.
func (s *Service) RegisterHandlers(server *httphelp.Server, prefix string) {
	panic("not implemented")
}

// Client is a typed client for a Service exposed over HTTP by
// [Service.RegisterHandlers]. It encodes requests with the httphelp POST
// convention.
type Client struct {
	// Unexported fields are elided until implementation.
}

// NewClient returns a Client for a Service whose handlers are mounted at
// baseURL (the server address joined with the RegisterHandlers prefix, for
// example "https://host/api/authz/"). If httpClient is nil, http.DefaultClient
// is used.
func NewClient(baseURL string, httpClient *http.Client) *Client {
	panic("not implemented")
}

// Check calls the remote check endpoint.
func (c *Client) Check(ctx context.Context, req *CheckRequest) (*CheckResponse, error) {
	panic("not implemented")
}

// Write calls the remote write endpoint.
func (c *Client) Write(ctx context.Context, req *WriteRequest) (*WriteResponse, error) {
	panic("not implemented")
}

// Read calls the remote read endpoint.
func (c *Client) Read(ctx context.Context, req *ReadRequest) (*ReadResponse, error) {
	panic("not implemented")
}

// Expand calls the remote expand endpoint.
func (c *Client) Expand(ctx context.Context, req *ExpandRequest) (*ExpandResponse, error) {
	panic("not implemented")
}

// ListObjects calls the remote list-objects endpoint.
func (c *Client) ListObjects(ctx context.Context, req *ListObjectsRequest) (*ListObjectsResponse, error) {
	panic("not implemented")
}

// ListUsers calls the remote list-users endpoint.
func (c *Client) ListUsers(ctx context.Context, req *ListUsersRequest) (*ListUsersResponse, error) {
	panic("not implemented")
}

// WriteConfig calls the remote config/write endpoint. cfg.Version is the
// compare-and-set expectation (0 to create); the returned config carries the
// newly assigned Version. A version mismatch is a conflict error.
func (c *Client) WriteConfig(ctx context.Context, cfg *NamespaceConfig) (*NamespaceConfig, error) {
	panic("not implemented")
}

// ReadConfig calls the remote config/read endpoint.
func (c *Client) ReadConfig(ctx context.Context, namespace string) (*NamespaceConfig, error) {
	panic("not implemented")
}

// ListConfigs calls the remote config/list endpoint.
func (c *Client) ListConfigs(ctx context.Context) ([]NamespaceConfig, error) {
	panic("not implemented")
}

// ReadConfigVersion calls the remote config/read-version endpoint.
func (c *Client) ReadConfigVersion(ctx context.Context, namespace string, version uint64) (*NamespaceConfig, error) {
	panic("not implemented")
}

// ListConfigVersions calls the remote config/list-versions endpoint.
func (c *Client) ListConfigVersions(ctx context.Context, namespace string) ([]uint64, error) {
	panic("not implemented")
}
