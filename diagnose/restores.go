package diagnose

import (
	"slices"
	"strings"
	"time"

	"github.com/kubebao/openbao-operator-client/evidence"
)

type associatedRestore struct {
	value       evidence.Restore
	association evidence.Association
	holdMatch   bool
}

func evaluateRestores(input ClusterInput, policy Policy, coverage Coverage) (Coverage, []Finding) {
	coverage.State, coverage.Reason, coverage.Outcome = NotRequested, ReasonNotRequested, evidence.CollectionNotRequested
	if input.Restores == nil {
		return coverage, nil
	}
	read := input.Restores.Collection
	coverage.Outcome = read.Outcome
	if read.Outcome == evidence.CollectionNotRequested {
		return coverage, nil
	}
	coverage.State = Insufficient
	if reason := relatedTarget(input, policy); reason != ReasonNone {
		coverage.Reason = reason
		return coverage, nil
	}
	result := evaluation{state: Evaluated, reason: ReasonNone}
	if read.Outcome != evidence.CollectionComplete || input.Collection.Outcome != evidence.CollectionComplete {
		result.insufficient(ReasonScopeIncomplete)
	}
	if read.Scope.Selector != "" {
		result.insufficient(ReasonScopeRestricted)
	}
	if input.Observation.RestoreHold.State == evidence.ValueInvalid {
		result.insufficient(ReasonMissingField)
	}
	records, matches := restoreAssociations(input, policy, &result)
	boundHold := false
	for _, record := range records {
		if record.association.State != evidence.AssociationConfirmed {
			continue
		}
		held := record.holdMatch && matches == 1
		boundHold = boundHold || held
		restoreConcern(input, record, held, policy, &result)
	}
	if input.Observation.RestoreHold.State == evidence.ValueReported && !boundHold {
		result.insufficient(ReasonAssociation)
	}
	coverage.State, coverage.Reason, coverage.Reasons = result.state, result.reason, result.coverageReasons()
	return coverage, result.findings
}

func restoreAssociations(input ClusterInput, policy Policy, result *evaluation) ([]associatedRestore, int) {
	values := slices.Clone(input.Restores.Observations)
	slices.SortFunc(values, func(a, b evidence.Restore) int {
		return strings.Compare(a.Reference.Identity.UID, b.Reference.Identity.UID)
	})
	records, matches := []associatedRestore{}, 0
	hold := input.Observation.RestoreHold
	for _, request := range values {
		if !fresh(request.Reference, input.Restores.Collection, policy) {
			result.insufficient(ReasonStaleEvidence)
			continue
		}
		association, err := evidence.AssociateRestore(*input.Observation, request)
		if err != nil {
			result.insufficient(ReasonAssociation)
			continue
		}
		match := hold.State == evidence.ValueReported && (*hold.Value == request.Reference.Identity.UID ||
			request.Execution.OperationID.State == evidence.ValueReported && *hold.Value == *request.Execution.OperationID.Value)
		if match {
			matches++
		}
		if association.State != evidence.AssociationConfirmed && association.State != evidence.AssociationForeign {
			result.insufficient(ReasonAssociation)
		}
		records = append(records, associatedRestore{request, association, match})
	}
	return records, matches
}

func restoreConcern(input ClusterInput, record associatedRestore, held bool, policy Policy, result *evaluation) {
	request := record.value
	restoreTimes(request, result)
	phase, stage, cleanup, terminal := request.Phase, request.Execution.Stage, request.Target.Cleanup, request.Execution.TerminalResult
	phaseKnown := phase.State == evidence.ValueReported &&
		slices.Contains([]string{"Pending", "Validating", "Running", "Completed", "Failed", "Unknown"}, *phase.Value)
	stageKnown := stage.State == evidence.ValueReported && slices.Contains([]string{
		"Prepared", "Committed", "Created", "TerminalObserved", "FollowThroughComplete", "Unknown",
	}, *stage.Value)
	if !phaseKnown || stage.State == evidence.ValueInvalid || cleanup.State == evidence.ValueInvalid ||
		terminal.State == evidence.ValueInvalid || request.TargetLifecycle.State == evidence.ValueInvalid ||
		request.CleanupAfterSeconds.State == evidence.ValueInvalid ||
		request.Execution.State == evidence.ObjectReported && !stageKnown ||
		terminal.State == evidence.ValueReported && !slices.Contains([]string{"Succeeded", "Failed"}, *terminal.Value) ||
		cleanup.State == evidence.ValueReported && !slices.Contains([]string{"Pending", "Complete", "Failed"}, *cleanup.Value) {
		result.insufficient(ReasonMissingField)
	}
	unsafe := phaseKnown && (*phase.Value == "Unknown" || *phase.Value == "Failed") ||
		stageKnown && *stage.Value == "Unknown" || terminal.State == evidence.ValueReported && *terminal.Value == "Failed"
	cleanupFailed := cleanup.State == evidence.ValueReported && *cleanup.Value == "Failed"
	var found *Finding
	switch {
	case held && phaseKnown && *phase.Value == "Unknown" && stageKnown && *stage.Value == "Unknown" &&
		disposableRetention(input, record, policy):
		found = restoreFinding(
			input,
			record,
			Info,
			"The operator reports snapshot application on the UID-confirmed disposable target; "+
				"cleanup is pending within the configured retention period.",
		)
	case held && (unsafe || cleanupFailed):
		found = restoreFinding(input, record, Error, "A UID-confirmed restore matching the reported hold requires recovery inspection.")
	case policy.ReportHistoricalRestores && (unsafe || cleanupFailed):
		found = restoreFinding(input, record, Warning,
			"The operator records a restore outcome requiring investigation; this does not establish an active failure.")
	case phaseKnown && slices.Contains([]string{"Pending", "Validating", "Running"}, *phase.Value):
		found = restoreFinding(input, record, Info, "The UID-confirmed restore reports an in-progress phase; completion is not verified.")
	}
	if found != nil {
		result.add(found)
	}
}

func disposableRetention(input ClusterInput, record associatedRestore, policy Policy) bool {
	request := record.value
	read := input.Restores.Collection
	if read.Outcome != evidence.CollectionComplete || read.Scope.Selector != "" || input.Collection.Outcome != evidence.CollectionComplete {
		return false
	}
	if request.TargetLifecycle.State != evidence.ValueReported || *request.TargetLifecycle.Value != "Disposable" ||
		request.CleanupAfterSeconds.State != evidence.ValueReported || *request.CleanupAfterSeconds.Value <= 0 ||
		request.Target.State != evidence.ObjectReported || request.Target.UID.State != evidence.ValueReported ||
		*request.Target.UID.Value != input.Subject.UID || request.Target.AppliedAt.State != evidence.ValueReported ||
		request.Execution.TerminalResult.State != evidence.ValueReported ||
		*request.Execution.TerminalResult.Value != "Succeeded" ||
		request.AdministratorDisposition.State != evidence.ValueUnknown ||
		request.Restart != evidence.ObjectAbsent && request.Restart != evidence.ObjectNull {
		return false
	}
	cleanup := request.Target.Cleanup
	if cleanup.State == evidence.ValueInvalid || cleanup.State == evidence.ValueReported && *cleanup.Value != "Pending" {
		return false
	}
	if !validRetentionTimes(request) {
		return false
	}
	deadline := request.Target.AppliedAt.Value.Add(time.Duration(*request.CleanupAfterSeconds.Value) * time.Second)
	return policy.ReferenceTime.Before(deadline)
}

func validRetentionTimes(request evidence.Restore) bool {
	read := *request.Reference.CollectedAt.Value
	for _, value := range []evidence.Timestamp{
		request.CreatedAt, request.StartedAt, request.CompletedAt, request.Target.AppliedAt, request.RestartCompletedAt,
	} {
		if value.State == evidence.ValueInvalid || value.State == evidence.ValueReported && value.Value.After(read) {
			return false
		}
	}
	if request.StartedAt.State == evidence.ValueReported && request.CompletedAt.State == evidence.ValueReported &&
		request.CompletedAt.Value.Before(*request.StartedAt.Value) {
		return false
	}
	for _, before := range []evidence.Timestamp{request.CreatedAt, request.StartedAt} {
		if before.State == evidence.ValueReported && request.Target.AppliedAt.Value.Before(*before.Value) {
			return false
		}
	}
	return true
}

func restoreTimes(request evidence.Restore, result *evaluation) {
	for _, value := range []evidence.Timestamp{
		request.CreatedAt, request.StartedAt, request.CompletedAt, request.Target.AppliedAt, request.RestartCompletedAt,
	} {
		if value.State == evidence.ValueInvalid ||
			value.State == evidence.ValueReported && value.Value.After(*request.Reference.CollectedAt.Value) {
			result.insufficient(ReasonTime)
		}
	}
	if request.StartedAt.State == evidence.ValueReported && request.CompletedAt.State == evidence.ValueReported &&
		request.CompletedAt.Value.Before(*request.StartedAt.Value) {
		result.insufficient(ReasonTime)
	}
}

func restoreFinding(input ClusterInput, record associatedRestore, severity Severity, summary string) *Finding {
	found := resourceFinding(
		record.value.Reference,
		RuleRestore,
		"",
		severity,
		summary,
		"status.phase",
		"status.execution.stage",
		"status.execution.operationID",
		"status.execution.terminalResult",
		"status.target.cleanup",
	)
	if severity == Info && record.value.Phase.State == evidence.ValueReported && *record.value.Phase.Value == "Unknown" {
		for _, field := range []string{"spec.targetLifecycle", "spec.cleanupAfterSeconds", "status.target.appliedAt"} {
			ref := record.value.Reference
			ref.Field = field
			found.References = append(found.References, ref)
		}
	}
	root := input.Observation.Reference
	root.Field = "metadata.uid"
	found.References = append(found.References, root)
	root.Field = "metadata.annotations[openbao.org/restore-hold]"
	found.References = append(found.References, root)
	ref := record.value.Reference
	if record.association.Basis == evidence.BasisRestoreOrigin {
		ref = root
	}
	ref.Field = string(record.association.Basis)
	found.References = append(found.References, ref)
	return found
}
