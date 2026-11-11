package operations_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/operations"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

const acceptedPause = `{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",` +
	`"metadata":{"namespace":"team","name":"bao","uid":"cluster-1","resourceVersion":"opaque-2","generation":8},` +
	`"spec":{"paused":true},"status":{"message":"CANARY_PROVIDER_MESSAGE"}}`

func newControlClient(t testing.TB, transport http.RoundTripper) *operations.Client {
	t.Helper()
	client, err := operations.NewClient("https://kubernetes.example/proxy/cluster-a", transport)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestSubmitGuardedPatchAndDryRun(t *testing.T) {
	t.Parallel()
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "write", true: "dry-run"}[dryRun], func(t *testing.T) {
			calls := 0
			body := &trackingBody{Reader: strings.NewReader(acceptedPause)}
			client := newControlClient(t, transportFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.Method != http.MethodPatch || request.Header.Get("Content-Type") != "application/json-patch+json" ||
					request.Header.Get("Accept") != "application/json" ||
					request.URL.Path != "/proxy/cluster-a/apis/openbao.org/v1alpha1/namespaces/team/openbaoclusters/bao" {
					t.Fatalf("unexpected request: %s %s", request.Method, request.URL)
				}
				if (request.URL.Query().Get("dryRun") == "All") != dryRun || len(request.URL.Query()) != boolInt(dryRun) {
					t.Fatalf("unexpected query: %s", request.URL.RawQuery)
				}
				patch, err := io.ReadAll(request.Body)
				if err != nil {
					t.Fatal(err)
				}
				expected, err := pausePlan(t).Patch()
				if err != nil || string(patch) != string(expected) {
					t.Fatalf("submitted patch differs from preview: %s", patch)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
			}))
			options := operations.DefaultSubmitOptions()
			options.DryRun = dryRun
			result, err := client.Submit(context.Background(), pausePlan(t), options)
			want := operations.OutcomeAccepted
			if dryRun {
				want = operations.OutcomeDryRunAccepted
			}
			if err != nil || result.Outcome != want || result.Requests != 1 || calls != 1 || body.closes != 1 ||
				result.Receipt == nil || *result.Receipt.Reference.ResourceVersion.Value != "opaque-2" ||
				*result.Receipt.Generation.Value != 8 || result.CompletedAt.Before(result.StartedAt) {
				t.Fatalf("submit result = %+v, error = %v, calls = %d, closes = %d", result, err, calls, body.closes)
			}
			assertSanitizedResult(t, result, err)
		})
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestSubmitClassifiesServerRejectionWithoutReadingBody(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status int
		want   operations.Outcome
	}{
		{http.StatusUnauthorized, operations.OutcomeDenied},
		{http.StatusForbidden, operations.OutcomeDenied},
		{http.StatusConflict, operations.OutcomeConflict},
		{http.StatusPreconditionFailed, operations.OutcomeConflict},
		{http.StatusNotFound, operations.OutcomeNotFound},
		{http.StatusBadRequest, operations.OutcomeInvalid},
		{http.StatusUnprocessableEntity, operations.OutcomeInvalid},
		{http.StatusUnsupportedMediaType, operations.OutcomeInvalid},
		{http.StatusTooManyRequests, operations.OutcomeRateLimited},
		{http.StatusServiceUnavailable, operations.OutcomeUnknown},
		{http.StatusGatewayTimeout, operations.OutcomeUnknown},
		{http.StatusFound, operations.OutcomeUnknown},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			body := &trackingBody{Reader: strings.NewReader("CANARY_PROVIDER_MESSAGE")}
			calls := 0
			client := newControlClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tc.status, Body: body}, nil
			}))
			result, err := client.Submit(context.Background(), pausePlan(t), operations.DefaultSubmitOptions())
			if err != nil || result.Outcome != tc.want || result.StatusCode != tc.status ||
				result.Receipt != nil || calls != 1 || body.closes != 1 || body.reads != 0 {
				t.Fatalf("rejection = %+v, %v; calls=%d closes=%d reads=%d", result, err, calls, body.closes, body.reads)
			}
			assertSanitizedResult(t, result, err)
		})
	}
}

func TestSubmitMaintenanceRequiresMatchingValue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, spec string
		want       operations.Outcome
	}{
		{"matching", `{"maintenance":{"enabled":true}}`, operations.OutcomeAccepted},
		{"other-value", `{"maintenance":{"enabled":false}}`, operations.OutcomeUnknown},
		{"missing-value", `{"maintenance":{}}`, operations.OutcomeUnknown},
		{"missing-object", `{}`, operations.OutcomeUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := operations.PrepareMaintenance(controlCluster(t, ""), true)
			if err != nil {
				t.Fatal(err)
			}
			raw := strings.Replace(acceptedPause, `{"paused":true}`, tc.spec, 1)
			client := newControlClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(raw))}, nil
			}))
			result, err := client.Submit(context.Background(), plan, operations.DefaultSubmitOptions())
			if err != nil || result.Outcome != tc.want || (result.Receipt != nil) != (tc.want == operations.OutcomeAccepted) {
				t.Fatalf("maintenance result = %+v, %v", result, err)
			}
		})
	}
}

func TestSubmitPreservesUnknownAfterAmbiguousResponses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
	}{
		{"replaced-target", strings.Replace(acceptedPause, "cluster-1", "cluster-2", 1)},
		{"wrong-namespace", strings.Replace(acceptedPause, `"namespace":"team"`, `"namespace":"other"`, 1)},
		{"wrong-group", strings.Replace(acceptedPause, "openbao.org/v1alpha1", "other.example/v1alpha1", 1)},
		{"missing-resource-version", strings.Replace(acceptedPause, `"resourceVersion":"opaque-2",`, "", 1)},
		{"stale-generation", strings.Replace(acceptedPause, `"generation":8`, `"generation":6`, 1)},
		{"wrong-control-value", strings.Replace(acceptedPause, `"paused":true`, `"paused":false`, 1)},
		{"invalid-control-value", strings.Replace(acceptedPause, `"paused":true`, `"paused":null`, 1)},
		{"deleting-target", strings.Replace(acceptedPause, `"generation":8`,
			`"generation":8,"deletionTimestamp":"2026-11-11T12:00:00Z"`, 1)},
		{"duplicate-keys", strings.Replace(acceptedPause, `"paused":true`, `"paused":false,"paused":true`, 1)},
		{"invalid-json", "CANARY_PROVIDER_MESSAGE"},
		{"empty-response", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackingBody{Reader: strings.NewReader(tc.body)}
			client := newControlClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
			}))
			result, err := client.Submit(context.Background(), pausePlan(t), operations.DefaultSubmitOptions())
			if err != nil || result.Outcome != operations.OutcomeUnknown || result.Receipt != nil || body.closes != 1 {
				t.Fatalf("ambiguous response = %+v, %v; closes=%d", result, err, body.closes)
			}
			assertSanitizedResult(t, result, err)
		})
	}
}

type trackingBody struct {
	io.Reader
	reads, closes int
	closeError    error
}

func (b *trackingBody) Read(value []byte) (int, error) {
	b.reads++
	return b.Reader.Read(value)
}

func (b *trackingBody) Close() error {
	b.closes++
	return b.closeError
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("CANARY_READ_ERROR") }

func TestSubmitClosesBodiesAndBoundsResponses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		reader        io.Reader
		contentLength int64
		closeError    error
		transportErr  error
	}{
		{"declared-limit", strings.NewReader(acceptedPause), 1024, nil, nil},
		{"streamed-limit", strings.NewReader(strings.Repeat("x", 1024)), -1, nil, nil},
		{"read-error", failingReader{}, -1, nil, nil},
		{"close-error", strings.NewReader(acceptedPause), -1, errors.New("CANARY_CLOSE_ERROR"), nil},
		{"response-plus-error", strings.NewReader(acceptedPause), -1, nil, errors.New("CANARY_TRANSPORT_ERROR")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackingBody{Reader: tc.reader, closeError: tc.closeError}
			calls := 0
			client := newControlClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: http.StatusOK, Body: body, ContentLength: tc.contentLength}, tc.transportErr
			}))
			options := operations.DefaultSubmitOptions()
			if strings.Contains(tc.name, "limit") {
				options.MaxResponseBytes = 64
			}
			result, err := client.Submit(context.Background(), pausePlan(t), options)
			if err != nil || result.Outcome != operations.OutcomeUnknown || result.Receipt != nil || calls != 1 ||
				body.closes != 1 || result.Bytes > options.MaxResponseBytes+1 {
				t.Fatalf("bounded result = %+v, %v; calls=%d closes=%d", result, err, calls, body.closes)
			}
			assertSanitizedResult(t, result, err)
		})
	}
}

func TestSubmitCancellationAndTimeoutNeverRetry(t *testing.T) {
	t.Parallel()
	for _, before := range []bool{false, true} {
		t.Run(map[bool]string{false: "during-request", true: "before-request"}[before], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if before {
				cancel()
			}
			calls := 0
			client := newControlClient(t, transportFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				cancel()
				<-request.Context().Done()
				return nil, errors.New("CANARY_CANCELED_ERROR")
			}))
			result, err := client.Submit(ctx, pausePlan(t), operations.DefaultSubmitOptions())
			want, wantCalls := operations.OutcomeUnknown, 1
			if before {
				want, wantCalls = operations.OutcomeNotSubmitted, 0
			}
			if err != nil || result.Outcome != want || calls != wantCalls || result.Requests != wantCalls {
				t.Fatalf("cancellation = %+v, %v; calls=%d", result, err, calls)
			}
			assertSanitizedResult(t, result, err)
		})
	}
	calls := 0
	client := newControlClient(t, transportFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		<-request.Context().Done()
		return nil, request.Context().Err()
	}))
	options := operations.DefaultSubmitOptions()
	options.Timeout = time.Millisecond
	result, err := client.Submit(context.Background(), pausePlan(t), options)
	if err != nil || result.Outcome != operations.OutcomeUnknown || calls != 1 {
		t.Fatalf("timeout = %+v, %v; calls=%d", result, err, calls)
	}
}

func TestClientRejectsUnsafeEndpointsAndInvalidRequests(t *testing.T) {
	t.Parallel()
	calls := 0
	transport := transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected request")
	})
	for _, endpoint := range []string{
		"", "file:///tmp/endpoint", "https://user:CANARY@host", "https://host?token=CANARY", "https://host?",
		"https://host#CANARY", "https://host/a/../b", "https://host/a/./b", "https://host/a/%2e%2e/b",
		"https://host/a/%252e%252e/b", "https://host/a\\b", "https://host/%2fapis", "https:///missing-host",
	} {
		if _, err := operations.NewClient(endpoint, transport); !errors.Is(err, operations.ErrInvalidRequest) ||
			strings.Contains(err.Error(), "CANARY") {
			t.Fatalf("endpoint error = %v", err)
		}
	}
	if _, err := operations.NewClient("https://host", nil); !errors.Is(err, operations.ErrInvalidRequest) {
		t.Fatalf("nil transport error = %v", err)
	}
	client := newControlClient(t, transport)
	plan := pausePlan(t)
	plan.Target.Identity.Name = "../../secrets"
	if _, err := client.Submit(context.Background(), plan, operations.DefaultSubmitOptions()); !errors.Is(err, operations.ErrInvalidPlan) {
		t.Fatalf("invalid plan error = %v", err)
	}
	options := operations.DefaultSubmitOptions()
	options.Timeout = 0
	if _, err := client.Submit(context.Background(), pausePlan(t), options); !errors.Is(err, operations.ErrInvalidRequest) {
		t.Fatalf("invalid options error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("invalid input made %d requests", calls)
	}
}

func assertSanitizedResult(t testing.TB, result operations.Result, err error) {
	t.Helper()
	raw, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(raw), "CANARY") || err != nil && strings.Contains(err.Error(), "CANARY") {
		t.Fatal("raw provider or transport data escaped")
	}
}
