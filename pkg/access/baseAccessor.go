package access

import (
	"github.com/kcp-dev/logicalcluster/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kuadrant/kcp-glbc/pkg/util/metadata"
)

type baseAccessor struct {
	Accessor
	object interface{}
}

func (a *baseAccessor) GetMetadataObject() metav1.Object {
	return a.object.(metav1.Object)
}

//TODO PB 07/09/2022:
//	- uncomment when flaky GVK issues are resolved
//	- remove the GetKind method
//	- update Accessor interface
//	- Move String() method back to baseAccessor using GetGVK instead of GetKind

// GetGVK returns the GVK for the object this accessor is wrapped around. However, it is sometimes will not be populated
// for more info see: https://github.com/kubernetes-sigs/controller-runtime/issues/1517#issuecomment-839979174
//func (a *baseAccessor) GetGVK() schema.GroupVersionKind {
//	return a.GetRuntimeObject().GetObjectKind().GroupVersionKind()
//}

func (a *baseAccessor) AddAnnotation(key, value string) {
	metadata.AddAnnotation(a.GetMetadataObject(), key, value)
}

func (a *baseAccessor) GetAnnotation(key string) (string, bool) {
	if a.GetMetadataObject().GetAnnotations() == nil {
		return "", false
	}
	if v, ok := a.GetMetadataObject().GetAnnotations()[key]; ok {
		return v, ok
	}
	return "", false
}

func (a *baseAccessor) HasAnnotation(key string) bool {
	_, ok := a.GetAnnotation(key)
	return ok
}

func (a *baseAccessor) RemoveAnnotation(key string) {
	metadata.RemoveAnnotation(a.GetMetadataObject(), key)
}

func (a *baseAccessor) AddLabel(key, value string) {
	metadata.AddLabel(a.GetMetadataObject(), key, value)
}

func (a *baseAccessor) RemoveLabel(key string) {
	metadata.RemoveLabel(a.GetMetadataObject(), key)
}

func (a *baseAccessor) GetNamespaceName() types.NamespacedName {
	return types.NamespacedName{
		Namespace: a.GetMetadataObject().GetNamespace(),
		Name:      a.GetMetadataObject().GetName(),
	}
}

func (a *baseAccessor) GetDeletionTimestamp() *metav1.Time {
	return a.GetMetadataObject().GetDeletionTimestamp()
}

func (a *baseAccessor) GetRuntimeObject() runtime.Object {
	return a.object.(runtime.Object)
}

func (a *baseAccessor) GetFinalizers() []string {
	return a.GetMetadataObject().GetFinalizers()
}

func (a *baseAccessor) GetLogicalCluster() logicalcluster.Name {
	return logicalcluster.From(a.GetMetadataObject())
}
