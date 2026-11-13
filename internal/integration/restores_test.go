package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/diagnose"
	"github.com/kubebao/openbao-operator-client/evidence"
)

const restorePrefix = "OPENBAO_CLIENT_RESTORES_"

// TestCompletedDisposableRestore checks the retained receipt after the operator
// deletes its disposable target. It does not interpret absence as recovery.
func TestCompletedDisposableRestore(t *testing.T) {
	f := restoreQualification(t)
	if f.phase != "Completed" || f.wantFinding != "none" || f.wantCoverage != "insufficient" {
		t.Fatal("terminal qualification requires Completed, no finding, and insufficient current-target coverage")
	}
	transport := &http.Transport{MaxIdleConns: 2, IdleConnTimeout: 10 * time.Second}
	t.Cleanup(transport.CloseIdleConnections)
	client, err := collect.NewClient(f.endpoint, f.readTransport(transport))
	if err != nil {
		t.Fatal("initialize terminal restore collector")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	request := collect.Request{
		Scope: evidence.Scope{
			Origin: f.origin, Group: f.group, Version: "v1alpha1", Resource: "openbaoclusters",
			Namespace: f.namespace, Name: f.targetName, Action: evidence.ReadGet,
		},
		Contract: f.contract, ExpectedUID: f.targetUID, Limits: collect.DefaultLimits(), Projection: adapter.DefaultProjectionLimits(),
	}
	clusters, err := client.Clusters(ctx, request)
	if err != nil || clusters.Collection.Outcome != evidence.CollectionDeleted || len(clusters.Observations) != 0 {
		t.Fatal("disposable target must be absent without a replacement observation")
	}
	request.Scope.Resource, request.Scope.Name, request.Scope.Action = "openbaorestores", "", evidence.ReadList
	request.ExpectedUID = ""
	restores, err := client.Restores(ctx, request)
	if err != nil || restores.Collection.Outcome != evidence.CollectionComplete {
		t.Fatal("collect retained terminal restore receipts")
	}
	observed := f.observation(t, restores.Observations)
	if observed.TargetLifecycle.State != evidence.ValueReported || *observed.TargetLifecycle.Value != "Disposable" ||
		observed.Execution.TerminalResult.State != evidence.ValueReported || *observed.Execution.TerminalResult.Value != "Succeeded" ||
		observed.Target.UID.State != evidence.ValueReported || *observed.Target.UID.Value != f.targetUID ||
		observed.Target.Cleanup.State != evidence.ValueReported || *observed.Target.Cleanup.Value != "Complete" ||
		observed.Target.AppliedAt.State != evidence.ValueReported {
		t.Fatal("completed disposable receipt lacks successful execution, original target UID, application time, or complete cleanup")
	}
	capture := diagnose.Capture{SchemaVersion: evidence.SchemaVersion, Clusters: []diagnose.ClusterInput{{
		Subject: f.targetIdentity(), AdapterContract: string(f.contract), Collection: clusters.Collection,
		Restores: &diagnose.ResourceInput[evidence.Restore]{
			AdapterContract: string(f.contract), Collection: restores.Collection, Observations: restores.Observations,
		},
	}}}
	policy := diagnose.DefaultPolicy(time.Now().UTC(), []string{string(f.contract)})
	policy.Rules = []diagnose.RuleID{diagnose.RuleRestore}
	policy.RestoreUIDBindingContracts = []string{string(f.contract)}
	report, err := diagnose.Analyze(capture, policy)
	if err != nil {
		t.Fatal("analyze the absent target without inferring recovery")
	}
	f.assertReport(t, report)
	t.Log("retained Completed receipt confirms reported target cleanup; current target remains absent")
}

type restoreFixture struct {
	endpoint, origin, namespace, group string
	targetName, targetUID              string
	requestName, requestUID, phase     string
	wantFinding, wantCoverage          string
	contract                           adapter.Contract
}

func restoreQualification(t *testing.T) restoreFixture {
	t.Helper()
	endpoint := os.Getenv(restorePrefix + "ENDPOINT")
	if endpoint == "" {
		t.Skip("set OPENBAO_CLIENT_RESTORES_ENDPOINT for read-only restore qualification")
	}
	value := func(name string) string { return os.Getenv(restorePrefix + name) }
	f := restoreFixture{
		endpoint: endpoint, origin: value("CONTEXT"), namespace: value("NAMESPACE"), group: value("GROUP"),
		targetName: value("TARGET"), targetUID: value("TARGET_UID"),
		requestName: value("REQUEST"), requestUID: value("REQUEST_UID"), phase: value("PHASE"),
		wantFinding: value("FINDING"), wantCoverage: value("COVERAGE"),
	}
	switch f.group {
	case "kubebao.org":
		f.contract = adapter.KubeBaoV1Alpha1Snapshot
	case "openbao.org":
		f.contract = adapter.V1Alpha1Snapshot
	}
	if !f.valid() {
		t.Fatal("restore qualification requires a loopback HTTP proxy, declared identities, inspected group, and bounded expectations")
	}
	return f
}

func validRestoreEndpoint(endpoint string) bool {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" ||
		parsed.Fragment != "" || !net.ParseIP(parsed.Hostname()).IsLoopback() {
		return false
	}
	port, err := strconv.Atoi(parsed.Port())
	return err == nil && port > 0 && port <= 65535
}

func validRestoreResourceName(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if !qualificationName.MatchString(label) {
			return false
		}
	}
	return true
}

func (f restoreFixture) targetIdentity() evidence.Identity {
	return evidence.Identity{
		Origin: f.origin, Group: f.group, Kind: "OpenBaoCluster", Namespace: f.namespace, Name: f.targetName, UID: f.targetUID,
	}
}

func (f restoreFixture) requestIdentity() evidence.Identity {
	return evidence.Identity{
		Origin: f.origin, Group: f.group, Kind: "OpenBaoRestore", Namespace: f.namespace, Name: f.requestName, UID: f.requestUID,
	}
}

func (f restoreFixture) valid() bool {
	inspected := f.group == "kubebao.org" && f.contract == adapter.KubeBaoV1Alpha1Snapshot ||
		f.group == "openbao.org" && f.contract == adapter.V1Alpha1Snapshot
	return validRestoreEndpoint(f.endpoint) && inspected && f.targetIdentity().Validate() == nil &&
		f.requestIdentity().Validate() == nil && len(f.namespace) <= 63 && qualificationName.MatchString(f.namespace) &&
		validRestoreResourceName(f.targetName) && validRestoreResourceName(f.requestName) &&
		slices.Contains([]string{"Pending", "Validating", "Running", "Completed", "Failed", "Unknown"}, f.phase) &&
		slices.Contains([]string{"none", "info", "warning", "error"}, f.wantFinding) &&
		slices.Contains([]string{"evaluated", "insufficient"}, f.wantCoverage)
}

func (f restoreFixture) permits(request *http.Request) bool {
	endpoint, err := url.Parse(f.endpoint)
	if err != nil || request == nil || request.URL == nil || request.Method != http.MethodGet || request.URL.User != nil ||
		request.URL.Scheme != endpoint.Scheme || request.URL.Host != endpoint.Host || request.URL.Fragment != "" ||
		request.Host != "" && request.Host != endpoint.Host {
		return false
	}
	prefix := "/apis/" + f.group + "/v1alpha1/namespaces/" + f.namespace + "/"
	if request.URL.EscapedPath() == prefix+"openbaoclusters/"+f.targetName {
		return request.URL.RawQuery == ""
	}
	if request.URL.EscapedPath() != prefix+"openbaorestores" {
		return false
	}
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		return false
	}
	for name, values := range query {
		if name != "limit" && name != "continue" || len(values) != 1 {
			return false
		}
	}
	limit, err := strconv.Atoi(query.Get("limit"))
	return err == nil && limit > 0 && limit <= collect.DefaultLimits().PageSize
}

func (f restoreFixture) readTransport(base http.RoundTripper) http.RoundTripper {
	return qualificationTransport(func(request *http.Request) (*http.Response, error) {
		if !f.permits(request) {
			return nil, errors.New("request outside declared read-only restore qualification scope")
		}
		return base.RoundTrip(request)
	})
}

func (f restoreFixture) collect(t *testing.T, ctx context.Context) (diagnose.Capture, evidence.Restore) {
	t.Helper()
	transport := &http.Transport{MaxIdleConns: 2, IdleConnTimeout: 10 * time.Second}
	t.Cleanup(transport.CloseIdleConnections)
	client, err := collect.NewClient(f.endpoint, f.readTransport(transport))
	if err != nil {
		t.Fatal("initialize restore qualification collector")
	}
	request := collect.Request{
		Scope: evidence.Scope{
			Origin: f.origin, Group: f.group, Version: "v1alpha1", Resource: "openbaoclusters",
			Namespace: f.namespace, Name: f.targetName, Action: evidence.ReadGet,
		},
		Contract: f.contract, ExpectedUID: f.targetUID, Limits: collect.DefaultLimits(), Projection: adapter.DefaultProjectionLimits(),
	}
	clusters, err := client.Clusters(ctx, request)
	if err != nil || clusters.Collection.Outcome != evidence.CollectionComplete || len(clusters.Observations) != 1 {
		t.Fatal("collect the declared target UID")
	}
	target := clusters.Observations[0]
	if target.Reference.Identity != f.targetIdentity() || target.Reference.APIVersion != f.group+"/v1alpha1" {
		t.Fatal("collected target differs from the declared scope and identity")
	}
	restores := request
	restores.Scope.Resource, restores.Scope.Name, restores.Scope.Action = "openbaorestores", "", evidence.ReadList
	restores.ExpectedUID = ""
	related, err := client.Related(ctx, target, collect.RelatedPlan{
		Restores: &restores, Verify: request, Limits: collect.DefaultLimits(),
	})
	if err != nil || related.Outcome != evidence.CollectionComplete || related.Restores == nil ||
		related.Restores.Collection.Outcome != evidence.CollectionComplete || related.Verification.State != evidence.VerificationVerified {
		t.Fatal("collect complete restore scope and verify the original target UID")
	}
	observed := f.observation(t, related.Restores.Observations)
	capture := diagnose.Capture{SchemaVersion: evidence.SchemaVersion, Clusters: []diagnose.ClusterInput{{
		Subject: target.Reference.Identity, AdapterContract: string(f.contract), Observation: &target,
		Collection: clusters.Collection, Verification: &related.Verification,
		Restores: &diagnose.ResourceInput[evidence.Restore]{
			AdapterContract: string(f.contract), Collection: related.Restores.Collection, Observations: related.Restores.Observations,
		},
	}}}
	return capture, observed
}

func (f restoreFixture) observation(t *testing.T, requests []evidence.Restore) evidence.Restore {
	t.Helper()
	for _, observed := range requests {
		if observed.Reference.Identity != f.requestIdentity() {
			continue
		}
		if observed.Reference.APIVersion != f.group+"/v1alpha1" || observed.Phase.State != evidence.ValueReported ||
			*observed.Phase.Value != f.phase {
			t.Fatal("the declared restore UID does not report the expected API version and phase")
		}
		return observed
	}
	t.Fatal("the declared restore UID is absent from the scoped LIST")
	return evidence.Restore{}
}

func (f restoreFixture) assertReport(t *testing.T, report diagnose.Report) {
	t.Helper()
	found := false
	for _, finding := range report.Findings {
		if finding.Subject == f.requestIdentity() && finding.Rule == diagnose.RuleRestore {
			found = true
			if string(finding.Severity) != f.wantFinding {
				t.Fatal("restore finding severity differs from the declared expectation")
			}
		}
	}
	if found != (f.wantFinding != "none") {
		t.Fatal("restore finding presence differs from the declared expectation")
	}
	covered := false
	for _, coverage := range report.Coverage {
		if coverage.Rule == diagnose.RuleRestore && coverage.Subject == f.targetIdentity() {
			covered = true
			if string(coverage.State) != f.wantCoverage {
				t.Fatalf("restore coverage is %s, expected %s", coverage.State, f.wantCoverage)
			}
		}
	}
	if !covered {
		t.Fatal("restore coverage is missing")
	}
}

func assertRestoreReplay(t *testing.T, capture diagnose.Capture, policy diagnose.Policy, report diagnose.Report) {
	t.Helper()
	encoded, err := json.Marshal(capture)
	if err != nil {
		t.Fatal("serialize projected restore capture")
	}
	var replay diagnose.Capture
	if err := json.Unmarshal(encoded, &replay); err != nil {
		t.Fatal("decode projected restore capture")
	}
	replayed, err := diagnose.Analyze(replay, policy)
	if err != nil {
		t.Fatal("replay projected restore evidence")
	}
	first, err := json.Marshal(report)
	if err != nil {
		t.Fatal("serialize projected report")
	}
	second, err := json.Marshal(replayed)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("serialized restore replay changed the diagnostic report")
	}
}

// TestOperatorNonemptyRestore reads a caller-provisioned request. It never creates,
// retries, acknowledges, or cleans up a restore, including an unknown execution.
func TestOperatorNonemptyRestore(t *testing.T) {
	f := restoreQualification(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	capture, observed := f.collect(t, ctx)
	target := *capture.Clusters[0].Observation
	association, err := adapter.AssociateRestore(target, observed)
	if err != nil || association.State != evidence.AssociationConfirmed || association.Subject != f.requestIdentity() ||
		association.Target != f.targetIdentity() {
		t.Fatal("restore lacks documented target UID confirmation")
	}
	policy := diagnose.DefaultPolicy(time.Now().UTC(), []string{string(f.contract)})
	policy.Rules = []diagnose.RuleID{diagnose.RuleRestore}
	policy.RestoreUIDBindingContracts = []string{string(f.contract)}
	report, err := diagnose.Analyze(capture, policy)
	if err != nil {
		t.Fatal("analyze projected restore evidence")
	}
	f.assertReport(t, report)
	assertRestoreReplay(t, capture, policy, report)
	t.Logf("group=%s phase=%s association=%s basis=%s finding=%s coverage=%s candidates=%d",
		f.group, f.phase, association.State, association.Basis, f.wantFinding, f.wantCoverage,
		len(capture.Clusters[0].Restores.Observations))
}

func restoreGuardFixture() restoreFixture {
	return restoreFixture{
		endpoint: "http://127.0.0.1:8001", origin: "declared-demo", namespace: "team", group: "kubebao.org",
		targetName: "target", targetUID: "target-uid", requestName: "request", requestUID: "request-uid", phase: "Unknown",
		wantFinding: "error", wantCoverage: "insufficient", contract: adapter.KubeBaoV1Alpha1Snapshot,
	}
}

func TestRestoreQualificationConfigurationGuard(t *testing.T) {
	t.Parallel()
	for _, change := range []func(*restoreFixture){
		func(f *restoreFixture) { f.endpoint = "http://example.com:8001" },
		func(f *restoreFixture) { f.endpoint = "https://127.0.0.1:8001" },
		func(f *restoreFixture) { f.endpoint = "http://127.0.0.1" },
		func(f *restoreFixture) { f.endpoint = "http://127.0.0.1:0" },
		func(f *restoreFixture) { f.endpoint = "http://127.0.0.1:65536" },
		func(f *restoreFixture) { f.endpoint = "http://user:password@127.0.0.1:8001" },
		func(f *restoreFixture) { f.endpoint = "http://127.0.0.1:8001/prefix" },
		func(f *restoreFixture) { f.endpoint = "http://127.0.0.1:8001?query=value" },
		func(f *restoreFixture) { f.endpoint = "http://127.0.0.1:8001#fragment" },
		func(f *restoreFixture) { f.origin = "" },
		func(f *restoreFixture) { f.namespace = "../other" },
		func(f *restoreFixture) { f.targetName = ".." },
		func(f *restoreFixture) { f.targetUID = "" },
		func(f *restoreFixture) { f.requestName = "" },
		func(f *restoreFixture) { f.requestUID = "" },
		func(f *restoreFixture) { f.group = "other.example" },
		func(f *restoreFixture) { f.contract = adapter.V1Alpha1Snapshot },
		func(f *restoreFixture) { f.phase = "Succeeded" },
		func(f *restoreFixture) { f.wantFinding = "critical" },
		func(f *restoreFixture) { f.wantCoverage = "healthy" },
	} {
		f := restoreGuardFixture()
		change(&f)
		if f.valid() {
			t.Fatal("restore qualification accepted an invalid endpoint, identity, contract, or expectation")
		}
	}
	for _, endpoint := range []string{"http://127.0.0.1:8001", "http://[::1]:8001"} {
		f := restoreGuardFixture()
		f.endpoint = endpoint
		if !f.valid() {
			t.Fatal("restore qualification rejected a declared loopback read scope")
		}
	}
}

func TestRestoreQualificationTransportBlocksWritesAndScopeExpansion(t *testing.T) {
	t.Parallel()
	f := restoreGuardFixture()
	for _, path := range []string{
		"/apis/kubebao.org/v1alpha1/namespaces/team/openbaoclusters/target",
		"/apis/kubebao.org/v1alpha1/namespaces/team/openbaorestores?limit=100",
		"/apis/kubebao.org/v1alpha1/namespaces/team/openbaorestores?limit=50&continue=opaque%2Btoken",
	} {
		request, err := http.NewRequest(http.MethodGet, f.endpoint+path, http.NoBody)
		if err != nil || !f.permits(request) {
			t.Fatal("read guard rejected the declared target GET or unfiltered paginated restore LIST")
		}
	}
	for _, value := range []struct{ method, endpoint, path string }{
		{http.MethodPatch, f.endpoint, "/apis/kubebao.org/v1alpha1/namespaces/team/openbaoclusters/target"},
		{http.MethodPost, f.endpoint, "/apis/kubebao.org/v1alpha1/namespaces/team/openbaorestores"},
		{http.MethodDelete, f.endpoint, "/apis/kubebao.org/v1alpha1/namespaces/team/openbaorestores/request"},
		{http.MethodGet, "http://127.0.0.1:9001", "/apis/kubebao.org/v1alpha1/namespaces/team/openbaorestores?limit=100"},
		{http.MethodGet, f.endpoint, "/apis/openbao.org/v1alpha1/namespaces/team/openbaorestores?limit=100"},
		{http.MethodGet, f.endpoint, "/apis/kubebao.org/v1alpha1/namespaces/other/openbaorestores?limit=100"},
		{http.MethodGet, f.endpoint, "/apis/kubebao.org/v1alpha1/openbaorestores?limit=100"},
		{http.MethodGet, f.endpoint, "/apis/kubebao.org/v1alpha1/namespaces/team/openbaoclusters/other"},
		{http.MethodGet, f.endpoint, "/api/v1/namespaces/team/secrets"},
		{http.MethodGet, f.endpoint, "/apis"},
		{http.MethodGet, f.endpoint, "/apis/kubebao.org/v1alpha1/namespaces/team/openbaorestores?limit=100&labelSelector=app%3Dbao"},
		{
			http.MethodGet, f.endpoint,
			"/apis/kubebao.org/v1alpha1/namespaces/team/openbaorestores?limit=100&fieldSelector=metadata.name%3Drequest",
		},
		{http.MethodGet, f.endpoint, "/apis/kubebao.org/v1alpha1/namespaces/team/openbaorestores?limit=100&limit=1"},
	} {
		request, err := http.NewRequest(value.method, value.endpoint+value.path, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		called := false
		transport := f.readTransport(qualificationTransport(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("unexpected transport call")
		}))
		response, err := transport.RoundTrip(request)
		if response != nil && response.Body != nil {
			if err := response.Body.Close(); err != nil {
				t.Fatal(err)
			}
		}
		if err == nil || called {
			t.Fatal("restore qualification transport admitted a write, restricted LIST, or undeclared read scope")
		}
	}
}
