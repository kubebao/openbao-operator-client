package evidence

import "strings"

const SchemaVersion = "1"

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
	SchemaVersion      string     `json:"schemaVersion"`
	AdapterContract    string     `json:"adapterContract"`
	Reference          Reference  `json:"reference"`
	CreatedAt          Timestamp  `json:"createdAt"`
	Generation         Integer    `json:"generation"`
	ObservedGeneration Integer    `json:"observedGeneration"`
	DesiredVersion     Text       `json:"desiredVersion"`
	ObservedVersion    Text       `json:"observedVersion"`
	Phase              Text       `json:"phase"`
	DesiredReplicas    Integer    `json:"desiredReplicas"`
	ReadyReplicas      Integer    `json:"readyReplicas"`
	Paused             Boolean    `json:"paused"`
	Conditions         Conditions `json:"conditions"`
}

func (c Cluster) Acknowledgement() GenerationState {
	return compareGenerationFields(c.Generation, c.ObservedGeneration)
}

// Validate checks the normalized value, including after serialized replay.
// It does not select an adapter or establish producer freshness.
func (c Cluster) Validate() error {
	if c.SchemaVersion != SchemaVersion || !validText(c.AdapterContract, 253, false) || c.Reference.Validate() != nil ||
		c.Reference.Identity.Kind != "OpenBaoCluster" || c.CreatedAt.Validate() != nil || c.Generation.Validate() != nil ||
		c.ObservedGeneration.Validate() != nil || c.Conditions.Validate() != nil || c.Paused.Validate() != nil {
		return ErrInvalidEvidence
	}
	for _, value := range []Text{c.DesiredVersion, c.ObservedVersion, c.Phase} {
		if value.Validate() != nil {
			return ErrInvalidEvidence
		}
	}
	for _, value := range []Integer{c.DesiredReplicas, c.ReadyReplicas} {
		if value.Validate() != nil || value.Value != nil && (*value.Value < 0 || *value.Value > 2147483647) {
			return ErrInvalidEvidence
		}
	}
	if c.Generation.State == ValueImplied || c.ObservedGeneration.State == ValueImplied || c.DesiredReplicas.State == ValueImplied {
		return ErrInvalidEvidence
	}
	if c.ReadyReplicas.State == ValueImplied && (c.Acknowledgement() != GenerationCurrent || c.DesiredReplicas.State != ValueReported) {
		return ErrInvalidEvidence
	}
	return nil
}
