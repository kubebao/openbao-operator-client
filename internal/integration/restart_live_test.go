package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/evidence"
	"github.com/kubebao/openbao-operator-client/operations"
)

const restartPrefix = "OPENBAO_CLIENT_RESTART_"

var (
	restartNames = []string{"voter", "read-pool", "zero-read", "blue-green"}
	restartUID   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

type restartFixture struct {
	endpoint, origin, namespace, group string
	mutation                           bool
	targets                            map[string]string
	reads                              *collect.Client
}

func restartSettings(getenv func(string) string) (restartFixture, error) {
	f := restartFixture{
		endpoint: getenv(restartPrefix + "ENDPOINT"), origin: getenv(restartPrefix + "CONTEXT"),
		namespace: getenv(restartPrefix + "NAMESPACE"), group: getenv(restartPrefix + "GROUP"),
		mutation: getenv(restartPrefix+"ALLOW_MUTATION") == "true",
	}
	raw := getenv(restartPrefix + "FIXTURES")
	if f.endpoint == "" && f.origin == "" && f.namespace == "" && f.group == "" && raw == "" &&
		getenv(restartPrefix+"ALLOW_MUTATION") == "" {
		return f, nil
	}
	if json.Unmarshal([]byte(raw), &f.targets) != nil || !f.valid() {
		return restartFixture{}, errors.New("restart qualification requires the declared isolated scope, UID tuple, and mutation opt-in")
	}
	return f, nil
}

func (f restartFixture) valid() bool {
	u, err := url.Parse(f.endpoint)
	if err != nil || !validRestoreEndpoint(f.endpoint) || u.ForceQuery || u.RawPath != "" ||
		f.origin != "kind-client-restart-rc1" || f.namespace != "restart-tests" || f.group != "kubebao.org" ||
		!f.mutation || len(f.targets) != len(restartNames) {
		return false
	}
	seen := map[string]bool{}
	for _, name := range restartNames {
		uid := f.targets[name]
		if !restartUID.MatchString(uid) || seen[uid] {
			return false
		}
		seen[uid] = true
	}
	return true
}

// permits excludes discovery, Secrets, subresources, other namespaces, other API
// groups, and every mutation except PATCH on the three declared rolling fixtures.
func (f restartFixture) permits(r *http.Request) bool {
	base, err := url.Parse(f.endpoint)
	if err != nil || r.URL.Scheme != base.Scheme || r.URL.Host != base.Host || r.URL.RawPath != "" ||
		r.URL.User != nil || r.URL.Fragment != "" || r.URL.ForceQuery || r.URL.Opaque != "" {
		return false
	}
	clusterPath := "/apis/kubebao.org/v1alpha1/namespaces/restart-tests/openbaoclusters/"
	if name, ok := strings.CutPrefix(r.URL.Path, clusterPath); ok && slices.Contains(restartNames, name) {
		return r.Method == http.MethodGet && r.URL.RawQuery == "" ||
			r.Method == http.MethodPatch && name != "blue-green" &&
				(r.URL.RawQuery == "" || r.URL.RawQuery == "dryRun=All")
	}
	if r.Method != http.MethodGet || (r.URL.Path != "/apis/apps/v1/namespaces/restart-tests/statefulsets" &&
		r.URL.Path != "/api/v1/namespaces/restart-tests/pods") {
		return false
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return false
	}
	for key, values := range q {
		if !slices.Contains([]string{"limit", "labelSelector", "continue"}, key) || len(values) != 1 {
			return false
		}
	}
	limit, err := strconv.Atoi(q.Get("limit"))
	name := strings.TrimPrefix(q.Get("labelSelector"), "openbao.org/cluster=")
	return err == nil && limit > 0 && limit <= 32 && slices.Contains(restartNames, name) &&
		q.Get("labelSelector") == "openbao.org/cluster="+name && len(q.Get("continue")) <= 4096
}

type restartWire struct {
	fixture restartFixture
	base    http.RoundTripper
	denied  bool
	discard bool
	calls   atomic.Int64
	patches atomic.Int64
	accepts atomic.Int64
}

func (w *restartWire) RoundTrip(r *http.Request) (*http.Response, error) {
	if !w.fixture.permits(r) {
		return nil, errors.New("request is outside the restart qualification scope")
	}
	w.calls.Add(1)
	if r.Method == http.MethodPatch {
		w.patches.Add(1)
	}
	if w.denied {
		r = r.Clone(r.Context())
		r.Header.Set("Impersonate-User", "system:serviceaccount:restart-tests:restart-reader")
	}
	response, err := w.base.RoundTrip(r)
	if err == nil && response != nil && r.Method == http.MethodPatch && response.StatusCode >= 200 && response.StatusCode < 300 {
		w.accepts.Add(1)
		if w.discard {
			if response.Body != nil {
				_ = response.Body.Close()
			}
			return nil, errors.New("qualification discarded an accepted response")
		}
	}
	return response, err
}

func (f restartFixture) request(name string) collect.Request {
	limits := collect.DefaultLimits()
	limits.MaxPages, limits.MaxCandidates, limits.PageSize = 4, 32, 16
	limits.MaxBytes, limits.MaxResponseBytes, limits.MaxRetainedBytes, limits.Timeout = 2<<20, 1<<20, 1<<20, 10*time.Second
	return collect.Request{
		Scope: evidence.Scope{
			Origin: f.origin, Group: f.group, Version: "v1alpha1", Resource: "openbaoclusters",
			Namespace: f.namespace, Name: name, Action: evidence.ReadGet,
		},
		Contract: adapter.KubeBaoV1Alpha1, ExpectedUID: f.targets[name], Limits: limits, Projection: adapter.DefaultProjectionLimits(),
	}
}

func (f restartFixture) cluster(t *testing.T, ctx context.Context, name string) evidence.Cluster {
	t.Helper()
	r, err := f.reads.Clusters(ctx, f.request(name))
	if err != nil || r.Collection.Outcome != evidence.CollectionComplete || len(r.Observations) != 1 {
		t.Fatalf("restart fixture read failed: outcome=%s candidates=%d", r.Collection.Outcome, len(r.Observations))
	}
	return r.Observations[0]
}

func (f restartFixture) related(ctx context.Context, c evidence.Cluster) (collect.RelatedResult, error) {
	verify := f.request(c.Reference.Identity.Name)
	set := verify
	set.Contract, set.ExpectedUID = adapter.KubernetesAppsV1, ""
	set.Scope.Group, set.Scope.Version, set.Scope.Resource = "apps", "v1", "statefulsets"
	set.Scope.Name, set.Scope.Action = "", evidence.ReadList
	set.Scope.Selector = "openbao.org/cluster=" + c.Reference.Identity.Name
	pod := set
	pod.Contract, pod.Scope.Group, pod.Scope.Resource = adapter.KubernetesCoreV1, "", "pods"
	return f.reads.Related(ctx, c, collect.RelatedPlan{StatefulSets: &set, Pods: &pod, Verify: verify, Limits: verify.Limits})
}

func restartPlan(t *testing.T, c evidence.Cluster) operations.RestartPlan {
	t.Helper()
	p, err := operations.PrepareRestart(c, adapter.KubeBaoV1Alpha1, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal("prepare a new restart for the declared fixture")
	}
	return p
}

func restartSubmit(t *testing.T, ctx context.Context, client *operations.Client, p operations.RestartPlan,
	dryRun bool, expected operations.Outcome,
) operations.Result {
	t.Helper()
	o := operations.DefaultSubmitOptions()
	o.DryRun, o.Timeout = dryRun, 10*time.Second
	r, err := client.SubmitRestart(ctx, p, o)
	if err != nil || r.Outcome != expected || r.Requests != 1 {
		t.Fatalf("restart submission outcome=%s status=%d requests=%d expected=%s", r.Outcome, r.StatusCode, r.Requests, expected)
	}
	return r
}

func sameRestartValue(a, b evidence.Text) bool {
	return a.State == b.State && (a.Value == nil && b.Value == nil || a.Value != nil && b.Value != nil && *a.Value == *b.Value)
}

func (f restartFixture) unchanged(t *testing.T, before evidence.Cluster) {
	t.Helper()
	after := f.cluster(t, t.Context(), before.Reference.Identity.Name)
	if !sameRestartValue(before.Runtime.RestartAt, after.Runtime.RestartAt) ||
		before.Generation.Value == nil || after.Generation.Value == nil || *before.Generation.Value != *after.Generation.Value {
		t.Fatal("a nonpersisting restart check changed the marker or generation")
	}
}

func (f restartFixture) guards(t *testing.T, wire *restartWire, writes *operations.Client) {
	t.Helper()
	c := f.cluster(t, t.Context(), "voter")
	p := restartPlan(t, c)
	restartSubmit(t, t.Context(), writes, p, true, operations.OutcomeDryRunAccepted)
	f.unchanged(t, c)
	denied := &restartWire{fixture: f, base: wire.base, denied: true}
	client, err := operations.NewClient(f.endpoint, denied)
	if err != nil {
		t.Fatal("initialize denied restart client")
	}
	r := restartSubmit(t, t.Context(), client, p, false, operations.OutcomeDenied)
	if r.StatusCode != http.StatusForbidden {
		t.Fatal("the read-only fixture ServiceAccount was not denied")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := wire.calls.Load()
	r, err = writes.SubmitRestart(ctx, p, operations.DefaultSubmitOptions())
	if err != nil || r.Outcome != operations.OutcomeNotSubmitted || r.Requests != 0 || wire.calls.Load() != calls {
		t.Fatal("a canceled restart reached the transport")
	}
	for _, guard := range []string{"uid", "resource-version", "generation"} {
		c = f.cluster(t, t.Context(), "voter")
		p = restartPlan(t, c)
		switch guard {
		case "uid":
			p.Target.Identity.UID = "wrong-restart-qualification-uid"
		case "resource-version":
			v := "stale-restart-qualification-version"
			p.Target.ResourceVersion.Value = &v
		case "generation":
			v := *p.Generation.Value + 1
			p.Generation.Value = &v
		}
		r = restartSubmit(t, t.Context(), writes, p, false, operations.OutcomeInvalid)
		if r.StatusCode != http.StatusUnprocessableEntity {
			t.Fatal("the API server did not reject a restart precondition with HTTP422")
		}
		f.unchanged(t, c)
	}
	blue := f.cluster(t, t.Context(), "blue-green")
	calls = wire.calls.Load()
	_, err = operations.PrepareRestart(blue, adapter.KubeBaoV1Alpha1, time.Now().UTC().Format(time.RFC3339Nano))
	if !errors.Is(err, operations.ErrRestartBlocked) || wire.calls.Load() != calls {
		t.Fatal("BlueGreen restart was not refused before transport")
	}
	t.Log("server dry-run, RBAC denial, cancellation, three patch preconditions, and BlueGreen refusal passed")
}

func restartOwnedPods(t *testing.T, c evidence.Cluster, r collect.RelatedResult) map[string]bool {
	t.Helper()
	if r.Outcome != evidence.CollectionComplete || r.Verification.State != evidence.VerificationVerified ||
		r.StatefulSets == nil || r.Pods == nil {
		t.Fatal("restart replacement proof requires complete reads and a verified target")
	}
	parents := map[evidence.Identity]bool{}
	for _, a := range r.Associations {
		if a.Subject.Kind == "StatefulSet" && a.Target == c.Reference.Identity && a.State == evidence.AssociationConfirmed {
			parents[a.Subject] = true
		}
	}
	uids := map[string]bool{}
	for _, a := range r.Associations {
		if a.Subject.Kind == "Pod" && parents[a.Target] && a.State == evidence.AssociationConfirmed {
			uids[a.Subject.UID] = true
		}
	}
	return uids
}

func (f restartFixture) await(t *testing.T, p operations.RestartPlan) (evidence.Cluster, collect.RelatedResult) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	var a operations.Assessment
	for {
		c := f.cluster(t, ctx, p.Target.Identity.Name)
		r, err := f.related(ctx, c)
		if err == nil {
			a, err = operations.AssessRestart(p, c, r, operations.DefaultAssessmentPolicy(time.Now().UTC()))
		}
		if err != nil {
			t.Fatal("assess bounded restart evidence")
		}
		if a.State == operations.AssessmentObserved {
			return c, r
		}
		select {
		case <-ctx.Done():
			t.Fatalf("restart convergence timed out: state=%s reasons=%v", a.State, a.Reasons)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (f restartFixture) rollout(t *testing.T, name string, want int, writes *operations.Client, base http.RoundTripper) {
	t.Helper()
	c := f.cluster(t, t.Context(), name)
	r, err := f.related(t.Context(), c)
	if err != nil {
		t.Fatal("read the pre-request workload population")
	}
	before := restartOwnedPods(t, c, r)
	if len(before) != want {
		t.Fatal("the declared restart fixture has an unexpected owned Pod population")
	}
	// Bind the mutation to a fresh revision after the baseline read. This does
	// not retry a submitted request.
	c = f.cluster(t, t.Context(), name)
	p := restartPlan(t, c)
	client := writes
	discard := &restartWire{fixture: f, base: base, discard: true}
	expected := operations.OutcomeAccepted
	if name == "voter" {
		client, err = operations.NewClient(f.endpoint, discard)
		if err != nil {
			t.Fatal("initialize response-discard qualification")
		}
		expected = operations.OutcomeUnknown
	}
	restartSubmit(t, t.Context(), client, p, false, expected)
	c, r = f.await(t, p)
	after := restartOwnedPods(t, c, r)
	if len(after) != want {
		t.Fatal("restart convergence lost an expected owned Pod")
	}
	for uid := range before {
		if after[uid] {
			t.Fatal("convergence retained an original Pod UID")
		}
	}
	if name == "voter" && (discard.patches.Load() != 1 || discard.accepts.Load() != 1) {
		t.Fatal("unknown submission did not remain one accepted PATCH followed only by reads")
	}
	t.Logf("fixture=%s ownedPodsReplaced=%d submission=%s rollout=observed", name, want, expected)
}

// TestOperatorRestartQualification uses four UID-declared fixtures in a dedicated
// Kind cluster. Only the three rolling fixtures can receive PATCH requests.
func TestOperatorRestartQualification(t *testing.T) {
	f, err := restartSettings(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if f.endpoint == "" {
		t.Skip("set OPENBAO_CLIENT_RESTART_* for explicitly authorized isolated restart qualification")
	}
	base := &http.Transport{MaxIdleConns: 4, IdleConnTimeout: 10 * time.Second}
	t.Cleanup(base.CloseIdleConnections)
	wire := &restartWire{fixture: f, base: base}
	f.reads, err = collect.NewClient(f.endpoint, wire)
	if err != nil {
		t.Fatal("initialize bounded restart reads")
	}
	writes, err := operations.NewClient(f.endpoint, wire)
	if err != nil {
		t.Fatal("initialize guarded restart writes")
	}
	f.guards(t, wire, writes)
	for index, name := range restartNames[:3] {
		t.Run(name, func(t *testing.T) {
			f.rollout(t, name, []int{1, 4, 1}[index], writes, base)
		})
	}
}

func TestRestartQualificationScopeGuard(t *testing.T) {
	t.Parallel()
	fixture := `{"voter":"00000000-0000-0000-0000-000000000001","read-pool":"00000000-0000-0000-0000-000000000002",` +
		`"zero-read":"00000000-0000-0000-0000-000000000003","blue-green":"00000000-0000-0000-0000-000000000004"}`
	settings := map[string]string{
		"ENDPOINT": "http://127.0.0.1:18164", "CONTEXT": "kind-client-restart-rc1",
		"NAMESPACE": "restart-tests", "GROUP": "kubebao.org", "ALLOW_MUTATION": "true", "FIXTURES": fixture,
	}
	getenv := func(k string) string { return settings[strings.TrimPrefix(k, restartPrefix)] }
	f, err := restartSettings(getenv)
	if err != nil {
		t.Fatal("the dedicated restart fixture was rejected")
	}
	for key, value := range map[string]string{
		"ENDPOINT": "http://example.com:18164", "CONTEXT": "production",
		"NAMESPACE": "default", "GROUP": "openbao.org", "ALLOW_MUTATION": "false", "FIXTURES": `{}`,
	} {
		original := settings[key]
		settings[key] = value
		if _, err := restartSettings(getenv); err == nil {
			t.Fatalf("unsafe restart setting accepted: %s", key)
		}
		settings[key] = original
	}
	for _, path := range []string{
		"/api/v1/namespaces/restart-tests/secrets", "/api/v1/namespaces/restart-tests/pods/voter-0/exec",
		"/apis/openbao.org/v1alpha1/namespaces/restart-tests/openbaoclusters/voter",
		"/apis/kubebao.org/v1alpha1/namespaces/default/openbaoclusters/voter",
	} {
		r, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.endpoint+path, nil)
		if err != nil || f.permits(r) {
			t.Fatal("an out-of-scope restart qualification request was accepted")
		}
	}
	base := qualificationTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("canceled fixture read reached the transport")
		return nil, nil
	})
	reader, err := collect.NewClient(f.endpoint, base)
	if err != nil {
		t.Fatal(err)
	}
	for _, deadline := range []bool{false, true} {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
		want := evidence.CollectionTimeout
		if !deadline {
			cancel()
			ctx, cancel = context.WithCancel(t.Context())
			cancel()
			want = evidence.CollectionCancelled
		}
		r, err := reader.Clusters(ctx, f.request("voter"))
		cancel()
		if err != nil || r.Collection.Outcome != want || r.Requests != 0 {
			t.Fatal("canceled or expired restart read was not classified without transport")
		}
	}
}
