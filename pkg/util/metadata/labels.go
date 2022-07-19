package metadata

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

func HasLabel(obj metav1.Object, key string) bool {
	labels := obj.GetLabels()
	if labels == nil {
		return false
	}
	_, ok := labels[key]
	return ok
}
