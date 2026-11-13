package collect

import (
	"context"
	"errors"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

type VerificationResult struct {
	Verification  evidence.TargetVerification `json:"verification"`
	Requests      int                         `json:"requests"`
	Bytes         int                         `json:"bytes"`
	RetainedBytes int                         `json:"retainedBytes"`
}

func (c *Client) validateTarget(target evidence.Cluster, request Request) error {
	if err := target.Validate(); err != nil {
		return evidence.At("target", errors.Join(ErrInvalidRequest, err))
	}
	info, err := adapter.Describe(request.Contract)
	if err != nil || info.APIGroup != target.Reference.Identity.Group || info.APIVersion != target.Reference.APIVersion {
		return evidence.At("contract", ErrInvalidRequest)
	}
	if target.Reference.CollectedAt.Value.After(time.Now()) {
		return evidence.At("target.reference.collectedAt", ErrInvalidRequest)
	}
	if request.Scope.Action != evidence.ReadGet {
		return evidence.At("scope.action", ErrInvalidRequest)
	}
	if err := c.validate(request, "OpenBaoCluster", "openbaoclusters"); err != nil {
		return err
	}
	id, scope := target.Reference.Identity, request.Scope
	apiVersion := scope.Version
	if scope.Group != "" {
		apiVersion = scope.Group + "/" + scope.Version
	}
	if scope.Origin != id.Origin || scope.Group != id.Group || scope.Namespace != id.Namespace || scope.Name != id.Name {
		return evidence.At("scope", ErrInvalidRequest)
	}
	if request.ExpectedUID != id.UID {
		return evidence.At("expectedUID", ErrInvalidRequest)
	}
	if target.Reference.APIVersion != apiVersion {
		return evidence.At("scope.version", ErrInvalidRequest)
	}
	return nil
}

func uncollectedVerification(target evidence.Cluster, request Request) evidence.TargetVerification {
	return evidence.TargetVerification{
		Before: target.Reference, BeforeGeneration: target.Generation, AfterGeneration: evidence.Integer{State: evidence.ValueUnknown},
		Read: evidence.Collection{Scope: request.Scope, Outcome: evidence.CollectionNotCollected}, State: evidence.VerificationInsufficient,
	}
}

// VerifyTarget performs exactly one pinned named GET. It never rediscovers a group.
func (c *Client) VerifyTarget(ctx context.Context, target evidence.Cluster, request Request) (VerificationResult, error) {
	if ctx == nil {
		return VerificationResult{}, evidence.At("context", ErrInvalidRequest)
	}
	if err := c.validateTarget(target, request); err != nil {
		return VerificationResult{}, err
	}
	read, err := c.Clusters(ctx, request)
	if err != nil {
		return VerificationResult{}, err
	}
	value, err := evidence.NewTargetVerification(target, read.Collection, read.Observations)
	if err != nil {
		return VerificationResult{}, evidence.At("verification", errors.Join(ErrInvalidRequest, err))
	}
	return VerificationResult{
		Verification: value, Requests: read.Requests, Bytes: read.Bytes, RetainedBytes: read.RetainedBytes,
	}, nil
}
