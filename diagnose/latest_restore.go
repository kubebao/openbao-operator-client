package diagnose

import (
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
		path := indexPath("requests", index)
		association, err := evidence.AssociateRestore(target, request)
		if err != nil {
			return evidence.Restore{}, false, evidence.At(path, err)
		}
		id := request.Reference.Identity
		if seen[id] {
			return evidence.Restore{}, false, evidence.At(path+".reference.identity", evidence.ErrInvalidEvidence)
		}
		seen[id] = true
		if association.State != evidence.AssociationConfirmed || request.CreatedAt.State != evidence.ValueReported {
			continue
		}
		if request.CreatedAt.Value.After(*request.Reference.CollectedAt.Value) {
			continue
		}
		if !found || laterRestore(request, latest) {
			latest = request
			found = true
		}
	}
	return latest, found, nil
}

// laterRestore orders requests by creation time, newest first, then by UID ascending.
func laterRestore(candidate, current evidence.Restore) bool {
	created, latest := *candidate.CreatedAt.Value, *current.CreatedAt.Value
	if !created.Equal(latest) {
		return created.After(latest)
	}
	return strings.Compare(candidate.Reference.Identity.UID, current.Reference.Identity.UID) < 0
}
