package contracttest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	clientcollect "github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/diagnose"
	"github.com/kubebao/openbao-operator-client/evidence"
)

type scope struct {
	Selected   bool   `json:"selected"`
	Requested  bool   `json:"requested"`
	Applicable bool   `json:"applicable"`
	Outcome    string `json:"outcome"`
	Valid      bool   `json:"valid"`
	Count      int    `json:"count"`
}

type coverage struct {
	State  string `json:"state"`
	Absent bool   `json:"absent"`
}

func cover(s scope) coverage {
	at := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	read := evidence.Collection{
		Scope: evidence.Scope{
			Origin: "origin", Group: "apps", Version: "v1", Resource: "statefulsets",
			Namespace: "team", Action: evidence.ReadList,
		}, Outcome: evidence.CollectionOutcome(s.Outcome), Items: s.Count,
		Interval: evidence.Interval{StartedAt: at, CompletedAt: at},
	}
	if read.Outcome == evidence.CollectionNotCollected || read.Outcome == evidence.CollectionNotRequested {
		read.Interval = evidence.Interval{}
	}
	applicable := diagnose.Applies
	if !s.Applicable {
		applicable = diagnose.DoesNotApply
	}
	value := diagnose.ResolveCoverage(diagnose.CoverageInput{
		Selected: s.Selected, Requested: s.Requested, Applicability: applicable,
		Collection: read, EvidenceValid: s.Valid,
	})
	return coverage{State: string(value.State), Absent: value.AbsenceConfirmed}
}

func TestCoverage(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name  string   `json:"name"`
		Scope scope    `json:"scope"`
		Want  coverage `json:"want"`
	}](t, "coverage")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) { equal(t, cover(tc.Scope), tc.Want) })
	}
}

type page struct {
	UIDs    []string `json:"uids"`
	Next    string   `json:"next"`
	Outcome string   `json:"outcome"`
	Elapsed int      `json:"elapsed"`
	Bytes   int      `json:"bytes"`
}

type limits struct {
	Pages      int `json:"pages"`
	Candidates int `json:"candidates"`
	Bytes      int `json:"bytes"`
	Duration   int `json:"duration"`
}

type collectionResult struct {
	Outcome string   `json:"outcome"`
	UIDs    []string `json:"uids"`
	Reads   int      `json:"reads"`
}

// transcriptTransport translates fixture outcomes into HTTP responses and typed
// transport errors. Padding scales synthetic byte units without changing limits.
type transcriptTransport struct {
	pages    []page
	after    string
	duration int
	index    int
}

func (r *transcriptTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, "/openbaoclusters/bao") {
		if r.after == "" {
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		body := fmt.Sprintf(`{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",`+
			`"metadata":{"namespace":"team","name":"bao","uid":%q,"generation":1}}`, r.after)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	if r.index >= len(r.pages) {
		return nil, io.ErrUnexpectedEOF
	}
	p := r.pages[r.index]
	r.index++
	switch p.Outcome {
	case "cancelled":
		return nil, context.Canceled
	case "timeout":
		return nil, context.DeadlineExceeded
	case "denied", "unsupported":
		status := http.StatusForbidden
		if p.Outcome == "unsupported" {
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("PROVIDER_CANARY"))}, nil
	case "complete":
	default:
		return nil, errors.New(p.Outcome)
	}
	if p.Elapsed >= r.duration {
		return nil, context.DeadlineExceeded
	}
	if p.Bytes < 0 {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("null"))}, nil
	}
	items := []any{}
	for _, uid := range p.UIDs {
		items = append(items, map[string]any{
			"metadata": map[string]any{"namespace": "team", "name": uid, "uid": uid},
			"spec":     map[string]any{"cluster": "bao"},
		})
	}
	body, err := json.Marshal(map[string]any{
		"apiVersion": "openbao.org/v1alpha1", "kind": "OpenBaoRestoreList",
		"metadata": map[string]any{"resourceVersion": "10", "continue": p.Next}, "items": items,
	})
	if err != nil {
		return nil, err
	}
	padding := p.Bytes*100 - len(body)
	if padding > 0 {
		body = append(body, strings.Repeat(" ", padding)...)
	}
	return &http.Response{StatusCode: http.StatusOK, ContentLength: -1, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
}

func collectTranscript(t *testing.T, pages []page, limit limits, before, after string) collectionResult {
	t.Helper()
	result := collectionResult{Outcome: "invalid", UIDs: []string{}}
	target, err := fixtureTarget(t, identity{
		Origin: "origin-1", Group: "openbao.org", Kind: "OpenBaoCluster",
		Namespace: "team", Name: "bao", UID: before,
	}, request{})
	if err != nil {
		return result
	}
	one := int64(1)
	target.Generation = evidence.Integer{State: evidence.ValueReported, Value: &one}
	reader, err := clientcollect.NewClient(
		"https://example.test",
		&transcriptTransport{pages: pages, after: after, duration: limit.Duration},
	)
	if err != nil {
		t.Fatal(err)
	}
	related := clientcollect.Request{
		Scope: evidence.Scope{
			Origin: "origin-1", Group: "openbao.org", Version: "v1alpha1", Resource: "openbaorestores",
			Namespace: "team", Action: evidence.ReadList,
		},
		Contract: adapter.OpenBaoV1Alpha1, Limits: clientcollect.DefaultLimits(), Projection: adapter.DefaultProjectionLimits(),
	}
	related.Limits.MaxPages, related.Limits.MaxCandidates = limit.Pages, limit.Candidates
	related.Limits.PageSize = min(100, limit.Candidates)
	related.Limits.MaxBytes, related.Limits.MaxResponseBytes = limit.Bytes*100, limit.Bytes*100
	related.Limits.Timeout = time.Duration(limit.Duration) * time.Second
	verify := related
	verify.Scope.Resource, verify.Scope.Action, verify.Scope.Name, verify.ExpectedUID = "openbaoclusters", evidence.ReadGet, "bao", before
	verify.Limits = clientcollect.DefaultLimits()
	global := clientcollect.DefaultLimits()
	global.MaxPages = max(1, limit.Pages+1)
	actual, err := reader.Related(
		context.Background(),
		target,
		clientcollect.RelatedPlan{Restores: &related, Verify: verify, Limits: global},
	)
	if err != nil {
		return result
	}
	result.Outcome, result.Reads = string(actual.Restores.Collection.Outcome), actual.Restores.Requests
	for _, value := range actual.Restores.Observations {
		result.UIDs = append(result.UIDs, value.Reference.Identity.UID)
	}
	switch actual.Verification.State {
	case evidence.VerificationDeleted:
		result.Outcome = "deleted"
	case evidence.VerificationReplaced:
		result.Outcome = "replaced"
	}
	return result
}

func TestCollectionLimits(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name   string           `json:"name"`
		Pages  []page           `json:"pages"`
		Limits limits           `json:"limits"`
		Before string           `json:"before"`
		After  string           `json:"after"`
		Want   collectionResult `json:"want"`
	}](t, "collection")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) { equal(t, collectTranscript(t, tc.Pages, tc.Limits, tc.Before, tc.After), tc.Want) })
	}
}
