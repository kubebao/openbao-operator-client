package operations

import (
	"errors"
	"strings"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

// ErrRestartBlocked reports a cluster whose state or strategy does not permit a guarded restart.
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
	if err := cluster.Validate(); err != nil {
		return RestartPlan{}, invalidPlan("cluster", err)
	}
	if cluster.DeletionTimestamp.State != evidence.ValueUnknown {
		return RestartPlan{}, invalidPlan("cluster.deletionTimestamp", nil)
	}
	if cluster.Runtime.RestartAt.State == evidence.ValueInvalid {
		return RestartPlan{}, invalidPlan("cluster.runtime.restartAt", nil)
	}
	if restartRestricted(cluster) {
		return RestartPlan{}, ErrRestartBlocked
	}
	if current := cluster.Runtime.RestartAt.Value; current != nil && strings.TrimSpace(*current) == marker {
		return RestartPlan{}, invalidPlan("restartAt", nil)
	}
	plan := RestartPlan{
		Contract:     contract,
		Target:       copyReference(cluster.Reference),
		Generation:   copyInteger(cluster.Generation),
		RuntimeState: cluster.Runtime.State,
		RestartAt:    marker,
	}
	if err := plan.Validate(); err != nil {
		return RestartPlan{}, err
	}
	return plan, nil
}

// restartRestricted reports whether the cluster state or strategy blocks a restart.
func restartRestricted(cluster evidence.Cluster) bool {
	if cluster.Acknowledgement() != evidence.GenerationCurrent {
		return true
	}
	if cluster.Paused.Value == nil || *cluster.Paused.Value {
		return true
	}
	if !textEquals(cluster.UpgradeStrategy, "RollingUpdate") || !textEquals(cluster.AcceptedUpgradeStrategy, "RollingUpdate") {
		return true
	}
	if objectPresent(cluster.BlueGreen.State) || objectPresent(cluster.OperationLock.State) {
		return true
	}
	if cluster.RestoreHold.State != evidence.ValueUnknown {
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

// objectPresent reports whether an object state is anything other than absent or null.
func objectPresent(state evidence.ObjectState) bool {
	return state != evidence.ObjectAbsent && state != evidence.ObjectNull
}

func textEquals(value evidence.Text, expected string) bool {
	return value.State == evidence.ValueReported && value.Value != nil && *value.Value == expected
}

// Validate checks the control target, a bounded RFC 3339 marker, and a known runtime object state.
func (p RestartPlan) Validate() error {
	if err := validateControlTarget(p.Contract, p.Target, p.Generation); err != nil {
		return err
	}
	if len(p.RestartAt) > evidence.MaxNameLength {
		return invalidPlan("restartAt", nil)
	}
	marker, err := time.Parse(time.RFC3339Nano, p.RestartAt)
	if err != nil || marker.Year() < 1 || marker.Year() > 9999 {
		return invalidPlan("restartAt", nil)
	}
	switch p.RuntimeState {
	case evidence.ObjectAbsent, evidence.ObjectNull, evidence.ObjectReported:
		return nil
	default:
		return invalidPlan("runtimeState", nil)
	}
}

// Patch changes only runtime.restartAt after testing UID, resource version and generation.
func (p RestartPlan) Patch() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	change := patchOperation{Operation: "add", Path: "/spec/runtime/restartAt", Value: p.RestartAt}
	if p.RuntimeState != evidence.ObjectReported {
		change.Path = "/spec/runtime"
		change.Value = map[string]string{"restartAt": p.RestartAt}
	}
	return marshalControlPatch(p.Target, p.Generation, change)
}
