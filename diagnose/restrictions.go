package diagnose

import "github.com/kubebao/openbao-operator-client/evidence"

func reportedBoolean(value evidence.Boolean) bool {
	return value.State == evidence.ValueReported || value.State == evidence.ValueImplied
}

func restriction(result *evaluation, cluster evidence.Cluster, value evidence.Boolean, field, summary string, severity Severity) {
	if !reportedBoolean(value) {
		result.insufficient(ReasonMissingField)
		return
	}
	if *value.Value {
		found := finding(cluster, RuleRestriction, severity, summary, field)
		found.Subsubject = field
		result.add(found)
	}
}

func restrictions(cluster evidence.Cluster) evaluation {
	result := evaluation{state: CoverageEvaluated, reason: ReasonNone}
	restriction(&result, cluster, cluster.Paused, "spec.paused",
		"Reconciliation is requested paused; existing work can continue.", SeverityInfo)
	switch cluster.Maintenance.State {
	case evidence.ObjectReported:
		restriction(&result, cluster, cluster.Maintenance.Enabled, "spec.maintenance.enabled",
			"Maintenance is requested; workload annotation propagation is not verified.", SeverityInfo)
	case evidence.ObjectAbsent, evidence.ObjectNull:
	case evidence.ObjectUnknown, evidence.ObjectInvalid:
		result.insufficient(ReasonMissingField)
	}
	switch cluster.RestoreHold.State {
	case evidence.ValueReported:
		value := finding(cluster, RuleRestriction, SeverityInfo, "A reported restore hold restricts ordinary management.",
			"metadata.annotations[openbao.org/restore-hold]")
		value.Subsubject = "metadata.annotations[openbao.org/restore-hold]"
		result.add(value)
	case evidence.ValueUnknown:
	case evidence.ValueInvalid, evidence.ValueImplied:
		result.insufficient(ReasonMissingField)
	}
	switch cluster.BreakGlass.State {
	case evidence.ObjectReported:
		restriction(
			&result,
			cluster,
			cluster.BreakGlass.Active,
			"status.breakGlass.active",
			"Break-glass restrictions are reported active.",
			SeverityWarning,
		)
	case evidence.ObjectAbsent, evidence.ObjectNull:
	case evidence.ObjectUnknown, evidence.ObjectInvalid:
		result.insufficient(ReasonMissingField)
	}
	return result
}

func controllerErrors(cluster evidence.Cluster) evaluation {
	result := evaluation{state: CoverageEvaluated, reason: ReasonNone}
	for _, entry := range []struct {
		value  evidence.ControllerError
		domain string
		field  string
	}{
		{cluster.WorkloadError, "workload", "status.workload.lastError"},
		{cluster.AdminOpsError, "adminOps", "status.adminOps.lastError"},
	} {
		switch entry.value.State {
		case evidence.ObjectAbsent, evidence.ObjectNull:
			continue
		case evidence.ObjectUnknown, evidence.ObjectInvalid:
			result.insufficient(ReasonMissingField)
			continue
		case evidence.ObjectReported:
		}
		if entry.value.Reason.State != evidence.ValueReported {
			result.insufficient(ReasonMissingField)
			continue
		}
		if inconsistentTime(entry.value.At, *cluster.Reference.CollectedAt.Value) {
			result.insufficient(ReasonTime)
		}
		value := finding(
			cluster,
			RuleController,
			SeverityWarning,
			"The operator records a controller error; this does not establish an active failure.",
			entry.field,
		)
		value.Subsubject = entry.domain
		result.add(value)
	}
	return result
}
