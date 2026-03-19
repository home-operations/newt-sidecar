package config

import (
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid: gateway mode",
			cfg: Config{
				SiteID:         "site-1",
				GatewayName:    "my-gw",
				TargetHostname: "gw.internal",
			},
			wantErr: false,
		},
		{
			name: "valid: enable-service only",
			cfg: Config{
				SiteID:        "site-1",
				EnableService: true,
			},
			wantErr: false,
		},
		{
			name: "valid: auto-service only",
			cfg: Config{
				SiteID:      "site-1",
				AutoService: true,
			},
			wantErr: false,
		},
		{
			name: "valid: gateway + enable-service",
			cfg: Config{
				SiteID:         "site-1",
				GatewayName:    "my-gw",
				TargetHostname: "gw.internal",
				EnableService:  true,
			},
			wantErr: false,
		},
		{
			name:    "invalid: missing site-id",
			cfg:     Config{GatewayName: "my-gw", TargetHostname: "gw.internal"},
			wantErr: true,
		},
		{
			name:    "invalid: gateway-name set but no target-hostname",
			cfg:     Config{SiteID: "site-1", GatewayName: "my-gw"},
			wantErr: true,
		},
		{
			name:    "invalid: no mode selected",
			cfg:     Config{SiteID: "site-1"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewConfig_Defaults(t *testing.T) {
	cfg, err := newConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Output != "/etc/newt/blueprint.yaml" {
		t.Errorf("Output = %q, want %q", cfg.Output, "/etc/newt/blueprint.yaml")
	}
	if cfg.TargetPort != 443 {
		t.Errorf("TargetPort = %d, want 443", cfg.TargetPort)
	}
	if cfg.TargetMethod != "https" {
		t.Errorf("TargetMethod = %q, want %q", cfg.TargetMethod, "https")
	}
	if !cfg.SSL {
		t.Error("SSL = false, want true")
	}
	if cfg.AnnotationPrefix != "newt-sidecar" {
		t.Errorf("AnnotationPrefix = %q, want %q", cfg.AnnotationPrefix, "newt-sidecar")
	}
	if cfg.HealthPort != 8080 {
		t.Errorf("HealthPort = %d, want 8080", cfg.HealthPort)
	}
}

func TestNewConfig_EnvOverridesDefaults(t *testing.T) {
	t.Setenv("NEWTSC_SITE_ID", "my-site")
	t.Setenv("NEWTSC_GATEWAY_NAME", "my-gw")
	t.Setenv("NEWTSC_GATEWAY_NAMESPACE", "network")
	t.Setenv("NEWTSC_NAMESPACE", "default")
	t.Setenv("NEWTSC_OUTPUT", "/tmp/blueprint.yaml")
	t.Setenv("NEWTSC_TARGET_HOSTNAME", "gw.internal")
	t.Setenv("NEWTSC_TARGET_PORT", "8443")
	t.Setenv("NEWTSC_TARGET_METHOD", "h2c")
	t.Setenv("NEWTSC_DENY_COUNTRIES", "RU,CN")
	t.Setenv("NEWTSC_SSL", "false")
	t.Setenv("NEWTSC_ANNOTATION_PREFIX", "my-prefix")
	t.Setenv("NEWTSC_ENABLE_SERVICE", "true")
	t.Setenv("NEWTSC_AUTO_SERVICE", "true")
	t.Setenv("NEWTSC_ALL_PORTS", "true")
	t.Setenv("NEWTSC_AUTH_SSO_ROLES", "Member,Admin")
	t.Setenv("NEWTSC_AUTH_SSO_USERS", "user@example.com")
	t.Setenv("NEWTSC_AUTH_SSO_IDP", "3")
	t.Setenv("NEWTSC_AUTH_WHITELIST_USERS", "other@example.com")
	t.Setenv("NEWTSC_HEALTH_PORT", "9090")

	cfg, err := newConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"SiteID", cfg.SiteID, "my-site"},
		{"GatewayName", cfg.GatewayName, "my-gw"},
		{"GatewayNamespace", cfg.GatewayNamespace, "network"},
		{"Namespace", cfg.Namespace, "default"},
		{"Output", cfg.Output, "/tmp/blueprint.yaml"},
		{"TargetHostname", cfg.TargetHostname, "gw.internal"},
		{"TargetPort", cfg.TargetPort, 8443},
		{"TargetMethod", cfg.TargetMethod, "h2c"},
		{"DenyCountries", cfg.DenyCountries, "RU,CN"},
		{"SSL", cfg.SSL, false},
		{"AnnotationPrefix", cfg.AnnotationPrefix, "my-prefix"},
		{"EnableService", cfg.EnableService, true},
		{"AutoService", cfg.AutoService, true},
		{"AllPorts", cfg.AllPorts, true},
		{"AuthSSORoles", cfg.AuthSSORoles, "Member,Admin"},
		{"AuthSSOUsers", cfg.AuthSSOUsers, "user@example.com"},
		{"AuthSSOIDP", cfg.AuthSSOIDP, 3},
		{"AuthWhitelistUsers", cfg.AuthWhitelistUsers, "other@example.com"},
		{"HealthPort", cfg.HealthPort, 9090},
	}

	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestNewConfig_PartialEnv(t *testing.T) {
	t.Setenv("NEWTSC_SITE_ID", "partial-site")
	t.Setenv("NEWTSC_TARGET_PORT", "80")

	cfg, err := newConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.SiteID != "partial-site" {
		t.Errorf("SiteID = %q, want %q", cfg.SiteID, "partial-site")
	}
	if cfg.TargetPort != 80 {
		t.Errorf("TargetPort = %d, want 80", cfg.TargetPort)
	}
	// Unset fields should retain their hard-coded defaults.
	if cfg.TargetMethod != "https" {
		t.Errorf("TargetMethod = %q, want %q (default)", cfg.TargetMethod, "https")
	}
	if cfg.HealthPort != 8080 {
		t.Errorf("HealthPort = %d, want 8080 (default)", cfg.HealthPort)
	}
}
