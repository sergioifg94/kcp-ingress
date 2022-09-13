package reconcilers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/rs/xid"

	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	kuadrantclientv1 "github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/clientset/versioned"
	"github.com/kuadrant/kcp-glbc/pkg/traffic"
	"github.com/kuadrant/kcp-glbc/pkg/util/metadata"
)

type HostReconciler struct {
	ManagedDomain          string
	Log                    logr.Logger
	CustomHostsEnabled     bool
	KuadrantClient         kuadrantclientv1.ClusterInterface
	GetDomainVerifications func(ctx context.Context, accessor traffic.Interface) (*v1.DomainVerificationList, error)
	CreateOrUpdateTraffic  traffic.CreateOrUpdateTraffic
	DeleteTraffic          traffic.DeleteTraffic
}

func (r *HostReconciler) GetName() string {
	return "Host Reconciler"
}

func (r *HostReconciler) Reconcile(ctx context.Context, accessor traffic.Interface) (traffic.ReconcileStatus, error) {
	if !metadata.HasAnnotation(accessor, traffic.ANNOTATION_HCG_HOST) {
		// Let's assign it a global hostname if any
		generatedHost := fmt.Sprintf("%s.%s", xid.New(), r.ManagedDomain)
		metadata.AddAnnotation(accessor, traffic.ANNOTATION_HCG_HOST, generatedHost)
		//we need this host set and saved on the accessor before we go any further so force an update
		// if this is not saved we end up with a new host and the certificate can have the wrong host
		return traffic.ReconcileStatusStop, nil
	}
	if !r.CustomHostsEnabled {
		hcgHost := accessor.GetAnnotations()[traffic.ANNOTATION_HCG_HOST]
		replacedHosts := accessor.ReplaceCustomHosts(hcgHost)
		if len(replacedHosts) > 0 {
			metadata.AddAnnotation(accessor, traffic.ANNOTATION_HCG_CUSTOM_HOST_REPLACED, fmt.Sprintf(" replaced custom hosts %v to the glbc host due to custom host policy not being allowed", replacedHosts))
		}
		return traffic.ReconcileStatusContinue, nil
	}
	dvs, err := r.GetDomainVerifications(ctx, accessor)
	if err != nil {
		return traffic.ReconcileStatusContinue, fmt.Errorf("error getting domain verifications: %v", err)
	}
	err = accessor.ProcessCustomHosts(ctx, dvs, r.CreateOrUpdateTraffic, r.DeleteTraffic)
	if err != nil {
		return traffic.ReconcileStatusStop, fmt.Errorf("error processing custom hosts: %v", err)
	}
	return traffic.ReconcileStatusContinue, nil
}
