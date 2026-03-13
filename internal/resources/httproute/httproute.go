package httproute

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/home-operations/newt-sidecar/internal/blueprint"
	"github.com/home-operations/newt-sidecar/internal/config"
	"github.com/home-operations/newt-sidecar/internal/resources"
)

// Definition returns a ResourceDefinition for HTTPRoute resources.
func Definition() *resources.ResourceDefinition {
	return &resources.ResourceDefinition{
		GVR: schema.GroupVersionResource{
			Group:    "gateway.networking.k8s.io",
			Version:  "v1",
			Resource: "httproutes",
		},
		ConvertFunc:   resources.CreateConvertFunc(reflect.TypeOf(gatewayv1.HTTPRoute{})),
		ShouldProcess: shouldProcess,
		BuildEntries:  buildEntries,
	}
}

func shouldProcess(obj metav1.Object, cfg *config.Config) bool {
	route, ok := obj.(*gatewayv1.HTTPRoute)
	if !ok {
		return false
	}

	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	// Explicit opt-out.
	if v, ok := annotations[cfg.AnnotationPrefix+"/enabled"]; ok && (v == "false" || v == "0") {
		return false
	}

	// Must reference the configured gateway.
	return referencesGateway(route, cfg.GatewayName, cfg.GatewayNamespace)
}

func buildEntries(obj metav1.Object, cfg *config.Config) map[string]blueprint.Resource {
	route, ok := obj.(*gatewayv1.HTTPRoute)
	if !ok {
		return nil
	}

	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	entries := make(map[string]blueprint.Resource, len(route.Spec.Hostnames))
	for _, h := range route.Spec.Hostnames {
		hostname := string(h)
		key := blueprint.HostnameToKey(hostname)
		entries[key] = blueprint.BuildResource(route.Name, hostname, annotations, cfg)
	}
	return entries
}

func referencesGateway(route *gatewayv1.HTTPRoute, gatewayName, gatewayNamespace string) bool {
	if gatewayName == "" {
		return true
	}
	for _, parent := range route.Spec.ParentRefs {
		if parent.Name != gatewayv1.ObjectName(gatewayName) {
			continue
		}
		if gatewayNamespace != "" && parent.Namespace != nil && string(*parent.Namespace) != gatewayNamespace {
			continue
		}
		return true
	}
	return false
}
