package evidence

// ObjectState records JSON object presence independently of its child values.
// Unknown means an ancestor was not observed. Absent means a valid parent omitted
// this field. Null preserves an explicit null; it does not establish health.
type ObjectState string

const (
	ObjectUnknown  ObjectState = "unknown"
	ObjectAbsent   ObjectState = "absent"
	ObjectNull     ObjectState = "null"
	ObjectReported ObjectState = "reported"
	ObjectInvalid  ObjectState = "invalid"
)

func (s ObjectState) Valid() bool {
	switch s {
	case ObjectUnknown, ObjectAbsent, ObjectNull, ObjectReported, ObjectInvalid:
		return true
	default:
		return false
	}
}

type observedField interface {
	Validate() error
	fieldState() ValueState
}

func (v Integer) fieldState() ValueState    { return v.State }
func (v Boolean) fieldState() ValueState    { return v.State }
func (v Text) fieldState() ValueState       { return v.State }
func (v Timestamp) fieldState() ValueState  { return v.State }
func (v Conditions) fieldState() ValueState { return v.State }
func (v Ordinals) fieldState() ValueState   { return v.State }

func validateObject(state ObjectState, fields ...observedField) error {
	if !state.Valid() {
		return ErrInvalidEvidence
	}
	for _, field := range fields {
		if field.Validate() != nil || state != ObjectReported && field.fieldState() != ValueUnknown {
			return ErrInvalidEvidence
		}
	}
	return nil
}

func validateChild(parent, child ObjectState) error {
	if parent != ObjectReported && child != ObjectUnknown {
		return ErrInvalidEvidence
	}
	return nil
}

// Ordinals is a sorted, unique set of nonnegative int32 workload ordinals.
// Missing and invalid arrays retain no values. Explicit empty arrays remain reported.
type Ordinals struct {
	State  ValueState `json:"state"`
	Values []int64    `json:"values"`
}

func (v Ordinals) Validate() error {
	if v.State == ValueUnknown || v.State == ValueInvalid {
		if v.Values == nil {
			return nil
		}
		return ErrInvalidEvidence
	}
	if v.State != ValueReported || v.Values == nil {
		return ErrInvalidEvidence
	}
	previous := int64(-1)
	for _, value := range v.Values {
		if value < 0 || value > 2147483647 || value <= previous {
			return ErrInvalidEvidence
		}
		previous = value
	}
	return nil
}

// ControllerError excludes the provider message and records only its structured signal.
type ControllerError struct {
	State  ObjectState `json:"state"`
	Reason Text        `json:"reason"`
	At     Timestamp   `json:"at"`
}

func (v ControllerError) Validate() error { return validateObject(v.State, v.Reason, v.At) }

type RestoreTest struct {
	State          ObjectState `json:"state"`
	Active         ObjectState `json:"active"`
	Last           ObjectState `json:"last"`
	LastOutcome    Text        `json:"lastOutcome"`
	LastReason     Text        `json:"lastReason"`
	LastSuccessAt  Timestamp   `json:"lastSuccessAt"`
	FinishedAt     Timestamp   `json:"finishedAt"`
	SnapshotKey    Text        `json:"snapshotKey"`
	SnapshotDigest Text        `json:"snapshotDigest"`
	Conditions     Conditions  `json:"conditions"`
}

func (v RestoreTest) Validate() error {
	if !v.Active.Valid() || validateChild(v.State, v.Active) != nil || validateChild(v.State, v.Last) != nil ||
		validateObject(v.State, v.LastSuccessAt, v.Conditions) != nil {
		return ErrInvalidEvidence
	}
	return validateObject(v.Last, v.LastOutcome, v.LastReason, v.FinishedAt, v.SnapshotKey, v.SnapshotDigest)
}

// Backup keeps the configuration and reported status distinct. Counters are never
// defaulted to zero, and a recent failure never overwrites an earlier success time.
type Backup struct {
	Configuration       ObjectState `json:"configuration"`
	State               ObjectState `json:"state"`
	LastSuccessAt       Timestamp   `json:"lastSuccessAt"`
	LastAttemptAt       Timestamp   `json:"lastAttemptAt"`
	NextScheduledAt     Timestamp   `json:"nextScheduledAt"`
	ConsecutiveFailures Integer     `json:"consecutiveFailures"`
	LastFailureReason   Text        `json:"lastFailureReason"`
	LastFailureAt       Timestamp   `json:"lastFailureAt"`
	SizeBytes           Integer     `json:"sizeBytes"`
	SnapshotKey         Text        `json:"snapshotKey"`
	SnapshotDigest      Text        `json:"snapshotDigest"`
	RestoreTest         RestoreTest `json:"restoreTest"`
}

func (v Backup) Validate() error {
	if !v.Configuration.Valid() || validateObject(v.State, v.LastSuccessAt, v.LastAttemptAt, v.NextScheduledAt,
		v.ConsecutiveFailures, v.LastFailureReason, v.LastFailureAt, v.SizeBytes, v.SnapshotKey, v.SnapshotDigest) != nil ||
		validateChild(v.State, v.RestoreTest.State) != nil || v.RestoreTest.Validate() != nil {
		return ErrInvalidEvidence
	}
	for _, count := range []Integer{v.ConsecutiveFailures, v.SizeBytes} {
		if count.State == ValueImplied || count.Value != nil && *count.Value < 0 {
			return ErrInvalidEvidence
		}
	}
	if v.ConsecutiveFailures.Value != nil && *v.ConsecutiveFailures.Value > 2147483647 {
		return ErrInvalidEvidence
	}
	return nil
}

type Upgrade struct {
	State            ObjectState     `json:"state"`
	ExecutionID      Text            `json:"executionID"`
	TargetVersion    Text            `json:"targetVersion"`
	FromVersion      Text            `json:"fromVersion"`
	StartedAt        Timestamp       `json:"startedAt"`
	CurrentPartition Integer         `json:"currentPartition"`
	CompletedPods    Ordinals        `json:"completedPods"`
	Failure          ControllerError `json:"failure"`
}

func (v Upgrade) Validate() error {
	if validateObject(v.State, v.ExecutionID, v.TargetVersion, v.FromVersion, v.StartedAt, v.CurrentPartition, v.CompletedPods) != nil ||
		validateChild(v.State, v.Failure.State) != nil || v.Failure.Validate() != nil {
		return ErrInvalidEvidence
	}
	return validateCount(v.CurrentPartition)
}

type BlueGreen struct {
	State           ObjectState `json:"state"`
	Phase           Text        `json:"phase"`
	OperationID     Text        `json:"operationID"`
	BlueRevision    Text        `json:"blueRevision"`
	GreenRevision   Text        `json:"greenRevision"`
	GreenVersion    Text        `json:"greenVersion"`
	StartedAt       Timestamp   `json:"startedAt"`
	JobFailureCount Integer     `json:"jobFailureCount"`
	LastFailedJob   Text        `json:"lastFailedJob"`
}

func (v BlueGreen) Validate() error {
	if validateObject(v.State, v.Phase, v.OperationID, v.BlueRevision, v.GreenRevision,
		v.GreenVersion, v.StartedAt, v.JobFailureCount, v.LastFailedJob) != nil ||
		ValidateWorkloadRevision(v.BlueRevision) != nil || ValidateWorkloadRevision(v.GreenRevision) != nil {
		return ErrInvalidEvidence
	}
	return validateCount(v.JobFailureCount)
}

type OperationLock struct {
	State      ObjectState `json:"state"`
	Operation  Text        `json:"operation"`
	Holder     Text        `json:"holder"`
	AcquiredAt Timestamp   `json:"acquiredAt"`
	RenewedAt  Timestamp   `json:"renewedAt"`
}

func (v OperationLock) Validate() error {
	return validateObject(v.State, v.Operation, v.Holder, v.AcquiredAt, v.RenewedAt)
}

type BreakGlass struct {
	State     ObjectState `json:"state"`
	Active    Boolean     `json:"active"`
	Reason    Text        `json:"reason"`
	EnteredAt Timestamp   `json:"enteredAt"`
}

func (v BreakGlass) Validate() error {
	if v.Active.State == ValueImplied {
		return ErrInvalidEvidence
	}
	return validateObject(v.State, v.Active, v.Reason, v.EnteredAt)
}

// LegacyRestore is historical restart metadata, not proof of ownership or snapshot application.
type LegacyRestore struct {
	State              ObjectState `json:"state"`
	Name               Text        `json:"name"`
	UID                Text        `json:"uid"`
	RestartCompletedAt Timestamp   `json:"restartCompletedAt"`
}

func (v LegacyRestore) Validate() error {
	return validateObject(v.State, v.Name, v.UID, v.RestartCompletedAt)
}

type Maintenance struct {
	State   ObjectState `json:"state"`
	Enabled Boolean     `json:"enabled"`
}

func (v Maintenance) Validate() error {
	if v.Enabled.State == ValueImplied {
		return ErrInvalidEvidence
	}
	return validateObject(v.State, v.Enabled)
}

type ReplicaCounts struct {
	Configuration ObjectState `json:"configuration"`
	State         ObjectState `json:"state"`
	Desired       Integer     `json:"desired"`
	Ready         Integer     `json:"ready"`
}

func (v ReplicaCounts) Validate() error {
	if validateObject(v.Configuration, v.Desired) != nil || validateObject(v.State, v.Ready) != nil {
		return ErrInvalidEvidence
	}
	if validateCount(v.Desired) != nil {
		return ErrInvalidEvidence
	}
	return validateCount(v.Ready)
}

func validateCount(v Integer) error {
	if v.Validate() != nil || v.State == ValueImplied || v.Value != nil && (*v.Value < 0 || *v.Value > 2147483647) {
		return ErrInvalidEvidence
	}
	return nil
}
