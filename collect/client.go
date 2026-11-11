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
	"strconv"
	"strings"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

var ErrInvalidRequest = errors.New("invalid collection request")

// Client performs GET requests through a caller-configured transport. It does not
// load credentials, follow redirects, retry, or discover alternative API groups.
// The transport must honor request cancellation and must not follow redirects.
type Client struct {
	endpoint  url.URL
	transport http.RoundTripper
}

func NewClient(endpoint string, transport http.RoundTripper) (*Client, error) {
	base, err := url.Parse(endpoint)
	if err != nil || transport == nil || base == nil || base.Host == "" ||
		(base.Scheme != "https" && base.Scheme != "http") || base.User != nil ||
		base.RawQuery != "" || base.ForceQuery || base.Fragment != "" || base.RawPath != "" {
		return nil, ErrInvalidRequest
	}
	for _, part := range strings.Split(base.Path, "/") {
		if part == "." || part == ".." {
			return nil, ErrInvalidRequest
		}
	}
	return &Client{endpoint: *base, transport: transport}, nil
}

// Limits bound one GET or paginated LIST, including all its requests.
// Byte limits count response bodies and serialized observations independently.
type Limits struct {
	MaxPages         int
	MaxCandidates    int
	MaxBytes         int
	MaxResponseBytes int
	MaxRetainedBytes int
	PageSize         int
	Timeout          time.Duration
}

func DefaultLimits() Limits {
	return Limits{
		MaxPages: 8, MaxCandidates: 500, MaxBytes: 8 * 1024 * 1024, MaxResponseBytes: 1024 * 1024,
		MaxRetainedBytes: 8 * 1024 * 1024, PageSize: 100, Timeout: 15 * time.Second,
	}
}

func (l Limits) valid() bool {
	return l.MaxPages > 0 && l.MaxCandidates > 0 && l.MaxBytes > 0 && l.MaxBytes < math.MaxInt &&
		l.MaxResponseBytes > 0 && l.MaxResponseBytes < math.MaxInt && l.MaxRetainedBytes > 0 &&
		l.PageSize > 0 && l.PageSize <= l.MaxCandidates && l.Timeout > 0
}

type Request struct {
	Scope              evidence.Scope
	Contract           adapter.Contract
	Limits             Limits
	Projection         adapter.ProjectionLimits
	ExpectedUID        string
	AllowAllNamespaces bool
}

// Result contains only projected observations. An incomplete result can retain
// observations, but cannot establish absence or describe a replacement as its predecessor.
type Result[T any] struct {
	Collection   evidence.Collection `json:"collection"`
	Observations []T                 `json:"observations"`
	Requests     int                 `json:"requests"`
	Bytes        int                 `json:"bytes"`
}

var pathComponent = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)

func (c *Client) validate(request Request, kind, resource string) error {
	if c == nil || c.transport == nil || request.Scope.Validate() != nil || !request.Limits.valid() ||
		request.Projection.MaxBytes <= 0 || request.Projection.MaxConditions <= 0 ||
		request.Projection.MaxEntries <= 0 || request.Limits.PageSize > request.Projection.MaxEntries {
		return ErrInvalidRequest
	}
	info, err := adapter.Describe(request.Contract)
	if err != nil || request.Scope.Group != info.APIGroup ||
		request.Scope.Version != strings.TrimPrefix(info.APIVersion, info.APIGroup+"/") || request.Scope.Resource != resource {
		return ErrInvalidRequest
	}
	allowed := false
	for _, candidate := range info.Kinds {
		allowed = allowed || kind == candidate
	}
	if !allowed {
		return ErrInvalidRequest
	}
	for _, value := range []string{
		request.Scope.Group, request.Scope.Version, request.Scope.Resource,
		request.Scope.Namespace, request.Scope.Name,
	} {
		if value != "" && (!pathComponent.MatchString(value) || value == "." || value == "..") {
			return ErrInvalidRequest
		}
	}
	switch request.Scope.Action {
	case evidence.ReadGet:
		if request.Scope.Namespace == "" {
			return ErrInvalidRequest
		}
	case evidence.ReadList:
		if request.ExpectedUID != "" || request.Scope.Namespace == "" && !request.AllowAllNamespaces {
			return ErrInvalidRequest
		}
	default:
		return ErrInvalidRequest
	}
	if request.ExpectedUID != "" {
		id := evidence.Identity{
			Origin: request.Scope.Origin, Group: info.APIGroup, Kind: kind, Namespace: request.Scope.Namespace,
			Name: request.Scope.Name, UID: request.ExpectedUID,
		}
		if id.Validate() != nil {
			return ErrInvalidRequest
		}
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
	parts = append(parts, scope.Resource)
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
	var network net.Error
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) ||
		errors.As(err, &network) && network.Timeout() {
		return evidence.CollectionTimeout
	}
	return evidence.CollectionFailed
}

func statusOutcome(status int, action evidence.ReadAction) evidence.CollectionOutcome {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return evidence.CollectionDenied
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

func (c *Client) read(ctx context.Context, endpoint string, action evidence.ReadAction, budget int) ([]byte, evidence.CollectionOutcome) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, evidence.CollectionInvalid
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.transport.RoundTrip(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, contextOutcome(ctx, err)
	}
	if response == nil || response.Body == nil {
		return nil, evidence.CollectionInvalid
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, statusOutcome(response.StatusCode, action)
	}
	if response.ContentLength > int64(budget) {
		_ = response.Body.Close()
		return nil, evidence.CollectionSizeLimit
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, int64(budget)+1))
	closeErr := response.Body.Close()
	if ctx.Err() != nil || readErr != nil {
		return nil, contextOutcome(ctx, readErr)
	}
	if len(body) > budget {
		return nil, evidence.CollectionSizeLimit
	}
	if closeErr != nil {
		return nil, evidence.CollectionFailed
	}
	return body, evidence.CollectionComplete
}
