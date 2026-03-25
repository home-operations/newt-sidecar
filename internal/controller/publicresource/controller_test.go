package publicresource

import (
	"testing"

	v1alpha1 "github.com/home-operations/newt-sidecar/api/v1alpha1"
	"github.com/home-operations/newt-sidecar/internal/blueprint"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		spec    v1alpha1.PublicResourceSpec
		wantErr bool
	}{
		{
			name: "valid http with fullDomain",
			spec: v1alpha1.PublicResourceSpec{
				Protocol:   "http",
				FullDomain: "app.example.com",
				Targets:    []v1alpha1.PublicTargetSpec{{Hostname: "svc", Port: 80}},
			},
			wantErr: false,
		},
		{
			name: "http missing fullDomain",
			spec: v1alpha1.PublicResourceSpec{
				Protocol: "http",
				Targets:  []v1alpha1.PublicTargetSpec{{Hostname: "svc", Port: 80}},
			},
			wantErr: true,
		},
		{
			name: "valid tcp with proxyPort",
			spec: v1alpha1.PublicResourceSpec{
				Protocol:  "tcp",
				ProxyPort: 8080,
				Targets:   []v1alpha1.PublicTargetSpec{{Hostname: "svc", Port: 80}},
			},
			wantErr: false,
		},
		{
			name: "tcp missing proxyPort",
			spec: v1alpha1.PublicResourceSpec{
				Protocol: "tcp",
				Targets:  []v1alpha1.PublicTargetSpec{{Hostname: "svc", Port: 80}},
			},
			wantErr: true,
		},
		{
			name: "valid udp with proxyPort",
			spec: v1alpha1.PublicResourceSpec{
				Protocol:  "udp",
				ProxyPort: 9090,
				Targets:   []v1alpha1.PublicTargetSpec{{Hostname: "svc", Port: 53}},
			},
			wantErr: false,
		},
		{
			name: "udp missing proxyPort",
			spec: v1alpha1.PublicResourceSpec{
				Protocol: "udp",
				Targets:  []v1alpha1.PublicTargetSpec{{Hostname: "svc", Port: 53}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(&tt.spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildResource(t *testing.T) {
	siteID := "test-site"

	t.Run("http resource defaults TLSServerName to FullDomain", func(t *testing.T) {
		spec := &v1alpha1.PublicResourceSpec{
			Name:       "my-app",
			Protocol:   "http",
			FullDomain: "app.example.com",
			Ssl:        true,
			Targets: []v1alpha1.PublicTargetSpec{
				{Hostname: "backend.default.svc", Port: 8080, Method: "http"},
			},
		}
		res := buildResource(spec, nil, siteID)

		if res.TLSServerName != "app.example.com" {
			t.Errorf("TLSServerName = %q, want %q", res.TLSServerName, "app.example.com")
		}
		if res.Protocol != "http" {
			t.Errorf("Protocol = %q, want %q", res.Protocol, "http")
		}
		if res.SSL != true {
			t.Error("SSL should be true")
		}
	})

	t.Run("http resource respects explicit TLSServerName", func(t *testing.T) {
		spec := &v1alpha1.PublicResourceSpec{
			Name:          "my-app",
			Protocol:      "http",
			FullDomain:    "app.example.com",
			TlsServerName: "override.example.com",
			Targets: []v1alpha1.PublicTargetSpec{
				{Hostname: "backend", Port: 8080},
			},
		}
		res := buildResource(spec, nil, siteID)

		if res.TLSServerName != "override.example.com" {
			t.Errorf("TLSServerName = %q, want %q", res.TLSServerName, "override.example.com")
		}
	})

	t.Run("tcp resource with proxyPort", func(t *testing.T) {
		spec := &v1alpha1.PublicResourceSpec{
			Name:      "my-tcp",
			Protocol:  "tcp",
			ProxyPort: 3306,
			Targets: []v1alpha1.PublicTargetSpec{
				{Hostname: "mysql.default.svc", Port: 3306},
			},
		}
		res := buildResource(spec, nil, siteID)

		if res.ProxyPort != 3306 {
			t.Errorf("ProxyPort = %d, want %d", res.ProxyPort, 3306)
		}
		if res.FullDomain != "" {
			t.Errorf("FullDomain = %q, want empty", res.FullDomain)
		}
	})

	t.Run("targets include siteID", func(t *testing.T) {
		spec := &v1alpha1.PublicResourceSpec{
			Name:       "my-app",
			Protocol:   "http",
			FullDomain: "app.example.com",
			Targets: []v1alpha1.PublicTargetSpec{
				{Hostname: "svc1", Port: 80},
				{Hostname: "svc2", Port: 8080},
			},
		}
		res := buildResource(spec, nil, siteID)

		if len(res.Targets) != 2 {
			t.Fatalf("Targets len = %d, want 2", len(res.Targets))
		}
		for i, tgt := range res.Targets {
			if tgt.Site != siteID {
				t.Errorf("Targets[%d].Site = %q, want %q", i, tgt.Site, siteID)
			}
		}
	})

	t.Run("auth from secret data", func(t *testing.T) {
		spec := &v1alpha1.PublicResourceSpec{
			Name:       "authed-app",
			Protocol:   "http",
			FullDomain: "secure.example.com",
			Targets: []v1alpha1.PublicTargetSpec{
				{Hostname: "svc", Port: 443},
			},
		}
		secretData := map[string]string{
			"password": "s3cret",
		}
		res := buildResource(spec, secretData, siteID)

		if res.Auth == nil {
			t.Fatal("Auth should not be nil")
		}
		if res.Auth.Password != "s3cret" {
			t.Errorf("Auth.Password = %q, want %q", res.Auth.Password, "s3cret")
		}
	})

	t.Run("nil auth when no auth spec and no secret", func(t *testing.T) {
		spec := &v1alpha1.PublicResourceSpec{
			Name:       "plain-app",
			Protocol:   "http",
			FullDomain: "plain.example.com",
			Targets: []v1alpha1.PublicTargetSpec{
				{Hostname: "svc", Port: 80},
			},
		}
		res := buildResource(spec, nil, siteID)

		if res.Auth != nil {
			t.Error("Auth should be nil when no auth is configured")
		}
	})

	t.Run("headers conversion", func(t *testing.T) {
		spec := &v1alpha1.PublicResourceSpec{
			Name:       "headers-app",
			Protocol:   "http",
			FullDomain: "h.example.com",
			Headers: []v1alpha1.PublicHeaderSpec{
				{Name: "X-Custom", Value: "val1"},
				{Name: "X-Other", Value: "val2"},
			},
			Targets: []v1alpha1.PublicTargetSpec{
				{Hostname: "svc", Port: 80},
			},
		}
		res := buildResource(spec, nil, siteID)

		if len(res.Headers) != 2 {
			t.Fatalf("Headers len = %d, want 2", len(res.Headers))
		}
		if res.Headers[0] != (blueprint.Header{Name: "X-Custom", Value: "val1"}) {
			t.Errorf("Headers[0] = %+v", res.Headers[0])
		}
	})

	t.Run("maintenance conversion", func(t *testing.T) {
		spec := &v1alpha1.PublicResourceSpec{
			Name:       "maint-app",
			Protocol:   "http",
			FullDomain: "m.example.com",
			Maintenance: &v1alpha1.PublicMaintenanceSpec{
				Enabled: true,
				Type:    "forced",
				Title:   "Down for maintenance",
			},
			Targets: []v1alpha1.PublicTargetSpec{
				{Hostname: "svc", Port: 80},
			},
		}
		res := buildResource(spec, nil, siteID)

		if res.Maintenance == nil {
			t.Fatal("Maintenance should not be nil")
		}
		if !res.Maintenance.Enabled {
			t.Error("Maintenance.Enabled should be true")
		}
		if res.Maintenance.Type != "forced" {
			t.Errorf("Maintenance.Type = %q, want %q", res.Maintenance.Type, "forced")
		}
	})

	t.Run("rules conversion", func(t *testing.T) {
		spec := &v1alpha1.PublicResourceSpec{
			Name:       "rules-app",
			Protocol:   "http",
			FullDomain: "r.example.com",
			Rules: []v1alpha1.PublicRuleSpec{
				{Action: "deny", Match: "country", Value: "CN", Priority: 10},
			},
			Targets: []v1alpha1.PublicTargetSpec{
				{Hostname: "svc", Port: 80},
			},
		}
		res := buildResource(spec, nil, siteID)

		if len(res.Rules) != 1 {
			t.Fatalf("Rules len = %d, want 1", len(res.Rules))
		}
		if res.Rules[0].Action != "deny" || res.Rules[0].Value != "CN" {
			t.Errorf("Rules[0] = %+v", res.Rules[0])
		}
	})

	t.Run("target healthcheck conversion", func(t *testing.T) {
		enabled := true
		spec := &v1alpha1.PublicResourceSpec{
			Name:       "hc-app",
			Protocol:   "http",
			FullDomain: "hc.example.com",
			Targets: []v1alpha1.PublicTargetSpec{
				{
					Hostname: "svc",
					Port:     80,
					Healthcheck: &v1alpha1.PublicHealthCheckSpec{
						Hostname: "svc",
						Port:     80,
						Enabled:  &enabled,
						Path:     "/healthz",
					},
				},
			},
		}
		res := buildResource(spec, nil, siteID)

		if res.Targets[0].HealthCheck == nil {
			t.Fatal("HealthCheck should not be nil")
		}
		if res.Targets[0].HealthCheck.Path != "/healthz" {
			t.Errorf("HealthCheck.Path = %q, want %q", res.Targets[0].HealthCheck.Path, "/healthz")
		}
	})
}

func TestBuildAuth(t *testing.T) {
	t.Run("sso with roles", func(t *testing.T) {
		spec := &v1alpha1.PublicAuthSpec{
			SsoEnabled: true,
			SsoRoles:   []string{"User", "Editor"},
		}
		auth := buildAuth(spec, nil)

		if auth == nil {
			t.Fatal("auth should not be nil")
		}
		if !auth.SSOEnabled {
			t.Error("SSOEnabled should be true")
		}
		if len(auth.SSORoles) != 2 {
			t.Errorf("SSORoles len = %d, want 2", len(auth.SSORoles))
		}
	})

	t.Run("basic auth from secret", func(t *testing.T) {
		secretData := map[string]string{
			"basic-auth-user":     "admin",
			"basic-auth-password": "hunter2",
		}
		auth := buildAuth(nil, secretData)

		if auth == nil {
			t.Fatal("auth should not be nil")
		}
		if auth.BasicAuth == nil {
			t.Fatal("BasicAuth should not be nil")
		}
		if auth.BasicAuth.User != "admin" {
			t.Errorf("BasicAuth.User = %q, want %q", auth.BasicAuth.User, "admin")
		}
		if auth.BasicAuth.Password != "hunter2" {
			t.Errorf("BasicAuth.Password = %q, want %q", auth.BasicAuth.Password, "hunter2")
		}
	})

	t.Run("pincode from secret", func(t *testing.T) {
		secretData := map[string]string{
			"pincode": "1234",
		}
		auth := buildAuth(nil, secretData)

		if auth == nil {
			t.Fatal("auth should not be nil")
		}
		if auth.Pincode != 1234 {
			t.Errorf("Pincode = %d, want %d", auth.Pincode, 1234)
		}
	})

	t.Run("nil when nothing set", func(t *testing.T) {
		auth := buildAuth(nil, nil)
		if auth != nil {
			t.Error("auth should be nil when nothing is configured")
		}
	})

	t.Run("nil when spec has all zero values", func(t *testing.T) {
		spec := &v1alpha1.PublicAuthSpec{}
		auth := buildAuth(spec, nil)
		if auth != nil {
			t.Error("auth should be nil when spec has all zero values")
		}
	})
}
