package operations_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
	"github.com/kubebao/openbao-operator-client/operations"
)

func controlCluster(t testing.TB, maintenance string) evidence.Cluster {
	t.Helper()
	raw := `{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",` +
		`"metadata":{"namespace":"team","name":"bao","uid":"cluster-1","resourceVersion":"opaque-1","generation":7},` +
		`"spec":{"version":"2.7.2","replicas":3,"paused":false` + maintenance + `},"status":{"observedGeneration":7}}`
	cluster, err := adapter.ProjectCluster([]byte(raw), adapter.ProjectionOptions{
		Origin: "origin-1", Contract: adapter.OpenBaoV1Alpha1,
		CollectedAt: time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC), Limits: adapter.DefaultProjectionLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return cluster
}

func pausePlan(t testing.TB) operations.Plan {
	t.Helper()
	plan, err := operations.PreparePause(controlCluster(t, ""), adapter.OpenBaoV1Alpha1, true)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestControlPatches(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, maintenance, path string
		control                 operations.Control
		enabled                 bool
		want                    string
	}{
		{"pause", "", "/spec/paused", operations.ControlPause, true, `true`},
		{"resume", "", "/spec/paused", operations.ControlPause, false, `false`},
		{"absent-maintenance", "", "/spec/maintenance", operations.ControlMaintenance, true, `{"enabled":true}`},
		{"null-maintenance", `,"maintenance":null`, "/spec/maintenance", operations.ControlMaintenance, false, `{"enabled":false}`},
		{
			"existing-maintenance", `,"maintenance":{"enabled":false}`, "/spec/maintenance/enabled",
			operations.ControlMaintenance, true, `true`,
		},
		{
			"preserve-other-fields", `,"maintenance":{"futureField":"CANARY"}`, "/spec/maintenance/enabled",
			operations.ControlMaintenance, false, `false`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cluster := controlCluster(t, tc.maintenance)
			var plan operations.Plan
			var err error
			if tc.control == operations.ControlPause {
				plan, err = operations.PreparePause(cluster, adapter.OpenBaoV1Alpha1, tc.enabled)
			} else {
				plan, err = operations.PrepareMaintenance(cluster, adapter.OpenBaoV1Alpha1, tc.enabled)
			}
			if err != nil {
				t.Fatal(err)
			}
			patch, err := plan.Patch()
			if err != nil {
				t.Fatal(err)
			}
			var steps []struct {
				Op, Path string
				Value    json.RawMessage
			}
			if err := json.Unmarshal(patch, &steps); err != nil {
				t.Fatal(err)
			}
			if len(steps) != 4 || steps[0].Op != "test" || steps[0].Path != "/metadata/uid" ||
				string(steps[0].Value) != `"cluster-1"` || steps[1].Op != "test" ||
				steps[1].Path != "/metadata/resourceVersion" || string(steps[1].Value) != `"opaque-1"` ||
				steps[2].Op != "test" || steps[2].Path != "/metadata/generation" || string(steps[2].Value) != "7" ||
				steps[3].Op != "add" || steps[3].Path != tc.path || string(steps[3].Value) != tc.want {
				t.Fatalf("unexpected guarded patch: %s", patch)
			}
			if strings.Contains(string(patch), "CANARY") {
				t.Fatal("patch copied unapproved fields")
			}
			patch[0] = '!'
			fresh, err := plan.Patch()
			if err != nil || len(fresh) == 0 || fresh[0] != '[' {
				t.Fatal("patch caller can mutate a later submission")
			}
		})
	}
}

func TestPrepareRejectsUnsafeEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*evidence.Cluster)
	}{
		{"unsupported-version", func(c *evidence.Cluster) { c.Reference.APIVersion = "openbao.org/v2" }},
		{"missing-version", func(c *evidence.Cluster) { c.Reference.ResourceVersion = evidence.Text{State: evidence.ValueUnknown} }},
		{"missing-generation", func(c *evidence.Cluster) { c.Generation = evidence.Integer{State: evidence.ValueUnknown} }},
		{"deleting", func(c *evidence.Cluster) {
			at := c.Reference.CollectedAt.Value.Add(time.Hour)
			c.DeletionTimestamp = evidence.Timestamp{State: evidence.ValueReported, Value: &at}
		}},
		{"invalid-deletion", func(c *evidence.Cluster) { c.DeletionTimestamp = evidence.Timestamp{State: evidence.ValueInvalid} }},
		{"missing-deletion-state", func(c *evidence.Cluster) { c.DeletionTimestamp = evidence.Timestamp{} }},
		{"path-injection", func(c *evidence.Cluster) { c.Reference.Identity.Name = "bao/../../secrets" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cluster := controlCluster(t, "")
			tc.mutate(&cluster)
			if _, err := operations.PreparePause(cluster, adapter.OpenBaoV1Alpha1, true); !errors.Is(err, operations.ErrInvalidPlan) {
				t.Fatalf("pause error = %v", err)
			}
			if _, err := operations.PrepareMaintenance(cluster, adapter.OpenBaoV1Alpha1, true); !errors.Is(err, operations.ErrInvalidPlan) {
				t.Fatalf("maintenance error = %v", err)
			}
		})
	}
}

func TestPlanReplayAndValidation(t *testing.T) {
	t.Parallel()
	original := pausePlan(t)
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var replay operations.Plan
	if err := json.Unmarshal(raw, &replay); err != nil || replay.Validate() != nil {
		t.Fatalf("valid plan replay failed: %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*operations.Plan)
	}{
		{"control", func(p *operations.Plan) { p.Control = "delete" }},
		{"group", func(p *operations.Plan) { p.Target.Identity.Group = "other.example" }},
		{"version", func(p *operations.Plan) { p.Target.APIVersion = "openbao.org/v2" }},
		{"field", func(p *operations.Plan) { p.Target.Field = "spec.paused" }},
		{"kind", func(p *operations.Plan) { p.Target.Identity.Kind = "Secret" }},
		{"namespace", func(p *operations.Plan) { p.Target.Identity.Namespace = "team?dryRun=All" }},
		{"name", func(p *operations.Plan) { p.Target.Identity.Name = "bao%2fsecrets" }},
		{"empty-resource-version", func(p *operations.Plan) { p.Target.ResourceVersion.Value = new(string) }},
		{"implied-resource-version", func(p *operations.Plan) { p.Target.ResourceVersion.State = evidence.ValueImplied }},
		{"zero-generation", func(p *operations.Plan) { p.Generation.Value = new(int64) }},
		{"implied-generation", func(p *operations.Plan) { p.Generation.State = evidence.ValueImplied }},
		{"pause-ancestor", func(p *operations.Plan) { p.MaintenanceState = evidence.ObjectReported }},
		{"maintenance-ancestor", func(p *operations.Plan) { p.Control = operations.ControlMaintenance }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var plan operations.Plan
			if err := json.Unmarshal(raw, &plan); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&plan)
			if !errors.Is(plan.Validate(), operations.ErrInvalidPlan) {
				t.Fatal("tampered plan is valid")
			}
			if _, err := plan.Patch(); !errors.Is(err, operations.ErrInvalidPlan) {
				t.Fatalf("tampered patch error = %v", err)
			}
		})
	}
}

func TestPrepareRejectsInvalidControlFields(t *testing.T) {
	t.Parallel()
	cluster := controlCluster(t, `,"maintenance":{"enabled":false}`)
	cluster.Paused = evidence.Boolean{State: evidence.ValueInvalid}
	if _, err := operations.PreparePause(cluster, adapter.OpenBaoV1Alpha1, true); !errors.Is(err, operations.ErrInvalidPlan) {
		t.Fatalf("invalid pause field error = %v", err)
	}
	cluster = controlCluster(t, `,"maintenance":{"enabled":false}`)
	cluster.Maintenance.Enabled = evidence.Boolean{State: evidence.ValueInvalid}
	if _, err := operations.PrepareMaintenance(cluster, adapter.OpenBaoV1Alpha1, true); !errors.Is(err, operations.ErrInvalidPlan) {
		t.Fatalf("invalid maintenance field error = %v", err)
	}
}

func TestPlanCopiesPreconditionsAndPreservesPausedIntent(t *testing.T) {
	t.Parallel()
	cluster := controlCluster(t, "")
	*cluster.Paused.Value = true
	plan, err := operations.PrepareMaintenance(cluster, adapter.OpenBaoV1Alpha1, true)
	if err != nil {
		t.Fatal(err)
	}
	*cluster.Reference.ResourceVersion.Value = "changed"
	*cluster.Generation.Value = 99
	*cluster.Reference.CollectedAt.Value = time.Time{}
	patch, err := plan.Patch()
	if err != nil || !strings.Contains(string(patch), `"opaque-1"`) || !strings.Contains(string(patch), `"value":7`) ||
		strings.Contains(string(patch), "/spec/paused") {
		t.Fatalf("plan mutated preconditions or resumed reconciliation: %s, %v", patch, err)
	}
}
