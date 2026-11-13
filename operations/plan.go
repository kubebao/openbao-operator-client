package operations

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

var ErrInvalidPlan = errors.New("invalid cluster control plan")

type Control string

const (
	ControlPause       Control = "pause"
	ControlMaintenance Control = "maintenance"
)

// Plan binds one requested control value to an observed cluster and revision.
// It grants no permission and does not establish that the operator can apply the change.
// Validate a deserialized plan before displaying or submitting it.
type Plan struct {
	Contract         adapter.Contract     `json:"contract"`
	Target           evidence.Reference   `json:"target"`
	Generation       evidence.Integer     `json:"generation"`
	Control          Control              `json:"control"`
	Enabled          bool                 `json:"enabled"`
	MaintenanceState evidence.ObjectState `json:"maintenanceState"`
}

// PreparePause plans a pause or resume without making requests.
func PreparePause(cluster evidence.Cluster, contract adapter.Contract, paused bool) (Plan, error) {
	return prepare(cluster, contract, ControlPause, paused)
}

// PrepareMaintenance plans a maintenance flag change. It never resumes reconciliation.
// A paused operator cannot propagate the flag to managed resources.
func PrepareMaintenance(cluster evidence.Cluster, contract adapter.Contract, enabled bool) (Plan, error) {
	return prepare(cluster, contract, ControlMaintenance, enabled)
}

func prepare(cluster evidence.Cluster, contract adapter.Contract, control Control, enabled bool) (Plan, error) {
	if cluster.Validate() != nil || cluster.DeletionTimestamp.State != evidence.ValueUnknown {
		return Plan{}, ErrInvalidPlan
	}
	if control == ControlPause && cluster.Paused.State != evidence.ValueReported && cluster.Paused.State != evidence.ValueImplied {
		return Plan{}, ErrInvalidPlan
	}
	if control == ControlMaintenance && cluster.Maintenance.Enabled.State == evidence.ValueInvalid {
		return Plan{}, ErrInvalidPlan
	}
	plan := Plan{
		Contract: contract, Target: copyReference(cluster.Reference),
		Generation: copyInteger(cluster.Generation), Control: control, Enabled: enabled,
		MaintenanceState: evidence.ObjectUnknown,
	}
	if control == ControlMaintenance {
		plan.MaintenanceState = cluster.Maintenance.State
	}
	if err := plan.Validate(); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func validResourceName(value string) bool {
	if value == "" || len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if !dnsLabel.MatchString(label) {
			return false
		}
	}
	return true
}

// Validate checks the fixed control surface and every optimistic-concurrency precondition.
// A valid plan can still be rejected by RBAC, admission, or a concurrent resource change.
func (p Plan) Validate() error {
	contract := p.Contract
	info, err := adapter.Describe(contract)
	if err != nil || (contract != adapter.OpenBaoV1Alpha1 && contract != adapter.KubeBaoV1Alpha1) ||
		p.Target.Validate() != nil ||
		p.Target.Field != "" || p.Target.Identity.Kind != "OpenBaoCluster" ||
		p.Target.Identity.Group != info.APIGroup || p.Target.APIVersion != info.APIVersion {
		return ErrInvalidPlan
	}
	id := p.Target.Identity
	if len(id.Namespace) > 63 || !dnsLabel.MatchString(id.Namespace) || !validResourceName(id.Name) {
		return ErrInvalidPlan
	}
	if p.Target.ResourceVersion.State != evidence.ValueReported || len(*p.Target.ResourceVersion.Value) > 253 ||
		p.Generation.Validate() != nil || p.Generation.State != evidence.ValueReported || *p.Generation.Value <= 0 {
		return ErrInvalidPlan
	}
	switch p.Control {
	case ControlPause:
		if p.MaintenanceState != evidence.ObjectUnknown {
			return ErrInvalidPlan
		}
	case ControlMaintenance:
		switch p.MaintenanceState {
		case evidence.ObjectAbsent, evidence.ObjectNull, evidence.ObjectReported:
		default:
			return ErrInvalidPlan
		}
	default:
		return ErrInvalidPlan
	}
	return nil
}

type patchOperation struct {
	Operation string `json:"op"`
	Path      string `json:"path"`
	Value     any    `json:"value"`
}

// Patch returns a fresh JSON Patch preview with UID, resource-version, and generation tests.
// Submit builds this patch again; it never accepts caller-supplied patch bytes.
func (p Plan) Patch() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	change := patchOperation{Operation: "add", Path: "/spec/paused", Value: p.Enabled}
	if p.Control == ControlMaintenance {
		change.Path = "/spec/maintenance/enabled"
		if p.MaintenanceState != evidence.ObjectReported {
			change.Path, change.Value = "/spec/maintenance", map[string]bool{"enabled": p.Enabled}
		}
	}
	operations := []patchOperation{
		{Operation: "test", Path: "/metadata/uid", Value: p.Target.Identity.UID},
		{Operation: "test", Path: "/metadata/resourceVersion", Value: *p.Target.ResourceVersion.Value},
		{Operation: "test", Path: "/metadata/generation", Value: *p.Generation.Value},
		change,
	}
	raw, err := json.Marshal(operations)
	if err != nil {
		return nil, ErrInvalidPlan
	}
	return raw, nil
}

func copyReference(value evidence.Reference) evidence.Reference {
	if value.ResourceVersion.Value != nil {
		copyValue := *value.ResourceVersion.Value
		value.ResourceVersion.Value = &copyValue
	}
	if value.CollectedAt.Value != nil {
		copyValue := *value.CollectedAt.Value
		value.CollectedAt.Value = &copyValue
	}
	return value
}

func copyInteger(value evidence.Integer) evidence.Integer {
	if value.Value != nil {
		copyValue := *value.Value
		value.Value = &copyValue
	}
	return value
}
