package httproute_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/home-operations/newt-sidecar/internal/blueprint"
	"github.com/home-operations/newt-sidecar/internal/config"
	"github.com/home-operations/newt-sidecar/internal/resources/httproute"
)

var baseCfg = &config.Config{
	AnnotationPrefix: "newt-sidecar",
	SiteID:           "test-site",
	TargetHostname:   "gw.local",
	TargetPort:       443,
}

func route(name string, hostnames []string, parents []gatewayv1.ParentReference, annotations map[string]string) *gatewayv1.HTTPRoute {
	hs := make([]gatewayv1.Hostname, len(hostnames))
	for i, h := range hostnames {
		hs[i] = gatewayv1.Hostname(h)
	}
	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: annotations,
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parents},
			Hostnames:       hs,
		},
	}
}

func parentRef(name, namespace string) gatewayv1.ParentReference {
	ref := gatewayv1.ParentReference{Name: gatewayv1.ObjectName(name)}
	if namespace != "" {
		ns := gatewayv1.Namespace(namespace)
		ref.Namespace = &ns
	}
	return ref
}

// --- FilterFunc ---

func TestFilter_NoGatewayConfigured_AcceptsAll(t *testing.T) {
	cfg := *baseCfg
	cfg.GatewayName = ""
	r := route("app", []string{"app.example.com"}, nil, nil)
	if !httproute.Definition().FilterFunc(r, &cfg) {
		t.Error("expected route to pass filter when no gateway configured")
	}
}

func TestFilter_GatewayMatch(t *testing.T) {
	cfg := *baseCfg
	cfg.GatewayName = "my-gateway"
	r := route("app", []string{"app.example.com"}, []gatewayv1.ParentReference{parentRef("my-gateway", "")}, nil)
	if !httproute.Definition().FilterFunc(r, &cfg) {
		t.Error("expected route to pass filter when gateway matches")
	}
}

func TestFilter_GatewayMismatch(t *testing.T) {
	cfg := *baseCfg
	cfg.GatewayName = "my-gateway"
	r := route("app", []string{"app.example.com"}, []gatewayv1.ParentReference{parentRef("other-gateway", "")}, nil)
	if httproute.Definition().FilterFunc(r, &cfg) {
		t.Error("expected route to be rejected when gateway does not match")
	}
}

func TestFilter_GatewayNamespaceMatch(t *testing.T) {
	cfg := *baseCfg
	cfg.GatewayName = "my-gateway"
	cfg.GatewayNamespace = "infra"
	r := route("app", []string{"app.example.com"}, []gatewayv1.ParentReference{parentRef("my-gateway", "infra")}, nil)
	if !httproute.Definition().FilterFunc(r, &cfg) {
		t.Error("expected route to pass filter when gateway name and namespace match")
	}
}

func TestFilter_GatewayNamespaceMismatch(t *testing.T) {
	cfg := *baseCfg
	cfg.GatewayName = "my-gateway"
	cfg.GatewayNamespace = "infra"
	// Route references the right gateway name but in a different namespace.
	r := route("app", []string{"app.example.com"}, []gatewayv1.ParentReference{parentRef("my-gateway", "other-ns")}, nil)
	if httproute.Definition().FilterFunc(r, &cfg) {
		t.Error("expected route to be rejected when gateway namespace does not match")
	}
}

func TestFilter_GatewayNamespaceConfigured_RouteHasNoNamespace_Passes(t *testing.T) {
	cfg := *baseCfg
	cfg.GatewayName = "my-gateway"
	cfg.GatewayNamespace = "infra"
	// ParentRef has no namespace set — should not be rejected.
	r := route("app", []string{"app.example.com"}, []gatewayv1.ParentReference{parentRef("my-gateway", "")}, nil)
	if !httproute.Definition().FilterFunc(r, &cfg) {
		t.Error("expected route with no namespace in parentRef to pass when gateway name matches")
	}
}

// --- BuildEntries ---

func TestBuildEntries_SingleHostname(t *testing.T) {
	d := httproute.Definition()
	r := route("home-assistant", []string{"home.example.com"}, nil, nil)
	entries := d.BuildEntries(context.Background(), r, nil, baseCfg)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	key := blueprint.HostnameToKey("home.example.com")
	res, ok := entries[key]
	if !ok {
		t.Fatalf("missing key %q", key)
	}
	if res.Name != "home-assistant" {
		t.Errorf("name = %q, want home-assistant", res.Name)
	}
	if res.FullDomain != "home.example.com" {
		t.Errorf("full-domain = %q, want home.example.com", res.FullDomain)
	}
}

func TestBuildEntries_MultipleHostnames_OneEntryEach(t *testing.T) {
	d := httproute.Definition()
	r := route("app", []string{"a.example.com", "b.example.com"}, nil, nil)
	entries := d.BuildEntries(context.Background(), r, nil, baseCfg)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries for 2 hostnames, got %d", len(entries))
	}
	for _, h := range []string{"a.example.com", "b.example.com"} {
		if _, ok := entries[blueprint.HostnameToKey(h)]; !ok {
			t.Errorf("missing entry for hostname %q", h)
		}
	}
}

func TestBuildEntries_NoHostnames_ReturnsEmpty(t *testing.T) {
	d := httproute.Definition()
	r := route("app", []string{}, nil, nil)
	entries := d.BuildEntries(context.Background(), r, nil, baseCfg)
	if len(entries) != 0 {
		t.Errorf("expected empty entries for route with no hostnames, got %d", len(entries))
	}
}

func TestBuildEntries_NameAnnotationOverride(t *testing.T) {
	d := httproute.Definition()
	r := route("app", []string{"app.example.com"}, nil, map[string]string{
		"newt-sidecar/name": "Custom Name",
	})
	entries := d.BuildEntries(context.Background(), r, nil, baseCfg)
	key := blueprint.HostnameToKey("app.example.com")
	if entries[key].Name != "Custom Name" {
		t.Errorf("name = %q, want %q", entries[key].Name, "Custom Name")
	}
}
