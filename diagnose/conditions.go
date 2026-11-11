package diagnose

import (
	"slices"

	"github.com/kubebao/openbao-operator-client/evidence"
)

func reportedCondition(cluster evidence.Cluster, name string) *evidence.Condition {
	if cluster.Conditions.State != evidence.ValueReported {
		return nil
	}
	for _, condition := range cluster.Conditions.Values {
		if condition.Type == name {
			return &condition
		}
	}
	return nil
}

func conditionField(name string) string { return "status.conditions[type=" + name + "]" }

func conditionTime(result *evaluation, cluster evidence.Cluster, condition evidence.Condition) {
	if condition.LastTransitionAt.State == evidence.ValueInvalid ||
		condition.LastTransitionAt.State == evidence.ValueReported &&
			condition.LastTransitionAt.Value.After(*cluster.Reference.CollectedAt.Value) {
		result.insufficient(ReasonTime)
	}
}

func availability(cluster evidence.Cluster) evaluation {
	condition := reportedCondition(cluster, "Available")
	if condition == nil {
		return evaluation{state: Insufficient, reason: ReasonMissingField}
	}
	if cluster.Acknowledgement() != evidence.GenerationCurrent ||
		condition.Acknowledgement(cluster.Generation) != evidence.GenerationCurrent {
		return evaluation{state: Insufficient, reason: ReasonGeneration}
	}
	if condition.Status == evidence.ConditionUnknown {
		return evaluation{state: Insufficient, reason: ReasonMissingField}
	}
	result := evaluation{state: Evaluated, reason: ReasonNone}
	conditionTime(&result, cluster, *condition)
	if condition.Status == evidence.ConditionFalse {
		result.add(finding(cluster, RuleAvailability, Warning, "The operator reports Available=False for the current generation.",
			"metadata.generation", "status.observedGeneration", conditionField("Available")))
	}
	return result
}

func sealed(cluster evidence.Cluster) evaluation {
	if cluster.Paused.State != evidence.ValueReported && cluster.Paused.State != evidence.ValueImplied {
		return evaluation{state: Insufficient, reason: ReasonMissingField}
	}
	if *cluster.Paused.Value {
		return evaluation{state: Evaluated, reason: ReasonNone}
	}
	condition := reportedCondition(cluster, "OpenBaoSealed")
	if condition == nil {
		return evaluation{state: Insufficient, reason: ReasonMissingField}
	}
	if condition.Acknowledgement(cluster.Generation) != evidence.GenerationCurrent {
		return evaluation{state: Insufficient, reason: ReasonGeneration}
	}
	if condition.Status == evidence.ConditionUnknown {
		return evaluation{state: Insufficient, reason: ReasonMissingField}
	}
	result := evaluation{state: Evaluated, reason: ReasonNone}
	conditionTime(&result, cluster, *condition)
	if condition.Status == evidence.ConditionTrue {
		result.add(finding(cluster, RuleSealed, Critical, "The operator reports OpenBaoSealed=True for the current generation.",
			"metadata.generation", conditionField("OpenBaoSealed")))
	}
	return result
}

func conditionPolarity(name string) (evidence.ConditionStatus, bool) {
	if slices.Contains([]string{"Degraded", "SecurityRisk", "EtcdEncryptionWarning", "NodeSecurityCapabilityMismatch"}, name) {
		return evidence.ConditionTrue, true
	}
	if slices.Contains([]string{
		"TLSReady", "ACMEIntegrationReady", "ACMECacheReady", "GatewayIntegrationReady", "IngressIntegrationReady",
		"APIServerNetworkReady", "AuditFileStorageReady", "ProductionReady", "UserAccessBootstrap", "CloudUnsealIdentityReady",
		"BackupConfigurationReady", "StorageConfigured", "ReadReplicaStorageConfigured", "ReadReplicasReady", "ReadServingAvailable",
		"RaftMembershipReady", "ReadReplicasAutopilotHealthy", "OpenBaoInitialized", "OpenBaoLeader", "PolicyReconciliationReady",
	}, name) {
		return evidence.ConditionFalse, true
	}
	return "", false
}

func conditions(cluster evidence.Cluster) evaluation {
	if cluster.Conditions.State != evidence.ValueReported {
		return evaluation{state: Insufficient, reason: ReasonMissingField}
	}
	result := evaluation{state: Evaluated, reason: ReasonNone}
	for _, condition := range cluster.Conditions.Values {
		adverse, recognized := conditionPolarity(condition.Type)
		if !recognized {
			continue
		}
		if condition.Acknowledgement(cluster.Generation) != evidence.GenerationCurrent {
			result.insufficient(ReasonGeneration)
			continue
		}
		if condition.Status == evidence.ConditionUnknown {
			result.insufficient(ReasonMissingField)
			continue
		}
		conditionTime(&result, cluster, condition)
		if condition.Status == adverse {
			value := finding(
				cluster,
				RuleCondition,
				Warning,
				"The operator reports a recognized adverse condition for the current generation.",
				"metadata.generation",
				conditionField(condition.Type),
			)
			value.Subsubject = condition.Type
			result.add(value)
		}
	}
	return result
}

func generation(cluster evidence.Cluster) evaluation {
	result := evaluation{state: Evaluated, reason: ReasonNone}
	switch cluster.Acknowledgement() {
	case evidence.GenerationUnknown:
		result.insufficient(ReasonGeneration)
	case evidence.GenerationStale:
		result.add(finding(cluster, RuleGeneration, Warning, "Operator status acknowledges an earlier resource generation.",
			"metadata.generation", "status.observedGeneration"))
	case evidence.GenerationInconsistent:
		result.add(finding(cluster, RuleGeneration, Warning, "Resource generation acknowledgement is inconsistent.",
			"metadata.generation", "status.observedGeneration"))
	case evidence.GenerationCurrent:
	}
	return result
}
