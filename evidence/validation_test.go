package evidence_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/evidence"
)

func TestValidationPathsNeverRenderCausesOrInvalidPathText(t *testing.T) {
	t.Parallel()
	cause := errors.New("PRIVATE_CANARY")
	err := evidence.At("observations[3]", evidence.At("metadata.reference", cause))
	var field *evidence.ValidationError
	if !errors.Is(err, cause) || !errors.As(err, &field) || field.Path != "observations[3].metadata.reference" ||
		strings.Contains(err.Error(), "CANARY") {
		t.Fatal("validation path lost its classification or rendered a cause")
	}
	if got := evidence.At("https://PRIVATE_CANARY", cause).Error(); strings.Contains(got, "CANARY") {
		t.Fatal("invalid path text escaped validation")
	}
	if evidence.At("field", nil) != nil {
		t.Fatal("a valid field acquired an error")
	}
}

func TestCompoundValidationReportsFieldPaths(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	unknown := evidence.Text{State: evidence.ValueUnknown}
	validContainer := evidence.ContainerStatus{
		Kind:             evidence.ContainerApp,
		Name:             "openbao",
		Ready:            evidence.Boolean{State: evidence.ValueReported, Value: new(true)},
		RestartCount:     evidence.Integer{State: evidence.ValueReported, Value: new(int64(0))},
		State:            evidence.ObjectReported,
		Waiting:          evidence.ObjectAbsent,
		WaitingReason:    unknown,
		Terminated:       evidence.ObjectAbsent,
		TerminatedReason: unknown,
		ExitCode:         evidence.Integer{State: evidence.ValueUnknown},
	}
	if err := validContainer.Validate(); err != nil {
		t.Fatalf("valid container rejected: %v", err)
	}
	scope := evidence.Scope{
		Origin:   "cluster-a",
		Version:  "v1",
		Resource: "pods",
		Action:   evidence.ReadGet,
		Name:     "bao-0",
	}
	for _, tc := range []struct {
		name  string
		value interface{ Validate() error }
		path  string
	}{
		{"container-name", func() evidence.ContainerStatus { c := validContainer; c.Name = ""; return c }(), "name"},
		{"container-kind", func() evidence.ContainerStatus { c := validContainer; c.Kind = "sidecar"; return c }(), "kind"},
		{"container-restarts", func() evidence.ContainerStatus {
			c := validContainer
			c.RestartCount = evidence.Integer{State: evidence.ValueReported, Value: new(int64(-1))}
			return c
		}(), "restartCount"},
		{"container-waiting-child", func() evidence.ContainerStatus {
			c := validContainer
			c.State, c.Waiting = evidence.ObjectAbsent, evidence.ObjectReported
			return c
		}(), "waiting"},
		{"container-exit-code", func() evidence.ContainerStatus {
			c := validContainer
			c.Terminated = evidence.ObjectReported
			c.ExitCode = evidence.Integer{State: evidence.ValueReported, Value: new(int64(1) << 40)}
			return c
		}(), "exitCode"},
		{"scope-selector-on-get", func() evidence.Scope { s := scope; s.Selector = "app=bao"; return s }(), "selector"},
		{"scope-action", func() evidence.Scope { s := scope; s.Action = "watch"; return s }(), "action"},
		{"scope-origin", func() evidence.Scope { s := scope; s.Origin = ""; return s }(), "origin"},
		{"collection-items", evidence.Collection{Scope: scope, Outcome: evidence.CollectionNotCollected, Items: 1}, "items"},
		{"collection-interval", evidence.Collection{Scope: scope, Outcome: evidence.CollectionComplete}, "interval.startedAt"},
		{"interval-order", evidence.Interval{StartedAt: at, CompletedAt: at.Add(-time.Second)}, "completedAt"},
		{"backup-implied-failures", evidence.Backup{
			Configuration:       evidence.ObjectReported,
			State:               evidence.ObjectReported,
			LastSuccessAt:       evidence.Timestamp{State: evidence.ValueReported, Value: &at},
			LastAttemptAt:       evidence.Timestamp{State: evidence.ValueReported, Value: new(at.Add(time.Second))},
			NextScheduledAt:     evidence.Timestamp{State: evidence.ValueUnknown},
			ConsecutiveFailures: evidence.Integer{State: evidence.ValueImplied, Value: new(int64(0))},
			LastFailureReason:   unknown,
			LastFailureAt:       evidence.Timestamp{State: evidence.ValueUnknown},
			SizeBytes:           evidence.Integer{State: evidence.ValueUnknown},
			SnapshotKey:         unknown,
			SnapshotDigest:      unknown,
			RestoreTest: evidence.RestoreTest{
				State:          evidence.ObjectAbsent,
				Active:         evidence.ObjectUnknown,
				Last:           evidence.ObjectUnknown,
				LastOutcome:    unknown,
				LastReason:     unknown,
				LastSuccessAt:  evidence.Timestamp{State: evidence.ValueUnknown},
				FinishedAt:     evidence.Timestamp{State: evidence.ValueUnknown},
				SnapshotKey:    unknown,
				SnapshotDigest: unknown,
				Conditions:     evidence.Conditions{State: evidence.ValueUnknown},
			},
		}, "consecutiveFailures"},
		{"ordinals-order", evidence.Ordinals{State: evidence.ValueReported, Values: []int64{2, 1}}, "values[1]"},
		{"conditions-missing-values", evidence.Conditions{State: evidence.ValueReported}, "values"},
		{"verbs-state", evidence.ReadVerbs{State: "future"}, "state"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.value.Validate()
			var field *evidence.ValidationError
			if !errors.Is(err, evidence.ErrInvalidEvidence) || !errors.As(err, &field) || field.Path != tc.path {
				t.Fatalf("Validate() = %v, want path %q", err, tc.path)
			}
		})
	}
}
