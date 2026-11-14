package operations

import (
	"errors"
	"strings"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

var ErrRestartBlocked = errors.New("restart is blocked or unsupported by the supplied evidence")

// RestartPlan binds a new runtime marker to one observed target and revision.
// It grants no permission and does not establish rollout or application health.
type RestartPlan struct {
	Contract     adapter.Contract     `json:"contract"`
	Target       evidence.Reference   `json:"target"`
	Generation   evidence.Integer     `json:"generation"`
	RuntimeState evidence.ObjectState `json:"runtimeState"`
	RestartAt    string               `json:"restartAt"`
}

// PrepareRestart requires a new RFC3339 marker and known, unrestricted rolling
// strategy. Callers generate the marker once and retain it after unknown outcomes.
func PrepareRestart(cluster evidence.Cluster, contract adapter.Contract, marker string) (RestartPlan, error) {
	if cluster.Validate() != nil || cluster.DeletionTimestamp.State != evidence.ValueUnknown ||
		cluster.Runtime.RestartAt.State == evidence.ValueInvalid {
		return RestartPlan{}, ErrInvalidPlan
	}
	if restartRestricted(cluster) {
		return RestartPlan{}, ErrRestartBlocked
	}
	if cluster.Runtime.RestartAt.Value != nil && strings.TrimSpace(*cluster.Runtime.RestartAt.Value) == marker {
		return RestartPlan{}, ErrInvalidPlan
	}
	plan := RestartPlan{
		Contract: contract, Target: copyReference(cluster.Reference), Generation: copyInteger(cluster.Generation),
		RuntimeState: cluster.Runtime.State, RestartAt: marker,
	}
	if err := plan.Validate(); err != nil {
		return RestartPlan{}, err
	}
	return plan, nil
}

func restartRestricted(cluster evidence.Cluster) bool {
	if cluster.Acknowledgement() != evidence.GenerationCurrent || cluster.Paused.Value == nil || *cluster.Paused.Value ||
		!textEquals(cluster.UpgradeStrategy, "RollingUpdate") || !textEquals(cluster.AcceptedUpgradeStrategy, "RollingUpdate") ||
		(cluster.BlueGreen.State != evidence.ObjectAbsent && cluster.BlueGreen.State != evidence.ObjectNull) ||
		cluster.RestoreHold.State != evidence.ValueUnknown ||
		(cluster.OperationLock.State != evidence.ObjectAbsent && cluster.OperationLock.State != evidence.ObjectNull) {
		return true
	}
	switch cluster.BreakGlass.State {
	case evidence.ObjectAbsent, evidence.ObjectNull:
	case evidence.ObjectReported:
		if cluster.BreakGlass.Active.Value == nil || *cluster.BreakGlass.Active.Value {
			return true
		}
	default:
		return true
	}
	return false
}

func textEquals(value evidence.Text, expected string) bool {
	return value.State == evidence.ValueReported && value.Value != nil && *value.Value == expected
}

func (p RestartPlan) Validate() error {
	if validateControlTarget(p.Contract, p.Target, p.Generation) != nil || len(p.RestartAt) > 253 {
		return ErrInvalidPlan
	}
	marker, err := time.Parse(time.RFC3339Nano, p.RestartAt)
	if err != nil || marker.Year() < 1 || marker.Year() > 9999 {
		return ErrInvalidPlan
	}
	switch p.RuntimeState {
	case evidence.ObjectAbsent, evidence.ObjectNull, evidence.ObjectReported:
		return nil
	default:
		return ErrInvalidPlan
	}
}

// Patch changes only runtime.restartAt after testing UID, resource version and generation.
func (p RestartPlan) Patch() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	change := patchOperation{Operation: "add", Path: "/spec/runtime/restartAt", Value: p.RestartAt}
	if p.RuntimeState != evidence.ObjectReported {
		change.Path, change.Value = "/spec/runtime", map[string]string{"restartAt": p.RestartAt}
	}
	return marshalControlPatch(p.Target, p.Generation, change)
}
