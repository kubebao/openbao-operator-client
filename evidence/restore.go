package evidence

// Restore contains reported request and execution facts. It never authorizes replay
// or equates submission, target application, and managed restart completion.
type Restore struct {
	SchemaVersion            string           `json:"schemaVersion"`
	AdapterContract          string           `json:"adapterContract"`
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

type RestoreTarget struct {
	State     ObjectState `json:"state"`
	UID       Text        `json:"uid"`
	AppliedAt Timestamp   `json:"appliedAt"`
	Cleanup   Text        `json:"cleanup"`
}

func (v RestoreTarget) Validate() error {
	return validateObject(v.State, v.UID, v.AppliedAt, v.Cleanup)
}

type RestoreExecution struct {
	State          ObjectState `json:"state"`
	TargetUID      Text        `json:"targetUID"`
	OperationID    Text        `json:"operationID"`
	Stage          Text        `json:"stage"`
	TerminalResult Text        `json:"terminalResult"`
}

func (v RestoreExecution) Validate() error {
	return validateObject(v.State, v.TargetUID, v.OperationID, v.Stage, v.TerminalResult)
}

func (v Restore) Validate() error {
	if v.SchemaVersion != SchemaVersion || !validText(v.AdapterContract, 253, false) || v.Reference.Validate() != nil ||
		v.Reference.Identity.Kind != "OpenBaoRestore" || !v.SubmissionClaim.Valid() ||
		v.Target.Validate() != nil || v.Execution.Validate() != nil ||
		v.Generation.State == ValueImplied || v.CleanupAfterSeconds.State == ValueImplied || v.Conditions.Validate() != nil {
		return ErrInvalidEvidence
	}
	if validateObject(v.Restart, v.RestartCompletedAt) != nil {
		return ErrInvalidEvidence
	}
	if v.CleanupAfterSeconds.Value != nil && (*v.CleanupAfterSeconds.Value < 0 || *v.CleanupAfterSeconds.Value > 86400) {
		return ErrInvalidEvidence
	}
	fields := []observedField{
		v.Generation, v.CreatedAt, v.TargetName, v.TargetLifecycle, v.CleanupAfterSeconds, v.Phase,
		v.StartedAt, v.CompletedAt, v.AdministratorDisposition,
	}
	for _, field := range fields {
		if field.Validate() != nil {
			return ErrInvalidEvidence
		}
	}
	return nil
}
