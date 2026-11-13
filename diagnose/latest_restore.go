package diagnose

import (
	"fmt"
	"strings"

	"github.com/kubebao/openbao-operator-client/evidence"
)

// LatestRestore selects a confirmed request for display. Ties use UID ascending.
// Selection does not establish causality, submission acceptance, or operation success.
func LatestRestore(
	target evidence.Cluster,
	requests []evidence.Restore,
) (evidence.Restore, bool, error) {
	if err := target.Validate(); err != nil {
		return evidence.Restore{}, false, evidence.At("target", err)
	}
	seen := map[evidence.Identity]bool{}
	var latest evidence.Restore
	found := false
	for index, request := range requests {
		association, err := evidence.AssociateRestore(target, request)
		if err != nil {
			return evidence.Restore{}, false, evidence.At(fmt.Sprintf("requests[%d]", index), err)
		}
		id := request.Reference.Identity
		if seen[id] {
			return evidence.Restore{}, false, evidence.At(
				fmt.Sprintf("requests[%d].reference.identity", index),
				evidence.ErrInvalidEvidence,
			)
		}
		seen[id] = true
		if association.State != evidence.AssociationConfirmed || request.CreatedAt.State != evidence.ValueReported ||
			request.CreatedAt.Value.After(*request.Reference.CollectedAt.Value) {
			continue
		}
		if !found || request.CreatedAt.Value.After(*latest.CreatedAt.Value) ||
			request.CreatedAt.Value.Equal(*latest.CreatedAt.Value) && strings.Compare(id.UID, latest.Reference.Identity.UID) < 0 {
			latest, found = request, true
		}
	}
	return latest, found, nil
}
