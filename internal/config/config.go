package config

import (
	"flag"
	"fmt"

	"github.com/caarlos0/env/v11"
)

// Config holds all runtime configuration loaded from CLI flags or environment variables.
// Environment variables are used as defaults; CLI flags take precedence.
type Config struct {
	// HTTPRoute options
	GatewayName      string `env:"NEWTSC_GATEWAY_NAME"`
	GatewayNamespace string `env:"NEWTSC_GATEWAY_NAMESPACE"`

	// Shared options
	Namespace        string `env:"NEWTSC_NAMESPACE"`
	Output           string `env:"NEWTSC_OUTPUT"`
	SiteID           string `env:"NEWTSC_SITE_ID"`
	TargetHostname   string `env:"NEWTSC_TARGET_HOSTNAME"`
	TargetPort       int    `env:"NEWTSC_TARGET_PORT"`
	TargetMethod     string `env:"NEWTSC_TARGET_METHOD"`
	DenyCountries    string `env:"NEWTSC_DENY_COUNTRIES"`
	SSL              bool   `env:"NEWTSC_SSL"`
	AnnotationPrefix string `env:"NEWTSC_ANNOTATION_PREFIX"`

	// Service discovery options
	EnableService bool `env:"NEWTSC_ENABLE_SERVICE"`
	AutoService   bool `env:"NEWTSC_AUTO_SERVICE"`
	AllPorts      bool `env:"NEWTSC_ALL_PORTS"`

	// SSO auth defaults (per-resource annotation always overrides)
	AuthSSORoles       string `env:"NEWTSC_AUTH_SSO_ROLES"`
	AuthSSOUsers       string `env:"NEWTSC_AUTH_SSO_USERS"`
	AuthSSOIDP         int    `env:"NEWTSC_AUTH_SSO_IDP"`
	AuthWhitelistUsers string `env:"NEWTSC_AUTH_WHITELIST_USERS"`

	// Health server port (0 = disabled)
	HealthPort int `env:"NEWTSC_HEALTH_PORT"`
}

// newConfig returns a Config initialised with hard-coded defaults and then
// overridden by any NEWTSC_* environment variables that are set.
func newConfig() (*Config, error) {
	cfg := &Config{
		Output:           "/etc/newt/blueprint.yaml",
		TargetPort:       443,
		TargetMethod:     "https",
		SSL:              true,
		AnnotationPrefix: "newt-sidecar",
		HealthPort:       8080,
	}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parsing environment variables: %w", err)
	}
	return cfg, nil
}

// Load parses configuration from environment variables and CLI flags.
// Environment variables are applied first; CLI flags override them.
func Load() (*Config, error) {
	cfg, err := newConfig()
	if err != nil {
		return nil, err
	}

	// CLI flags override environment variables; use current cfg values as defaults
	// so that env vars are still visible in --help output.

	// HTTPRoute flags
	flag.StringVar(&cfg.GatewayName, "gateway-name", cfg.GatewayName, "Gateway name to filter HTTPRoutes (required for HTTPRoute mode)")
	flag.StringVar(&cfg.GatewayNamespace, "gateway-namespace", cfg.GatewayNamespace, "Gateway namespace (empty = any)")

	// Shared flags
	flag.StringVar(&cfg.Namespace, "namespace", cfg.Namespace, "Watch namespace (empty = all)")
	flag.StringVar(&cfg.Output, "output", cfg.Output, "Output blueprint file path")
	flag.StringVar(&cfg.SiteID, "site-id", cfg.SiteID, "Pangolin site nice ID (required)")
	flag.StringVar(&cfg.TargetHostname, "target-hostname", cfg.TargetHostname, "Backend gateway hostname (required for HTTPRoute mode)")
	flag.IntVar(&cfg.TargetPort, "target-port", cfg.TargetPort, "Backend gateway port")
	flag.StringVar(&cfg.TargetMethod, "target-method", cfg.TargetMethod, "Backend method (http/https/h2c)")
	flag.StringVar(&cfg.DenyCountries, "deny-countries", cfg.DenyCountries, "Comma-separated country codes to deny")
	flag.BoolVar(&cfg.SSL, "ssl", cfg.SSL, "Enable SSL on HTTP resources")
	flag.StringVar(&cfg.AnnotationPrefix, "annotation-prefix", cfg.AnnotationPrefix, "Annotation prefix for per-resource overrides")

	// Service discovery flags
	flag.BoolVar(&cfg.EnableService, "enable-service", cfg.EnableService, "Enable Service discovery (annotation-mode: opt-in via newt-sidecar/enabled: true)")
	flag.BoolVar(&cfg.AutoService, "auto-service", cfg.AutoService, "Enable Service discovery (auto-mode: opt-out via newt-sidecar/enabled: false)")
	flag.BoolVar(&cfg.AllPorts, "all-ports", cfg.AllPorts, "Expose all TCP/UDP ports of a Service as individual blueprint entries")

	// SSO auth flags (cluster-wide defaults; per-resource annotation always wins).
	// There is deliberately no --auth-sso flag: SSO must be enabled per resource
	// via the newt-sidecar/auth-sso annotation.
	flag.StringVar(&cfg.AuthSSORoles, "auth-sso-roles", cfg.AuthSSORoles, "Default comma-separated Pangolin roles for SSO-enabled resources (empty = none)")
	flag.StringVar(&cfg.AuthSSOUsers, "auth-sso-users", cfg.AuthSSOUsers, "Default comma-separated user e-mails for SSO-enabled resources (empty = none)")
	flag.IntVar(&cfg.AuthSSOIDP, "auth-sso-idp", cfg.AuthSSOIDP, "Default Pangolin IdP ID for auto-login-idp (0 = not set)")
	flag.StringVar(&cfg.AuthWhitelistUsers, "auth-whitelist-users", cfg.AuthWhitelistUsers, "Default comma-separated user e-mails for whitelist-users (empty = none)")

	flag.IntVar(&cfg.HealthPort, "health-port", cfg.HealthPort, "Port for the health/readiness HTTP server (0 = disabled)")

	flag.Parse()

	return cfg, nil
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
