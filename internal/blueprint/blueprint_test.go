package blueprint_test

import (
	"testing"

	"github.com/home-operations/newt-sidecar/internal/blueprint"
	"github.com/home-operations/newt-sidecar/internal/config"
)

func TestHostnameToKey(t *testing.T) {
	tests := []struct {
		hostname string
		want     string
	}{
		{"home.erwanleboucher.dev", "home-erwanleboucher-dev"},
		{"wsflux.erwanleboucher.dev", "wsflux-erwanleboucher-dev"},
		{"simple", "simple"},
	}

	for _, tt := range tests {
		t.Run(tt.hostname, func(t *testing.T) {
			got := blueprint.HostnameToKey(tt.hostname)
			if got != tt.want {
				t.Errorf("HostnameToKey(%q) = %q, want %q", tt.hostname, got, tt.want)
			}
		})
	}
}

func TestBuildResource(t *testing.T) {
	cfg := &config.Config{
		SiteID:           "glistening-desert-rosy-boa",
		TargetHostname:   "kgateway-external.network.svc.cluster.local",
		TargetPort:       443,
		TargetMethod:     "https",
		DenyCountries:    "RU,CN",
		SSL:              true,
		AnnotationPrefix: "newt-sidecar",
	}

	r := blueprint.BuildResource("home-assistant", "home.erwanleboucher.dev", nil, cfg)

	if r.Name != "home-assistant" {
		t.Errorf("Name = %q, want %q", r.Name, "home-assistant")
	}
	if r.Protocol != "http" {
		t.Errorf("Protocol = %q, want %q", r.Protocol, "http")
	}
	if !r.SSL {
		t.Error("SSL should be true")
	}
	if r.FullDomain != "home.erwanleboucher.dev" {
		t.Errorf("FullDomain = %q, want %q", r.FullDomain, "home.erwanleboucher.dev")
	}
	if r.TLSServerName != "home.erwanleboucher.dev" {
		t.Errorf("TLSServerName = %q, want %q", r.TLSServerName, "home.erwanleboucher.dev")
	}
	if len(r.Rules) != 2 {
		t.Errorf("len(Rules) = %d, want 2", len(r.Rules))
	}
	if r.Rules[0].Action != "deny" || r.Rules[0].Match != "country" || r.Rules[0].Value != "RU" {
		t.Errorf("Rules[0] = %+v, want {deny country RU}", r.Rules[0])
	}
	if len(r.Targets) != 1 {
		t.Errorf("len(Targets) = %d, want 1", len(r.Targets))
	}
	if r.Targets[0].Site != "glistening-desert-rosy-boa" {
		t.Errorf("Targets[0].Site = %q, want %q", r.Targets[0].Site, "glistening-desert-rosy-boa")
	}
	if r.Targets[0].Hostname != "kgateway-external.network.svc.cluster.local" {
		t.Errorf("Targets[0].Hostname = %q, want %q", r.Targets[0].Hostname, "kgateway-external.network.svc.cluster.local")
	}
	if r.Targets[0].Port != 443 {
		t.Errorf("Targets[0].Port = %d, want 443", r.Targets[0].Port)
	}
	if r.Auth != nil {
		t.Error("Auth should be nil when newt-sidecar/auth-sso annotation is absent")
	}
}

func TestBuildResource_AnnotationOverrides(t *testing.T) {
	cfg := &config.Config{
		SiteID:           "test-site",
		TargetHostname:   "gw.local",
		TargetPort:       443,
		TargetMethod:     "https",
		SSL:              true,
		AnnotationPrefix: "newt-sidecar",
	}

	annotations := map[string]string{
		"newt-sidecar/name": "Custom Name",
		"newt-sidecar/ssl":  "false",
	}

	r := blueprint.BuildResource("original-name", "test.example.com", annotations, cfg)

	if r.Name != "Custom Name" {
		t.Errorf("Name = %q, want %q", r.Name, "Custom Name")
	}
	if r.SSL {
		t.Error("SSL should be false after annotation override")
	}
}

func TestServiceToKey(t *testing.T) {
	got := blueprint.ServiceToKey("default", "gameserver", "7777", "udp")
	want := "default-gameserver-7777-udp"
	if got != want {
		t.Errorf("ServiceToKey = %q, want %q", got, want)
	}
}

func TestBuildServiceResource_TCPMode(t *testing.T) {
	cfg := &config.Config{SiteID: "site-1", AnnotationPrefix: "newt-sidecar"}
	sp := blueprint.ServicePort{
		Name:           "game-tcp",
		Protocol:       "tcp",
		ProxyPort:      7777,
		TargetPort:     7777,
		TargetHostname: "game.default.svc.cluster.local",
	}
	r := blueprint.BuildServiceResource(sp, cfg)

	if r.Protocol != "tcp" {
		t.Errorf("protocol = %q, want tcp", r.Protocol)
	}
	if r.ProxyPort != 7777 {
		t.Errorf("proxy-port = %d, want 7777", r.ProxyPort)
	}
	if r.FullDomain != "" {
		t.Errorf("full-domain should be empty in TCP mode, got %q", r.FullDomain)
	}
	if r.TLSServerName != "" {
		t.Errorf("tls-server-name should be empty in TCP mode, got %q", r.TLSServerName)
	}
	if len(r.Rules) != 0 {
		t.Errorf("rules should be empty in TCP mode, got %d", len(r.Rules))
	}
	if r.Auth != nil {
		t.Error("auth should be nil in TCP mode")
	}
	if r.Targets[0].Hostname != "game.default.svc.cluster.local" {
		t.Errorf("target hostname = %q, want game.default.svc.cluster.local", r.Targets[0].Hostname)
	}
}

func TestBuildServiceResource_HTTPMode(t *testing.T) {
	cfg := &config.Config{
		SiteID:           "site-1",
		DenyCountries:    "RU",
		AnnotationPrefix: "newt-sidecar",
	}
	sp := blueprint.ServicePort{
		Name:           "app http",
		FullDomain:     "app.example.com",
		Method:         "https",
		SSL:            true,
		TargetPort:     8080,
		TargetHostname: "app.default.svc.cluster.local",
	}
	r := blueprint.BuildServiceResource(sp, cfg)

	if r.Protocol != "http" {
		t.Errorf("protocol = %q, want http", r.Protocol)
	}
	if r.FullDomain != "app.example.com" {
		t.Errorf("full-domain = %q, want app.example.com", r.FullDomain)
	}
	if r.TLSServerName != "app.example.com" {
		t.Errorf("tls-server-name = %q, want app.example.com", r.TLSServerName)
	}
	if r.ProxyPort != 0 {
		t.Errorf("proxy-port should be 0 in HTTP mode, got %d", r.ProxyPort)
	}
	if len(r.Rules) != 1 || r.Rules[0].Value != "RU" {
		t.Errorf("expected deny-country rule for RU, got %v", r.Rules)
	}
	if r.Auth != nil {
		t.Error("auth should be nil when auth-sso annotation is absent")
	}
	if r.Targets[0].Method != "https" {
		t.Errorf("target method = %q, want https", r.Targets[0].Method)
	}
}

func TestBuildResource_NoDenyCountries(t *testing.T) {
	cfg := &config.Config{
		SiteID:           "test-site",
		TargetHostname:   "gw.local",
		TargetPort:       80,
		TargetMethod:     "http",
		SSL:              false,
		AnnotationPrefix: "newt-sidecar",
	}

	r := blueprint.BuildResource("myroute", "myapp.example.com", nil, cfg)

	if len(r.Rules) != 0 {
		t.Errorf("Rules should be empty, got %d rules", len(r.Rules))
	}
}

func TestBuildResource_SSO_Enabled(t *testing.T) {
	cfg := &config.Config{
		SiteID:           "test-site",
		TargetHostname:   "gw.local",
		TargetPort:       443,
		TargetMethod:     "https",
		AnnotationPrefix: "newt-sidecar",
	}
	annotations := map[string]string{
		"newt-sidecar/auth-sso": "true",
	}

	r := blueprint.BuildResource("myroute", "myapp.example.com", annotations, cfg)

	if r.Auth == nil {
		t.Fatal("Auth should not be nil when newt-sidecar/auth-sso: true")
	}
	if !r.Auth.SSOEnabled {
		t.Error("SSOEnabled should be true")
	}
	if len(r.Auth.SSORoles) != 0 {
		t.Errorf("SSORoles should be empty, got %v", r.Auth.SSORoles)
	}
	if len(r.Auth.SSOUsers) != 0 {
		t.Errorf("SSOUsers should be empty, got %v", r.Auth.SSOUsers)
	}
	if r.Auth.AutoLoginIDP != 0 {
		t.Errorf("AutoLoginIDP should be 0, got %d", r.Auth.AutoLoginIDP)
	}
}

func TestBuildResource_SSO_WithAllFields(t *testing.T) {
	cfg := &config.Config{
		SiteID:           "test-site",
		TargetHostname:   "gw.local",
		TargetPort:       443,
		TargetMethod:     "https",
		AnnotationPrefix: "newt-sidecar",
	}
	annotations := map[string]string{
		"newt-sidecar/auth-sso":       "true",
		"newt-sidecar/auth-sso-roles": "Member,Developer",
		"newt-sidecar/auth-sso-users": "alice@example.com,bob@example.com",
		"newt-sidecar/auth-sso-idp":   "2",
	}

	r := blueprint.BuildResource("myroute", "myapp.example.com", annotations, cfg)

	if r.Auth == nil {
		t.Fatal("Auth should not be nil")
	}
	if len(r.Auth.SSORoles) != 2 || r.Auth.SSORoles[0] != "Member" || r.Auth.SSORoles[1] != "Developer" {
		t.Errorf("SSORoles = %v, want [Member Developer]", r.Auth.SSORoles)
	}
	if len(r.Auth.SSOUsers) != 2 || r.Auth.SSOUsers[0] != "alice@example.com" {
		t.Errorf("SSOUsers = %v, want [alice@example.com bob@example.com]", r.Auth.SSOUsers)
	}
	if r.Auth.AutoLoginIDP != 2 {
		t.Errorf("AutoLoginIDP = %d, want 2", r.Auth.AutoLoginIDP)
	}
}

func TestBuildResource_SSO_GlobalFlagDefaults(t *testing.T) {
	cfg := &config.Config{
		SiteID:           "test-site",
		TargetHostname:   "gw.local",
		TargetPort:       443,
		TargetMethod:     "https",
		AnnotationPrefix: "newt-sidecar",
		AuthSSORoles:     "Member",
		AuthSSOIDP:       1,
	}
	annotations := map[string]string{
		"newt-sidecar/auth-sso": "true",
	}

	r := blueprint.BuildResource("myroute", "myapp.example.com", annotations, cfg)

	if r.Auth == nil {
		t.Fatal("Auth should not be nil")
	}
	if len(r.Auth.SSORoles) != 1 || r.Auth.SSORoles[0] != "Member" {
		t.Errorf("SSORoles = %v, want [Member] from global flag", r.Auth.SSORoles)
	}
	if r.Auth.AutoLoginIDP != 1 {
		t.Errorf("AutoLoginIDP = %d, want 1 from global flag", r.Auth.AutoLoginIDP)
	}
}

func TestBuildResource_SSO_AnnotationOverridesGlobalFlag(t *testing.T) {
	cfg := &config.Config{
		SiteID:           "test-site",
		TargetHostname:   "gw.local",
		TargetPort:       443,
		TargetMethod:     "https",
		AnnotationPrefix: "newt-sidecar",
		AuthSSORoles:     "Member",
		AuthSSOIDP:       1,
	}
	annotations := map[string]string{
		"newt-sidecar/auth-sso":       "true",
		"newt-sidecar/auth-sso-roles": "Admin",
		"newt-sidecar/auth-sso-idp":   "5",
	}

	r := blueprint.BuildResource("myroute", "myapp.example.com", annotations, cfg)

	if r.Auth == nil {
		t.Fatal("Auth should not be nil")
	}
	if len(r.Auth.SSORoles) != 1 || r.Auth.SSORoles[0] != "Admin" {
		t.Errorf("SSORoles = %v, want [Admin] from annotation override", r.Auth.SSORoles)
	}
	if r.Auth.AutoLoginIDP != 5 {
		t.Errorf("AutoLoginIDP = %d, want 5 from annotation override", r.Auth.AutoLoginIDP)
	}
}

func TestBuildServiceResource_HTTPMode_SSO(t *testing.T) {
	cfg := &config.Config{
		SiteID:           "site-1",
		AnnotationPrefix: "newt-sidecar",
	}
	sp := blueprint.ServicePort{
		Name:           "app http",
		FullDomain:     "app.example.com",
		Method:         "http",
		SSL:            true,
		TargetPort:     8080,
		TargetHostname: "app.default.svc.cluster.local",
		Annotations: map[string]string{
			"newt-sidecar/auth-sso":       "true",
			"newt-sidecar/auth-sso-roles": "Member",
			"newt-sidecar/auth-sso-idp":   "1",
		},
	}
	r := blueprint.BuildServiceResource(sp, cfg)

	if r.Auth == nil {
		t.Fatal("Auth should not be nil in HTTP mode with auth-sso annotation")
	}
	if !r.Auth.SSOEnabled {
		t.Error("SSOEnabled should be true")
	}
	if len(r.Auth.SSORoles) != 1 || r.Auth.SSORoles[0] != "Member" {
		t.Errorf("SSORoles = %v, want [Member]", r.Auth.SSORoles)
	}
	if r.Auth.AutoLoginIDP != 1 {
		t.Errorf("AutoLoginIDP = %d, want 1", r.Auth.AutoLoginIDP)
	}
}

func TestBuildServiceResource_TCPMode_NoAuth(t *testing.T) {
	cfg := &config.Config{SiteID: "site-1", AnnotationPrefix: "newt-sidecar"}
	sp := blueprint.ServicePort{
		Name:           "game-tcp",
		Protocol:       "tcp",
		ProxyPort:      7777,
		TargetPort:     7777,
		TargetHostname: "game.default.svc.cluster.local",
		// No Annotations field: auth must never be emitted for TCP/UDP.
	}
	r := blueprint.BuildServiceResource(sp, cfg)

	if r.Auth != nil {
		t.Error("auth must be nil for TCP resources")
	}
}
