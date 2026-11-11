package diagnose

import "github.com/kubebao/openbao-operator-client/evidence"

func upgradeFailure(cluster evidence.Cluster) evaluation {
	result := evaluation{state: Evaluated, reason: ReasonNone}
	switch cluster.Upgrade.State {
	case evidence.ObjectAbsent, evidence.ObjectNull:
		return result
	case evidence.ObjectUnknown, evidence.ObjectInvalid:
		return evaluation{state: Insufficient, reason: ReasonMissingField}
	case evidence.ObjectReported:
	}
	switch cluster.Upgrade.Failure.State {
	case evidence.ObjectAbsent, evidence.ObjectNull:
		return result
	case evidence.ObjectUnknown, evidence.ObjectInvalid:
		return evaluation{state: Insufficient, reason: ReasonMissingField}
	case evidence.ObjectReported:
	}
	if cluster.Upgrade.Failure.Reason.State != evidence.ValueReported {
		return evaluation{state: Insufficient, reason: ReasonMissingField}
	}
	optionalTime(&result, cluster, cluster.Upgrade.Failure.At)
	result.add(finding(cluster, RuleUpgrade, Warning,
		"The operator records a rolling-upgrade failure; this does not establish an active failure.", "status.upgrade.failure"))
	return result
}

func restoreTest(cluster evidence.Cluster) evaluation {
	result := evaluation{state: Evaluated, reason: ReasonNone}
	test := cluster.Backup.RestoreTest
	switch test.State {
	case evidence.ObjectAbsent, evidence.ObjectNull:
		return result
	case evidence.ObjectUnknown, evidence.ObjectInvalid:
		return evaluation{state: Insufficient, reason: ReasonMissingField}
	case evidence.ObjectReported:
	}
	if restoreTestBlocked(&result, cluster, test.Conditions) {
		return result
	}
	// An active reservation suppresses the previous result, as in the console
	// baseline. Presence alone does not verify the child request or its progress.
	switch test.Active {
	case evidence.ObjectReported:
		return result
	case evidence.ObjectUnknown, evidence.ObjectInvalid:
		result.insufficient(ReasonMissingField)
		return result
	case evidence.ObjectAbsent, evidence.ObjectNull:
	}
	switch test.Last {
	case evidence.ObjectAbsent, evidence.ObjectNull:
		return result
	case evidence.ObjectUnknown, evidence.ObjectInvalid:
		result.insufficient(ReasonMissingField)
		return result
	case evidence.ObjectReported:
	}
	if test.LastOutcome.State != evidence.ValueReported ||
		(*test.LastOutcome.Value != "Passed" && *test.LastOutcome.Value != "Failed") {
		result.insufficient(ReasonMissingField)
		return result
	}
	if test.FinishedAt.State != evidence.ValueReported {
		result.insufficient(ReasonMissingField)
	}
	optionalTime(&result, cluster, test.FinishedAt)
	if *test.LastOutcome.Value == "Failed" {
		result.add(finding(cluster, RuleRestoreTest, Warning,
			"The operator records a failed restore test; the result covers its snapshot and disposable target only.",
			"status.backup.restoreTest.active", "status.backup.restoreTest.last.outcome", "status.backup.restoreTest.last.finishedAt"))
	}
	return result
}

func restoreTestBlocked(result *evaluation, cluster evidence.Cluster, conditions evidence.Conditions) bool {
	if conditions.State != evidence.ValueReported {
		result.insufficient(ReasonMissingField)
		return false
	}
	for _, condition := range conditions.Values {
		if condition.Type != "Passed" {
			continue
		}
		if condition.Acknowledgement(cluster.Generation) != evidence.GenerationCurrent {
			result.insufficient(ReasonGeneration)
			return false
		}
		conditionTime(result, cluster, condition)
		if condition.Status == evidence.ConditionUnknown ||
			condition.Status == evidence.ConditionFalse && condition.Reason.State != evidence.ValueReported {
			result.insufficient(ReasonMissingField)
			return false
		}
		if condition.Status == evidence.ConditionFalse && *condition.Reason.Value != "Failed" {
			result.add(finding(cluster, RuleRestoreTest, Warning,
				"The operator reports a restore-test condition requiring attention for the current generation.",
				"metadata.generation", "status.backup.restoreTest.conditions[type=Passed]"))
			return true
		}
		return false
	}
	result.insufficient(ReasonMissingField)
	return false
}
