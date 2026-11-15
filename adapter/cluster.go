package adapter

import (
	"slices"
	"strings"
	"time"

	"github.com/kubebao/openbao-operator-client/evidence"
)

// ProjectionOptions binds one read to a caller-supplied origin, time, and known contract.
type ProjectionOptions struct {
	Origin      string
	CollectedAt time.Time
	Contract    Contract
	Limits      ProjectionLimits
}

// ProjectCluster validates and projects approved OpenBaoCluster fields. Invalid optional
// fields remain distinguishable without discarding independent facts. Raw data and provider
// messages are never retained. The function performs no reads and never uses the wall clock.
func ProjectCluster(raw []byte, options ProjectionOptions) (evidence.Cluster, error) {
	object, ref, err := projectResource(raw, options, "OpenBaoCluster")
	if err != nil {
		return evidence.Cluster{}, err
	}
	result := evidence.Cluster{
		Reference:          ref,
		CreatedAt:          timestamp(object, "metadata", "creationTimestamp"),
		DeletionTimestamp:  timestamp(object, "metadata", "deletionTimestamp"),
		Generation:         integer(object, "metadata", "generation"),
		ObservedGeneration: integer(object, "status", "observedGeneration"),
		DesiredVersion:     version(object, "spec", "version"),
		ObservedVersion:    version(object, "status", "currentVersion"),
		Phase:              text(object, true, "status", "phase"),
		DesiredReplicas:    count(object, "spec", "replicas"),
		ReadyReplicas:      count(object, "status", "readyReplicas"),
		Paused:             boolean(object, "spec", "paused"),
		Conditions:         projectConditions(object, options.Limits.MaxConditions, "status", "conditions"),
	}
	projectOperationalFields(object, &result, options.Limits)
	runtime, state := objectAt(object, "spec", "runtime")
	result.Runtime = evidence.Runtime{State: state, RestartAt: text(runtime, false, "restartAt")}
	strategies := []string{"RollingUpdate", "BlueGreen"}
	result.UpgradeStrategy = enumText(object, strategies, "spec", "upgrade", "strategy")
	result.AcceptedUpgradeStrategy = enumText(object, strategies, "status", "acceptedUpgradeStrategy")
	if inspectedOperatorContract(options.Contract) {
		inferOmissions(object, &result)
	} else {
		result.RestoreOrigin = evidence.Text{State: evidence.ValueUnknown}
	}
	if result.Validate() != nil {
		return evidence.Cluster{}, &ProjectionError{Code: CodeInvalidInput}
	}
	return result, nil
}

// inferOmissions applies the pinned operator omission rules to unknown fields.
func inferOmissions(object map[string]any, result *evidence.Cluster) {
	if result.ReadyReplicas.State == evidence.ValueUnknown &&
		result.DesiredReplicas.State == evidence.ValueReported &&
		result.Acknowledgement() == evidence.GenerationCurrent {
		result.ReadyReplicas = evidence.Integer{State: evidence.ValueImplied, Value: new(int64(0))}
	}
	if backupSucceededLast(result.Backup) {
		result.Backup.ConsecutiveFailures = evidence.Integer{State: evidence.ValueImplied, Value: new(int64(0))}
	}
	if result.Paused.State == evidence.ValueUnknown {
		if spec, state := field(object, "spec"); state == evidence.ValueReported {
			if _, ok := spec.(map[string]any); ok {
				result.Paused = evidence.Boolean{State: evidence.ValueImplied, Value: new(false)}
			}
		}
	}
}

// backupSucceededLast reports whether an omitted failure count implies zero.
// The operator resets consecutiveFailures to an omitted zero in the same status update that records
// lastBackupTime, and records lastAttemptTime when an attempt starts. A success at or after the last attempt
// therefore means the latest attempt succeeded and no consecutive failures exist.
func backupSucceededLast(backup evidence.Backup) bool {
	return backup.State == evidence.ObjectReported &&
		backup.ConsecutiveFailures.State == evidence.ValueUnknown &&
		backup.LastSuccessAt.State == evidence.ValueReported &&
		backup.LastAttemptAt.State == evidence.ValueReported &&
		!backup.LastSuccessAt.Value.Before(*backup.LastAttemptAt.Value)
}

// projectConditions projects a bounded condition list sorted by type. Any malformed
// entry, duplicate type, or unknown status invalidates the whole set.
func projectConditions(object map[string]any, limit int, path ...string) evidence.Conditions {
	value, state := field(object, path...)
	if state != evidence.ValueReported {
		return evidence.Conditions{State: state}
	}
	items, ok := value.([]any)
	if !ok || len(items) > limit {
		return evidence.Conditions{State: evidence.ValueInvalid}
	}
	result := evidence.Conditions{State: evidence.ValueReported, Values: []evidence.Condition{}}
	seen := map[string]bool{}
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			return evidence.Conditions{State: evidence.ValueInvalid}
		}
		name, ok := fields["type"].(string)
		if !ok || !identifier.MatchString(name) || seen[name] {
			return evidence.Conditions{State: evidence.ValueInvalid}
		}
		status, _ := fields["status"].(string)
		switch evidence.ConditionStatus(status) {
		case evidence.ConditionTrue, evidence.ConditionFalse, evidence.ConditionUnknown:
		default:
			return evidence.Conditions{State: evidence.ValueInvalid}
		}
		seen[name] = true
		observed := integer(fields, "observedGeneration")
		if observed.State == evidence.ValueInvalid {
			return evidence.Conditions{State: evidence.ValueInvalid}
		}
		result.Values = append(result.Values, evidence.Condition{
			Type:               name,
			Status:             evidence.ConditionStatus(status),
			Reason:             text(fields, true, "reason"),
			ObservedGeneration: observed,
			LastTransitionAt:   timestamp(fields, "lastTransitionTime"),
		})
	}
	slices.SortFunc(result.Values, func(a, b evidence.Condition) int { return strings.Compare(a.Type, b.Type) })
	return result
}
