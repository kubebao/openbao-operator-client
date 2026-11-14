package evidence

import (
	"cmp"
	"regexp"
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

// WorkloadMetadata records approved resource metadata. Pool and Revision report
// Pod labels only. Grouping labels never establish resource ownership.
type WorkloadMetadata struct {
	Reference          Reference `json:"reference"`
	Owners             Owners    `json:"owners"`
	ClusterOwnerUID    Text      `json:"clusterOwnerUID"`
	RestartAt          Text      `json:"restartAt"`
	ControllerRevision Text      `json:"controllerRevision"`
	Maintenance        Text      `json:"maintenance"`
	Pool               Text      `json:"pool"`
	Revision           Text      `json:"revision"`
	DeletionTimestamp  Timestamp `json:"deletionTimestamp"`
}

var workloadRevision = regexp.MustCompile(`^[A-Za-z0-9](?:[-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?$`)

// ValidateWorkloadRevision validates a reported, nonempty Kubernetes label value.
// Unknown and invalid observations retain no value; implied revisions are invalid.
func ValidateWorkloadRevision(value Text) error {
	if value.Validate() != nil || value.Value != nil && !workloadRevision.MatchString(*value.Value) {
		return ErrInvalidEvidence
	}
	return nil
}

// ValidateWorkloadPool validates one reported operator pool label.
// It does not establish a resource's association with a cluster.
func ValidateWorkloadPool(value Text) error {
	if value.Validate() != nil || value.Value != nil && !slices.Contains([]string{"voter", "read-replica"}, *value.Value) {
		return ErrInvalidEvidence
	}
	return nil
}

func (v WorkloadMetadata) Validate() error {
	if err := checkFields(fieldCheck{"reference", v.Reference.Validate()}, fieldCheck{"owners", v.Owners.Validate()},
		fieldCheck{"clusterOwnerUID", v.ClusterOwnerUID.Validate()}, fieldCheck{"maintenance", v.Maintenance.Validate()},
		fieldCheck{"restartAt", v.RestartAt.Validate()},
		fieldCheck{"controllerRevision", ValidateWorkloadRevision(v.ControllerRevision)},
		fieldCheck{"pool", ValidateWorkloadPool(v.Pool)}, fieldCheck{"revision", ValidateWorkloadRevision(v.Revision)},
		fieldCheck{"deletionTimestamp", v.DeletionTimestamp.Validate()}); err != nil {
		return err
	}
	switch v.Reference.Identity.Kind {
	case "StatefulSet":
		if v.Reference.APIVersion != "apps/v1" {
			return At("reference.apiVersion", ErrInvalidEvidence)
		}
		if v.Pool.State != ValueUnknown || v.Revision.State != ValueUnknown || v.RestartAt.State != ValueUnknown ||
			v.ControllerRevision.State != ValueUnknown {
			return At("pool", ErrInvalidEvidence)
		}
	case "Pod":
		if v.Reference.APIVersion != "v1" {
			return At("reference.apiVersion", ErrInvalidEvidence)
		}
	default:
		return At("reference.identity.kind", ErrInvalidEvidence)
	}
	return nil
}

// StatefulSet retains selector and template grouping labels as separate facts.
// Consumers check their agreement only after confirming the cluster UID association.
type StatefulSet struct {
	Metadata           WorkloadMetadata `json:"metadata"`
	Generation         Integer          `json:"generation"`
	ObservedGeneration Integer          `json:"observedGeneration"`
	DesiredReplicas    Integer          `json:"desiredReplicas"`
	ReadyReplicas      Integer          `json:"readyReplicas"`
	SelectorPool       Text             `json:"selectorPool"`
	SelectorRevision   Text             `json:"selectorRevision"`
	TemplatePool       Text             `json:"templatePool"`
	TemplateRevision   Text             `json:"templateRevision"`
	TemplateRestartAt  Text             `json:"templateRestartAt"`
	UpdateStrategy     Text             `json:"updateStrategy"`
	RollingPartition   Integer          `json:"rollingPartition"`
	CurrentRevision    Text             `json:"currentRevision"`
	UpdateRevision     Text             `json:"updateRevision"`
	CurrentReplicas    Integer          `json:"currentReplicas"`
	UpdatedReplicas    Integer          `json:"updatedReplicas"`
	MinReadySeconds    Integer          `json:"minReadySeconds"`
	AvailableReplicas  Integer          `json:"availableReplicas"`
}

func (v StatefulSet) Acknowledgement() GenerationState {
	return compareGenerationFields(v.Generation, v.ObservedGeneration)
}

func (v StatefulSet) Validate() error {
	if err := checkFields(fieldCheck{"metadata", v.Metadata.Validate()}, fieldCheck{"generation", v.Generation.Validate()},
		fieldCheck{"observedGeneration", v.ObservedGeneration.Validate()}, fieldCheck{"desiredReplicas", validateCount(v.DesiredReplicas)},
		fieldCheck{"readyReplicas", validateReadyCount(v.ReadyReplicas)},
		fieldCheck{"selectorPool", ValidateWorkloadPool(v.SelectorPool)},
		fieldCheck{"selectorRevision", ValidateWorkloadRevision(v.SelectorRevision)},
		fieldCheck{"templatePool", ValidateWorkloadPool(v.TemplatePool)},
		fieldCheck{"templateRevision", ValidateWorkloadRevision(v.TemplateRevision)},
		fieldCheck{"templateRestartAt", v.TemplateRestartAt.Validate()},
		fieldCheck{"updateStrategy", validateEnum(v.UpdateStrategy, "RollingUpdate", "OnDelete")},
		fieldCheck{"rollingPartition", validateCount(v.RollingPartition)},
		fieldCheck{"currentRevision", ValidateWorkloadRevision(v.CurrentRevision)},
		fieldCheck{"updateRevision", ValidateWorkloadRevision(v.UpdateRevision)},
		fieldCheck{"currentReplicas", validateCount(v.CurrentReplicas)},
		fieldCheck{"updatedReplicas", validateCount(v.UpdatedReplicas)},
		fieldCheck{"minReadySeconds", validateCount(v.MinReadySeconds)},
		fieldCheck{"availableReplicas", validateCount(v.AvailableReplicas)}); err != nil {
		return err
	}
	if v.Metadata.Reference.Identity.Kind != "StatefulSet" {
		return At("metadata.reference.identity.kind", ErrInvalidEvidence)
	}
	if v.Generation.State == ValueImplied {
		return At("generation", ErrInvalidEvidence)
	}
	if v.ObservedGeneration.State == ValueImplied {
		return At("observedGeneration", ErrInvalidEvidence)
	}
	if v.ReadyReplicas.State == ValueImplied && (v.Acknowledgement() != GenerationCurrent || v.DesiredReplicas.State != ValueReported) {
		return At("readyReplicas", ErrInvalidEvidence)
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
	Metadata       WorkloadMetadata `json:"metadata"`
	Generation     Integer          `json:"generation"`
	Phase          Text             `json:"phase"`
	Conditions     Conditions       `json:"conditions"`
	InitContainers Containers       `json:"initContainers"`
	Containers     Containers       `json:"containers"`
}

func (v Pod) Validate() error {
	if err := checkFields(fieldCheck{"metadata", v.Metadata.Validate()}, fieldCheck{"generation", v.Generation.Validate()},
		fieldCheck{"phase", v.Phase.Validate()}, fieldCheck{"conditions", v.Conditions.Validate()},
		fieldCheck{"initContainers", v.InitContainers.Validate()}, fieldCheck{"containers", v.Containers.Validate()}); err != nil {
		return err
	}
	if v.Metadata.Reference.Identity.Kind != "Pod" {
		return At("metadata.reference.identity.kind", ErrInvalidEvidence)
	}
	if v.Generation.State == ValueImplied {
		return At("generation", ErrInvalidEvidence)
	}
	for _, value := range v.InitContainers.Values {
		if value.Kind != ContainerInit {
			return At("initContainers", ErrInvalidEvidence)
		}
	}
	for _, value := range v.Containers.Values {
		if value.Kind != ContainerApp {
			return At("containers", ErrInvalidEvidence)
		}
	}
	return nil
}
