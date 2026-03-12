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

// ResourceDefinition describes how to watch, filter, and convert a single
// Kubernetes resource type into blueprint entries.
//
// Modelled after the gatus-sidecar ResourceDefinition pattern so that adding
// a new resource type only requires implementing this struct — no new
// controller code is needed.
type ResourceDefinition struct {
	// GVR identifies the Kubernetes resource type to watch.
	GVR schema.GroupVersionResource

	// ConvertFunc converts an unstructured watch event into a typed metav1.Object.
	ConvertFunc func(*unstructured.Unstructured) (metav1.Object, error)

	// ShouldProcess decides whether the controller should process this object.
	ShouldProcess func(obj metav1.Object, cfg *config.Config) bool

	// BuildEntries converts the object into one or more blueprint resource entries.
	// The map key is the blueprint resource key; the value is the Resource.
	BuildEntries func(obj metav1.Object, cfg *config.Config) map[string]blueprint.Resource
}

// CreateConvertFunc creates a typed conversion function for the given target type.
func CreateConvertFunc(targetType reflect.Type) func(*unstructured.Unstructured) (metav1.Object, error) {
	return func(u *unstructured.Unstructured) (metav1.Object, error) {
		obj := reflect.New(targetType).Interface()
		if err := k8sruntime.DefaultUnstructuredConverter.FromUnstructured(u.Object, obj); err != nil {
			return nil, fmt.Errorf("failed to convert to %s: %w", targetType.Name(), err)
		}
		return obj.(metav1.Object), nil
	}
}
