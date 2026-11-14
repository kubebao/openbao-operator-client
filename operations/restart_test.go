package operations_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/evidence"
	"github.com/kubebao/openbao-operator-client/operations"
)

const restartMarker = "2026-11-11T10:00:01Z"

func restartText(s string) evidence.Text {
	return evidence.Text{State: evidence.ValueReported, Value: &s}
}

func restartCluster(t testing.TB, at time.Time, gen int64, native bool) evidence.Cluster {
	t.Helper()
	c := assessmentCluster(t, at, gen, false, false)
	c.UpgradeStrategy, c.AcceptedUpgradeStrategy = restartText("RollingUpdate"), restartText("RollingUpdate")
	if native {
		c.Reference.Identity.Group, c.Reference.APIVersion = "kubebao.org", "kubebao.org/v1alpha1"
	}
	return c
}

func restartPlan(t testing.TB, native bool) operations.RestartPlan {
	t.Helper()
	contract := adapter.OpenBaoV1Alpha1
	if native {
		contract = adapter.KubeBaoV1Alpha1
	}
	p, err := operations.PrepareRestart(restartCluster(t, assessmentTime, 1, native), contract, restartMarker)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func restartRelated(t testing.TB, c evidence.Cluster) collect.RelatedResult {
	t.Helper()
	r := assessmentRelated(t, c, false)
	s := &r.StatefulSets.Observations[0]
	s.UpdateStrategy = restartText("RollingUpdate")
	s.RollingPartition = assessmentInteger(0)
	s.CurrentRevision, s.UpdateRevision = restartText("bao-hash2"), restartText("bao-hash2")
	s.TemplateRestartAt = restartText(restartMarker)
	s.CurrentReplicas, s.UpdatedReplicas = assessmentInteger(1), assessmentInteger(1)
	p := &r.Pods.Observations[0]
	p.Metadata.RestartAt = restartText(restartMarker)
	p.Metadata.ControllerRevision = restartText("bao-hash2")
	p.Generation = assessmentInteger(1)
	p.Phase = restartText("Running")
	p.Conditions = evidence.Conditions{State: evidence.ValueReported, Values: []evidence.Condition{
		{
			Type:   "Ready",
			Status: evidence.ConditionTrue,
			Reason: evidence.Text{
				State: evidence.ValueUnknown,
			},
			ObservedGeneration: assessmentInteger(1),
			LastTransitionAt:   evidence.Timestamp{State: evidence.ValueUnknown},
		},
	}}
	return r
}

func restartObserved(t testing.TB, native bool) (operations.RestartPlan, evidence.Cluster, collect.RelatedResult) {
	t.Helper()
	p := restartPlan(t, native)
	c := restartCluster(t, assessmentTime.Add(2*time.Second), 2, native)
	c.Runtime = evidence.Runtime{State: evidence.ObjectReported, RestartAt: restartText(restartMarker)}
	return p, c, restartRelated(t, c)
}

func TestRestartPlanPreservesParentAndGuardsTarget(t *testing.T) {
	t.Parallel()
	for _, native := range []bool{false, true} {
		for _, state := range []evidence.ObjectState{evidence.ObjectAbsent, evidence.ObjectNull, evidence.ObjectReported} {
			c := restartCluster(t, assessmentTime, 1, native)
			c.Runtime.State = state
			contract := adapter.OpenBaoV1Alpha1
			if native {
				contract = adapter.KubeBaoV1Alpha1
			}
			p, err := operations.PrepareRestart(c, contract, restartMarker)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := p.Patch()
			if err != nil {
				t.Fatal(err)
			}
			var ops []struct {
				Op, Path string
				Value    json.RawMessage
			}
			if err = json.Unmarshal(raw, &ops); err != nil {
				t.Fatal(err)
			}
			if len(ops) != 4 || ops[0].Op != "test" || ops[0].Path != "/metadata/uid" || string(ops[0].Value) != `"cluster-1"` ||
				ops[1].Path != "/metadata/resourceVersion" || ops[2].Path != "/metadata/generation" || ops[3].Op != "add" {
				t.Fatal("unguarded patch")
			}
			path := "/spec/runtime"
			if state == evidence.ObjectReported {
				path += "/restartAt"
			}
			if ops[3].Path != path {
				t.Fatal("runtime parent replacement")
			}
			var replay operations.RestartPlan
			if err = json.Unmarshal(mustJSON(t, p), &replay); err != nil || !reflect.DeepEqual(p, replay) {
				t.Fatal("plan replay changed", err)
			}
		}
	}
}

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestRestartPreparationRequiresKnownUnrestrictedStrategy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*evidence.Cluster)
	}{
		{"paused", func(c *evidence.Cluster) {
			v := true
			c.Paused = evidence.Boolean{State: evidence.ValueReported, Value: &v}
		}},
		{"hold", func(c *evidence.Cluster) { c.RestoreHold = restartText("request-1") }},
		{"stale", func(c *evidence.Cluster) {
			c.ObservedGeneration = assessmentInteger(0)
			c.ReadyReplicas = evidence.Integer{State: evidence.ValueUnknown}
		}},
		{"desired-bluegreen", func(c *evidence.Cluster) { c.UpgradeStrategy = restartText("BlueGreen") }},
		{"accepted-bluegreen", func(c *evidence.Cluster) { c.AcceptedUpgradeStrategy = restartText("BlueGreen") }},
		{"unknown-accepted", func(c *evidence.Cluster) { c.AcceptedUpgradeStrategy = evidence.Text{State: evidence.ValueUnknown} }},
		{"unknown-desired", func(c *evidence.Cluster) { c.UpgradeStrategy = evidence.Text{State: evidence.ValueUnknown} }},
		{"bluegreen-status", func(c *evidence.Cluster) {
			c.BlueGreen.State = evidence.ObjectReported
			c.BlueGreen.Phase = restartText("Idle")
		}},
		{"operation-lock", func(c *evidence.Cluster) {
			c.OperationLock.State = evidence.ObjectReported
			c.OperationLock.Operation = restartText("Upgrade")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := restartCluster(t, assessmentTime, 1, true)
			tc.change(&c)
			if _, e := operations.PrepareRestart(c, adapter.KubeBaoV1Alpha1, restartMarker); !errors.Is(e, operations.ErrRestartBlocked) {
				t.Fatal(e)
			}
		})
	}
	c := restartCluster(t, assessmentTime, 1, true)
	v := true
	c.Maintenance.Enabled = evidence.Boolean{State: evidence.ValueReported, Value: &v}
	if _, e := operations.PrepareRestart(c, adapter.KubeBaoV1Alpha1, restartMarker); e != nil {
		t.Fatal("maintenance does not pause reconciliation", e)
	}
	c.Runtime = evidence.Runtime{State: evidence.ObjectReported, RestartAt: restartText(restartMarker)}
	if _, e := operations.PrepareRestart(c, adapter.KubeBaoV1Alpha1, restartMarker); !errors.Is(e, operations.ErrInvalidPlan) {
		t.Fatal("unchanged marker accepted")
	}
	c.Runtime.RestartAt = restartText("  " + restartMarker + "  ")
	if _, e := operations.PrepareRestart(c, adapter.KubeBaoV1Alpha1, restartMarker); !errors.Is(e, operations.ErrInvalidPlan) {
		t.Fatal("operator-equivalent whitespace-padded marker accepted")
	}
	for _, marker := range []string{"", " marker ", "2026-11-11T10:00:01Z\n", "CANARY"} {
		if _, e := operations.PrepareRestart(
			restartCluster(t, assessmentTime, 1, true),
			adapter.KubeBaoV1Alpha1,
			marker,
		); !errors.Is(
			e,
			operations.ErrInvalidPlan,
		) {
			t.Fatal("invalid marker accepted")
		}
	}
}

func TestRestartPlanJSONRejectsAmbiguousIntent(t *testing.T) {
	t.Parallel()
	raw := string(mustJSON(t, restartPlan(t, true)))
	for _, bad := range []string{
		strings.Replace(raw, `"restartAt":"`+restartMarker+`"`, `"restartAt":null`, 1),
		strings.Replace(raw, `"restartAt":"`+restartMarker+`"`, `"RestartAt":"`+restartMarker+`"`, 1),
		strings.Replace(raw, `"restartAt":"`+restartMarker+`"`, `"restartAt":"`+restartMarker+`","restartAt":"`+restartMarker+`"`, 1),
		strings.Replace(raw, `"runtimeState":"absent"`, `"runtimeState":"unknown"`, 1),
		raw + " {}", `{"restartAt":"` + restartMarker + `"}`,
	} {
		var p operations.RestartPlan
		if json.Unmarshal([]byte(bad), &p) == nil {
			t.Fatal("ambiguous plan accepted")
		}
	}
}

func TestRestartAssessmentRequiresRolloutEvidence(t *testing.T) {
	t.Parallel()
	for _, native := range []bool{false, true} {
		p, c, r := restartObserved(t, native)
		a, e := operations.AssessRestart(p, c, r, assessmentPolicy())
		if e != nil || a.State != operations.AssessmentObserved || a.OperatorObserved.Value == nil || !*a.OperatorObserved.Value {
			t.Fatalf("healthy rollout %+v %v", a, e)
		}
	}
	for _, tc := range []struct {
		name   string
		change func(*evidence.Cluster, *collect.RelatedResult)
	}{
		{"denied", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.Pods.Collection.Outcome = evidence.CollectionDenied
		}},
		{"partial", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.StatefulSets.Collection.Outcome = evidence.CollectionPartial
		}},
		{"filtered", func(_ *evidence.Cluster, r *collect.RelatedResult) { r.Pods.Collection.Scope.Selector = "other=true" }},
		{"stale", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			at := assessmentTime.Add(-time.Hour)
			r.Pods.Observations[0].Metadata.Reference.CollectedAt.Value = &at
		}},
		{"target-replaced", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.Verification.After.Identity.UID = "replacement"
			r.Verification.State = evidence.VerificationReplaced
		}},
		{"pod-owner", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.Pods.Observations[0].Metadata.Owners.Values[0].UID = "foreign"
		}},
		{"pod-marker", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.Pods.Observations[0].Metadata.RestartAt = restartText("old")
		}},
		{"template-marker", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.StatefulSets.Observations[0].TemplateRestartAt = restartText("old")
		}},
		{"pod-revision", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.Pods.Observations[0].Metadata.ControllerRevision = restartText("old")
		}},
		{"set-revision", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.StatefulSets.Observations[0].CurrentRevision = restartText("old")
		}},
		{"partition", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.StatefulSets.Observations[0].RollingPartition = assessmentInteger(1)
		}},
		{"on-delete", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.StatefulSets.Observations[0].UpdateStrategy = restartText("OnDelete")
		}},
		{"set-unready", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.StatefulSets.Observations[0].ReadyReplicas = assessmentInteger(0)
		}},
		{"pod-unready", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.Pods.Observations[0].Conditions.Values[0].Status = evidence.ConditionFalse
		}},
		{"pod-generation", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.Pods.Observations[0].Conditions.Values[0].ObservedGeneration = assessmentInteger(0)
		}},
		{"pod-deleting", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			at := assessmentTime
			r.Pods.Observations[0].Metadata.DeletionTimestamp = evidence.Timestamp{State: evidence.ValueReported, Value: &at}
		}},
		{"missing-pod", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.Pods.Observations = []evidence.Pod{}
			r.Pods.Collection.Items = 0
		}},
		{"min-ready-unavailable", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.StatefulSets.Observations[0].MinReadySeconds = assessmentInteger(10)
			r.StatefulSets.Observations[0].AvailableReplicas = assessmentInteger(0)
		}},
		{"duplicate-pod", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.Pods.Observations = append(r.Pods.Observations, r.Pods.Observations[0])
			r.Pods.Collection.Items++
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, c, r := restartObserved(t, true)
			tc.change(&c, &r)
			a, e := operations.AssessRestart(p, c, r, assessmentPolicy())
			if e == nil && a.State == operations.AssessmentObserved {
				t.Fatalf("unsafe observed %+v", a)
			}
		})
	}
}

func TestRestartZeroReadPoolNeedsNoPodsOrInventedCounters(t *testing.T) {
	t.Parallel()
	p, c, _ := restartObserved(t, true)
	c.ReadReplicas.Configuration = evidence.ObjectReported
	c.ReadReplicas.Desired = assessmentInteger(0)
	// Verification binds the exact resource version/generation of the updated public topology.
	r := restartRelated(t, c)
	zero := r.StatefulSets.Observations[0]
	zero.Metadata.Reference.Identity.Name = "bao-read"
	zero.Metadata.Reference.Identity.UID = "read-set"
	zero.SelectorPool, zero.TemplatePool = restartText("read-replica"), restartText("read-replica")
	zero.DesiredReplicas = assessmentInteger(0)
	zero.ReadyReplicas, zero.CurrentReplicas, zero.UpdatedReplicas = evidence.Integer{
		State: evidence.ValueUnknown,
	}, evidence.Integer{
		State: evidence.ValueUnknown,
	}, evidence.Integer{
		State: evidence.ValueUnknown,
	}
	r.StatefulSets.Observations = append(r.StatefulSets.Observations, zero)
	r.StatefulSets.Collection.Items++
	a, e := operations.AssessRestart(p, c, r, assessmentPolicy())
	if e != nil || a.State != operations.AssessmentObserved {
		t.Fatalf("zero pool %+v %v", a, e)
	}
	zeroPod := r.Pods.Observations[0]
	zeroPod.Metadata.Owners.Values = append([]evidence.OwnerReference{}, zeroPod.Metadata.Owners.Values...)
	zeroPod.Metadata.Reference.Identity.Name = "bao-read-0"
	zeroPod.Metadata.Reference.Identity.UID = "read-pod"
	zeroPod.Metadata.Pool = restartText("read-replica")
	zeroPod.Metadata.Owners.Values[0].Name = "bao-read"
	zeroPod.Metadata.Owners.Values[0].UID = "read-set"
	r.Pods.Observations = append(r.Pods.Observations, zeroPod)
	r.Pods.Collection.Items++
	a, e = operations.AssessRestart(p, c, r, assessmentPolicy())
	if e == nil && a.State == operations.AssessmentObserved {
		t.Fatal("surplus read Pod accepted")
	}
}

func TestSubmitRestartOnceAndValidateReceipt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		status         int
		marker         string
		transportError bool
		want           operations.Outcome
	}{
		{"accepted", 200, restartMarker, false, operations.OutcomeAccepted},
		{"wrong-marker", 200, "old", false, operations.OutcomeUnknown},
		{"denied", 403, restartMarker, false, operations.OutcomeDenied},
		{"conflict", 409, restartMarker, false, operations.OutcomeConflict},
		{"ambiguous", 200, restartMarker, true, operations.OutcomeUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			p := restartPlan(t, true)
			client := newControlClient(t, transportFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodPatch || !strings.Contains(req.URL.Path, "/kubebao.org/v1alpha1/") {
					t.Fatal("wrong write scope")
				}
				if tc.transportError {
					return nil, errors.New("CANARY_TRANSPORT")
				}
				raw := `{"apiVersion":"kubebao.org/v1alpha1",
"kind":"OpenBaoCluster",
"metadata":{"name":"bao",
"namespace":"team",
"uid":"cluster-1",
"generation":2,
"resourceVersion":"new"},
"spec":{"runtime":{"restartAt":"` + tc.marker + `"}},"status":{"message":"CANARY_PROVIDER_MESSAGE"}}`
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(raw))}, nil
			}))
			a, e := client.SubmitRestart(context.Background(), p, operations.DefaultSubmitOptions())
			if e != nil || a.Outcome != tc.want || calls != 1 {
				t.Fatalf("%+v %v calls%d", a, e, calls)
			}
			assertSanitizedResult(t, a, e)
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	client := newControlClient(t, transportFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, nil }))
	a, e := client.SubmitRestart(ctx, restartPlan(t, true), operations.DefaultSubmitOptions())
	if e != nil || a.Outcome != operations.OutcomeNotSubmitted || calls != 0 {
		t.Fatal("canceled restart submitted")
	}
}
