package evidence

import "math"

// ObjectState records JSON object presence independently of its child values.
// Unknown means an ancestor was not observed. Absent means a valid parent omitted
// this field. Null preserves an explicit null; it does not establish health.
type ObjectState string

const (
	// ObjectUnknown means an ancestor object was not observed.
	ObjectUnknown ObjectState = "unknown"
	// ObjectAbsent means a valid parent omitted the object.
	ObjectAbsent ObjectState = "absent"
	// ObjectNull means the producer reported an explicit null.
	ObjectNull ObjectState = "null"
	// ObjectReported means the producer reported the object.
	ObjectReported ObjectState = "reported"
	// ObjectInvalid means the object was present but was not a JSON object.
	ObjectInvalid ObjectState = "invalid"
)

// Valid reports whether s is a defined object state.
func (s ObjectState) Valid() bool {
	switch s {
	case ObjectUnknown, ObjectAbsent, ObjectNull, ObjectReported, ObjectInvalid:
		return true
	default:
		return false
	}
}

// Ordinals is a sorted, unique set of nonnegative int32 workload ordinals.
// Missing and invalid arrays retain no values. Explicit empty arrays remain reported.
type Ordinals struct {
	State  ValueState `json:"state"`
	Values []int64    `json:"values"`
}

// Validate checks the set's presence contract and that reported ordinals are sorted, unique, and in range.
func (v Ordinals) Validate() error {
	reported, err := validateSet(v.State, v.Values)
	if !reported {
		return err
	}
	previous := int64(-1)
	for i, value := range v.Values {
		if value < 0 || value > math.MaxInt32 || value <= previous {
			return At(itemPath("values", i), ErrInvalidEvidence)
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

// Validate checks the object state and its fields.
func (v ControllerError) Validate() error {
	return validateObject("state", v.State,
		objectField{"reason", v.Reason},
		objectField{"at", v.At},
	)
}

// RestoreTest records the reported restore-test configuration, the active test,
// and the last completed test.
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

// Validate checks the nested object states and their fields.
func (v RestoreTest) Validate() error {
	if !v.Active.Valid() {
		return At("active", ErrInvalidEvidence)
	}
	if err := validateChild(v.State, "active", v.Active); err != nil {
		return err
	}
	if err := validateChild(v.State, "last", v.Last); err != nil {
		return err
	}
	if err := validateObject("state", v.State,
		objectField{"lastSuccessAt", v.LastSuccessAt},
		objectField{"conditions", v.Conditions},
	); err != nil {
		return err
	}
	return validateObject("last", v.Last,
		objectField{"lastOutcome", v.LastOutcome},
		objectField{"lastReason", v.LastReason},
		objectField{"finishedAt", v.FinishedAt},
		objectField{"snapshotKey", v.SnapshotKey},
		objectField{"snapshotDigest", v.SnapshotDigest},
	)
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

// Validate checks the object states, their fields, and the counter ranges.
// A zero failure count can be implied only from a reported success at or after
// the last reported attempt.
func (v Backup) Validate() error {
	if !v.Configuration.Valid() {
		return At("configuration", ErrInvalidEvidence)
	}
	if err := validateObject("state", v.State,
		objectField{"lastSuccessAt", v.LastSuccessAt},
		objectField{"lastAttemptAt", v.LastAttemptAt},
		objectField{"nextScheduledAt", v.NextScheduledAt},
		objectField{"consecutiveFailures", v.ConsecutiveFailures},
		objectField{"lastFailureReason", v.LastFailureReason},
		objectField{"lastFailureAt", v.LastFailureAt},
		objectField{"sizeBytes", v.SizeBytes},
		objectField{"snapshotKey", v.SnapshotKey},
		objectField{"snapshotDigest", v.SnapshotDigest},
	); err != nil {
		return err
	}
	if err := validateChild(v.State, "restoreTest.state", v.RestoreTest.State); err != nil {
		return err
	}
	if err := v.RestoreTest.Validate(); err != nil {
		return At("restoreTest", err)
	}
	if v.ConsecutiveFailures.State == ValueImplied && !v.successCoversLastAttempt() {
		return At("consecutiveFailures", ErrInvalidEvidence)
	}
	if v.ConsecutiveFailures.Value != nil &&
		(*v.ConsecutiveFailures.Value < 0 || *v.ConsecutiveFailures.Value > math.MaxInt32) {
		return At("consecutiveFailures", ErrInvalidEvidence)
	}
	if v.SizeBytes.State == ValueImplied || v.SizeBytes.Value != nil && *v.SizeBytes.Value < 0 {
		return At("sizeBytes", ErrInvalidEvidence)
	}
	return nil
}

func (v Backup) successCoversLastAttempt() bool {
	return v.LastSuccessAt.State == ValueReported &&
		v.LastAttemptAt.State == ValueReported &&
		!v.LastSuccessAt.Value.Before(*v.LastAttemptAt.Value)
}

// Upgrade records the reported rolling upgrade status.
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

// Validate checks the object state, its fields, and the failure signal.
func (v Upgrade) Validate() error {
	if err := validateObject("state", v.State,
		objectField{"executionID", v.ExecutionID},
		objectField{"targetVersion", v.TargetVersion},
		objectField{"fromVersion", v.FromVersion},
		objectField{"startedAt", v.StartedAt},
		objectField{"currentPartition", v.CurrentPartition},
		objectField{"completedPods", v.CompletedPods},
	); err != nil {
		return err
	}
	if err := validateChild(v.State, "failure.state", v.Failure.State); err != nil {
		return err
	}
	return checkFields(
		fieldCheck{"failure", v.Failure.Validate()},
		fieldCheck{"currentPartition", validateCount(v.CurrentPartition)},
	)
}

// BlueGreen records the reported blue-green upgrade status.
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

// Validate checks the object state, its fields, and the revision labels.
func (v BlueGreen) Validate() error {
	if err := validateObject("state", v.State,
		objectField{"phase", v.Phase},
		objectField{"operationID", v.OperationID},
		objectField{"blueRevision", v.BlueRevision},
		objectField{"greenRevision", v.GreenRevision},
		objectField{"greenVersion", v.GreenVersion},
		objectField{"startedAt", v.StartedAt},
		objectField{"jobFailureCount", v.JobFailureCount},
		objectField{"lastFailedJob", v.LastFailedJob},
	); err != nil {
		return err
	}
	return checkFields(
		fieldCheck{"blueRevision", ValidateWorkloadRevision(v.BlueRevision)},
		fieldCheck{"greenRevision", ValidateWorkloadRevision(v.GreenRevision)},
		fieldCheck{"jobFailureCount", validateCount(v.JobFailureCount)},
	)
}

// OperationLock records the reported holder of the cluster operation lock.
type OperationLock struct {
	State      ObjectState `json:"state"`
	Operation  Text        `json:"operation"`
	Holder     Text        `json:"holder"`
	AcquiredAt Timestamp   `json:"acquiredAt"`
	RenewedAt  Timestamp   `json:"renewedAt"`
}

// Validate checks the object state and its fields.
func (v OperationLock) Validate() error {
	return validateObject("state", v.State,
		objectField{"operation", v.Operation},
		objectField{"holder", v.Holder},
		objectField{"acquiredAt", v.AcquiredAt},
		objectField{"renewedAt", v.RenewedAt},
	)
}

// BreakGlass records the reported break-glass status.
type BreakGlass struct {
	State     ObjectState `json:"state"`
	Active    Boolean     `json:"active"`
	Reason    Text        `json:"reason"`
	EnteredAt Timestamp   `json:"enteredAt"`
}

// Validate checks the object state and its fields. Active is never implied.
func (v BreakGlass) Validate() error {
	if v.Active.State == ValueImplied {
		return At("active", ErrInvalidEvidence)
	}
	return validateObject("state", v.State,
		objectField{"active", v.Active},
		objectField{"reason", v.Reason},
		objectField{"enteredAt", v.EnteredAt},
	)
}

// LegacyRestore is historical restart metadata, not proof of ownership or snapshot application.
type LegacyRestore struct {
	State              ObjectState `json:"state"`
	Name               Text        `json:"name"`
	UID                Text        `json:"uid"`
	RestartCompletedAt Timestamp   `json:"restartCompletedAt"`
}

// Validate checks the object state and its fields.
func (v LegacyRestore) Validate() error {
	return validateObject("state", v.State,
		objectField{"name", v.Name},
		objectField{"uid", v.UID},
		objectField{"restartCompletedAt", v.RestartCompletedAt},
	)
}

// Maintenance records the requested maintenance mode.
type Maintenance struct {
	State   ObjectState `json:"state"`
	Enabled Boolean     `json:"enabled"`
}

// Validate checks the object state and its field. Enabled is never implied.
func (v Maintenance) Validate() error {
	if v.Enabled.State == ValueImplied {
		return At("enabled", ErrInvalidEvidence)
	}
	return validateObject("state", v.State, objectField{"enabled", v.Enabled})
}

// ReplicaCounts records the configured and reported counts of a replica pool.
type ReplicaCounts struct {
	Configuration ObjectState `json:"configuration"`
	State         ObjectState `json:"state"`
	Desired       Integer     `json:"desired"`
	Ready         Integer     `json:"ready"`
}

// Validate checks the object states and that both counts are in range.
func (v ReplicaCounts) Validate() error {
	if err := validateObject("configuration", v.Configuration, objectField{"desired", v.Desired}); err != nil {
		return err
	}
	if err := validateObject("state", v.State, objectField{"ready", v.Ready}); err != nil {
		return err
	}
	return checkFields(
		fieldCheck{"desired", validateCount(v.Desired)},
		fieldCheck{"ready", validateCount(v.Ready)},
	)
}

// Runtime records the optional runtime request object without applying defaults.
type Runtime struct {
	State     ObjectState `json:"state"`
	RestartAt Text        `json:"restartAt"`
}

// Validate checks the object state and its field.
func (v Runtime) Validate() error {
	return validateObject("state", v.State, objectField{"restartAt", v.RestartAt})
}
