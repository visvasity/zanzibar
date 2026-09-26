// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"errors"
	"net/http"

	"github.com/visvasity/httphelp"
)

// Data-plane endpoint path suffixes, served by [Service.Handler].
const (
	PathCheck       = "check"
	PathWrite       = "write"
	PathRead        = "read"
	PathExpand      = "expand"
	PathListObjects = "list-objects"
	PathListUsers   = "list-users"
)

// Config-plane endpoint path suffixes, served by [Service.ConfigHandler]. They
// are relative to wherever the config handler is mounted, so "write" here is
// distinct from the data-plane PathWrite.
const (
	PathConfigWrite        = "write"
	PathConfigRead         = "read"
	PathConfigList         = "list"
	PathConfigReadVersion  = "read-version"
	PathConfigListVersions = "list-versions"
)

// ConfigReadRequest is the body of the config read endpoint.
type ConfigReadRequest struct {
	Namespace string `json:"namespace"`
}

// ConfigListRequest is the body of the config list endpoint. It carries no
// parameters today; the reserved field lets the value encode cleanly.
type ConfigListRequest struct {
	Reserved bool `json:"-"`
}

// ConfigListResponse is the body returned by the config list endpoint.
type ConfigListResponse struct {
	Configs []NamespaceConfig `json:"configs"`
}

// ConfigReadVersionRequest is the body of the config read-version endpoint.
type ConfigReadVersionRequest struct {
	Namespace string `json:"namespace"`
	Version   uint64 `json:"version"`
}

// ConfigListVersionsRequest is the body of the config list-versions endpoint.
type ConfigListVersionsRequest struct {
	Namespace string `json:"namespace"`
}

// ConfigListVersionsResponse is the body returned by the config list-versions
// endpoint: the stored versions in ascending order.
type ConfigListVersionsResponse struct {
	Versions []uint64 `json:"versions"`
}

// wireError is the transport form of an [Error], so a typed client can
// reconstruct the category and context (errors.Is/As keep working across HTTP).
type wireError struct {
	Code     ErrorCode `json:"code,omitempty"`
	Message  string    `json:"message,omitempty"`
	Object   string    `json:"object,omitempty"`
	Relation string    `json:"relation,omitempty"`
	Subject  string    `json:"subject,omitempty"`
}

// respEnvelope wraps every response so that a logical error is carried
// explicitly rather than lost in the httphelp 200-with-Error-body convention
// (§14.3).
type respEnvelope[T any] struct {
	Data  *T         `json:"data,omitempty"`
	Error *wireError `json:"error,omitempty"`
}

func toWireError(err error) *wireError {
	var ze *Error
	if errors.As(err, &ze) {
		return &wireError{Code: ze.Code, Message: ze.Message, Object: ze.Object, Relation: ze.Relation, Subject: ze.Subject}
	}
	return &wireError{Message: err.Error()}
}

func (w *wireError) toError() *Error {
	return &Error{Code: w.Code, Message: w.Message, Object: w.Object, Relation: w.Relation, Subject: w.Subject}
}

// postHandler adapts a Service method to an httphelp handler, packing the result
// (or error) into a respEnvelope. The handler function itself never returns an
// error, so httphelp always encodes the envelope.
func postHandler[REQ, RESP any](f func(context.Context, *REQ) (*RESP, error)) http.Handler {
	return httphelp.PostHandler2(func(ctx context.Context, req *REQ, out *respEnvelope[RESP]) error {
		data, err := f(ctx, req)
		if err != nil {
			out.Error = toWireError(err)
			return nil
		}
		out.Data = data
		return nil
	})
}

// Handler returns the data-plane HTTP handler: check, write, read, expand,
// list-objects, and list-users, served at those paths (each with a leading
// slash) relative to the handler root. Mount it wherever you like, for example:
//
//	mux.Handle("/api/authz/", http.StripPrefix("/api/authz", svc.Handler()))
//
// The library performs no authentication. The host MUST place its own
// authn/authz middleware in front of the mutating endpoint (PathWrite), and any
// read endpoints it considers sensitive, before exposing this handler (§14.4).
// The config plane is a separate handler; see [Service.ConfigHandler].
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/"+PathCheck, postHandler(s.Check))
	mux.Handle("/"+PathWrite, postHandler(s.Write))
	mux.Handle("/"+PathRead, postHandler(s.Read))
	mux.Handle("/"+PathExpand, postHandler(s.Expand))
	mux.Handle("/"+PathListObjects, postHandler(s.ListObjects))
	mux.Handle("/"+PathListUsers, postHandler(s.ListUsers))
	return mux
}

// ConfigHandler returns the schema-administration HTTP handler: write, read,
// list, read-version, and list-versions, served at those paths relative to the
// handler root. It is separate from [Service.Handler] so the host can mount it
// elsewhere and behind stricter authorization — writing a NamespaceConfig
// rewrites a namespace's entire authorization semantics and is a high-privilege
// operation (§16). The host MUST gate it accordingly; the library does not.
func (s *Service) ConfigHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/"+PathConfigWrite, postHandler(s.WriteConfig))
	mux.Handle("/"+PathConfigRead, postHandler(func(ctx context.Context, req *ConfigReadRequest) (*NamespaceConfig, error) {
		return s.ReadConfig(ctx, req.Namespace)
	}))
	mux.Handle("/"+PathConfigList, postHandler(func(ctx context.Context, _ *ConfigListRequest) (*ConfigListResponse, error) {
		cfgs, err := s.ListConfigs(ctx)
		if err != nil {
			return nil, err
		}
		return &ConfigListResponse{Configs: cfgs}, nil
	}))
	mux.Handle("/"+PathConfigReadVersion, postHandler(func(ctx context.Context, req *ConfigReadVersionRequest) (*NamespaceConfig, error) {
		return s.ReadConfigVersion(ctx, req.Namespace, req.Version)
	}))
	mux.Handle("/"+PathConfigListVersions, postHandler(func(ctx context.Context, req *ConfigListVersionsRequest) (*ConfigListVersionsResponse, error) {
		vs, err := s.ListConfigVersions(ctx, req.Namespace)
		if err != nil {
			return nil, err
		}
		return &ConfigListVersionsResponse{Versions: vs}, nil
	}))
	return mux
}

func callPost[REQ, RESP any](ctx context.Context, baseURL string, httpClient *http.Client, path string, req *REQ) (*RESP, error) {
	var env respEnvelope[RESP]
	if err := httphelp.CallPostHandler(ctx, baseURL+path, req, &env, httpClient); err != nil {
		return nil, err
	}
	if env.Error != nil {
		return nil, env.Error.toError()
	}
	return env.Data, nil
}

// Client is a typed client for a Service data plane exposed via
// [Service.Handler]. It reconstructs typed errors from the response envelope.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient returns a data-plane Client for a Service's [Service.Handler] mounted
// at baseURL (for example "https://host/api/authz/"). baseURL should end with
// "/". If httpClient is nil, http.DefaultClient is used.
func NewClient(baseURL string, httpClient *http.Client) *Client {
	return &Client{baseURL: baseURL, httpClient: httpClient}
}

// Check calls the remote check endpoint.
func (c *Client) Check(ctx context.Context, req *CheckRequest) (*CheckResponse, error) {
	return callPost[CheckRequest, CheckResponse](ctx, c.baseURL, c.httpClient, PathCheck, req)
}

// Write calls the remote write endpoint.
func (c *Client) Write(ctx context.Context, req *WriteRequest) (*WriteResponse, error) {
	return callPost[WriteRequest, WriteResponse](ctx, c.baseURL, c.httpClient, PathWrite, req)
}

// Read calls the remote read endpoint.
func (c *Client) Read(ctx context.Context, req *ReadRequest) (*ReadResponse, error) {
	return callPost[ReadRequest, ReadResponse](ctx, c.baseURL, c.httpClient, PathRead, req)
}

// Expand calls the remote expand endpoint.
func (c *Client) Expand(ctx context.Context, req *ExpandRequest) (*ExpandResponse, error) {
	return callPost[ExpandRequest, ExpandResponse](ctx, c.baseURL, c.httpClient, PathExpand, req)
}

// ListObjects calls the remote list-objects endpoint.
func (c *Client) ListObjects(ctx context.Context, req *ListObjectsRequest) (*ListObjectsResponse, error) {
	return callPost[ListObjectsRequest, ListObjectsResponse](ctx, c.baseURL, c.httpClient, PathListObjects, req)
}

// ListUsers calls the remote list-users endpoint.
func (c *Client) ListUsers(ctx context.Context, req *ListUsersRequest) (*ListUsersResponse, error) {
	return callPost[ListUsersRequest, ListUsersResponse](ctx, c.baseURL, c.httpClient, PathListUsers, req)
}

// ConfigClient is a typed client for a Service config plane exposed via
// [Service.ConfigHandler].
type ConfigClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewConfigClient returns a config-plane client for a Service's
// [Service.ConfigHandler] mounted at baseURL. baseURL should end with "/". If
// httpClient is nil, http.DefaultClient is used.
func NewConfigClient(baseURL string, httpClient *http.Client) *ConfigClient {
	return &ConfigClient{baseURL: baseURL, httpClient: httpClient}
}

// WriteConfig calls the remote config write endpoint. cfg.Version is the
// compare-and-set expectation (0 to create); the returned config carries the
// newly assigned Version. A version mismatch is a conflict error.
func (c *ConfigClient) WriteConfig(ctx context.Context, cfg *NamespaceConfig) (*NamespaceConfig, error) {
	return callPost[NamespaceConfig, NamespaceConfig](ctx, c.baseURL, c.httpClient, PathConfigWrite, cfg)
}

// ReadConfig calls the remote config read endpoint.
func (c *ConfigClient) ReadConfig(ctx context.Context, namespace string) (*NamespaceConfig, error) {
	return callPost[ConfigReadRequest, NamespaceConfig](ctx, c.baseURL, c.httpClient, PathConfigRead, &ConfigReadRequest{Namespace: namespace})
}

// ListConfigs calls the remote config list endpoint.
func (c *ConfigClient) ListConfigs(ctx context.Context) ([]NamespaceConfig, error) {
	resp, err := callPost[ConfigListRequest, ConfigListResponse](ctx, c.baseURL, c.httpClient, PathConfigList, &ConfigListRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Configs, nil
}

// ReadConfigVersion calls the remote config read-version endpoint.
func (c *ConfigClient) ReadConfigVersion(ctx context.Context, namespace string, version uint64) (*NamespaceConfig, error) {
	return callPost[ConfigReadVersionRequest, NamespaceConfig](ctx, c.baseURL, c.httpClient, PathConfigReadVersion, &ConfigReadVersionRequest{Namespace: namespace, Version: version})
}

// ListConfigVersions calls the remote config list-versions endpoint.
func (c *ConfigClient) ListConfigVersions(ctx context.Context, namespace string) ([]uint64, error) {
	resp, err := callPost[ConfigListVersionsRequest, ConfigListVersionsResponse](ctx, c.baseURL, c.httpClient, PathConfigListVersions, &ConfigListVersionsRequest{Namespace: namespace})
	if err != nil {
		return nil, err
	}
	return resp.Versions, nil
}
