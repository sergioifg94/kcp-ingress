package ingress

import (
	"encoding/json"
	"fmt"
	"strings"

	logicalcluster "github.com/kcp-dev/logicalcluster/v2"
	networkingv1 "k8s.io/api/networking/v1"

	"github.com/kuadrant/kcp-glbc/pkg/util/workloadMigration"
)

//GetStatus will return a set of statuses for each targeted cluster
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
