package adapter

import "github.com/kubebao/openbao-operator-client/evidence"

// ProjectRestore projects reported OpenBaoRestore facts without interpreting request
// age, proving ownership, or inferring submission/application/restart success.
// The selected contracts do not define an OpenBaoRecovery API.
func ProjectRestore(raw []byte, options ProjectionOptions) (evidence.Restore, error) {
	object, ref, err := projectResource(raw, options, "OpenBaoRestore")
	if err != nil {
		return evidence.Restore{}, err
	}
	target, targetState := objectAt(object, "status", "target")
	execution, executionState := objectAt(object, "status", "execution")
	_, claimState := objectAt(object, "status", "submissionClaim")
	restart, restartState := objectAt(object, "status", "restart")
	result := evidence.Restore{
		Reference:  ref,
		Generation: integer(object, "metadata", "generation"),
		CreatedAt:  timestamp(object, "metadata", "creationTimestamp"),
		TargetName: text(
			object,
			true,
			"spec",
			"cluster",
		),
		TargetLifecycle:     enumText(object, []string{"Retain", "Disposable"}, "spec", "targetLifecycle"),
		CleanupAfterSeconds: restoreCleanupSeconds(object),
		Phase: enumText(
			object,
			[]string{"Pending", "Validating", "Running", "Completed", "Failed", "Unknown"},
			"status",
			"phase",
		),
		StartedAt:                timestamp(object, "status", "startTime"),
		CompletedAt:              timestamp(object, "status", "completionTime"),
		AdministratorDisposition: enumText(object, []string{"Resume", "Abandon"}, "status", "administratorDisposition"),
		Target: evidence.RestoreTarget{
			State: targetState, UID: text(target, true, "uid"), AppliedAt: timestamp(target, "appliedAt"),
			Cleanup: enumText(target, []string{"Pending", "Complete", "Failed"}, "cleanup"),
		},
		Execution: evidence.RestoreExecution{
			State:       executionState,
			TargetUID:   text(execution, true, "targetUID"),
			OperationID: text(execution, true, "operationID"),
			Stage: enumText(
				execution,
				[]string{"Prepared", "Committed", "Created", "TerminalObserved", "FollowThroughComplete", "Unknown"},
				"stage",
			),
			TerminalResult: enumText(execution, []string{"Succeeded", "Failed"}, "terminalResult"),
		},
		SubmissionClaim:    claimState,
		Restart:            restartState,
		RestartCompletedAt: timestamp(restart, "completedAt"),
		Conditions:         projectConditions(object, options.Limits.MaxConditions, "status", "conditions"),
	}
	if !inspectedOperatorContract(options.Contract) {
		result.Target.UID = evidence.Text{State: evidence.ValueUnknown}
		result.Execution.TargetUID = evidence.Text{State: evidence.ValueUnknown}
	}
	if result.Validate() != nil {
		return evidence.Restore{}, &ProjectionError{Code: InvalidInput}
	}
	return result, nil
}

func restoreCleanupSeconds(object map[string]any) evidence.Integer {
	result := integer(object, "spec", "cleanupAfterSeconds")
	if result.Value != nil && (*result.Value < 0 || *result.Value > 86400) {
		return evidence.Integer{State: evidence.ValueInvalid}
	}
	return result
}
