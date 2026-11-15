package operations

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

// ErrInvalidPlan reports a plan that fails validation or cannot be prepared from the supplied evidence.
var ErrInvalidPlan = errors.New("invalid cluster control plan")

// maxNamespaceLength is the Kubernetes DNS label limit for namespaces.
const maxNamespaceLength = 63

// Control identifies the cluster field that a plan changes.
type Control string

const (
	// ControlPause changes spec.paused.
	ControlPause Control = "pause"
	// ControlMaintenance changes spec.maintenance.enabled.
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
	if err := cluster.Validate(); err != nil {
		return Plan{}, invalidPlan("cluster", err)
	}
	if cluster.DeletionTimestamp.State != evidence.ValueUnknown {
		return Plan{}, invalidPlan("cluster.deletionTimestamp", nil)
	}
	if control == ControlPause && !knownBoolean(cluster.Paused) {
		return Plan{}, invalidPlan("cluster.paused", nil)
	}
	if control == ControlMaintenance && cluster.Maintenance.Enabled.State == evidence.ValueInvalid {
		return Plan{}, invalidPlan("cluster.maintenance.enabled", nil)
	}
	plan := Plan{
		Contract:         contract,
		Target:           copyReference(cluster.Reference),
		Generation:       copyInteger(cluster.Generation),
		Control:          control,
		Enabled:          enabled,
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

// invalidPlan attributes ErrInvalidPlan, and an optional cause, to a field path.
func invalidPlan(path string, cause error) error {
	return evidence.At(path, errors.Join(ErrInvalidPlan, cause))
}

// knownBoolean reports whether value retains a reported or implied value.
func knownBoolean(value evidence.Boolean) bool {
	return value.State == evidence.ValueReported || value.State == evidence.ValueImplied
}

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func validResourceName(value string) bool {
	if value == "" || len(value) > evidence.MaxNameLength {
		return false
	}
	for label := range strings.SplitSeq(value, ".") {
		if !dnsLabel.MatchString(label) {
			return false
		}
	}
	return true
}

// Validate checks the fixed control surface and every optimistic-concurrency precondition.
// A valid plan can still be rejected by RBAC, admission, or a concurrent resource change.
func (p Plan) Validate() error {
	if err := validateControlTarget(p.Contract, p.Target, p.Generation); err != nil {
		return err
	}
	switch p.Control {
	case ControlPause:
		if p.MaintenanceState != evidence.ObjectUnknown {
			return invalidPlan("maintenanceState", nil)
		}
	case ControlMaintenance:
		switch p.MaintenanceState {
		case evidence.ObjectAbsent, evidence.ObjectNull, evidence.ObjectReported:
		default:
			return invalidPlan("maintenanceState", nil)
		}
	default:
		return invalidPlan("control", nil)
	}
	return nil
}

// validateControlTarget checks a supported contract, an exact OpenBaoCluster
// reference, a bounded reported resource version, and a positive generation.
func validateControlTarget(contract adapter.Contract, target evidence.Reference, generation evidence.Integer) error {
	if contract != adapter.OpenBaoV1Alpha1 && contract != adapter.KubeBaoV1Alpha1 {
		return invalidPlan("contract", nil)
	}
	info, err := adapter.Describe(contract)
	if err != nil {
		return invalidPlan("contract", nil)
	}
	if err := target.Validate(); err != nil {
		return invalidPlan("target", err)
	}
	if target.Field != "" {
		return invalidPlan("target.field", nil)
	}
	id := target.Identity
	if id.Kind != "OpenBaoCluster" {
		return invalidPlan("target.identity.kind", nil)
	}
	if id.Group != info.APIGroup {
		return invalidPlan("target.identity.group", nil)
	}
	if target.APIVersion != info.APIVersion {
		return invalidPlan("target.apiVersion", nil)
	}
	if len(id.Namespace) > maxNamespaceLength || !dnsLabel.MatchString(id.Namespace) {
		return invalidPlan("target.identity.namespace", nil)
	}
	if !validResourceName(id.Name) {
		return invalidPlan("target.identity.name", nil)
	}
	version := target.ResourceVersion
	if version.State != evidence.ValueReported || len(*version.Value) > evidence.MaxNameLength {
		return invalidPlan("target.resourceVersion", nil)
	}
	if err := generation.Validate(); err != nil {
		return invalidPlan("generation", err)
	}
	if generation.State != evidence.ValueReported || *generation.Value <= 0 {
		return invalidPlan("generation", nil)
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
			change.Path = "/spec/maintenance"
			change.Value = map[string]bool{"enabled": p.Enabled}
		}
	}
	return marshalControlPatch(p.Target, p.Generation, change)
}

func marshalControlPatch(target evidence.Reference, generation evidence.Integer, change patchOperation) ([]byte, error) {
	operations := []patchOperation{
		{Operation: "test", Path: "/metadata/uid", Value: target.Identity.UID},
		{Operation: "test", Path: "/metadata/resourceVersion", Value: *target.ResourceVersion.Value},
		{Operation: "test", Path: "/metadata/generation", Value: *generation.Value},
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
		value.ResourceVersion.Value = new(*value.ResourceVersion.Value)
	}
	if value.CollectedAt.Value != nil {
		value.CollectedAt.Value = new(*value.CollectedAt.Value)
	}
	return value
}

func copyInteger(value evidence.Integer) evidence.Integer {
	if value.Value != nil {
		value.Value = new(*value.Value)
	}
	return value
}
