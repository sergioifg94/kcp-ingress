package ingress

import (
	"encoding/json"
	"fmt"
	"strings"

	logicalcluster "github.com/kcp-dev/logicalcluster/v2"
	"github.com/kuadrant/kcp-glbc/pkg/util/metadata"
	"github.com/kuadrant/kcp-glbc/pkg/util/workloadMigration"
	networkingv1 "k8s.io/api/networking/v1"
)

//GetStatus will return a set of statuses for each targetted cluster
func GetStatus(i *networkingv1.Ingress) (map[logicalcluster.Name]*networkingv1.IngressStatus, error) {
	statuses := map[logicalcluster.Name]*networkingv1.IngressStatus{}
	for k, v := range i.Annotations {
		status := &networkingv1.IngressStatus{}
		if !strings.Contains(k, workloadMigration.WorkloadStatusAnnotation) {
			continue
		}
		annotationParts := strings.Split(k, "/")
		if len(annotationParts) < 2 {
			return nil, fmt.Errorf("advanced scheduling annotation malformed %s value %s", workloadMigration.WorkloadStatusAnnotation, i.Annotations[k])
		}
		clusterName := annotationParts[1]
		//skip IP record if cluster is being deleted by KCP
		if metadata.HasAnnotation(i, workloadMigration.WorkloadDeletingAnnotation+annotationParts[1]) {
			continue
		}
		err := json.Unmarshal([]byte(v), status)
		if err != nil {
			return statuses, err
		}
		statuses[logicalcluster.New(clusterName)] = status
	}

	cluster := logicalcluster.From(i)
	statuses[cluster] = &i.Status
	return statuses, nil
}
