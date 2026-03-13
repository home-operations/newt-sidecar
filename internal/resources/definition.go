package resources

import (
	"fmt"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/home-operations/newt-sidecar/internal/blueprint"
	"github.com/home-operations/newt-sidecar/internal/config"
)

type AutoConfigFunc func(cfg *config.Config) bool
type FilterFunc func(obj metav1.Object, cfg *config.Config) bool
type BuildEntriesFunc func(obj metav1.Object, cfg *config.Config) map[string]blueprint.Resource

// ResourceDefinition describes how to watch, filter, and convert a single
// Kubernetes resource type into blueprint entries.
// AutoConfigFunc nil = opt-out mode (process all unless disabled); non-nil = auto or annotation mode.
type ResourceDefinition struct {
	GVR            schema.GroupVersionResource
	ConvertFunc    func(*unstructured.Unstructured) (metav1.Object, error)
	AutoConfigFunc AutoConfigFunc
	FilterFunc     FilterFunc
	BuildEntries   BuildEntriesFunc
}

func CreateConvertFunc(targetType reflect.Type) func(*unstructured.Unstructured) (metav1.Object, error) {
	return func(u *unstructured.Unstructured) (metav1.Object, error) {
		obj := reflect.New(targetType).Interface()
		if err := k8sruntime.DefaultUnstructuredConverter.FromUnstructured(u.Object, obj); err != nil {
			return nil, fmt.Errorf("failed to convert to %s: %w", targetType.Name(), err)
		}
		return obj.(metav1.Object), nil
	}
}
