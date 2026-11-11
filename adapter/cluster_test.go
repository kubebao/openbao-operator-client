package adapter_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func projectionOptions() adapter.ProjectionOptions {
	return adapter.ProjectionOptions{
		Origin: "origin-1", CollectedAt: time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC),
		Contract: adapter.V1Alpha1Snapshot, Limits: adapter.DefaultProjectionLimits(),
	}
}

func clusterFixture(t testing.TB) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/cluster.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestClusterProjection(t *testing.T) {
	t.Parallel()
	options := projectionOptions()
	got, err := adapter.ProjectCluster(clusterFixture(t), options)
	if err != nil {
		t.Fatal(err)
	}
	if got.Validate() != nil {
		t.Fatal("projection did not produce valid normalized evidence")
	}
	if got.Reference.Identity != (evidence.Identity{
		Origin: "origin-1", Group: "openbao.org", Kind: "OpenBaoCluster", Namespace: "team", Name: "bao", UID: "cluster-1",
	}) {
		t.Fatalf("identity = %+v", got.Reference.Identity)
	}
	if *got.Generation.Value != 9007199254741007 || *got.Reference.ResourceVersion.Value != "9007199254741007" {
		t.Fatal("exact generation or opaque resource version was lost")
	}
	if got.Acknowledgement() != evidence.GenerationCurrent ||
		got.Conditions.Values[0].Acknowledgement(got.Generation) != evidence.GenerationStale {
		t.Fatal("resource acknowledgement changed condition acknowledgement")
	}
	if got.ReadyReplicas.State != evidence.ValueImplied || *got.ReadyReplicas.Value != 0 ||
		got.Paused.State != evidence.ValueImplied || *got.Paused.Value {
		t.Fatal("documented omissions were not retained")
	}
	if *got.DesiredVersion.Value != "2.7.0+ent" || *got.ObservedVersion.Value != "2.7.0+ent" {
		t.Fatal("version build metadata was lost")
	}
	if !got.Reference.CollectedAt.Value.Equal(options.CollectedAt) || got.CreatedAt.Value.Format(time.RFC3339) != "2026-11-10T07:00:00Z" ||
		got.Conditions.Values[0].LastTransitionAt.Value.Format(time.RFC3339) != "2026-01-01T11:00:00Z" {
		t.Fatal("collection, creation, and transition time were conflated")
	}
	serialized, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"CANARY", "message", "credentials", "annotations", "exitCode", "resultCode", "health"} {
		if bytes.Contains(serialized, []byte(forbidden)) {
			t.Fatalf("unapproved field retained: %s", forbidden)
		}
	}
	var replay evidence.Cluster
	decoder := json.NewDecoder(bytes.NewReader(serialized))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&replay); err != nil {
		t.Fatal(err)
	}
	if replay.Validate() != nil || !reflect.DeepEqual(got, replay) {
		t.Fatal("serialized evidence changed normalized facts")
	}
}

func TestClusterOmissionPolicyIsExplicit(t *testing.T) {
	t.Parallel()
	options := projectionOptions()
	options.Contract = adapter.V1Alpha1Unqualified
	got, err := adapter.ProjectCluster(clusterFixture(t), options)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReadyReplicas.State != evidence.ValueUnknown || got.Paused.State != evidence.ValueUnknown {
		t.Fatal("unqualified projection inferred omitted values")
	}
	options.Contract = "openbao.org/v1alpha1@unknown"
	_, err = adapter.ProjectCluster(clusterFixture(t), options)
	assertProjectionError(t, err, adapter.UnsupportedContract)
}

func TestMalformedOptionalFieldsPreserveIndependentFacts(t *testing.T) {
	t.Parallel()
	data := clusterFixture(t)
	data = bytes.Replace(data, []byte(`"reason": "Waiting"`), []byte(`"reason": "token=CANARY"`), 1)
	data = bytes.Replace(data, []byte(`"lastTransitionTime": "2026-01-01T12:00:00+01:00"`),
		[]byte(`"lastTransitionTime": "CANARY"`), 1)
	data = bytes.Replace(data, []byte(`"version": "2.7.0+ent"`), []byte(`"version": "CANARY"`), 1)
	got, err := adapter.ProjectCluster(data, projectionOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got.Conditions.State != evidence.ValueReported || got.Conditions.Values[0].Reason.State != evidence.ValueInvalid ||
		got.Conditions.Values[0].LastTransitionAt.State != evidence.ValueInvalid || got.DesiredVersion.State != evidence.ValueInvalid ||
		got.ReadyReplicas.State != evidence.ValueImplied {
		t.Fatal("malformed optional fields discarded independent facts or acquired a default")
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("CANARY")) {
		t.Fatal("invalid optional field retained raw data")
	}
}

func assertProjectionError(t testing.TB, err error, code adapter.ErrorCode) {
	t.Helper()
	var classified *adapter.ProjectionError
	if !errors.As(err, &classified) || classified.Code != code {
		t.Fatalf("error = %v, want classification %s", err, code)
	}
	if strings.Contains(err.Error(), "CANARY") {
		t.Fatal("source data retained in error")
	}
}

func TestProjectionBoundaryErrors(t *testing.T) {
	t.Parallel()
	base := clusterFixture(t)
	duplicateUID := bytes.Replace(base, []byte(`"uid": "cluster-1"`), []byte(`"uid":"CANARY","uid":"cluster-1"`), 1)
	for _, tc := range []struct {
		name string
		data []byte
		code adapter.ErrorCode
	}{
		{"null", []byte("null"), adapter.InvalidInput},
		{"array", []byte("[]"), adapter.InvalidInput},
		{"malformed", []byte(`{"CANARY":`), adapter.InvalidInput},
		{"trailing", append(bytes.Clone(base), []byte(`{}`)...), adapter.InvalidInput},
		{"duplicate-key", duplicateUID, adapter.InvalidInput},
		{"missing-uid", bytes.Replace(base, []byte(`"uid": "cluster-1",`), nil, 1), adapter.InvalidInput},
		{"control-uid", bytes.Replace(base, []byte(`"cluster-1"`), []byte(`"cluster\n1"`), 1), adapter.InvalidInput},
		{"surrogate-uid", bytes.Replace(base, []byte(`"cluster-1"`), []byte(`"\ud800"`), 1), adapter.InvalidInput},
		{"invalid-utf8", bytes.Replace(base, []byte("cluster-1"), []byte{0xff}, 1), adapter.InvalidInput},
		{"other-group", bytes.Replace(base, []byte("openbao.org/v1alpha1"), []byte("other.org/v1alpha1"), 1), adapter.ResourceMismatch},
		{"other-kind", bytes.Replace(base, []byte("OpenBaoCluster"), []byte("Secret"), 1), adapter.ResourceMismatch},
		{"nesting-limit", []byte(`{"unknown":` + strings.Repeat("[", 70) + `0` + strings.Repeat("]", 70) + `}`), adapter.ProjectionLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := adapter.ProjectCluster(tc.data, projectionOptions())
			assertProjectionError(t, err, tc.code)
			if !reflect.DeepEqual(got, evidence.Cluster{}) {
				t.Fatal("failed projection returned partial identity")
			}
		})
	}
}

func TestProjectionLimitsAndClock(t *testing.T) {
	t.Parallel()
	data := clusterFixture(t)
	options := projectionOptions()
	options.Limits.MaxBytes = len(data)
	if _, err := adapter.ProjectCluster(data, options); err != nil {
		t.Fatal(err)
	}
	options.Limits.MaxBytes--
	_, err := adapter.ProjectCluster(data, options)
	assertProjectionError(t, err, adapter.ProjectionLimit)
	options = projectionOptions()
	options.Limits.MaxConditions = 1
	got, err := adapter.ProjectCluster(data, options)
	if err != nil {
		t.Fatal(err)
	}
	if got.Conditions.State != evidence.ValueInvalid || got.DesiredReplicas.Value == nil {
		t.Fatal("condition limit discarded independent facts or retained oversized conditions")
	}
	for _, limits := range []adapter.ProjectionLimits{{}, {MaxBytes: 100}, {MaxConditions: 1}, {MaxBytes: -1, MaxConditions: 1}} {
		options = projectionOptions()
		options.Limits = limits
		_, err := adapter.ProjectCluster(data, options)
		assertProjectionError(t, err, adapter.InvalidInput)
	}
	for _, clock := range []time.Time{{}, time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)} {
		options = projectionOptions()
		options.CollectedAt = clock
		_, err := adapter.ProjectCluster(data, options)
		assertProjectionError(t, err, adapter.InvalidInput)
	}
}

func TestConditionOrderAndEmptySerialization(t *testing.T) {
	t.Parallel()
	data := clusterFixture(t)
	var source map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&source); err != nil {
		t.Fatal(err)
	}
	status := source["status"].(map[string]any)
	conditions := status["conditions"].([]any)
	conditions[0], conditions[1] = conditions[1], conditions[0]
	reordered, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	first, err := adapter.ProjectCluster(data, projectionOptions())
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.ProjectCluster(reordered, projectionOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("condition order changed evidence")
	}
	status["conditions"] = []any{}
	empty, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	got, err := adapter.ProjectCluster(empty, projectionOptions())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"conditions":{"state":"reported","values":[]}`)) {
		t.Fatal("reported empty condition set lost its presence")
	}
}

func TestContractDescriptionDoesNotShareMutableState(t *testing.T) {
	t.Parallel()
	first, err := adapter.Describe(adapter.V1Alpha1Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	first.Capabilities[0] = "changed"
	second, err := adapter.Describe(adapter.V1Alpha1Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if second.Capabilities[0] != adapter.CapabilityCluster || second.SourceRevision != "9671c30623d609f948cf073e167f376de4ea9a85" {
		t.Fatal("contract description was changed by a caller")
	}
}

func TestRestoreBindingCapabilityRequiresInspectedSemantics(t *testing.T) {
	t.Parallel()
	for _, contract := range []adapter.Contract{adapter.V1Alpha1Snapshot, adapter.V1Alpha1Unqualified} {
		info, err := adapter.Describe(contract)
		if err != nil {
			t.Fatal(err)
		}
		binding := false
		for _, capability := range info.Capabilities {
			binding = binding || capability == adapter.CapabilityRestoreUIDBinding
		}
		if binding != (contract == adapter.V1Alpha1Snapshot) {
			t.Fatal("unqualified projection acquired inspected UID-binding semantics")
		}
	}
}

func FuzzProjectCluster(f *testing.F) {
	f.Add(clusterFixture(f))
	f.Add(operationalFixture(f))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"uid":"\ud800"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := adapter.ProjectCluster(data, projectionOptions())
		if err != nil {
			var classified *adapter.ProjectionError
			if !errors.As(err, &classified) {
				t.Fatal("projection exposed an unclassified error")
			}
			return
		}
		if got.Validate() != nil {
			t.Fatal("projection produced invalid evidence")
		}
		if _, err := json.Marshal(got); err != nil {
			t.Fatal("projection cannot be serialized")
		}
	})
}
