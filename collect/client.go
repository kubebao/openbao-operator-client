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
		return nil, evidence.At("endpoint", ErrInvalidRequest)
	}
	for _, part := range strings.Split(base.Path, "/") {
		if part == "." || part == ".." {
			return nil, evidence.At("endpoint", ErrInvalidRequest)
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
		MaxPages: 8, MaxCandidates: 500, MaxBytes: 8 * 1024 * 1024, MaxResponseBytes: 8 * 1024 * 1024,
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
	Collection    evidence.Collection `json:"collection"`
	Observations  []T                 `json:"observations"`
	Requests      int                 `json:"requests"`
	Bytes         int                 `json:"bytes"`
	RetainedBytes int                 `json:"retainedBytes"`
}

var pathComponent = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)

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
	if request.Projection.MaxBytes <= 0 || request.Projection.MaxConditions <= 0 || request.Projection.MaxEntries <= 0 {
		return evidence.At("projection", ErrInvalidRequest)
	}
	if request.Limits.PageSize > request.Projection.MaxEntries {
		return evidence.At("limits.pageSize", ErrInvalidRequest)
	}
	info, err := adapter.Describe(request.Contract)
	if err != nil {
		return evidence.At("contract", ErrInvalidRequest)
	}
	if request.Scope.Group != info.APIGroup || request.Scope.Version != strings.TrimPrefix(info.APIVersion, info.APIGroup+"/") ||
		request.Scope.Resource != resource {
		return evidence.At("scope", ErrInvalidRequest)
	}
	allowed := false
	for _, candidate := range info.Kinds {
		allowed = allowed || kind == candidate
	}
	if !allowed {
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
		if id.Validate() != nil {
			return evidence.At("expectedUID", ErrInvalidRequest)
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
