package diagnose

import "github.com/kubebao/openbao-operator-client/evidence"

func knownInteger(value evidence.Integer) bool {
	return value.State == evidence.ValueReported || value.State == evidence.ValueImplied
}

func readiness(cluster evidence.Cluster, policy Policy) evaluation {
	if cluster.Acknowledgement() != evidence.GenerationCurrent {
		return evaluation{state: CoverageInsufficient, reason: ReasonGeneration}
	}
	if !knownInteger(cluster.DesiredReplicas) || !knownInteger(cluster.ReadyReplicas) {
		return evaluation{state: CoverageInsufficient, reason: ReasonMissingField}
	}
	if !reportedBoolean(cluster.Paused) || cluster.CreatedAt.State != evidence.ValueReported {
		return evaluation{state: CoverageInsufficient, reason: ReasonMissingField}
	}
	if cluster.CreatedAt.Value.After(*cluster.Reference.CollectedAt.Value) {
		return evaluation{state: CoverageInsufficient, reason: ReasonTime}
	}
	result := evaluation{state: CoverageEvaluated, reason: ReasonNone}
	if *cluster.Paused.Value || policy.ReferenceTime.Sub(*cluster.CreatedAt.Value) < policy.StartupGrace {
		return result
	}
	desired, ready := *cluster.DesiredReplicas.Value, *cluster.ReadyReplicas.Value
	if desired == 0 || ready >= desired {
		return result
	}
	severity := SeverityWarning
	if ready == 0 {
		severity = SeverityCritical
	}
	result.add(finding(cluster, RuleReadiness, severity, "The operator reports fewer ready replicas than desired.",
		"metadata.generation", "status.observedGeneration", "status.readyReplicas", "spec.replicas"))
	return result
}

func backupAge(cluster evidence.Cluster, policy Policy) evaluation {
	if result := backupPrerequisites(cluster.Backup); result.state != CoverageEvaluated {
		return result
	}
	if *cluster.Backup.ConsecutiveFailures.Value > 0 {
		return evaluation{state: CoverageEvaluated, reason: ReasonHigherPriority}
	}
	if cluster.Backup.LastSuccessAt.State != evidence.ValueReported {
		return evaluation{state: CoverageInsufficient, reason: ReasonMissingField}
	}
	if cluster.Backup.LastSuccessAt.Value.After(*cluster.Reference.CollectedAt.Value) {
		return evaluation{state: CoverageInsufficient, reason: ReasonTime}
	}
	result := evaluation{state: CoverageEvaluated, reason: ReasonNone}
	if policy.ReferenceTime.Sub(*cluster.Backup.LastSuccessAt.Value) > policy.MaxBackupAge {
		result.add(finding(cluster, RuleBackupAge, SeverityWarning, "Recorded backup completion age exceeds the configured limit.",
			"spec.backup", "status.backup.consecutiveFailures", "status.backup.lastBackupTime"))
	}
	return result
}
