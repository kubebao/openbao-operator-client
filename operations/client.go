package operations

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

var ErrInvalidRequest = errors.New("invalid cluster control request")

// Client submits one guarded PATCH through a caller-configured transport.
// The transport must honor context cancellation and must not follow redirects or
// retry requests. Client does not load credentials, discover targets, or refresh plans.
type Client struct {
	endpoint  url.URL
	transport http.RoundTripper
}

func NewClient(endpoint string, transport http.RoundTripper) (*Client, error) {
	base, err := url.Parse(endpoint)
	if err != nil || transport == nil || base == nil || base.Hostname() == "" || base.Opaque != "" ||
		(base.Scheme != "https" && base.Scheme != "http") || base.User != nil ||
		base.RawQuery != "" || base.ForceQuery || base.Fragment != "" || base.RawPath != "" ||
		strings.ContainsAny(base.Path, "\\%") {
		return nil, ErrInvalidRequest
	}
	for _, part := range strings.Split(base.Path, "/") {
		if part == "." || part == ".." {
			return nil, ErrInvalidRequest
		}
	}
	return &Client{endpoint: *base, transport: transport}, nil
}

// SubmitOptions bound a single request, including response projection.
// DryRun asks the API server to validate the PATCH without persisting it.
type SubmitOptions struct {
	DryRun           bool
	Timeout          time.Duration
	MaxResponseBytes int
	Projection       adapter.ProjectionLimits
}

func DefaultSubmitOptions() SubmitOptions {
	return SubmitOptions{
		Timeout: 15 * time.Second, MaxResponseBytes: 8 * 1024 * 1024, Projection: adapter.DefaultProjectionLimits(),
	}
}

func (o SubmitOptions) valid() bool {
	return o.Timeout > 0 && o.MaxResponseBytes > 0 && o.MaxResponseBytes < math.MaxInt &&
		o.Projection.MaxBytes > 0 && o.Projection.MaxConditions > 0 && o.Projection.MaxEntries > 0
}

type Outcome string

const (
	// OutcomeNotSubmitted means cancellation prevented the transport call.
	OutcomeNotSubmitted Outcome = "not_submitted"
	// OutcomeAccepted means the API returned the requested value for the bound target.
	// It does not establish that the operator observed or applied the control.
	OutcomeAccepted       Outcome = "accepted"
	OutcomeDryRunAccepted Outcome = "dry_run_accepted"
	OutcomeDenied         Outcome = "denied"
	OutcomeConflict       Outcome = "conflict"
	OutcomeNotFound       Outcome = "not_found"
	// OutcomeInvalid includes validation failures and JSON Patch test failures
	// reported as HTTP 422. The status code alone cannot distinguish them.
	OutcomeInvalid     Outcome = "invalid"
	OutcomeRateLimited Outcome = "rate_limited"
	// OutcomeUnknown means the response cannot establish whether the PATCH persisted.
	// Do not resubmit automatically. Read the bound target and assess its state.
	OutcomeUnknown Outcome = "unknown"
)

// Receipt records the API response identity and generation. A dry-run receipt is
// hypothetical and must not be used as evidence that any value was persisted.
type Receipt struct {
	Reference  evidence.Reference `json:"reference"`
	Generation evidence.Integer   `json:"generation"`
}

// Result contains bounded classifications and approved fields only. Provider
// messages, raw response bodies, and transport error strings are excluded.
type Result struct {
	Outcome     Outcome   `json:"outcome"`
	StatusCode  int       `json:"statusCode"`
	Receipt     *Receipt  `json:"receipt,omitempty"`
	Requests    int       `json:"requests"`
	Bytes       int       `json:"bytes"`
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
}

// Submit makes at most one PATCH and never retries or waits for reconciliation.
// Input errors are returned as sanitized errors. Transport and server outcomes
// are returned in Result, including an unknown outcome after ambiguous failures.
func (c *Client) Submit(ctx context.Context, plan Plan, options SubmitOptions) (result Result, err error) {
	if c == nil || c.transport == nil || ctx == nil || !options.valid() {
		return Result{}, ErrInvalidRequest
	}
	patch, err := plan.Patch()
	if err != nil {
		return Result{}, err
	}
	return c.submit(
		ctx,
		plan.Target,
		patch,
		options,
		func(raw []byte) (Receipt, bool) { return responseReceipt(raw, plan, options.Projection) },
	)
}

func (c *Client) submit(ctx context.Context, target evidence.Reference, patch []byte, options SubmitOptions,
	receipt func([]byte) (Receipt, bool),
) (result Result, err error) {
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	result = Result{Outcome: OutcomeNotSubmitted, StartedAt: time.Now().UTC()}
	defer func() { result.CompletedAt = time.Now().UTC() }()
	if ctx.Err() != nil {
		return result, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.requestURL(target, options.DryRun), bytes.NewReader(patch))
	if err != nil {
		return Result{}, ErrInvalidRequest
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json-patch+json")
	result.Outcome, result.Requests = OutcomeUnknown, 1
	response, requestErr := c.transport.RoundTrip(request)
	if requestErr != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return result, nil
	}
	if response == nil {
		return result, nil
	}
	result.StatusCode = response.StatusCode
	bodyClosed := response.Body == nil
	if !bodyClosed {
		defer func() {
			if !bodyClosed {
				_ = response.Body.Close()
			}
		}()
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		result.Outcome = statusOutcome(response.StatusCode)
		return result, nil
	}
	if response.Body == nil || ctx.Err() != nil {
		return result, nil
	}
	budget := min(options.MaxResponseBytes, options.Projection.MaxBytes)
	if response.ContentLength > int64(budget) {
		return result, nil
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, int64(budget)+1))
	closeErr := response.Body.Close()
	bodyClosed = true
	result.Bytes = len(body)
	if readErr != nil || closeErr != nil || len(body) > budget || ctx.Err() != nil {
		return result, nil
	}
	projectedReceipt, valid := receipt(body)
	if !valid || ctx.Err() != nil {
		return result, nil
	}
	result.Outcome, result.Receipt = OutcomeAccepted, &projectedReceipt
	if options.DryRun {
		result.Outcome = OutcomeDryRunAccepted
	}
	return result, nil
}

func (c *Client) requestURL(target evidence.Reference, dryRun bool) string {
	id := target.Identity
	version := strings.TrimPrefix(target.APIVersion, id.Group+"/")
	endpoint := c.endpoint.JoinPath("apis", id.Group, version, "namespaces", id.Namespace, "openbaoclusters", id.Name)
	if dryRun {
		endpoint.RawQuery = url.Values{"dryRun": {"All"}}.Encode()
	}
	return endpoint.String()
}

func statusOutcome(status int) Outcome {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return OutcomeDenied
	case http.StatusConflict, http.StatusPreconditionFailed:
		return OutcomeConflict
	case http.StatusNotFound:
		return OutcomeNotFound
	case http.StatusBadRequest, http.StatusUnprocessableEntity, http.StatusUnsupportedMediaType:
		return OutcomeInvalid
	case http.StatusTooManyRequests:
		return OutcomeRateLimited
	default:
		return OutcomeUnknown
	}
}

func responseReceipt(raw []byte, plan Plan, limits adapter.ProjectionLimits) (Receipt, bool) {
	cluster, err := adapter.ProjectCluster(raw, adapter.ProjectionOptions{
		Origin:      plan.Target.Identity.Origin,
		CollectedAt: time.Now().UTC(),
		Contract:    plan.Contract,
		Limits:      limits,
	})
	if err != nil || cluster.Reference.Identity != plan.Target.Identity ||
		cluster.Reference.ResourceVersion.State != evidence.ValueReported ||
		cluster.Generation.State != evidence.ValueReported || *cluster.Generation.Value < *plan.Generation.Value ||
		cluster.DeletionTimestamp.State != evidence.ValueUnknown {
		return Receipt{}, false
	}
	value := cluster.Paused
	if plan.Control == ControlMaintenance {
		value = cluster.Maintenance.Enabled
	}
	if value.State != evidence.ValueReported && value.State != evidence.ValueImplied || *value.Value != plan.Enabled {
		return Receipt{}, false
	}
	return Receipt{Reference: cluster.Reference, Generation: cluster.Generation}, true
}
