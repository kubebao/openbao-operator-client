package diagnose

import (
	"time"

	"github.com/kubebao/openbao-operator-client/evidence"
)

func backupPrerequisites(backup evidence.Backup) evaluation {
	if backup.Configuration == evidence.ObjectAbsent {
		return evaluation{state: CoverageNotApplicable, reason: ReasonNotConfigured}
	}
	if backup.Configuration != evidence.ObjectReported || backup.State != evidence.ObjectReported {
		return evaluation{state: CoverageInsufficient, reason: ReasonMissingField}
	}
	if !knownInteger(backup.ConsecutiveFailures) {
		return evaluation{state: CoverageInsufficient, reason: ReasonMissingField}
	}
	return evaluation{state: CoverageEvaluated, reason: ReasonNone}
}

// inconsistentTime reports an invalid timestamp or one later than the read that observed it.
func inconsistentTime(value evidence.Timestamp, read time.Time) bool {
	if value.State == evidence.ValueInvalid {
		return true
	}
	return value.State == evidence.ValueReported && value.Value.After(read)
}

func optionalTime(result *evaluation, cluster evidence.Cluster, value evidence.Timestamp) {
	if inconsistentTime(value, *cluster.Reference.CollectedAt.Value) {
		result.insufficient(ReasonTime)
	}
}

func backupFailures(cluster evidence.Cluster) evaluation {
	result := backupPrerequisites(cluster.Backup)
	if result.state != CoverageEvaluated || *cluster.Backup.ConsecutiveFailures.Value == 0 {
		return result
	}
	optionalTime(&result, cluster, cluster.Backup.LastFailureAt)
	result.add(finding(cluster, RuleBackupFailed, SeverityWarning, "The operator reports consecutive backup failures.",
		"spec.backup", "status.backup.consecutiveFailures"))
	return result
}

func backupAbsent(cluster evidence.Cluster) evaluation {
	result := backupPrerequisites(cluster.Backup)
	if result.state != CoverageEvaluated {
		return result
	}
	if *cluster.Backup.ConsecutiveFailures.Value > 0 {
		return evaluation{state: CoverageEvaluated, reason: ReasonHigherPriority}
	}
	switch cluster.Backup.LastSuccessAt.State {
	case evidence.ValueUnknown:
		result.add(finding(cluster, RuleBackupAbsent, SeverityWarning,
			"No successful backup completion timestamp is recorded in the observed backup status.",
			"spec.backup", "status.backup", "status.backup.consecutiveFailures", "status.backup.lastBackupTime"))
	case evidence.ValueReported:
		optionalTime(&result, cluster, cluster.Backup.LastSuccessAt)
	case evidence.ValueInvalid, evidence.ValueImplied:
		result.insufficient(ReasonMissingField)
	}
	return result
}
