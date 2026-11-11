package diagnose

import "github.com/kubebao/openbao-operator-client/evidence"

func backupPrerequisites(backup evidence.Backup) evaluation {
	if backup.Configuration == evidence.ObjectAbsent {
		return evaluation{state: NotApplicable, reason: ReasonNotConfigured}
	}
	if backup.Configuration != evidence.ObjectReported || backup.State != evidence.ObjectReported ||
		backup.ConsecutiveFailures.State != evidence.ValueReported {
		return evaluation{state: Insufficient, reason: ReasonMissingField}
	}
	return evaluation{state: Evaluated, reason: ReasonNone}
}

func optionalTime(result *evaluation, cluster evidence.Cluster, value evidence.Timestamp) {
	if value.State == evidence.ValueInvalid ||
		value.State == evidence.ValueReported && value.Value.After(*cluster.Reference.CollectedAt.Value) {
		result.insufficient(ReasonTime)
	}
}

func backupFailures(cluster evidence.Cluster) evaluation {
	result := backupPrerequisites(cluster.Backup)
	if result.state != Evaluated || *cluster.Backup.ConsecutiveFailures.Value == 0 {
		return result
	}
	optionalTime(&result, cluster, cluster.Backup.LastFailureAt)
	result.add(finding(cluster, RuleBackupFailed, Warning, "The operator reports consecutive backup failures.",
		"spec.backup", "status.backup.consecutiveFailures"))
	return result
}

func backupAbsent(cluster evidence.Cluster) evaluation {
	result := backupPrerequisites(cluster.Backup)
	if result.state != Evaluated {
		return result
	}
	if *cluster.Backup.ConsecutiveFailures.Value > 0 {
		return evaluation{state: Evaluated, reason: ReasonHigherPriority}
	}
	switch cluster.Backup.LastSuccessAt.State {
	case evidence.ValueUnknown:
		result.add(finding(cluster, RuleBackupAbsent, Warning,
			"No successful backup completion timestamp is recorded in the observed backup status.",
			"spec.backup", "status.backup", "status.backup.consecutiveFailures", "status.backup.lastBackupTime"))
	case evidence.ValueReported:
		optionalTime(&result, cluster, cluster.Backup.LastSuccessAt)
	case evidence.ValueInvalid, evidence.ValueImplied:
		result.insufficient(ReasonMissingField)
	}
	return result
}
