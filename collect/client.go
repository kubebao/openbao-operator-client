package collect

import (
	"context"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
	"github.com/kubebao/openbao-operator-client/internal/apiendpoint"
)

// Default collection limits for one GET or paginated LIST.
const (
	defaultMaxPages      = 8
	defaultMaxCandidates = 500
	defaultMaxBytes      = 8 * 1024 * 1024
	defaultPageSize      = 100
	defaultTimeout       = 15 * time.Second
)

// ErrInvalidRequest reports a client, scope, contract, or limit that cannot be collected.
// Errors that wrap it identify the field path but never the field value.
var ErrInvalidRequest = errors.New("invalid collection request")

// Client performs GET requests through a caller-configured transport. It does not
// load credentials, follow redirects, retry, or discover alternative API groups.
// The transport must honor request cancellation and must not follow redirects.
type Client struct {
	endpoint  url.URL
	transport http.RoundTripper
}

// NewClient returns a client for an http or https API server base URL with an
// optional unescaped path prefix. It rejects a nil transport and endpoints with
// user information, queries, fragments, opaque forms, escaped paths, or dot segments.
func NewClient(endpoint string, transport http.RoundTripper) (*Client, error) {
	if transport == nil {
		return nil, evidence.At("transport", ErrInvalidRequest)
	}
	base, err := apiendpoint.Parse(endpoint)
	if err != nil {
		return nil, evidence.At("endpoint", ErrInvalidRequest)
	}
	return &Client{endpoint: base, transport: transport}, nil
}

// Limits bound one GET or paginated LIST, including all its requests.
// Byte limits count response bodies and serialized observations independently.
type Limits struct {
	// MaxPages bounds the number of requests.
	MaxPages int
	// MaxCandidates bounds the number of admitted observations.
	MaxCandidates int
	// MaxBytes bounds the total size of all response bodies.
	MaxBytes int
	// MaxResponseBytes bounds the size of one response body.
	MaxResponseBytes int
	// MaxRetainedBytes bounds the total serialized size of retained observations.
	MaxRetainedBytes int
	// PageSize is the LIST page size. It cannot exceed MaxCandidates.
	PageSize int
	// Timeout bounds the whole read, including every page.
	Timeout time.Duration
}

// DefaultLimits returns conservative limits for one interactive read.
func DefaultLimits() Limits {
	return Limits{
		MaxPages:         defaultMaxPages,
		MaxCandidates:    defaultMaxCandidates,
		MaxBytes:         defaultMaxBytes,
		MaxResponseBytes: defaultMaxBytes,
		MaxRetainedBytes: defaultMaxBytes,
		PageSize:         defaultPageSize,
		Timeout:          defaultTimeout,
	}
}

func (l Limits) valid() bool {
	if l.MaxPages <= 0 || l.MaxCandidates <= 0 || l.Timeout <= 0 {
		return false
	}
	if l.MaxBytes <= 0 || l.MaxBytes == math.MaxInt {
		return false
	}
	if l.MaxResponseBytes <= 0 || l.MaxResponseBytes == math.MaxInt {
		return false
	}
	if l.MaxRetainedBytes <= 0 {
		return false
	}
	return l.PageSize > 0 && l.PageSize <= l.MaxCandidates
}

// Request declares one read: its exact scope, the projection contract, and its limits.
type Request struct {
	// Scope is the exact resource boundary. Collection never broadens it.
	Scope evidence.Scope
	// Contract selects the adapter projection for the response.
	Contract adapter.Contract
	// Limits bound the read.
	Limits Limits
	// Projection bounds each projected document.
	Projection adapter.ProjectionLimits
	// ExpectedUID, when set on a GET, classifies a different UID as a replacement.
	ExpectedUID string
	// AllowAllNamespaces permits a LIST with an empty namespace.
	AllowAllNamespaces bool
}

// Result contains only projected observations. An incomplete result can retain
// observations, but cannot establish absence or describe a replacement as its predecessor.
type Result[T any] struct {
	Collection    evidence.Collection `json:"collection"`
	Observations  []T                 `json:"observations"`
	Requests      int                 `json:"requests"`
	Bytes         int                 `json:"bytes"`
	RetainedBytes int                 `json:"retainedBytes"`
}

var pathComponent = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)

func validProjectionLimits(limits adapter.ProjectionLimits) bool {
	return limits.MaxBytes > 0 && limits.MaxConditions > 0 && limits.MaxEntries > 0
}

func (c *Client) validate(request Request, kind, resource string) error {
	if c == nil || c.transport == nil {
		return evidence.At("client", ErrInvalidRequest)
	}
	if err := request.Scope.Validate(); err != nil {
		return evidence.At("scope", errors.Join(ErrInvalidRequest, err))
	}
	if !request.Limits.valid() {
		return evidence.At("limits", ErrInvalidRequest)
	}
	if !validProjectionLimits(request.Projection) {
		return evidence.At("projection", ErrInvalidRequest)
	}
	if request.Limits.PageSize > request.Projection.MaxEntries {
		return evidence.At("limits.pageSize", ErrInvalidRequest)
	}
	info, err := adapter.Describe(request.Contract)
	if err != nil {
		return evidence.At("contract", ErrInvalidRequest)
	}
	if request.Scope.Group != info.APIGroup ||
		request.Scope.Version != strings.TrimPrefix(info.APIVersion, info.APIGroup+"/") ||
		request.Scope.Resource != resource {
		return evidence.At("scope", ErrInvalidRequest)
	}
	if !slices.Contains(info.Kinds, kind) {
		return evidence.At("contract", ErrInvalidRequest)
	}
	if err := validateReadScope(request); err != nil {
		return err
	}
	if request.ExpectedUID != "" {
		id := evidence.Identity{
			Origin:    request.Scope.Origin,
			Group:     info.APIGroup,
			Kind:      kind,
			Namespace: request.Scope.Namespace,
			Name:      request.Scope.Name,
			UID:       request.ExpectedUID,
		}
		if err := id.Validate(); err != nil {
			return evidence.At("expectedUID", errors.Join(ErrInvalidRequest, err))
		}
	}
	return nil
}

func validateReadScope(request Request) error {
	for _, field := range []struct{ name, value string }{
		{"group", request.Scope.Group},
		{"version", request.Scope.Version},
		{"resource", request.Scope.Resource},
		{"namespace", request.Scope.Namespace},
		{"name", request.Scope.Name},
	} {
		if field.value != "" && (!pathComponent.MatchString(field.value) || field.value == "." || field.value == "..") {
			return evidence.At("scope."+field.name, ErrInvalidRequest)
		}
	}
	switch request.Scope.Action {
	case evidence.ReadGet:
		if request.Scope.Namespace == "" {
			return evidence.At("scope.namespace", ErrInvalidRequest)
		}
	case evidence.ReadList:
		if request.ExpectedUID != "" {
			return evidence.At("expectedUID", ErrInvalidRequest)
		}
		if request.Scope.Namespace == "" && !request.AllowAllNamespaces {
			return evidence.At("allowAllNamespaces", ErrInvalidRequest)
		}
	case evidence.ReadDiscovery:
		if request.Scope.Namespace != "" || request.Scope.Selector != "" || request.ExpectedUID != "" {
			return evidence.At("scope", ErrInvalidRequest)
		}
	default:
		return evidence.At("scope.action", ErrInvalidRequest)
	}
	return nil
}

func (c *Client) requestURL(scope evidence.Scope, limit int, next string) string {
	parts := []string{"apis", scope.Group, scope.Version}
	if scope.Group == "" {
		parts = []string{"api", scope.Version}
	}
	if scope.Namespace != "" {
		parts = append(parts, "namespaces", scope.Namespace)
	}
	if scope.Action != evidence.ReadDiscovery {
		parts = append(parts, scope.Resource)
	}
	if scope.Action == evidence.ReadGet {
		parts = append(parts, scope.Name)
	}
	endpoint := c.endpoint.JoinPath(parts...)
	if scope.Action == evidence.ReadList {
		query := url.Values{"limit": {strconv.Itoa(limit)}}
		if scope.Selector != "" {
			query.Set("labelSelector", scope.Selector)
		}
		if next != "" {
			query.Set("continue", next)
		}
		endpoint.RawQuery = query.Encode()
	}
	return endpoint.String()
}

func contextOutcome(ctx context.Context, err error) evidence.CollectionOutcome {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return evidence.CollectionCancelled
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return evidence.CollectionTimeout
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return evidence.CollectionTimeout
	}
	return evidence.CollectionFailed
}

func statusOutcome(status int, action evidence.ReadAction) evidence.CollectionOutcome {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return evidence.CollectionDenied
	case http.StatusGone:
		return evidence.CollectionExpired
	case http.StatusTooManyRequests:
		return evidence.CollectionRateLimited
	case http.StatusNotFound:
		if action == evidence.ReadGet {
			return evidence.CollectionDeleted
		}
		return evidence.CollectionUnsupported
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return evidence.CollectionTimeout
	default:
		return evidence.CollectionFailed
	}
}

func (c *Client) read(
	ctx context.Context,
	endpoint string,
	action evidence.ReadAction,
	budget int,
) ([]byte, int, evidence.CollectionOutcome) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, evidence.CollectionInvalid
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.transport.RoundTrip(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, 0, contextOutcome(ctx, err)
	}
	if response == nil || response.Body == nil {
		return nil, 0, evidence.CollectionInvalid
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, 0, statusOutcome(response.StatusCode, action)
	}
	if response.ContentLength > int64(budget) {
		_ = response.Body.Close()
		return nil, 0, evidence.CollectionSizeLimit
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, int64(budget)+1))
	closeErr := response.Body.Close()
	if ctx.Err() != nil || readErr != nil {
		return nil, len(body), contextOutcome(ctx, readErr)
	}
	if len(body) > budget {
		return nil, len(body), evidence.CollectionSizeLimit
	}
	if closeErr != nil {
		return nil, len(body), evidence.CollectionFailed
	}
	return body, len(body), evidence.CollectionComplete
}
