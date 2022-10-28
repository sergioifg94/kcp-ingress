package traffic

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
)

type HostReconciler struct {
	ManagedDomain          string
	Log                    logr.Logger
	GetDomainVerifications func(ctx context.Context, accessor Interface) (*v1.DomainVerificationList, error)
	CreateOrUpdateTraffic  CreateOrUpdateTraffic
	DeleteTraffic          DeleteTraffic
}

func (r *HostReconciler) GetName() string {
	return "Host Reconciler"
}

func (r *HostReconciler) Reconcile(ctx context.Context, accessor Interface) (ReconcileStatus, error) {
	dvs, err := r.GetDomainVerifications(ctx, accessor)
	if err != nil {
		return ReconcileStatusContinue, fmt.Errorf("error getting domain verifications: %v", err)
	}
	err = accessor.ProcessCustomHosts(ctx, dvs, r.CreateOrUpdateTraffic, r.DeleteTraffic)
	if err != nil {
		return ReconcileStatusStop, fmt.Errorf("error processing custom hosts: %v", err)
	}
	return ReconcileStatusContinue, nil
}
