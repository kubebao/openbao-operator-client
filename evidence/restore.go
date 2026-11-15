package evidence

// maxCleanupAfterSeconds bounds a restore's requested cleanup delay to one day.
const maxCleanupAfterSeconds = 24 * 60 * 60

// Restore contains reported request and execution facts. It never authorizes replay
// or equates submission, target application, and managed restart completion.
type Restore struct {
	Reference                Reference        `json:"reference"`
	Generation               Integer          `json:"generation"`
	CreatedAt                Timestamp        `json:"createdAt"`
	TargetName               Text             `json:"targetName"`
	TargetLifecycle          Text             `json:"targetLifecycle"`
	CleanupAfterSeconds      Integer          `json:"cleanupAfterSeconds"`
	Phase                    Text             `json:"phase"`
	StartedAt                Timestamp        `json:"startedAt"`
	CompletedAt              Timestamp        `json:"completedAt"`
	AdministratorDisposition Text             `json:"administratorDisposition"`
	Target                   RestoreTarget    `json:"target"`
	Execution                RestoreExecution `json:"execution"`
	SubmissionClaim          ObjectState      `json:"submissionClaim"`
	Restart                  ObjectState      `json:"restart"`
	RestartCompletedAt       Timestamp        `json:"restartCompletedAt"`
	Conditions               Conditions       `json:"conditions"`
}

// RestoreTarget records the reported target binding of a restore.
type RestoreTarget struct {
	State     ObjectState `json:"state"`
	UID       Text        `json:"uid"`
	AppliedAt Timestamp   `json:"appliedAt"`
	Cleanup   Text        `json:"cleanup"`
}

// Validate checks the object state and its fields.
func (v RestoreTarget) Validate() error {
	return validateObject("state", v.State,
		objectField{"uid", v.UID},
		objectField{"appliedAt", v.AppliedAt},
		objectField{"cleanup", v.Cleanup},
	)
}

// RestoreExecution records the reported execution of a restore.
type RestoreExecution struct {
	State          ObjectState `json:"state"`
	TargetUID      Text        `json:"targetUID"`
	OperationID    Text        `json:"operationID"`
	Stage          Text        `json:"stage"`
	TerminalResult Text        `json:"terminalResult"`
}

// Validate checks the object state and its fields.
func (v RestoreExecution) Validate() error {
	return validateObject("state", v.State,
		objectField{"targetUID", v.TargetUID},
		objectField{"operationID", v.OperationID},
		objectField{"stage", v.Stage},
		objectField{"terminalResult", v.TerminalResult},
	)
}

// Validate checks the reference, nested objects, and fields. Generation and
// cleanup delay are never implied, and the cleanup delay is at most one day.
func (v Restore) Validate() error {
	if err := checkFields(
		fieldCheck{"reference", v.Reference.Validate()},
		fieldCheck{"generation", v.Generation.Validate()},
		fieldCheck{"createdAt", v.CreatedAt.Validate()},
		fieldCheck{"targetName", v.TargetName.Validate()},
		fieldCheck{"targetLifecycle", v.TargetLifecycle.Validate()},
		fieldCheck{"cleanupAfterSeconds", v.CleanupAfterSeconds.Validate()},
		fieldCheck{"phase", v.Phase.Validate()},
		fieldCheck{"startedAt", v.StartedAt.Validate()},
		fieldCheck{"completedAt", v.CompletedAt.Validate()},
		fieldCheck{"administratorDisposition", v.AdministratorDisposition.Validate()},
		fieldCheck{"target", v.Target.Validate()},
		fieldCheck{"execution", v.Execution.Validate()},
		fieldCheck{"conditions", v.Conditions.Validate()},
	); err != nil {
		return err
	}
	if v.Reference.Identity.Kind != "OpenBaoRestore" {
		return At("reference.identity.kind", ErrInvalidEvidence)
	}
	if !v.SubmissionClaim.Valid() {
		return At("submissionClaim", ErrInvalidEvidence)
	}
	if err := validateObject("restart", v.Restart, objectField{"restartCompletedAt", v.RestartCompletedAt}); err != nil {
		return err
	}
	if v.Generation.State == ValueImplied {
		return At("generation", ErrInvalidEvidence)
	}
	cleanup := v.CleanupAfterSeconds
	if cleanup.State == ValueImplied || cleanup.Value != nil && (*cleanup.Value < 0 || *cleanup.Value > maxCleanupAfterSeconds) {
		return At("cleanupAfterSeconds", ErrInvalidEvidence)
	}
	return nil
}
