package evidence

import "strings"

const SchemaVersion = "4"

type ConditionStatus string

const (
	ConditionTrue    ConditionStatus = "True"
	ConditionFalse   ConditionStatus = "False"
	ConditionUnknown ConditionStatus = "Unknown"
)

// Condition excludes provider messages. Reason contains a bounded structured code.
type Condition struct {
	Type               string          `json:"type"`
	Status             ConditionStatus `json:"status"`
	Reason             Text            `json:"reason"`
	ObservedGeneration Integer         `json:"observedGeneration"`
	LastTransitionAt   Timestamp       `json:"lastTransitionAt"`
}

func (c Condition) Validate() error {
	if !validText(c.Type, 128, false) || c.Reason.Validate() != nil || c.ObservedGeneration.Validate() != nil ||
		c.ObservedGeneration.State == ValueImplied || c.LastTransitionAt.Validate() != nil {
		return ErrInvalidEvidence
	}
	switch c.Status {
	case ConditionTrue, ConditionFalse, ConditionUnknown:
		return nil
	default:
		return ErrInvalidEvidence
	}
}

func (c Condition) Acknowledgement(generation Integer) GenerationState {
	return compareGenerationFields(generation, c.ObservedGeneration)
}

// Conditions distinguishes uncollected or invalid conditions from an observed empty set.
// Reported entries must have unique types and be sorted by type.
type Conditions struct {
	State  ValueState  `json:"state"`
	Values []Condition `json:"values"`
}

func (c Conditions) Validate() error {
	if c.State == ValueUnknown || c.State == ValueInvalid {
		if c.Values == nil {
			return nil
		}
		return ErrInvalidEvidence
	}
	if c.State != ValueReported || c.Values == nil {
		return ErrInvalidEvidence
	}
	previous := ""
	for _, value := range c.Values {
		if value.Validate() != nil || strings.Compare(previous, value.Type) >= 0 {
			return ErrInvalidEvidence
		}
		previous = value.Type
	}
	return nil
}

// Cluster contains approved facts projected from one OpenBaoCluster read.
// AdapterContract names projection semantics, not operator integration qualification.
// Health, history, process results, and provider messages are not part of this value.
type Cluster struct {
	SchemaVersion      string          `json:"schemaVersion"`
	AdapterContract    string          `json:"adapterContract"`
	Reference          Reference       `json:"reference"`
	CreatedAt          Timestamp       `json:"createdAt"`
	DeletionTimestamp  Timestamp       `json:"deletionTimestamp"`
	Generation         Integer         `json:"generation"`
	ObservedGeneration Integer         `json:"observedGeneration"`
	DesiredVersion     Text            `json:"desiredVersion"`
	ObservedVersion    Text            `json:"observedVersion"`
	Phase              Text            `json:"phase"`
	DesiredReplicas    Integer         `json:"desiredReplicas"`
	ReadyReplicas      Integer         `json:"readyReplicas"`
	Paused             Boolean         `json:"paused"`
	Conditions         Conditions      `json:"conditions"`
	Backup             Backup          `json:"backup"`
	Upgrade            Upgrade         `json:"upgrade"`
	BlueGreen          BlueGreen       `json:"blueGreen"`
	ReadReplicas       ReplicaCounts   `json:"readReplicas"`
	Maintenance        Maintenance     `json:"maintenance"`
	RestoreOrigin      Text            `json:"restoreOrigin"`
	RestoreHold        Text            `json:"restoreHold"`
	OperationLock      OperationLock   `json:"operationLock"`
	BreakGlass         BreakGlass      `json:"breakGlass"`
	LegacyRestore      LegacyRestore   `json:"legacyRestore"`
	WorkloadError      ControllerError `json:"workloadError"`
	AdminOpsError      ControllerError `json:"adminOpsError"`
}

func (c Cluster) Acknowledgement() GenerationState {
	return compareGenerationFields(c.Generation, c.ObservedGeneration)
}

// Validate checks the normalized value, including after serialized replay.
// It does not select an adapter or establish producer freshness.
func (c Cluster) Validate() error {
	if c.SchemaVersion != SchemaVersion {
		return At("schemaVersion", ErrInvalidEvidence)
	}
	if !validText(c.AdapterContract, 253, false) {
		return At("adapterContract", ErrInvalidEvidence)
	}
	if err := checkFields(
		fieldCheck{"reference", c.Reference.Validate()}, fieldCheck{"createdAt", c.CreatedAt.Validate()},
		fieldCheck{"deletionTimestamp", c.DeletionTimestamp.Validate()},
		fieldCheck{"generation", c.Generation.Validate()}, fieldCheck{"observedGeneration", c.ObservedGeneration.Validate()},
		fieldCheck{"conditions", c.Conditions.Validate()}, fieldCheck{"paused", c.Paused.Validate()},
		fieldCheck{"desiredVersion", c.DesiredVersion.Validate()}, fieldCheck{"observedVersion", c.ObservedVersion.Validate()},
		fieldCheck{"phase", c.Phase.Validate()}, fieldCheck{"desiredReplicas", validateCount(c.DesiredReplicas)},
		fieldCheck{"readyReplicas", validateReadyCount(c.ReadyReplicas)},
	); err != nil {
		return err
	}
	if c.Reference.Identity.Kind != "OpenBaoCluster" {
		return At("reference.identity.kind", ErrInvalidEvidence)
	}
	if c.Generation.State == ValueImplied {
		return At("generation", ErrInvalidEvidence)
	}
	if c.ObservedGeneration.State == ValueImplied {
		return At("observedGeneration", ErrInvalidEvidence)
	}
	if c.ReadyReplicas.State == ValueImplied && (c.Acknowledgement() != GenerationCurrent || c.DesiredReplicas.State != ValueReported) {
		return At("readyReplicas", ErrInvalidEvidence)
	}
	return checkFields(
		fieldCheck{
			"backup",
			c.Backup.Validate(),
		},
		fieldCheck{"upgrade", c.Upgrade.Validate()},
		fieldCheck{"blueGreen", c.BlueGreen.Validate()},
		fieldCheck{"readReplicas", c.ReadReplicas.Validate()},
		fieldCheck{"maintenance", c.Maintenance.Validate()},
		fieldCheck{"restoreOrigin", c.RestoreOrigin.Validate()},
		fieldCheck{"restoreHold", c.RestoreHold.Validate()},
		fieldCheck{"operationLock", c.OperationLock.Validate()},
		fieldCheck{"breakGlass", c.BreakGlass.Validate()},
		fieldCheck{
			"legacyRestore",
			c.LegacyRestore.Validate(),
		},
		fieldCheck{"workloadError", c.WorkloadError.Validate()},
		fieldCheck{"adminOpsError", c.AdminOpsError.Validate()},
	)
}

func validateReadyCount(v Integer) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if v.Value != nil && (*v.Value < 0 || *v.Value > 2147483647) {
		return ErrInvalidEvidence
	}
	return nil
}
