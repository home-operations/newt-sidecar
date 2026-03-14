package service_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/home-operations/newt-sidecar/internal/blueprint"
	"github.com/home-operations/newt-sidecar/internal/config"
	"github.com/home-operations/newt-sidecar/internal/resources/service"
)

var baseCfg = &config.Config{
	AnnotationPrefix: "newt-sidecar",
	SiteID:           "test-site",
	TargetHostname:   "gw.local",
	TargetPort:       443,
}

func buildEntries(svc *corev1.Service, cfg *config.Config) map[string]blueprint.Resource {
	def := service.Definition()
	return def.BuildEntries(svc, cfg)
}

func svc(name string, ports []corev1.ServicePort, annotations map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{Ports: ports},
	}
}

func port(name string, p int32, proto corev1.Protocol) corev1.ServicePort {
	return corev1.ServicePort{Name: name, Port: p, Protocol: proto}
}

// --- Port selection ---

func TestBuildEntries_SinglePort(t *testing.T) {
	entries := buildEntries(svc("app", []corev1.ServicePort{port("", 8080, corev1.ProtocolTCP)}, nil), baseCfg)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	key := blueprint.ServiceToKey("default", "app", "8080", "tcp")
	r, ok := entries[key]
	if !ok {
		t.Fatalf("missing expected key %q", key)
	}
	if r.Protocol != "tcp" || r.ProxyPort != 8080 {
		t.Errorf("got protocol=%q proxyPort=%d, want tcp/8080", r.Protocol, r.ProxyPort)
	}
}

func TestBuildEntries_MultiPort_FallsBackToHTTPPort(t *testing.T) {
	ports := []corev1.ServicePort{
		port("grpc", 9090, corev1.ProtocolTCP),
		port("http", 8080, corev1.ProtocolTCP),
	}
	entries := buildEntries(svc("app", ports, nil), baseCfg)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	key := blueprint.ServiceToKey("default", "app", "8080", "tcp")
	if _, ok := entries[key]; !ok {
		t.Errorf("expected key for port named 'http' (%q), got keys: %v", key, keys(entries))
	}
}

func TestBuildEntries_MultiPort_NoHTTPPort_ReturnsNil(t *testing.T) {
	ports := []corev1.ServicePort{
		port("grpc", 9090, corev1.ProtocolTCP),
		port("metrics", 9091, corev1.ProtocolTCP),
	}
	entries := buildEntries(svc("app", ports, nil), baseCfg)
	if entries != nil {
		t.Errorf("expected nil for ambiguous multi-port service, got %v", entries)
	}
}

func TestBuildEntries_PortAnnotation_ByNumber(t *testing.T) {
	ports := []corev1.ServicePort{
		port("grpc", 9090, corev1.ProtocolTCP),
		port("http", 8080, corev1.ProtocolTCP),
	}
	annotations := map[string]string{"newt-sidecar/port": "9090"}
	entries := buildEntries(svc("app", ports, annotations), baseCfg)
	key := blueprint.ServiceToKey("default", "app", "9090", "tcp")
	if _, ok := entries[key]; !ok {
		t.Errorf("expected port 9090 to be selected, got keys: %v", keys(entries))
	}
}

func TestBuildEntries_PortAnnotation_ByName(t *testing.T) {
	ports := []corev1.ServicePort{
		port("grpc", 9090, corev1.ProtocolTCP),
		port("http", 8080, corev1.ProtocolTCP),
	}
	annotations := map[string]string{"newt-sidecar/port": "grpc"}
	entries := buildEntries(svc("app", ports, annotations), baseCfg)
	key := blueprint.ServiceToKey("default", "app", "9090", "tcp")
	if _, ok := entries[key]; !ok {
		t.Errorf("expected port named 'grpc' to be selected, got keys: %v", keys(entries))
	}
}

func TestBuildEntries_PortAnnotation_NoMatch_ReturnsNil(t *testing.T) {
	ports := []corev1.ServicePort{port("http", 8080, corev1.ProtocolTCP)}
	annotations := map[string]string{"newt-sidecar/port": "9999"}
	entries := buildEntries(svc("app", ports, annotations), baseCfg)
	if entries != nil {
		t.Errorf("expected nil for unmatched port annotation, got %v", entries)
	}
}

// --- Protocol ---

func TestBuildEntries_UDPPort(t *testing.T) {
	entries := buildEntries(svc("game", []corev1.ServicePort{port("game", 7777, corev1.ProtocolUDP)}, nil), baseCfg)
	key := blueprint.ServiceToKey("default", "game", "7777", "udp")
	r, ok := entries[key]
	if !ok {
		t.Fatalf("expected UDP entry at key %q, got keys: %v", key, keys(entries))
	}
	if r.Protocol != "udp" {
		t.Errorf("protocol = %q, want udp", r.Protocol)
	}
}

func TestBuildEntries_ProtocolAnnotationOverride(t *testing.T) {
	annotations := map[string]string{"newt-sidecar/protocol": "udp"}
	entries := buildEntries(svc("app", []corev1.ServicePort{port("data", 5000, corev1.ProtocolTCP)}, annotations), baseCfg)
	key := blueprint.ServiceToKey("default", "app", "5000", "udp")
	if _, ok := entries[key]; !ok {
		t.Errorf("protocol annotation override not applied; got keys: %v", keys(entries))
	}
}

// --- All-ports mode ---

func TestBuildEntries_AllPorts_GlobalFlag(t *testing.T) {
	cfg := *baseCfg
	cfg.AllPorts = true
	ports := []corev1.ServicePort{
		port("http", 8080, corev1.ProtocolTCP),
		port("metrics", 9090, corev1.ProtocolTCP),
	}
	entries := buildEntries(svc("app", ports, nil), &cfg)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries in all-ports mode, got %d", len(entries))
	}
}

func TestBuildEntries_AllPorts_Annotation_OverridesFlag(t *testing.T) {
	cfg := *baseCfg
	cfg.AllPorts = false
	ports := []corev1.ServicePort{
		port("http", 8080, corev1.ProtocolTCP),
		port("grpc", 9090, corev1.ProtocolTCP),
	}
	annotations := map[string]string{"newt-sidecar/all-ports": "true"}
	entries := buildEntries(svc("app", ports, annotations), &cfg)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries when all-ports annotation overrides flag, got %d", len(entries))
	}
}

func TestBuildEntries_AllPorts_Annotation_DisablesGlobalFlag(t *testing.T) {
	cfg := *baseCfg
	cfg.AllPorts = true
	ports := []corev1.ServicePort{
		port("http", 8080, corev1.ProtocolTCP),
		port("grpc", 9090, corev1.ProtocolTCP),
	}
	annotations := map[string]string{"newt-sidecar/all-ports": "false"}
	entries := buildEntries(svc("app", ports, annotations), &cfg)
	if len(entries) != 1 {
		t.Errorf("expected 1 entry when all-ports annotation=false overrides flag, got %d", len(entries))
	}
}

func TestBuildEntries_AllPorts_TCPAndUDP_SamePortNumber(t *testing.T) {
	cfg := *baseCfg
	cfg.AllPorts = true
	ports := []corev1.ServicePort{
		port("tcp", 7777, corev1.ProtocolTCP),
		port("udp", 7777, corev1.ProtocolUDP),
	}
	entries := buildEntries(svc("game", ports, nil), &cfg)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries for same port with different protocols, got %d: %v", len(entries), keys(entries))
	}
}

func TestBuildEntries_AllPorts_UnnamedPort_UsesNumber(t *testing.T) {
	cfg := *baseCfg
	cfg.AllPorts = true
	ports := []corev1.ServicePort{
		{Port: 8080, Protocol: corev1.ProtocolTCP},
	}
	entries := buildEntries(svc("app", ports, nil), &cfg)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	for _, r := range entries {
		if r.Name != "app-8080" {
			t.Errorf("display name = %q, want %q", r.Name, "app-8080")
		}
	}
}

// --- HTTP mode ---

func TestBuildEntries_HTTPMode_FullDomain(t *testing.T) {
	annotations := map[string]string{
		"newt-sidecar/full-domain": "app.example.com",
	}
	entries := buildEntries(svc("app", []corev1.ServicePort{port("http", 8080, corev1.ProtocolTCP)}, annotations), baseCfg)
	key := blueprint.HostnameToKey("app.example.com")
	r, ok := entries[key]
	if !ok {
		t.Fatalf("expected key %q, got keys: %v", key, keys(entries))
	}
	if r.Protocol != "http" {
		t.Errorf("protocol = %q, want http", r.Protocol)
	}
	if r.FullDomain != "app.example.com" {
		t.Errorf("full-domain = %q, want app.example.com", r.FullDomain)
	}
	if r.Name != "app-http" {
		t.Errorf("display name = %q, want app-http", r.Name)
	}
}

func TestBuildEntries_HTTPMode_DisplayName_UnnamedPort(t *testing.T) {
	// Single unnamed port in HTTP mode: name should be "<svc>-<port number>".
	annotations := map[string]string{
		"newt-sidecar/full-domain": "app.example.com",
	}
	ports := []corev1.ServicePort{{Port: 4000, Protocol: corev1.ProtocolTCP}}
	entries := buildEntries(svc("app", ports, annotations), baseCfg)
	r := entries[blueprint.HostnameToKey("app.example.com")]
	if r.Name != "app-4000" {
		t.Errorf("display name = %q, want app-4000", r.Name)
	}
}

func TestBuildEntries_HTTPMode_MethodAnnotation(t *testing.T) {
	annotations := map[string]string{
		"newt-sidecar/full-domain": "app.example.com",
		"newt-sidecar/method":      "h2c",
	}
	entries := buildEntries(svc("app", []corev1.ServicePort{port("http", 8080, corev1.ProtocolTCP)}, annotations), baseCfg)
	r := entries[blueprint.HostnameToKey("app.example.com")]
	if len(r.Targets) == 0 || r.Targets[0].Method != "h2c" {
		t.Errorf("target method = %q, want h2c", r.Targets[0].Method)
	}
}

func TestBuildEntries_HTTPMode_SSLAnnotationOverridesCfg(t *testing.T) {
	cfg := *baseCfg
	cfg.SSL = false
	annotations := map[string]string{
		"newt-sidecar/full-domain": "app.example.com",
		"newt-sidecar/ssl":         "true",
	}
	entries := buildEntries(svc("app", []corev1.ServicePort{port("http", 8080, corev1.ProtocolTCP)}, annotations), &cfg)
	r := entries[blueprint.HostnameToKey("app.example.com")]
	if !r.SSL {
		t.Error("SSL should be true from annotation override")
	}
}

func TestBuildEntries_NameAnnotationOverride(t *testing.T) {
	annotations := map[string]string{"newt-sidecar/name": "My App"}
	entries := buildEntries(svc("app", []corev1.ServicePort{port("http", 8080, corev1.ProtocolTCP)}, annotations), baseCfg)
	for _, r := range entries {
		if r.Name != "My App" {
			t.Errorf("name = %q, want %q", r.Name, "My App")
		}
	}
}

// --- all-ports + full-domain interaction ---

func TestBuildEntries_FullDomain_TakesPrecedenceOverAllPorts(t *testing.T) {
	// --all-ports is set globally but full-domain annotation is present:
	// must produce a single HTTP entry, not multiple TCP entries.
	cfg := *baseCfg
	cfg.AllPorts = true
	ports := []corev1.ServicePort{
		port("http", 8080, corev1.ProtocolTCP),
		port("metrics", 9090, corev1.ProtocolTCP),
	}
	annotations := map[string]string{
		"newt-sidecar/full-domain": "app.example.com",
	}
	entries := buildEntries(svc("app", ports, annotations), &cfg)
	if len(entries) != 1 {
		t.Fatalf("expected 1 HTTP entry when full-domain overrides all-ports, got %d: %v", len(entries), keys(entries))
	}
	r := entries[blueprint.HostnameToKey("app.example.com")]
	if r.Protocol != "http" {
		t.Errorf("protocol = %q, want http", r.Protocol)
	}
}

// --- SSO auth ---

func TestBuildEntries_HTTPMode_SSO_Enabled(t *testing.T) {
	annotations := map[string]string{
		"newt-sidecar/full-domain": "app.example.com",
		"newt-sidecar/auth-sso":   "true",
	}
	entries := buildEntries(svc("app", []corev1.ServicePort{port("http", 8080, corev1.ProtocolTCP)}, annotations), baseCfg)
	r := entries[blueprint.HostnameToKey("app.example.com")]
	if r.Auth == nil {
		t.Fatal("auth should not be nil when auth-sso=true")
	}
	if !r.Auth.SSOEnabled {
		t.Error("sso-enabled should be true")
	}
}

func TestBuildEntries_HTTPMode_SSO_AllFields(t *testing.T) {
	annotations := map[string]string{
		"newt-sidecar/full-domain":    "app.example.com",
		"newt-sidecar/auth-sso":       "true",
		"newt-sidecar/auth-sso-roles": "Member,Developer",
		"newt-sidecar/auth-sso-users": "alice@example.com",
		"newt-sidecar/auth-sso-idp":   "2",
	}
	entries := buildEntries(svc("app", []corev1.ServicePort{port("http", 8080, corev1.ProtocolTCP)}, annotations), baseCfg)
	r := entries[blueprint.HostnameToKey("app.example.com")]
	if r.Auth == nil {
		t.Fatal("auth should not be nil")
	}
	if len(r.Auth.SSORoles) != 2 {
		t.Errorf("sso-roles = %v, want 2 entries", r.Auth.SSORoles)
	}
	if len(r.Auth.SSOUsers) != 1 || r.Auth.SSOUsers[0] != "alice@example.com" {
		t.Errorf("sso-users = %v, want [alice@example.com]", r.Auth.SSOUsers)
	}
	if r.Auth.AutoLoginIDP != 2 {
		t.Errorf("auto-login-idp = %d, want 2", r.Auth.AutoLoginIDP)
	}
}

func TestBuildEntries_HTTPMode_SSO_GlobalDefault(t *testing.T) {
	cfg := *baseCfg
	cfg.AuthSSORoles = "Member"
	cfg.AuthSSOIDP = 1
	annotations := map[string]string{
		"newt-sidecar/full-domain": "app.example.com",
		"newt-sidecar/auth-sso":   "true",
	}
	entries := buildEntries(svc("app", []corev1.ServicePort{port("http", 8080, corev1.ProtocolTCP)}, annotations), &cfg)
	r := entries[blueprint.HostnameToKey("app.example.com")]
	if r.Auth == nil {
		t.Fatal("auth should not be nil")
	}
	if len(r.Auth.SSORoles) != 1 || r.Auth.SSORoles[0] != "Member" {
		t.Errorf("sso-roles = %v, want [Member] from global default", r.Auth.SSORoles)
	}
	if r.Auth.AutoLoginIDP != 1 {
		t.Errorf("auto-login-idp = %d, want 1 from global default", r.Auth.AutoLoginIDP)
	}
}

func TestBuildEntries_TCPMode_NoAuth(t *testing.T) {
	// SSO annotation set but resource is TCP: auth must be absent.
	annotations := map[string]string{
		"newt-sidecar/auth-sso": "true",
	}
	entries := buildEntries(svc("db", []corev1.ServicePort{port("postgres", 5432, corev1.ProtocolTCP)}, annotations), baseCfg)
	key := blueprint.ServiceToKey("default", "db", "5432", "tcp")
	r, ok := entries[key]
	if !ok {
		t.Fatalf("expected TCP entry at key %q", key)
	}
	if r.Auth != nil {
		t.Error("auth must be nil for TCP resources")
	}
}

func keys(m map[string]blueprint.Resource) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
