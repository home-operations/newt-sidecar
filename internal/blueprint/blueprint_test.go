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

	r := blueprint.BuildResource("home-assistant", "home.erwanleboucher.dev", nil, nil, cfg)

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
		t.Error("Auth should be nil when auth-sso annotation is absent")
	}
	if r.Enabled != nil {
		t.Error("Enabled should be nil when annotation is absent")
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

	r := blueprint.BuildResource("original-name", "test.example.com", annotations, nil, cfg)

	if r.Name != "Custom Name" {
		t.Errorf("Name = %q, want %q", r.Name, "Custom Name")
	}
	if r.SSL {
		t.Error("SSL should be false after annotation override")
	}
}

func TestBuildResource_Enabled(t *testing.T) {
	cfg := &config.Config{
		SiteID:           "test-site",
		TargetHostname:   "gw.local",
		TargetPort:       443,
		AnnotationPrefix: "newt-sidecar",
	}

	t.Run("enabled true", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", map[string]string{
			"newt-sidecar/enabled": "true",
		}, nil, cfg)
		if r.Enabled == nil || !*r.Enabled {
			t.Error("Enabled should be *true")
		}
	})

	t.Run("enabled false", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", map[string]string{
			"newt-sidecar/enabled": "false",
		}, nil, cfg)
		if r.Enabled == nil || *r.Enabled {
			t.Error("Enabled should be *false")
		}
	})

	t.Run("absent", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", nil, nil, cfg)
		if r.Enabled != nil {
			t.Error("Enabled should be nil when annotation is absent")
		}
	})
}

func TestBuildResource_HostHeader(t *testing.T) {
	cfg := &config.Config{
		SiteID:           "test-site",
		TargetHostname:   "gw.local",
		TargetPort:       443,
		AnnotationPrefix: "newt-sidecar",
	}

	r := blueprint.BuildResource("r", "app.example.com", map[string]string{
		"newt-sidecar/host-header": "custom.internal",
	}, nil, cfg)

	if r.HostHeader != "custom.internal" {
		t.Errorf("HostHeader = %q, want %q", r.HostHeader, "custom.internal")
	}
}

func TestBuildResource_Headers(t *testing.T) {
	cfg := &config.Config{
		SiteID:           "test-site",
		TargetHostname:   "gw.local",
		TargetPort:       443,
		AnnotationPrefix: "newt-sidecar",
	}

	t.Run("valid JSON", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", map[string]string{
			"newt-sidecar/headers": `[{"name":"X-Foo","value":"bar"},{"name":"X-Baz","value":"qux"}]`,
		}, nil, cfg)
		if len(r.Headers) != 2 {
			t.Fatalf("len(Headers) = %d, want 2", len(r.Headers))
		}
		if r.Headers[0].Name != "X-Foo" || r.Headers[0].Value != "bar" {
			t.Errorf("Headers[0] = %+v", r.Headers[0])
		}
		if r.Headers[1].Name != "X-Baz" || r.Headers[1].Value != "qux" {
			t.Errorf("Headers[1] = %+v", r.Headers[1])
		}
	})

	t.Run("invalid JSON silently returns nil", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", map[string]string{
			"newt-sidecar/headers": `not-json`,
		}, nil, cfg)
		if r.Headers != nil {
			t.Error("Headers should be nil on parse error")
		}
	})

	t.Run("absent annotation", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", nil, nil, cfg)
		if r.Headers != nil {
			t.Error("Headers should be nil when annotation absent")
		}
	})
}

func TestBuildResource_Maintenance(t *testing.T) {
	cfg := &config.Config{
		SiteID:           "test-site",
		TargetHostname:   "gw.local",
		TargetPort:       443,
		AnnotationPrefix: "newt-sidecar",
	}

	t.Run("maintenance enabled with all fields", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", map[string]string{
			"newt-sidecar/maintenance-enabled":        "true",
			"newt-sidecar/maintenance-type":           "forced",
			"newt-sidecar/maintenance-title":          "Down for maintenance",
			"newt-sidecar/maintenance-message":        "Back soon",
			"newt-sidecar/maintenance-estimated-time": "2h",
		}, nil, cfg)
		if r.Maintenance == nil {
			t.Fatal("Maintenance should not be nil")
		}
		if !r.Maintenance.Enabled {
			t.Error("Maintenance.Enabled should be true")
		}
		if r.Maintenance.Type != "forced" {
			t.Errorf("Type = %q, want forced", r.Maintenance.Type)
		}
		if r.Maintenance.Title != "Down for maintenance" {
			t.Errorf("Title = %q", r.Maintenance.Title)
		}
		if r.Maintenance.Message != "Back soon" {
			t.Errorf("Message = %q", r.Maintenance.Message)
		}
		if r.Maintenance.EstimatedTime != "2h" {
			t.Errorf("EstimatedTime = %q", r.Maintenance.EstimatedTime)
		}
	})

	t.Run("maintenance absent", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", nil, nil, cfg)
		if r.Maintenance != nil {
			t.Error("Maintenance should be nil when annotation absent")
		}
	})

	t.Run("maintenance-enabled=false", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", map[string]string{
			"newt-sidecar/maintenance-enabled": "false",
		}, nil, cfg)
		if r.Maintenance != nil {
			t.Error("Maintenance should be nil when disabled")
		}
	})

	t.Run("maintenance-enabled typo returns nil", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", map[string]string{
			"newt-sidecar/maintenance-enabled": "ture",
		}, nil, cfg)
		if r.Maintenance != nil {
			t.Error("Maintenance should be nil for a non-truthy value like 'ture'")
		}
	})
}

func TestBuildResource_TLSServerName(t *testing.T) {
	cfg := &config.Config{
		SiteID:           "test-site",
		TargetHostname:   "gw.local",
		TargetPort:       443,
		AnnotationPrefix: "newt-sidecar",
	}

	t.Run("defaults to hostname", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", nil, nil, cfg)
		if r.TLSServerName != "app.example.com" {
			t.Errorf("TLSServerName = %q, want app.example.com", r.TLSServerName)
		}
	})

	t.Run("annotation overrides hostname", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", map[string]string{
			"newt-sidecar/tls-server-name": "backend.internal",
		}, nil, cfg)
		if r.TLSServerName != "backend.internal" {
			t.Errorf("TLSServerName = %q, want backend.internal", r.TLSServerName)
		}
	})
}

func TestBuildResource_TargetExtras(t *testing.T) {
	cfg := &config.Config{
		SiteID:           "test-site",
		TargetHostname:   "gw.local",
		TargetPort:       443,
		AnnotationPrefix: "newt-sidecar",
	}

	t.Run("target-path and path-match", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", map[string]string{
			"newt-sidecar/target-path":       "/api",
			"newt-sidecar/target-path-match": "prefix",
		}, nil, cfg)
		if r.Targets[0].Path != "/api" {
			t.Errorf("Path = %q, want /api", r.Targets[0].Path)
		}
		if r.Targets[0].PathMatch != "prefix" {
			t.Errorf("PathMatch = %q, want prefix", r.Targets[0].PathMatch)
		}
	})

	t.Run("invalid path-match is ignored", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", map[string]string{
			"newt-sidecar/target-path-match": "invalid",
		}, nil, cfg)
		if r.Targets[0].PathMatch != "" {
			t.Errorf("PathMatch should be empty for invalid value, got %q", r.Targets[0].PathMatch)
		}
	})

	t.Run("target-rewrite-path and rewrite-match", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", map[string]string{
			"newt-sidecar/target-rewrite-path":  "/new",
			"newt-sidecar/target-rewrite-match": "stripPrefix",
		}, nil, cfg)
		if r.Targets[0].RewritePath != "/new" {
			t.Errorf("RewritePath = %q, want /new", r.Targets[0].RewritePath)
		}
		if r.Targets[0].RewriteMatch != "stripPrefix" {
			t.Errorf("RewriteMatch = %q, want stripPrefix", r.Targets[0].RewriteMatch)
		}
	})

	t.Run("target-priority valid range", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", map[string]string{
			"newt-sidecar/target-priority": "200",
		}, nil, cfg)
		if r.Targets[0].Priority != 200 {
			t.Errorf("Priority = %d, want 200", r.Targets[0].Priority)
		}
	})

	t.Run("target-priority out of range is ignored", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", map[string]string{
			"newt-sidecar/target-priority": "9999",
		}, nil, cfg)
		if r.Targets[0].Priority != 0 {
			t.Errorf("Priority should be 0 for out-of-range value, got %d", r.Targets[0].Priority)
		}
	})

	t.Run("target-internal-port", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", map[string]string{
			"newt-sidecar/target-internal-port": "8080",
		}, nil, cfg)
		if r.Targets[0].InternalPort != 8080 {
			t.Errorf("InternalPort = %d, want 8080", r.Targets[0].InternalPort)
		}
	})

	t.Run("target-healthcheck JSON", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", map[string]string{
			"newt-sidecar/target-healthcheck": `{"hostname":"backend","port":8080,"path":"/health","interval":30}`,
		}, nil, cfg)
		if r.Targets[0].HealthCheck == nil {
			t.Fatal("HealthCheck should not be nil")
		}
		if r.Targets[0].HealthCheck.Hostname != "backend" {
			t.Errorf("HealthCheck.Hostname = %q, want backend", r.Targets[0].HealthCheck.Hostname)
		}
		if r.Targets[0].HealthCheck.Port != 8080 {
			t.Errorf("HealthCheck.Port = %d, want 8080", r.Targets[0].HealthCheck.Port)
		}
		if r.Targets[0].HealthCheck.Path != "/health" {
			t.Errorf("HealthCheck.Path = %q, want /health", r.Targets[0].HealthCheck.Path)
		}
		if r.Targets[0].HealthCheck.Interval != 30 {
			t.Errorf("HealthCheck.Interval = %d, want 30", r.Targets[0].HealthCheck.Interval)
		}
	})

	t.Run("invalid target-healthcheck JSON is ignored", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", map[string]string{
			"newt-sidecar/target-healthcheck": `not-json`,
		}, nil, cfg)
		if r.Targets[0].HealthCheck != nil {
			t.Error("HealthCheck should be nil on parse error")
		}
	})

	t.Run("no extras — zero values", func(t *testing.T) {
		r := blueprint.BuildResource("r", "app.example.com", nil, nil, cfg)
		tgt := r.Targets[0]
		if tgt.Path != "" || tgt.PathMatch != "" || tgt.RewritePath != "" ||
			tgt.RewriteMatch != "" || tgt.Priority != 0 || tgt.InternalPort != 0 ||
			tgt.HealthCheck != nil {
			t.Errorf("unexpected target extras on resource with no annotations: %+v", tgt)
		}
	})
}

func TestBuildServiceResource_HTTPMode_TLSServerName(t *testing.T) {
	cfg := &config.Config{SiteID: "site-1", AnnotationPrefix: "newt-sidecar"}

	t.Run("defaults to FullDomain", func(t *testing.T) {
		sp := blueprint.ServicePort{
			Name:           "app",
			FullDomain:     "app.example.com",
			Method:         "https",
			TargetPort:     8080,
			TargetHostname: "app.default.svc.cluster.local",
		}
		r := blueprint.BuildServiceResource(sp, cfg)
		if r.TLSServerName != "app.example.com" {
			t.Errorf("TLSServerName = %q, want app.example.com", r.TLSServerName)
		}
	})

	t.Run("overridden by TLSServerName field", func(t *testing.T) {
		sp := blueprint.ServicePort{
			Name:           "app",
			FullDomain:     "app.example.com",
			TLSServerName:  "backend.internal",
			Method:         "https",
			TargetPort:     8080,
			TargetHostname: "app.default.svc.cluster.local",
		}
		r := blueprint.BuildServiceResource(sp, cfg)
		if r.TLSServerName != "backend.internal" {
			t.Errorf("TLSServerName = %q, want backend.internal", r.TLSServerName)
		}
	})
}

func TestBuildServiceResource_HTTPMode_ExtrasForwarded(t *testing.T) {
	cfg := &config.Config{SiteID: "site-1", AnnotationPrefix: "newt-sidecar"}
	hc := &blueprint.HealthCheck{Hostname: "backend", Port: 8080, Path: "/health"}
	sp := blueprint.ServicePort{
		Name:               "app",
		FullDomain:         "app.example.com",
		Method:             "https",
		TargetPort:         8080,
		TargetHostname:     "app.default.svc.cluster.local",
		HostHeader:         "custom.internal",
		Headers:            []blueprint.Header{{Name: "X-Foo", Value: "bar"}},
		TargetPath:         "/api",
		TargetPathMatch:    "prefix",
		TargetRewritePath:  "/",
		TargetRewriteMatch: "stripPrefix",
		TargetPriority:     500,
		TargetInternalPort: 9090,
		TargetHealthCheck:  hc,
	}
	r := blueprint.BuildServiceResource(sp, cfg)

	if r.HostHeader != "custom.internal" {
		t.Errorf("HostHeader = %q, want custom.internal", r.HostHeader)
	}
	if len(r.Headers) != 1 || r.Headers[0].Name != "X-Foo" {
		t.Errorf("Headers = %v", r.Headers)
	}
	tgt := r.Targets[0]
	if tgt.Path != "/api" {
		t.Errorf("Path = %q, want /api", tgt.Path)
	}
	if tgt.PathMatch != "prefix" {
		t.Errorf("PathMatch = %q, want prefix", tgt.PathMatch)
	}
	if tgt.RewritePath != "/" {
		t.Errorf("RewritePath = %q, want /", tgt.RewritePath)
	}
	if tgt.RewriteMatch != "stripPrefix" {
		t.Errorf("RewriteMatch = %q, want stripPrefix", tgt.RewriteMatch)
	}
	if tgt.Priority != 500 {
		t.Errorf("Priority = %d, want 500", tgt.Priority)
	}
	if tgt.InternalPort != 9090 {
		t.Errorf("InternalPort = %d, want 9090", tgt.InternalPort)
	}
	if tgt.HealthCheck == nil || tgt.HealthCheck.Path != "/health" {
		t.Errorf("HealthCheck = %v", tgt.HealthCheck)
	}
}

func TestBuildAuth_SecretData(t *testing.T) {
	cfg := &config.Config{
		AnnotationPrefix: "newt-sidecar",
	}

	t.Run("pincode from secret", func(t *testing.T) {
		auth := blueprint.BuildAuth(map[string]string{}, map[string]string{"pincode": "1234"}, cfg)
		if auth == nil {
			t.Fatal("auth should not be nil")
		}
		if auth.Pincode != 1234 {
			t.Errorf("Pincode = %d, want 1234", auth.Pincode)
		}
	})

	t.Run("password from secret", func(t *testing.T) {
		auth := blueprint.BuildAuth(map[string]string{}, map[string]string{"password": "s3cr3t"}, cfg)
		if auth == nil {
			t.Fatal("auth should not be nil")
		}
		if auth.Password != "s3cr3t" {
			t.Errorf("Password = %q, want s3cr3t", auth.Password)
		}
	})

	t.Run("basic-auth from secret", func(t *testing.T) {
		auth := blueprint.BuildAuth(map[string]string{}, map[string]string{
			"basic-auth-user":     "alice",
			"basic-auth-password": "pass123",
		}, cfg)
		if auth == nil {
			t.Fatal("auth should not be nil")
		}
		if auth.BasicAuth == nil {
			t.Fatal("BasicAuth should not be nil")
		}
		if auth.BasicAuth.User != "alice" {
			t.Errorf("BasicAuth.User = %q, want alice", auth.BasicAuth.User)
		}
		if auth.BasicAuth.Password != "pass123" {
			t.Errorf("BasicAuth.Password = %q, want pass123", auth.BasicAuth.Password)
		}
	})

	t.Run("nil secretData returns nil when no other auth", func(t *testing.T) {
		auth := blueprint.BuildAuth(map[string]string{}, nil, cfg)
		if auth != nil {
			t.Error("auth should be nil when no auth fields set")
		}
	})
}

func TestBuildAuth_WhitelistUsers(t *testing.T) {
	cfg := &config.Config{
		AnnotationPrefix:   "newt-sidecar",
		AuthWhitelistUsers: "global@example.com",
	}

	t.Run("global default", func(t *testing.T) {
		auth := blueprint.BuildAuth(map[string]string{}, nil, cfg)
		if auth == nil {
			t.Fatal("auth should not be nil")
		}
		if len(auth.WhitelistUsers) != 1 || auth.WhitelistUsers[0] != "global@example.com" {
			t.Errorf("WhitelistUsers = %v", auth.WhitelistUsers)
		}
	})

	t.Run("annotation overrides global", func(t *testing.T) {
		auth := blueprint.BuildAuth(map[string]string{
			"newt-sidecar/auth-whitelist-users": "local@example.com,other@example.com",
		}, nil, cfg)
		if auth == nil {
			t.Fatal("auth should not be nil")
		}
		if len(auth.WhitelistUsers) != 2 {
			t.Errorf("WhitelistUsers = %v, want 2 entries", auth.WhitelistUsers)
		}
	})
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
		Rules:          blueprint.BuildRules(map[string]string{}, "newt-sidecar", cfg),
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
	if r.Targets[0].Method != "https" {
		t.Errorf("target method = %q, want https", r.Targets[0].Method)
	}
	if r.Auth != nil {
		t.Error("auth should be nil when Auth field not set on ServicePort")
	}
}

func TestBuildServiceResource_HTTPMode_WithAuth(t *testing.T) {
	cfg := &config.Config{SiteID: "site-1", AnnotationPrefix: "newt-sidecar"}
	sp := blueprint.ServicePort{
		Name:           "app http",
		FullDomain:     "app.example.com",
		Method:         "http",
		SSL:            true,
		TargetPort:     8080,
		TargetHostname: "app.default.svc.cluster.local",
		Auth: &blueprint.Auth{
			SSOEnabled: true,
			SSORoles:   []string{"Member"},
		},
	}
	r := blueprint.BuildServiceResource(sp, cfg)

	if r.Auth == nil {
		t.Fatal("auth should not be nil in HTTP mode when set")
	}
	if !r.Auth.SSOEnabled {
		t.Error("sso-enabled should be true")
	}
	if len(r.Auth.SSORoles) != 1 || r.Auth.SSORoles[0] != "Member" {
		t.Errorf("sso-roles = %v, want [Member]", r.Auth.SSORoles)
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

	r := blueprint.BuildResource("myroute", "myapp.example.com", nil, nil, cfg)

	if len(r.Rules) != 0 {
		t.Errorf("Rules should be empty, got %d rules", len(r.Rules))
	}
}

func TestBuildResource_SSO_AnnotationOnly(t *testing.T) {
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

	r := blueprint.BuildResource("myroute", "myapp.example.com", annotations, nil, cfg)

	if r.Auth == nil {
		t.Fatal("auth should not be nil when auth-sso=true")
	}
	if !r.Auth.SSOEnabled {
		t.Error("sso-enabled should be true")
	}
	if r.Auth.SSORoles != nil {
		t.Errorf("sso-roles should be nil when not set, got %v", r.Auth.SSORoles)
	}
	if r.Auth.SSOUsers != nil {
		t.Errorf("sso-users should be nil when not set, got %v", r.Auth.SSOUsers)
	}
	if r.Auth.AutoLoginIDP != 0 {
		t.Errorf("auto-login-idp should be 0 when not set, got %d", r.Auth.AutoLoginIDP)
	}
}

func TestBuildResource_SSO_AllFields(t *testing.T) {
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
		"newt-sidecar/auth-sso-idp":   "3",
	}

	r := blueprint.BuildResource("myroute", "myapp.example.com", annotations, nil, cfg)

	if r.Auth == nil {
		t.Fatal("auth should not be nil")
	}
	if len(r.Auth.SSORoles) != 2 || r.Auth.SSORoles[0] != "Member" || r.Auth.SSORoles[1] != "Developer" {
		t.Errorf("sso-roles = %v, want [Member Developer]", r.Auth.SSORoles)
	}
	if len(r.Auth.SSOUsers) != 2 {
		t.Errorf("sso-users = %v, want 2 entries", r.Auth.SSOUsers)
	}
	if r.Auth.AutoLoginIDP != 3 {
		t.Errorf("auto-login-idp = %d, want 3", r.Auth.AutoLoginIDP)
	}
}

func TestBuildResource_SSO_GlobalDefaultsOverriddenByAnnotation(t *testing.T) {
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
		"newt-sidecar/auth-sso-roles": "Admin-Custom",
		"newt-sidecar/auth-sso-idp":   "5",
	}

	r := blueprint.BuildResource("myroute", "myapp.example.com", annotations, nil, cfg)

	if r.Auth == nil {
		t.Fatal("auth should not be nil")
	}
	if len(r.Auth.SSORoles) != 1 || r.Auth.SSORoles[0] != "Admin-Custom" {
		t.Errorf("sso-roles = %v, want [Admin-Custom] (annotation should override global)", r.Auth.SSORoles)
	}
	if r.Auth.AutoLoginIDP != 5 {
		t.Errorf("auto-login-idp = %d, want 5 (annotation should override global)", r.Auth.AutoLoginIDP)
	}
}

func TestBuildResource_SSO_Absent(t *testing.T) {
	cfg := &config.Config{
		SiteID:           "test-site",
		TargetHostname:   "gw.local",
		TargetPort:       443,
		AnnotationPrefix: "newt-sidecar",
		AuthSSORoles:     "Member",
	}

	// Global defaults set but annotation not present: auth must remain nil.
	r := blueprint.BuildResource("myroute", "myapp.example.com", nil, nil, cfg)

	if r.Auth != nil {
		t.Error("auth should be nil when auth-sso annotation is absent, even if global defaults are set")
	}
}
