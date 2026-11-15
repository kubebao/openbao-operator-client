package operations

import "github.com/kubebao/openbao-operator-client/evidence"

// maintenancePool records the expected revision and replicas of one workload pool.
type maintenancePool struct {
	revision evidence.Text
	replicas int64
	found    bool
}

// maintenanceTopology describes settled public observations only. It does not
// predict the operator's in-flight upgrade state machine or choose a revision.
func maintenanceTopology(cluster evidence.Cluster, result *Assessment) map[string]*maintenancePool {
	if cluster.DesiredReplicas.State == evidence.ValueInvalid {
		result.add(AssessmentInsufficient, ReasonInvalidField)
		return nil
	}
	if cluster.DesiredReplicas.State != evidence.ValueReported || *cluster.DesiredReplicas.Value <= 0 {
		result.add(AssessmentInsufficient, ReasonMissingField)
		return nil
	}
	revision, known := maintenanceVoterRevision(cluster.BlueGreen, result)
	if !known {
		return nil
	}
	pools := map[string]*maintenancePool{
		"voter": {revision: revision, replicas: *cluster.DesiredReplicas.Value},
	}
	switch cluster.ReadReplicas.Configuration {
	case evidence.ObjectAbsent, evidence.ObjectNull:
	case evidence.ObjectReported:
		if cluster.ReadReplicas.Desired.State != evidence.ValueReported {
			reason := ReasonMissingField
			if cluster.ReadReplicas.Desired.State == evidence.ValueInvalid {
				reason = ReasonInvalidField
			}
			result.add(AssessmentInsufficient, reason)
			return nil
		}
		pools["read-replica"] = &maintenancePool{
			revision: evidence.Text{State: evidence.ValueUnknown},
			replicas: *cluster.ReadReplicas.Desired.Value,
		}
	case evidence.ObjectInvalid:
		result.add(AssessmentInsufficient, ReasonInvalidField)
		return nil
	default:
		result.add(AssessmentInsufficient, ReasonMissingField)
		return nil
	}
	return pools
}

// maintenanceVoterRevision returns the voter pool revision of a settled blue/green state.
func maintenanceVoterRevision(blueGreen evidence.BlueGreen, result *Assessment) (evidence.Text, bool) {
	unknown := evidence.Text{State: evidence.ValueUnknown}
	switch blueGreen.State {
	case evidence.ObjectAbsent, evidence.ObjectNull:
		return unknown, true
	case evidence.ObjectInvalid:
		result.add(AssessmentInsufficient, ReasonInvalidField)
		return unknown, false
	case evidence.ObjectUnknown:
		result.add(AssessmentInsufficient, ReasonMissingField)
		return unknown, false
	}
	if blueGreen.Phase.State == evidence.ValueInvalid ||
		blueGreen.BlueRevision.State == evidence.ValueInvalid ||
		blueGreen.GreenRevision.State == evidence.ValueInvalid {
		result.add(AssessmentInsufficient, ReasonInvalidField)
		return unknown, false
	}
	if blueGreen.Phase.State != evidence.ValueReported {
		result.add(AssessmentInsufficient, ReasonMissingField)
		return unknown, false
	}
	if *blueGreen.Phase.Value != "Idle" {
		result.add(AssessmentInsufficient, ReasonTopologyUnconfirmed)
		return unknown, false
	}
	if blueGreen.GreenRevision.State != evidence.ValueUnknown {
		result.add(AssessmentInsufficient, ReasonTopologyConflict)
		return unknown, false
	}
	// The pinned contract omits an empty BlueRevision on a present Idle object
	// for the original unrevisioned voter. This does not infer a label or owner.
	return blueGreen.BlueRevision, true
}

// maintenanceSetLabels returns a StatefulSet's pool and revision when its
// selector and template labels agree.
func maintenanceSetLabels(set evidence.StatefulSet, result *Assessment) (string, evidence.Text, bool) {
	unknown := evidence.Text{State: evidence.ValueUnknown}
	if set.SelectorPool.State == evidence.ValueInvalid ||
		set.TemplatePool.State == evidence.ValueInvalid ||
		set.SelectorRevision.State == evidence.ValueInvalid ||
		set.TemplateRevision.State == evidence.ValueInvalid {
		result.add(AssessmentInsufficient, ReasonInvalidField)
		return "", unknown, false
	}
	if set.SelectorPool.State != evidence.ValueReported || set.TemplatePool.State != evidence.ValueReported {
		result.add(AssessmentInsufficient, ReasonMissingField)
		return "", unknown, false
	}
	if !sameControlText(set.SelectorPool, set.TemplatePool) || !sameControlText(set.SelectorRevision, set.TemplateRevision) {
		result.add(AssessmentInsufficient, ReasonTopologyConflict)
		return "", unknown, false
	}
	return *set.SelectorPool.Value, set.SelectorRevision, true
}

// maintenanceSetTopology matches a confirmed StatefulSet to its expected pool.
func maintenanceSetTopology(set evidence.StatefulSet, pools map[string]*maintenancePool, result *Assessment) {
	name, revision, known := maintenanceSetLabels(set, result)
	if !known || pools == nil {
		return
	}
	pool := pools[name]
	if pool == nil || !sameControlText(revision, pool.revision) {
		result.add(AssessmentInsufficient, ReasonTopologyConflict)
		return
	}
	if pool.found {
		result.add(AssessmentInsufficient, ReasonTopologyAmbiguous)
	}
	pool.found = true
	if set.DesiredReplicas.State == evidence.ValueReported && *set.DesiredReplicas.Value != pool.replicas {
		result.add(AssessmentPending, ReasonWorkloadMissing)
	}
}

// maintenancePodTopology reports whether a Pod's labels match its StatefulSet's pool and revision.
func maintenancePodTopology(pod evidence.Pod, set evidence.StatefulSet, result *Assessment) bool {
	_, revision, known := maintenanceSetLabels(set, result)
	if !known {
		return false
	}
	if pod.Metadata.Pool.State == evidence.ValueInvalid || pod.Metadata.Revision.State == evidence.ValueInvalid {
		result.add(AssessmentInsufficient, ReasonInvalidField)
		return false
	}
	if pod.Metadata.Pool.State != evidence.ValueReported {
		result.add(AssessmentInsufficient, ReasonMissingField)
		return false
	}
	if pod.Metadata.Revision.State == evidence.ValueUnknown && revision.State == evidence.ValueReported {
		result.add(AssessmentInsufficient, ReasonMissingField)
		return false
	}
	if !sameControlText(pod.Metadata.Pool, set.SelectorPool) || !sameControlText(pod.Metadata.Revision, revision) {
		result.add(AssessmentInsufficient, ReasonTopologyConflict)
		return false
	}
	return true
}
