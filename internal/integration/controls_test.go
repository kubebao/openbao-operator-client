package integration

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/evidence"
	"github.com/kubebao/openbao-operator-client/operations"
)

const controlsPrefix = "OPENBAO_CLIENT_CONTROLS_"

type controlFixture struct {
	endpoint, origin, namespace, group, deniedUser string
	contract                                       adapter.Contract
	names                                          []string
	transport                                      *http.Transport
	reads                                          *collect.Client
	writes                                         *operations.Client
}

var qualificationName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func controlQualification(t *testing.T) controlFixture {
	t.Helper()
	endpoint := os.Getenv(controlsPrefix + "ENDPOINT")
	if endpoint == "" {
		t.Skip("set OPENBAO_CLIENT_CONTROLS_ENDPOINT for isolated operator qualification")
	}
	contextName := os.Getenv(controlsPrefix + "CONTEXT")
	namespace := os.Getenv(controlsPrefix + "NAMESPACE")
	if !validQualificationScope(endpoint, contextName, namespace) {
		t.Fatal("qualification requires a loopback proxy and matching kind-client-controls- / client-controls- scopes")
	}
	fixture := controlFixture{
		endpoint: endpoint, origin: contextName, namespace: namespace,
		group: os.Getenv(controlsPrefix + "GROUP"), deniedUser: os.Getenv(controlsPrefix + "DENIED_USER"),
		names:     strings.Split(os.Getenv(controlsPrefix+"FIXTURES"), ","),
		transport: &http.Transport{MaxIdleConns: 4, IdleConnTimeout: 15 * time.Second},
	}
	t.Cleanup(fixture.transport.CloseIdleConnections)
	switch fixture.group {
	case "openbao.org":
		fixture.contract = adapter.OpenBaoV1Alpha1
	case "kubebao.org":
		fixture.contract = adapter.KubeBaoV1Alpha1
	default:
		t.Fatal("qualification requires an approved operator API group")
	}
	if len(fixture.names) != 4 {
		t.Fatal("FIXTURES must list voter-only, positive read-pool, zero read-pool, and Idle blue/green names in that order")
	}
	seen := map[string]bool{}
	for _, name := range fixture.names {
		if !qualificationName.MatchString(name) || len(name) > 63 || seen[name] {
			t.Fatal("qualification fixture names must be distinct DNS labels")
		}
		seen[name] = true
	}
	if fixture.deniedUser != "system:serviceaccount:"+namespace+":controls-reader" {
		t.Fatal("DENIED_USER must select the fixture namespace controls-reader ServiceAccount")
	}
	var err error
	fixture.reads, err = collect.NewClient(endpoint, fixture.transport)
	if err != nil {
		t.Fatal("initialize qualification collection")
	}
	fixture.writes, err = operations.NewClient(endpoint, fixture.transport)
	if err != nil {
		t.Fatal("initialize qualification controls")
	}
	return fixture
}

func validQualificationScope(endpoint, contextName, namespace string) bool {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		parsed.Path != "" || parsed.Port() == "" {
		return false
	}
	ip := net.ParseIP(parsed.Hostname())
	if ip == nil || !ip.IsLoopback() || !strings.HasPrefix(namespace, "client-controls-") ||
		len(namespace) > 63 || !qualificationName.MatchString(namespace) {
		return false
	}
	return contextName == "kind-"+namespace && namespace != "client-controls-"
}

func TestControlQualificationScopeGuard(t *testing.T) {
	t.Parallel()
	for _, value := range []struct{ endpoint, context, namespace string }{
		{"http://example.com:8001", "kind-client-controls-test", "client-controls-test"},
		{"http://127.0.0.1:8001", "production", "client-controls-test"},
		{"http://127.0.0.1:8001", "kind-client-controls-test", "default"},
		{"http://user:password@127.0.0.1:8001", "kind-client-controls-test", "client-controls-test"},
		{"http://127.0.0.1:8001/prefix", "kind-client-controls-test", "client-controls-test"},
	} {
		if validQualificationScope(value.endpoint, value.context, value.namespace) {
			t.Fatal("accepted an unsafe operator qualification scope")
		}
	}
	if !validQualificationScope("http://127.0.0.1:8001", "kind-client-controls-test", "client-controls-test") {
		t.Fatal("rejected the dedicated loopback qualification scope")
	}
}

func (f controlFixture) request(name string) collect.Request {
	return collect.Request{
		Scope: evidence.Scope{
			Origin: f.origin, Group: f.group, Version: "v1alpha1", Resource: "openbaoclusters",
			Namespace: f.namespace, Name: name, Action: evidence.ReadGet,
		},
		Contract: f.contract, Limits: collect.DefaultLimits(), Projection: adapter.DefaultProjectionLimits(),
	}
}

func (f controlFixture) cluster(t *testing.T, ctx context.Context, name string) evidence.Cluster {
	t.Helper()
	result, err := f.reads.Clusters(ctx, f.request(name))
	if err != nil || result.Collection.Outcome != evidence.CollectionComplete || len(result.Observations) != 1 {
		t.Fatalf("fixture GET failed: outcome=%s observations=%d", result.Collection.Outcome, len(result.Observations))
	}
	return result.Observations[0]
}

func (f controlFixture) plan(t *testing.T, ctx context.Context, name string, control operations.Control, enabled bool) operations.Plan {
	t.Helper()
	cluster := f.cluster(t, ctx, name)
	var plan operations.Plan
	var err error
	if control == operations.ControlPause {
		plan, err = operations.PreparePause(cluster, f.contract, enabled)
	} else {
		plan, err = operations.PrepareMaintenance(cluster, f.contract, enabled)
	}
	if err != nil {
		t.Fatal("prepare fixture control")
	}
	return plan
}

func submitControl(t *testing.T, ctx context.Context, client *operations.Client,
	plan operations.Plan, dryRun bool, expected operations.Outcome,
) operations.Result {
	t.Helper()
	options := operations.DefaultSubmitOptions()
	options.DryRun = dryRun
	result, err := client.Submit(ctx, plan, options)
	if err != nil || result.Outcome != expected || result.Requests != 1 {
		t.Fatalf("control submission: control=%s enabled=%t outcome=%s status=%d requests=%d expected=%s",
			plan.Control, plan.Enabled, result.Outcome, result.StatusCode, result.Requests, expected)
	}
	if (expected == operations.OutcomeAccepted || expected == operations.OutcomeDryRunAccepted) &&
		(result.Receipt == nil || result.Receipt.Reference.Identity != plan.Target.Identity) {
		t.Fatal("accepted control lacks a receipt for the bound target")
	}
	return result
}

func (f controlFixture) related(ctx context.Context, cluster evidence.Cluster) (collect.RelatedResult, error) {
	request := f.request(cluster.Reference.Identity.Name)
	request.ExpectedUID = cluster.Reference.Identity.UID
	setRequest := request
	setRequest.Contract = adapter.KubernetesAppsV1
	setRequest.Scope.Group, setRequest.Scope.Version, setRequest.Scope.Resource = "apps", "v1", "statefulsets"
	setRequest.Scope.Action, setRequest.Scope.Name = evidence.ReadList, ""
	setRequest.Scope.Selector = "openbao.org/cluster=" + cluster.Reference.Identity.Name
	setRequest.ExpectedUID = ""
	podRequest := setRequest
	podRequest.Contract, podRequest.Scope.Group, podRequest.Scope.Resource = adapter.KubernetesCoreV1, "", "pods"
	return f.reads.Related(ctx, cluster, collect.RelatedPlan{
		StatefulSets: &setRequest, Pods: &podRequest, Verify: request, Limits: collect.DefaultLimits(),
	})
}

func (f controlFixture) await(t *testing.T, plan operations.Plan) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	var last operations.Assessment
	for {
		cluster := f.cluster(t, ctx, plan.Target.Identity.Name)
		var err error
		if plan.Control == operations.ControlPause {
			last, err = operations.AssessPause(plan, cluster, operations.DefaultAssessmentPolicy(time.Now().UTC()))
		} else {
			var related collect.RelatedResult
			related, err = f.related(ctx, cluster)
			if err == nil {
				last, err = operations.AssessMaintenance(plan, cluster, related, operations.DefaultAssessmentPolicy(time.Now().UTC()))
			}
		}
		if err != nil {
			t.Fatal("assess fixture control")
		}
		if last.State == operations.AssessmentObserved {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("control acknowledgement timed out: state=%s reasons=%v", last.State, last.Reasons)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func TestOperatorControls(t *testing.T) {
	fixture := controlQualification(t)
	ctx := t.Context()
	for index, name := range fixture.names {
		cluster := fixture.cluster(t, ctx, name)
		if !booleanIs(cluster.Paused, false) || !maintenanceOff(cluster.Maintenance) ||
			cluster.Acknowledgement() != evidence.GenerationCurrent {
			t.Fatal("fixtures must start reconciled, unpaused, and outside maintenance")
		}
		checkControlTopology(t, cluster, index)
	}
	if !t.Run("guarded-api-submission", func(t *testing.T) {
		fixture.qualifySubmission(t)
	}) {
		return
	}
	for _, name := range fixture.names {
		t.Run("maintenance-"+name, func(t *testing.T) {
			for _, enabled := range []bool{true, false} {
				plan := fixture.plan(t, t.Context(), name, operations.ControlMaintenance, enabled)
				submitControl(t, t.Context(), fixture.writes, plan, false, operations.OutcomeAccepted)
				fixture.await(t, plan)
			}
		})
	}
}

func booleanIs(value evidence.Boolean, expected bool) bool {
	return (value.State == evidence.ValueReported || value.State == evidence.ValueImplied) &&
		value.Value != nil && *value.Value == expected
}

func maintenanceOff(value evidence.Maintenance) bool {
	return value.State == evidence.ObjectAbsent || value.State == evidence.ObjectNull ||
		(value.State == evidence.ObjectReported && booleanIs(value.Enabled, false))
}

func checkControlTopology(t *testing.T, cluster evidence.Cluster, index int) {
	t.Helper()
	if cluster.ReadyReplicas.Value == nil || cluster.DesiredReplicas.Value == nil ||
		*cluster.ReadyReplicas.Value != *cluster.DesiredReplicas.Value {
		t.Fatal("qualification requires a healthy voter workload")
	}
	switch index {
	case 0:
		if cluster.ReadReplicas.Configuration != evidence.ObjectAbsent && cluster.ReadReplicas.Configuration != evidence.ObjectNull {
			t.Fatal("the first fixture must have no configured read pool")
		}
	case 1, 2:
		if cluster.ReadReplicas.Configuration != evidence.ObjectReported || cluster.ReadReplicas.Desired.Value == nil ||
			(index == 1 && *cluster.ReadReplicas.Desired.Value <= 0) || (index == 2 && *cluster.ReadReplicas.Desired.Value != 0) {
			t.Fatal("the second and third fixtures require positive and zero read pools")
		}
	case 3:
		if cluster.BlueGreen.State != evidence.ObjectReported || cluster.BlueGreen.Phase.Value == nil ||
			*cluster.BlueGreen.Phase.Value != "Idle" {
			t.Fatal("the fourth fixture requires reported Idle blue/green state")
		}
	}
}

type qualificationTransport func(*http.Request) (*http.Response, error)

func (f qualificationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func (f controlFixture) qualifySubmission(t *testing.T) {
	t.Helper()
	ctx, name := t.Context(), f.names[0]
	initial := f.cluster(t, ctx, name)
	plan := f.plan(t, ctx, name, operations.ControlPause, true)
	submitControl(t, ctx, f.writes, plan, true, operations.OutcomeDryRunAccepted)
	after := f.cluster(t, ctx, name)
	if after.Reference.Identity != initial.Reference.Identity || !booleanIs(after.Paused, false) ||
		*initial.Generation.Value != *after.Generation.Value {
		t.Fatal("server dry-run persisted a control change")
	}
	f.qualifyDeniedAndCanceled(t, plan)
	// The old plan becomes stale when the real pause changes metadata.generation.
	plan = f.plan(t, ctx, name, operations.ControlPause, true)
	submitControl(t, ctx, f.writes, plan, false, operations.OutcomeAccepted)
	f.await(t, plan)
	for _, guard := range []string{"uid", "resource-version", "generation"} {
		guardPlan := f.plan(t, ctx, name, operations.ControlPause, false)
		switch guard {
		case "uid":
			guardPlan.Target.Identity.UID = "wrong-qualification-uid"
		case "resource-version":
			guardPlan.Target.ResourceVersion = plan.Target.ResourceVersion
		case "generation":
			generation := *guardPlan.Generation.Value + 1
			guardPlan.Generation.Value = &generation
		}
		result := submitControl(t, ctx, f.writes, guardPlan, false, operations.OutcomeInvalid)
		if result.StatusCode != http.StatusUnprocessableEntity {
			t.Fatal("the API server did not reject a JSON Patch guard with HTTP 422")
		}
		if !booleanIs(f.cluster(t, ctx, name).Paused, true) {
			t.Fatal("a rejected guard changed the paused fixture")
		}
	}
	f.qualifyUnknown(t)
	// A write while paused still advances generation and can trigger a status write.
	// Wait for acknowledgement before preparing the next independent request.
	f.await(t, plan)
	maintenance := f.plan(t, ctx, name, operations.ControlMaintenance, false)
	submitControl(t, ctx, f.writes, maintenance, false, operations.OutcomeAccepted)
	f.await(t, plan)
	resume := f.plan(t, ctx, name, operations.ControlPause, false)
	submitControl(t, ctx, f.writes, resume, false, operations.OutcomeAccepted)
	f.await(t, resume)
}

func (f controlFixture) qualifyDeniedAndCanceled(t *testing.T, plan operations.Plan) {
	t.Helper()
	deniedTransport := qualificationTransport(func(request *http.Request) (*http.Response, error) {
		copyRequest := request.Clone(request.Context())
		copyRequest.Header.Set("Impersonate-User", f.deniedUser)
		return f.transport.RoundTrip(copyRequest)
	})
	denied, err := operations.NewClient(f.endpoint, deniedTransport)
	if err != nil {
		t.Fatal("initialize denied controls transport")
	}
	deniedResult := submitControl(t, t.Context(), denied, plan, false, operations.OutcomeDenied)
	if deniedResult.StatusCode != http.StatusForbidden {
		t.Fatal("the read-only ServiceAccount did not receive an RBAC denial")
	}
	calls := 0
	cancelTransport := qualificationTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		return f.transport.RoundTrip(request)
	})
	canceled, err := operations.NewClient(f.endpoint, cancelTransport)
	if err != nil {
		t.Fatal("initialize canceled controls transport")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := canceled.Submit(ctx, plan, operations.DefaultSubmitOptions())
	if err != nil || result.Outcome != operations.OutcomeNotSubmitted || result.Requests != 0 || calls != 0 {
		t.Fatal("a canceled control reached the transport")
	}
}

func (f controlFixture) qualifyUnknown(t *testing.T) {
	t.Helper()
	plan := f.plan(t, t.Context(), f.names[0], operations.ControlMaintenance, true)
	calls, accepted := 0, false
	transport := qualificationTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		response, err := f.transport.RoundTrip(request)
		if err != nil {
			return response, err
		}
		accepted = response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices
		if response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, errors.New("qualification discarded the completed response")
	})
	client, err := operations.NewClient(f.endpoint, transport)
	if err != nil {
		t.Fatal("initialize ambiguous controls transport")
	}
	submitControl(t, t.Context(), client, plan, false, operations.OutcomeUnknown)
	if calls != 1 || !accepted {
		t.Fatal("ambiguous outcome did not follow exactly one accepted real PATCH")
	}
	observed := f.cluster(t, t.Context(), f.names[0])
	if observed.Reference.Identity != plan.Target.Identity || !booleanIs(observed.Paused, true) ||
		!booleanIs(observed.Maintenance.Enabled, true) {
		t.Fatal("follow-up read did not establish maintenance while reconciliation remains paused")
	}
}
