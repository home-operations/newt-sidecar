package httproute

import (
	"context"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/home-operations/newt-sidecar/internal/blueprint"
	"github.com/home-operations/newt-sidecar/internal/config"
	"github.com/home-operations/newt-sidecar/internal/resources"
)

func Definition() *resources.ResourceDefinition {
	return &resources.ResourceDefinition{
		GVR: schema.GroupVersionResource{
			Group:    "gateway.networking.k8s.io",
			Version:  "v1",
			Resource: "httproutes",
		},
		ConvertFunc:  resources.CreateConvertFunc(reflect.TypeOf(gatewayv1.HTTPRoute{})),
		FilterFunc:   filterByGateway,
		BuildEntries: buildEntries,
	}
}

func filterByGateway(obj metav1.Object, cfg *config.Config) bool {
	route, ok := obj.(*gatewayv1.HTTPRoute)
	if !ok {
		return false
	}

	if cfg.GatewayName == "" {
		return true
	}

	return referencesGateway(route, cfg.GatewayName, cfg.GatewayNamespace)
}

func buildEntries(ctx context.Context, obj metav1.Object, secretData map[string]string, cfg *config.Config) map[string]blueprint.Resource {
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
		entries[key] = blueprint.BuildResource(route.Name, hostname, annotations, secretData, cfg)
	}
	return entries
}

func referencesGateway(route *gatewayv1.HTTPRoute, gatewayName, gatewayNamespace string) bool {
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
