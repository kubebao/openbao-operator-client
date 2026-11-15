package evidence

import (
	"cmp"
	"math"
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

// Group returns the API group of the owner's API version, or "" for the core group.
func (v OwnerReference) Group() string {
	group, _, qualified := strings.Cut(v.APIVersion, "/")
	if !qualified {
		return ""
	}
	return group
}

// Validate checks that every field is approved text and that the API version is well formed.
func (v OwnerReference) Validate() error {
	for _, field := range []struct{ name, value string }{
		{"apiVersion", v.APIVersion},
		{"kind", v.Kind},
		{"name", v.Name},
		{"uid", v.UID},
	} {
		if !ValidText(field.value, MaxNameLength) {
			return At(field.name, ErrInvalidEvidence)
		}
	}
	group, version, qualified := strings.Cut(v.APIVersion, "/")
	if qualified && (group == "" || version == "" || strings.Contains(version, "/")) {
		return At("apiVersion", ErrInvalidEvidence)
	}
	return nil
}

// Owners is a canonical, sorted set of owner references with unique UIDs.
type Owners struct {
	State  ValueState       `json:"state"`
	Values []OwnerReference `json:"values"`
}

func compareOwners(a, b OwnerReference) int {
	return cmp.Or(
		cmp.Compare(a.APIVersion, b.APIVersion),
		cmp.Compare(a.Kind, b.Kind),
		cmp.Compare(a.Name, b.Name),
		cmp.Compare(a.UID, b.UID),
		cmp.Compare(boolRank(a.Controller), boolRank(b.Controller)),
	)
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
		return Owners{}, At("values", ErrInvalidEvidence)
	}
	sorted := slices.Clone(values)
	slices.SortFunc(sorted, compareOwners)
	result := Owners{State: ValueReported, Values: sorted}
	if err := result.Validate(); err != nil {
		return Owners{}, err
	}
	return result, nil
}

// Validate checks the set's presence contract and that reported owners are valid, sorted, and have unique UIDs.
func (v Owners) Validate() error {
	reported, err := validateSet(v.State, v.Values)
	if !reported {
		return err
	}
	seen := map[string]bool{}
	for i, value := range v.Values {
		path := itemPath("values", i)
		if err := value.Validate(); err != nil {
			return At(path, err)
		}
		if seen[value.UID] {
			return At(path+".uid", ErrInvalidEvidence)
		}
		if i > 0 && compareOwners(v.Values[i-1], value) >= 0 {
			return At(path, ErrInvalidEvidence)
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
	if err := value.Validate(); err != nil {
		return err
	}
	if value.Value != nil && !workloadRevision.MatchString(*value.Value) {
		return ErrInvalidEvidence
	}
	return nil
}

// ValidateWorkloadPool validates one reported operator pool label.
// It does not establish a resource's association with a cluster.
func ValidateWorkloadPool(value Text) error {
	return validateEnum(value, "voter", "read-replica")
}

// Validate checks the metadata fields and the fields that each workload kind permits.
// StatefulSet metadata retains no Pod labels or restart annotation.
func (v WorkloadMetadata) Validate() error {
	if err := checkFields(
		fieldCheck{"reference", v.Reference.Validate()},
		fieldCheck{"owners", v.Owners.Validate()},
		fieldCheck{"clusterOwnerUID", v.ClusterOwnerUID.Validate()},
		fieldCheck{"maintenance", v.Maintenance.Validate()},
		fieldCheck{"restartAt", v.RestartAt.Validate()},
		fieldCheck{"controllerRevision", ValidateWorkloadRevision(v.ControllerRevision)},
		fieldCheck{"pool", ValidateWorkloadPool(v.Pool)},
		fieldCheck{"revision", ValidateWorkloadRevision(v.Revision)},
		fieldCheck{"deletionTimestamp", v.DeletionTimestamp.Validate()},
	); err != nil {
		return err
	}
	switch v.Reference.Identity.Kind {
	case "StatefulSet":
		if v.Reference.APIVersion != "apps/v1" {
			return At("reference.apiVersion", ErrInvalidEvidence)
		}
		for _, field := range []struct {
			name  string
			state ValueState
		}{
			{"pool", v.Pool.State},
			{"revision", v.Revision.State},
			{"restartAt", v.RestartAt.State},
			{"controllerRevision", v.ControllerRevision.State},
		} {
			if field.state != ValueUnknown {
				return At(field.name, ErrInvalidEvidence)
			}
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

// Acknowledgement compares the StatefulSet generation with the status observed generation.
func (v StatefulSet) Acknowledgement() GenerationState {
	return compareGenerationFields(v.Generation, v.ObservedGeneration)
}

// Validate checks the metadata, generations, counts, labels, and update strategy.
// Ready replicas can be implied zero only after current acknowledgement of a reported desired count.
func (v StatefulSet) Validate() error {
	if err := checkFields(
		fieldCheck{"metadata", v.Metadata.Validate()},
		fieldCheck{"generation", v.Generation.Validate()},
		fieldCheck{"observedGeneration", v.ObservedGeneration.Validate()},
		fieldCheck{"desiredReplicas", validateCount(v.DesiredReplicas)},
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
		fieldCheck{"availableReplicas", validateCount(v.AvailableReplicas)},
	); err != nil {
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
	if v.ReadyReplicas.State == ValueImplied &&
		(v.Acknowledgement() != GenerationCurrent || v.DesiredReplicas.State != ValueReported) {
		return At("readyReplicas", ErrInvalidEvidence)
	}
	return nil
}

// ContainerKind distinguishes init containers from application containers.
type ContainerKind string

const (
	// ContainerInit identifies an init container.
	ContainerInit ContainerKind = "init"
	// ContainerApp identifies an application container.
	ContainerApp ContainerKind = "app"
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

// Validate checks the name, kind, readiness, restart count, and nested state
// objects. Readiness and the exit code are never implied.
func (v ContainerStatus) Validate() error {
	if !ValidText(v.Name, MaxNameLength) {
		return At("name", ErrInvalidEvidence)
	}
	if v.Kind != ContainerInit && v.Kind != ContainerApp {
		return At("kind", ErrInvalidEvidence)
	}
	if err := v.Ready.Validate(); err != nil {
		return At("ready", err)
	}
	if v.Ready.State == ValueImplied {
		return At("ready", ErrInvalidEvidence)
	}
	if err := validateCount(v.RestartCount); err != nil {
		return At("restartCount", err)
	}
	if !v.State.Valid() {
		return At("state", ErrInvalidEvidence)
	}
	if err := validateChild(v.State, "waiting", v.Waiting); err != nil {
		return err
	}
	if err := validateChild(v.State, "terminated", v.Terminated); err != nil {
		return err
	}
	if err := validateObject("waiting", v.Waiting, objectField{"waitingReason", v.WaitingReason}); err != nil {
		return err
	}
	if err := validateObject("terminated", v.Terminated,
		objectField{"terminatedReason", v.TerminatedReason},
		objectField{"exitCode", v.ExitCode},
	); err != nil {
		return err
	}
	if v.ExitCode.State == ValueImplied {
		return At("exitCode", ErrInvalidEvidence)
	}
	if v.ExitCode.Value != nil && (*v.ExitCode.Value < math.MinInt32 || *v.ExitCode.Value > math.MaxInt32) {
		return At("exitCode", ErrInvalidEvidence)
	}
	return nil
}

// Containers is a set of container statuses sorted by kind and name.
type Containers struct {
	State  ValueState        `json:"state"`
	Values []ContainerStatus `json:"values"`
}

// Validate checks the set's presence contract and that reported statuses are valid, sorted, and unique.
func (v Containers) Validate() error {
	reported, err := validateSet(v.State, v.Values)
	if !reported {
		return err
	}
	previous := ""
	for i, value := range v.Values {
		if err := value.Validate(); err != nil {
			return At(itemPath("values", i), err)
		}
		key := string(value.Kind) + "/" + value.Name
		if strings.Compare(previous, key) >= 0 {
			return At(itemPath("values", i)+".name", ErrInvalidEvidence)
		}
		previous = key
	}
	return nil
}

// Pod contains approved facts projected from one Pod read.
type Pod struct {
	Metadata       WorkloadMetadata `json:"metadata"`
	Generation     Integer          `json:"generation"`
	Phase          Text             `json:"phase"`
	Conditions     Conditions       `json:"conditions"`
	InitContainers Containers       `json:"initContainers"`
	Containers     Containers       `json:"containers"`
}

// Validate checks the metadata, fields, and that each container list holds only its kind.
func (v Pod) Validate() error {
	if err := checkFields(
		fieldCheck{"metadata", v.Metadata.Validate()},
		fieldCheck{"generation", v.Generation.Validate()},
		fieldCheck{"phase", v.Phase.Validate()},
		fieldCheck{"conditions", v.Conditions.Validate()},
		fieldCheck{"initContainers", v.InitContainers.Validate()},
		fieldCheck{"containers", v.Containers.Validate()},
	); err != nil {
		return err
	}
	if v.Metadata.Reference.Identity.Kind != "Pod" {
		return At("metadata.reference.identity.kind", ErrInvalidEvidence)
	}
	if v.Generation.State == ValueImplied {
		return At("generation", ErrInvalidEvidence)
	}
	for i, value := range v.InitContainers.Values {
		if value.Kind != ContainerInit {
			return At("initContainers."+itemPath("values", i)+".kind", ErrInvalidEvidence)
		}
	}
	for i, value := range v.Containers.Values {
		if value.Kind != ContainerApp {
			return At("containers."+itemPath("values", i)+".kind", ErrInvalidEvidence)
		}
	}
	return nil
}
