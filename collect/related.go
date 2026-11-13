package collect

import (
	"context"
	"slices"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

// RelatedPlan declares every related scope and the final pinned target GET.
// Nil scopes cause no reads. Limits apply across the entire sequence.
type RelatedPlan struct {
	StatefulSets *Request
	Pods         *Request
	Restores     *Request
	Verify       Request
	Limits       Limits
}

type RelatedResult struct {
	Interval      evidence.Interval             `json:"interval"`
	Outcome       evidence.CollectionOutcome    `json:"outcome"`
	Verification  evidence.TargetVerification   `json:"verification"`
	StatefulSets  *Result[evidence.StatefulSet] `json:"statefulSets,omitempty"`
	Pods          *Result[evidence.Pod]         `json:"pods,omitempty"`
	Restores      *Result[evidence.Restore]     `json:"restores,omitempty"`
	Associations  []evidence.Association        `json:"associations"`
	Requests      int                           `json:"requests"`
	Bytes         int                           `json:"bytes"`
	RetainedBytes int                           `json:"retainedBytes"`
}

func (c *Client) validateRelated(target evidence.Cluster, plan RelatedPlan) error {
	if !plan.Limits.valid() {
		return evidence.At("limits", ErrInvalidRequest)
	}
	if err := c.validateTarget(target, plan.Verify); err != nil {
		return evidence.At("verify", err)
	}
	if plan.StatefulSets == nil && plan.Pods == nil && plan.Restores == nil {
		return evidence.At("scopes", ErrInvalidRequest)
	}
	for _, item := range []struct {
		request        *Request
		kind, resource string
	}{
		{plan.StatefulSets, "StatefulSet", "statefulsets"},
		{plan.Pods, "Pod", "pods"},
		{plan.Restores, "OpenBaoRestore", "openbaorestores"},
	} {
		if item.request == nil {
			continue
		}
		request := *item.request
		if err := c.validate(request, item.kind, item.resource); err != nil {
			return evidence.At(item.resource, err)
		}
		if request.Scope.Action != evidence.ReadList || request.Scope.Origin != target.Reference.Identity.Origin ||
			request.Scope.Namespace != target.Reference.Identity.Namespace {
			return evidence.At(item.resource+".scope", ErrInvalidRequest)
		}
		if item.kind == "OpenBaoRestore" && (request.Scope.Group != target.Reference.Identity.Group ||
			request.Scope.Group+"/"+request.Scope.Version != target.Reference.APIVersion) {
			return evidence.At(item.resource+".scope", ErrInvalidRequest)
		}
	}
	return nil
}

// Related collects only declared namespace scopes, then verifies the selected target.
// It makes no discovery calls and never switches group, name, namespace, or UID.
func (c *Client) Related(ctx context.Context, target evidence.Cluster, plan RelatedPlan) (result RelatedResult, err error) {
	if ctx == nil {
		return result, evidence.At("context", ErrInvalidRequest)
	}
	if validationErr := c.validateRelated(target, plan); validationErr != nil {
		return result, validationErr
	}
	ctx, cancel := context.WithTimeout(ctx, plan.Limits.Timeout)
	defer cancel()
	result = RelatedResult{
		Interval: evidence.Interval{StartedAt: time.Now().UTC()}, Outcome: evidence.CollectionComplete,
		Verification: uncollectedVerification(target, plan.Verify), Associations: []evidence.Association{},
	}
	defer func() {
		completed := time.Now()
		deadline, _ := ctx.Deadline()
		if ctx.Err() != nil || !completed.Before(deadline) {
			result.Outcome = contextOutcome(ctx, context.DeadlineExceeded)
		}
		// Derived associations do not extend a completed read's interval or deadline.
		result.Interval.CompletedAt = completed.UTC()
		if associationErr := associateRelated(target, &result); associationErr != nil {
			err = associationErr
		}
	}()
	if plan.StatefulSets != nil {
		read := relatedRead(ctx, *plan.StatefulSets, plan.Limits, &result, c.StatefulSets)
		result.StatefulSets = &read
	}
	if plan.Pods != nil {
		read := relatedRead(ctx, *plan.Pods, plan.Limits, &result, c.Pods)
		result.Pods = &read
	}
	if plan.Restores != nil {
		read := relatedRead(ctx, *plan.Restores, plan.Limits, &result, c.Restores)
		result.Restores = &read
	}
	verify, available := remaining(plan.Verify, plan.Limits, &result, true)
	if !available || ctx.Err() != nil {
		return result, nil
	}
	verification, verifyErr := c.VerifyTarget(ctx, target, verify)
	if verifyErr != nil {
		result.Outcome = evidence.CollectionInvalid
		return result, nil
	}
	result.Verification = verification.Verification
	result.Requests += verification.Requests
	result.Bytes += verification.Bytes
	result.RetainedBytes += verification.RetainedBytes
	switch result.Verification.State {
	case evidence.VerificationVerified:
	case evidence.VerificationDeleted:
		result.Outcome = evidence.CollectionDeleted
	case evidence.VerificationReplaced:
		result.Outcome = evidence.CollectionReplaced
	default:
		if result.Verification.Read.Outcome != evidence.CollectionComplete && result.Outcome == evidence.CollectionComplete {
			result.Outcome = evidence.CollectionPartial
		}
	}
	return result, nil
}

func candidates(result *RelatedResult) int {
	count := 0
	if result.StatefulSets != nil {
		count += result.StatefulSets.Collection.Items
	}
	if result.Pods != nil {
		count += result.Pods.Collection.Items
	}
	if result.Restores != nil {
		count += result.Restores.Collection.Items
	}
	return count
}

func remaining(request Request, limits Limits, result *RelatedResult, verify bool) (Request, bool) {
	reserve, availableCandidates := 1, limits.MaxCandidates-candidates(result)
	if verify {
		reserve, availableCandidates = 0, 1
	}
	request.Limits.MaxPages = min(request.Limits.MaxPages, limits.MaxPages-result.Requests-reserve)
	request.Limits.MaxCandidates = min(request.Limits.MaxCandidates, availableCandidates)
	request.Limits.MaxBytes = min(request.Limits.MaxBytes, limits.MaxBytes-result.Bytes)
	request.Limits.MaxResponseBytes = min(request.Limits.MaxResponseBytes, limits.MaxResponseBytes)
	request.Limits.MaxRetainedBytes = min(request.Limits.MaxRetainedBytes, limits.MaxRetainedBytes-result.RetainedBytes)
	request.Limits.PageSize = min(request.Limits.PageSize, limits.PageSize, request.Limits.MaxCandidates)
	if request.Limits.MaxPages <= 0 {
		result.Outcome = evidence.CollectionPageLimit
		return request, false
	}
	if request.Limits.MaxBytes <= 0 || request.Limits.MaxCandidates <= 0 || request.Limits.MaxRetainedBytes <= 0 {
		result.Outcome = evidence.CollectionSizeLimit
		return request, false
	}
	return request, true
}

func relatedRead[T any](ctx context.Context, request Request, limits Limits, result *RelatedResult,
	read func(context.Context, Request) (Result[T], error),
) Result[T] {
	skipped := Result[T]{
		Observations: []T{},
		Collection:   evidence.Collection{Scope: request.Scope, Outcome: evidence.CollectionNotCollected},
	}
	bounded, available := remaining(request, limits, result, false)
	if !available || ctx.Err() != nil {
		return skipped
	}
	value, err := read(ctx, bounded)
	if err != nil {
		result.Outcome = evidence.CollectionInvalid
		return skipped
	}
	result.Requests += value.Requests
	result.Bytes += value.Bytes
	result.RetainedBytes += value.RetainedBytes
	if value.Collection.Outcome != evidence.CollectionComplete && result.Outcome == evidence.CollectionComplete {
		result.Outcome = evidence.CollectionPartial
	}
	return value
}

func associateRelated(target evidence.Cluster, result *RelatedResult) error {
	sets := map[string]evidence.StatefulSet{}
	parents := map[string]evidence.Association{}
	if result.StatefulSets != nil {
		for _, set := range result.StatefulSets.Observations {
			association, err := adapter.AssociateStatefulSet(set, target.Reference.Identity)
			if err != nil {
				return err
			}
			sets[set.Metadata.Reference.Identity.UID], parents[set.Metadata.Reference.Identity.UID] = set, association
			result.Associations = append(result.Associations, association)
		}
	}
	if result.Pods != nil {
		for _, pod := range result.Pods.Observations {
			association, err := relatedPod(target.Reference.Identity, pod, sets, parents)
			if err != nil {
				return err
			}
			result.Associations = append(result.Associations, association)
		}
	}
	if result.Restores != nil {
		for _, restore := range result.Restores.Observations {
			association, err := adapter.AssociateRestore(target, restore)
			if err != nil {
				return err
			}
			result.Associations = append(result.Associations, association)
		}
	}
	slices.SortFunc(result.Associations, func(a, b evidence.Association) int {
		return slices.Compare(identityParts(a.Subject), identityParts(b.Subject))
	})
	return nil
}

func relatedPod(target evidence.Identity, pod evidence.Pod, sets map[string]evidence.StatefulSet,
	parents map[string]evidence.Association,
) (evidence.Association, error) {
	excluded, err := evidence.PodHasNonStatefulSetController(pod)
	if err != nil {
		return evidence.Association{}, err
	}
	for _, owner := range pod.Metadata.Owners.Values {
		set, exists := sets[owner.UID]
		if owner.Controller && exists {
			return adapter.AssociatePod(pod, set, parents[owner.UID])
		}
	}
	if excluded {
		return evidence.Association{
			Subject: pod.Metadata.Reference.Identity, Target: target, State: evidence.AssociationForeign,
		}, nil
	}
	return evidence.Association{
		Subject: pod.Metadata.Reference.Identity, Target: target, State: evidence.AssociationUnconfirmed,
	}, nil
}
