package config

import (
	"flag"
	"fmt"
)

// Config holds all runtime configuration loaded from CLI flags.
type Config struct {
	// HTTPRoute options
	GatewayName      string
	GatewayNamespace string

	// Shared options
	Namespace        string
	Output           string
	SiteID           string
	TargetHostname   string
	TargetPort       int
	TargetMethod     string
	DenyCountries    string
	SSL              bool
	AnnotationPrefix string

	// Service discovery options
	EnableService bool
	AutoService   bool
	AllPorts      bool

	// SSO auth defaults (per-resource annotation always overrides)
	AuthSSORoles       string
	AuthSSOUsers       string
	AuthSSOIDP         int
	AuthWhitelistUsers string

	// Health server port (0 = disabled)
	HealthPort int
}

// Load parses CLI flags and returns a populated Config.
func Load() *Config {
	cfg := &Config{}

	// HTTPRoute flags
	flag.StringVar(&cfg.GatewayName, "gateway-name", "", "Gateway name to filter HTTPRoutes (required for HTTPRoute mode)")
	flag.StringVar(&cfg.GatewayNamespace, "gateway-namespace", "", "Gateway namespace (empty = any)")

	// Shared flags
	flag.StringVar(&cfg.Namespace, "namespace", "", "Watch namespace (empty = all)")
	flag.StringVar(&cfg.Output, "output", "/etc/newt/blueprint.yaml", "Output blueprint file path")
	flag.StringVar(&cfg.SiteID, "site-id", "", "Pangolin site nice ID (required)")
	flag.StringVar(&cfg.TargetHostname, "target-hostname", "", "Backend gateway hostname (required for HTTPRoute mode)")
	flag.IntVar(&cfg.TargetPort, "target-port", 443, "Backend gateway port")
	flag.StringVar(&cfg.TargetMethod, "target-method", "https", "Backend method (http/https/h2c)")
	flag.StringVar(&cfg.DenyCountries, "deny-countries", "", "Comma-separated country codes to deny")
	flag.BoolVar(&cfg.SSL, "ssl", true, "Enable SSL on HTTP resources")
	flag.StringVar(&cfg.AnnotationPrefix, "annotation-prefix", "newt-sidecar", "Annotation prefix for per-resource overrides")

	// Service discovery flags
	flag.BoolVar(&cfg.EnableService, "enable-service", false, "Enable Service discovery (annotation-mode: opt-in via newt-sidecar/enabled: true)")
	flag.BoolVar(&cfg.AutoService, "auto-service", false, "Enable Service discovery (auto-mode: opt-out via newt-sidecar/enabled: false)")
	flag.BoolVar(&cfg.AllPorts, "all-ports", false, "Expose all TCP/UDP ports of a Service as individual blueprint entries")

	// SSO auth flags (cluster-wide defaults; per-resource annotation always wins).
	// There is deliberately no --auth-sso flag: SSO must be enabled per resource
	// via the newt-sidecar/auth-sso annotation.
	flag.StringVar(&cfg.AuthSSORoles, "auth-sso-roles", "", "Default comma-separated Pangolin roles for SSO-enabled resources (empty = none)")
	flag.StringVar(&cfg.AuthSSOUsers, "auth-sso-users", "", "Default comma-separated user e-mails for SSO-enabled resources (empty = none)")
	flag.IntVar(&cfg.AuthSSOIDP, "auth-sso-idp", 0, "Default Pangolin IdP ID for auto-login-idp (0 = not set)")
	flag.StringVar(&cfg.AuthWhitelistUsers, "auth-whitelist-users", "", "Default comma-separated user e-mails for whitelist-users (empty = none)")

	flag.IntVar(&cfg.HealthPort, "health-port", 8080, "Port for the health/readiness HTTP server (0 = disabled)")

	flag.Parse()

	return cfg
}

func (c *Config) Validate() error {
	if c.SiteID == "" {
		return fmt.Errorf("--site-id is required")
	}
	if c.GatewayName != "" && c.TargetHostname == "" {
		return fmt.Errorf("--target-hostname is required when --gateway-name is set")
	}
	if c.GatewayName == "" && !c.EnableService && !c.AutoService {
		return fmt.Errorf("at least one of --gateway-name, --enable-service, or --auto-service must be set")
	}
	return nil
}
