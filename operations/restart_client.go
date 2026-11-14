package operations

import (
	"context"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

// SubmitRestart submits at most one guarded PATCH. Unknown outcomes must be
// followed by reads of the same marker and target, never an automatic retry.
func (c *Client) SubmitRestart(ctx context.Context, plan RestartPlan, options SubmitOptions) (Result, error) {
	if c == nil || c.transport == nil || ctx == nil || !options.valid() {
		return Result{}, ErrInvalidRequest
	}
	patch, err := plan.Patch()
	if err != nil {
		return Result{}, err
	}
	return c.submit(ctx, plan.Target, patch, options, func(raw []byte) (Receipt, bool) {
		return restartReceipt(raw, plan, options.Projection)
	})
}

func restartReceipt(raw []byte, plan RestartPlan, limits adapter.ProjectionLimits) (Receipt, bool) {
	cluster, err := adapter.ProjectCluster(raw, adapter.ProjectionOptions{
		Origin: plan.Target.Identity.Origin, CollectedAt: time.Now().UTC(), Contract: plan.Contract, Limits: limits,
	})
	if err != nil || cluster.Reference.Identity != plan.Target.Identity || cluster.Reference.APIVersion != plan.Target.APIVersion ||
		cluster.Reference.ResourceVersion.State != evidence.ValueReported || cluster.Generation.State != evidence.ValueReported ||
		*cluster.Generation.Value <= *plan.Generation.Value || cluster.DeletionTimestamp.State != evidence.ValueUnknown ||
		cluster.Runtime.State != evidence.ObjectReported || !textEquals(cluster.Runtime.RestartAt, plan.RestartAt) {
		return Receipt{}, false
	}
	return Receipt{Reference: cluster.Reference, Generation: cluster.Generation}, true
}
