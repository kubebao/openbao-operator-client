package evidence

import (
	"cmp"
	"slices"
	"strings"
)

// OwnerReference retains the Kubernetes owner identity. Controller records an
// explicit true claim; an omitted, null, or false controller flag makes no claim.
type OwnerReference struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	UID        string `json:"uid"`
	Controller bool   `json:"controller"`
}

func (v OwnerReference) Group() string {
	group, _, qualified := strings.Cut(v.APIVersion, "/")
	if !qualified {
		return ""
	}
	return group
}

func (v OwnerReference) Validate() error {
	for _, value := range []string{v.APIVersion, v.Kind, v.Name, v.UID} {
		if !validText(value, 253, false) {
			return ErrInvalidEvidence
		}
	}
	group, version, qualified := strings.Cut(v.APIVersion, "/")
	if qualified && (group == "" || version == "" || strings.Contains(version, "/")) {
		return ErrInvalidEvidence
	}
	return nil
}

type Owners struct {
	State  ValueState       `json:"state"`
	Values []OwnerReference `json:"values"`
}

func compareOwners(a, b OwnerReference) int {
	return cmp.Or(slices.Compare([]string{a.APIVersion, a.Kind, a.Name, a.UID}, []string{b.APIVersion, b.Kind, b.Name, b.UID}),
		cmp.Compare(boolRank(a.Controller), boolRank(b.Controller)))
}

func boolRank(value bool) int {
	if value {
		return 1
	}
	return 0
}

// NewOwners returns an independent canonical set; duplicate UIDs are ambiguous.
func NewOwners(values []OwnerReference) (Owners, error) {
	if values == nil {
		return Owners{}, ErrInvalidEvidence
	}
	copyValues := slices.Clone(values)
	slices.SortFunc(copyValues, compareOwners)
	result := Owners{State: ValueReported, Values: copyValues}
	if result.Validate() != nil {
		return Owners{}, ErrInvalidEvidence
	}
	return result, nil
}

func (v Owners) Validate() error {
	if v.State == ValueUnknown || v.State == ValueInvalid {
		if v.Values == nil {
			return nil
		}
		return ErrInvalidEvidence
	}
	if v.State != ValueReported || v.Values == nil {
		return ErrInvalidEvidence
	}
	seen := map[string]bool{}
	for i, value := range v.Values {
		if value.Validate() != nil || seen[value.UID] || i > 0 && compareOwners(v.Values[i-1], value) >= 0 {
			return ErrInvalidEvidence
		}
		seen[value.UID] = true
	}
	return nil
}

type WorkloadMetadata struct {
	Reference         Reference `json:"reference"`
	Owners            Owners    `json:"owners"`
	ClusterOwnerUID   Text      `json:"clusterOwnerUID"`
	Maintenance       Text      `json:"maintenance"`
	DeletionTimestamp Timestamp `json:"deletionTimestamp"`
}

func (v WorkloadMetadata) Validate() error {
	if v.Reference.Validate() != nil || v.Owners.Validate() != nil || v.ClusterOwnerUID.Validate() != nil ||
		v.Maintenance.Validate() != nil ||
		v.DeletionTimestamp.Validate() != nil {
		return ErrInvalidEvidence
	}
	switch v.Reference.Identity.Kind {
	case "StatefulSet":
		if v.Reference.APIVersion != "apps/v1" {
			return ErrInvalidEvidence
		}
	case "Pod":
		if v.Reference.APIVersion != "v1" {
			return ErrInvalidEvidence
		}
	default:
		return ErrInvalidEvidence
	}
	return nil
}

type StatefulSet struct {
	SchemaVersion      string           `json:"schemaVersion"`
	AdapterContract    string           `json:"adapterContract"`
	Metadata           WorkloadMetadata `json:"metadata"`
	Generation         Integer          `json:"generation"`
	ObservedGeneration Integer          `json:"observedGeneration"`
	DesiredReplicas    Integer          `json:"desiredReplicas"`
	ReadyReplicas      Integer          `json:"readyReplicas"`
}

func (v StatefulSet) Acknowledgement() GenerationState {
	return compareGenerationFields(v.Generation, v.ObservedGeneration)
}

func (v StatefulSet) Validate() error {
	if v.SchemaVersion != SchemaVersion || !validText(v.AdapterContract, 253, false) || v.Metadata.Validate() != nil ||
		v.Metadata.Reference.Identity.Kind != "StatefulSet" || v.Generation.Validate() != nil || v.ObservedGeneration.Validate() != nil ||
		v.Generation.State == ValueImplied || v.ObservedGeneration.State == ValueImplied || validateCount(v.DesiredReplicas) != nil ||
		v.ReadyReplicas.Validate() != nil {
		return ErrInvalidEvidence
	}
	if v.ReadyReplicas.Value != nil && (*v.ReadyReplicas.Value < 0 || *v.ReadyReplicas.Value > 2147483647) {
		return ErrInvalidEvidence
	}
	if v.ReadyReplicas.State == ValueImplied && (v.Acknowledgement() != GenerationCurrent || v.DesiredReplicas.State != ValueReported) {
		return ErrInvalidEvidence
	}
	return nil
}

type ContainerKind string

const (
	ContainerInit ContainerKind = "init"
	ContainerApp  ContainerKind = "app"
)

// ContainerStatus excludes runtime messages and retains signed int32 termination codes.
type ContainerStatus struct {
	Kind             ContainerKind `json:"kind"`
	Name             string        `json:"name"`
	Ready            Boolean       `json:"ready"`
	RestartCount     Integer       `json:"restartCount"`
	State            ObjectState   `json:"state"`
	Waiting          ObjectState   `json:"waiting"`
	WaitingReason    Text          `json:"waitingReason"`
	Terminated       ObjectState   `json:"terminated"`
	TerminatedReason Text          `json:"terminatedReason"`
	ExitCode         Integer       `json:"exitCode"`
}

func (v ContainerStatus) Validate() error {
	if !validText(v.Name, 253, false) || (v.Kind != ContainerInit && v.Kind != ContainerApp) || v.Ready.Validate() != nil ||
		v.Ready.State == ValueImplied || validateCount(v.RestartCount) != nil || !v.State.Valid() ||
		validateChild(v.State, v.Waiting) != nil || validateChild(v.State, v.Terminated) != nil ||
		validateObject(v.Waiting, v.WaitingReason) != nil || validateObject(v.Terminated, v.TerminatedReason, v.ExitCode) != nil ||
		v.ExitCode.State == ValueImplied {
		return ErrInvalidEvidence
	}
	if v.ExitCode.Value != nil && (*v.ExitCode.Value < -2147483648 || *v.ExitCode.Value > 2147483647) {
		return ErrInvalidEvidence
	}
	return nil
}

type Containers struct {
	State  ValueState        `json:"state"`
	Values []ContainerStatus `json:"values"`
}

func (v Containers) Validate() error {
	if v.State == ValueUnknown || v.State == ValueInvalid {
		if v.Values == nil {
			return nil
		}
		return ErrInvalidEvidence
	}
	if v.State != ValueReported || v.Values == nil {
		return ErrInvalidEvidence
	}
	previous := ""
	for _, value := range v.Values {
		key := string(value.Kind) + "/" + value.Name
		if value.Validate() != nil || strings.Compare(previous, key) >= 0 {
			return ErrInvalidEvidence
		}
		previous = key
	}
	return nil
}

type Pod struct {
	SchemaVersion   string           `json:"schemaVersion"`
	AdapterContract string           `json:"adapterContract"`
	Metadata        WorkloadMetadata `json:"metadata"`
	Generation      Integer          `json:"generation"`
	Phase           Text             `json:"phase"`
	Conditions      Conditions       `json:"conditions"`
	InitContainers  Containers       `json:"initContainers"`
	Containers      Containers       `json:"containers"`
}

func (v Pod) Validate() error {
	if v.SchemaVersion != SchemaVersion || !validText(v.AdapterContract, 253, false) || v.Metadata.Validate() != nil ||
		v.Metadata.Reference.Identity.Kind != "Pod" || v.Generation.Validate() != nil || v.Generation.State == ValueImplied ||
		v.Phase.Validate() != nil || v.Conditions.Validate() != nil ||
		v.InitContainers.Validate() != nil || v.Containers.Validate() != nil {
		return ErrInvalidEvidence
	}
	for _, value := range v.InitContainers.Values {
		if value.Kind != ContainerInit {
			return ErrInvalidEvidence
		}
	}
	for _, value := range v.Containers.Values {
		if value.Kind != ContainerApp {
			return ErrInvalidEvidence
		}
	}
	return nil
}
