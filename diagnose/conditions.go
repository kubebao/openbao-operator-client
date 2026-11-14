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
	if slices.Contains([]string{"Degraded", "SecurityRisk", etcdEncryptionWarning, "NodeSecurityCapabilityMismatch"}, name) {
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

const (
	etcdEncryptionWarning    = "EtcdEncryptionWarning"
	noReadReplicasConfigured = "NoReadReplicasConfigured"
)

// readReplicaConditions describe the optional read-replica pool. They apply only when the pool is configured.
var readReplicaConditions = []string{
	"ReadReplicaStorageConfigured", "ReadReplicasReady", "ReadServingAvailable", "ReadReplicasAutopilotHealthy",
}

// readReplicasConfigured reports whether the spec requests read replicas. The second result is false when the spec does not decide it.
func readReplicasConfigured(cluster evidence.Cluster) (bool, bool) {
	pool := cluster.ReadReplicas
	switch {
	case pool.Configuration == evidence.ObjectAbsent || pool.Configuration == evidence.ObjectNull:
		return false, true
	case pool.Configuration == evidence.ObjectReported && pool.Desired.Value != nil &&
		(pool.Desired.State == evidence.ValueReported || pool.Desired.State == evidence.ValueImplied):
		return *pool.Desired.Value > 0, true
	default:
		return false, false
	}
}

// conditionApplies excludes read-replica conditions for a cluster without read replicas. The spec decides; when it is
// unknown, the operator's structured NoReadReplicasConfigured reason does. A spec that requests replicas always applies.
func conditionApplies(cluster evidence.Cluster, condition evidence.Condition) bool {
	if !slices.Contains(readReplicaConditions, condition.Type) {
		return true
	}
	if configured, known := readReplicasConfigured(cluster); known {
		return configured
	}
	return condition.Reason.State != evidence.ValueReported || *condition.Reason.Value != noReadReplicasConfigured
}

// conditionFinding names the recognized condition type and status. Reasons and messages stay out of findings.
func conditionFinding(cluster evidence.Cluster, condition evidence.Condition) *Finding {
	severity := Warning
	summary := "The operator reports " + condition.Type + "=" + string(condition.Status) + " for the current generation."
	if condition.Type == etcdEncryptionWarning {
		// The operator sets this advisory on every cluster because it cannot inspect the API server configuration.
		severity = Info
		summary = "The operator cannot verify that the Kubernetes API server encrypts Secrets at rest in etcd."
	}
	value := finding(cluster, RuleCondition, severity, summary, "metadata.generation", conditionField(condition.Type))
	value.Subsubject = condition.Type
	return value
}

func conditions(cluster evidence.Cluster) evaluation {
	if cluster.Conditions.State != evidence.ValueReported {
		return evaluation{state: Insufficient, reason: ReasonMissingField}
	}
	result := evaluation{state: Evaluated, reason: ReasonNone}
	for _, condition := range cluster.Conditions.Values {
		adverse, recognized := conditionPolarity(condition.Type)
		if !recognized || !conditionApplies(cluster, condition) {
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
			result.add(conditionFinding(cluster, condition))
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
