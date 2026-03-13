package blueprint

import (
	"fmt"
	"strings"

	"github.com/home-operations/newt-sidecar/internal/config"
)

type Blueprint struct {
	PublicResources map[string]Resource `yaml:"public-resources"`
}

type Resource struct {
	Name          string   `yaml:"name"`
	Protocol      string   `yaml:"protocol"`
	SSL           bool     `yaml:"ssl,omitempty"`
	FullDomain    string   `yaml:"full-domain,omitempty"`
	TLSServerName string   `yaml:"tls-server-name,omitempty"`
	ProxyPort     int      `yaml:"proxy-port,omitempty"`
	Auth          *Auth    `yaml:"auth,omitempty"`
	Rules         []Rule   `yaml:"rules,omitempty"`
	Targets       []Target `yaml:"targets"`
}

// Auth maps to the Pangolin blueprint auth block (SSO only).
type Auth struct {
	SSOEnabled   bool     `yaml:"sso-enabled"`
	SSORoles     []string `yaml:"sso-roles,omitempty"`
	SSOUsers     []string `yaml:"sso-users,omitempty"`
	AutoLoginIDP int      `yaml:"auto-login-idp,omitempty"`
}

type Rule struct {
	Action string `yaml:"action"`
	Match  string `yaml:"match"`
	Value  string `yaml:"value"`
}

type Target struct {
	Site     string `yaml:"site,omitempty"`
	Hostname string `yaml:"hostname"`
	Method   string `yaml:"method,omitempty"`
	Port     int    `yaml:"port"`
}

// HostnameToKey converts a hostname to a resource map key.
// Example: "home.example.com" -> "home-example-com"
func HostnameToKey(hostname string) string {
	return strings.ReplaceAll(hostname, ".", "-")
}

// ServiceToKey converts a namespace/name/port/protocol tuple to a stable resource map key.
// The protocol is included to correctly handle Services that expose the same port number
// for both TCP and UDP (e.g. game servers).
// Example: "default", "gameserver", "7777", "tcp" -> "default-gameserver-7777-tcp"
func ServiceToKey(namespace, name, port, protocol string) string {
	return fmt.Sprintf("%s-%s-%s-%s", namespace, name, port, protocol)
}

// buildDenyRules returns a Rule slice for each country in cfg.DenyCountries.
// Returns nil when DenyCountries is empty.
func buildDenyRules(cfg *config.Config) []Rule {
	if cfg.DenyCountries == "" {
		return nil
	}
	var rules []Rule
	for _, country := range strings.Split(cfg.DenyCountries, ",") {
		country = strings.TrimSpace(country)
		if country != "" {
			rules = append(rules, Rule{
				Action: "deny",
				Match:  "country",
				Value:  country,
			})
		}
	}
	return rules
}

// splitCSV splits a comma-separated string into a trimmed, non-empty slice.
// Returns nil when s is empty.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, v := range strings.Split(s, ",") {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// buildAuth constructs an Auth block from annotations and global config defaults.
// Returns nil when the newt-sidecar/auth-sso annotation is absent or not "true"/"1".
// Auth is only valid for HTTP resources; callers must not invoke this for TCP/UDP.
func buildAuth(annotations map[string]string, cfg *config.Config) *Auth {
	prefix := cfg.AnnotationPrefix

	v, ok := annotations[prefix+"/auth-sso"]
	if !ok || (v != "true" && v != "1") {
		return nil
	}

	rolesRaw := cfg.AuthSSORoles
	if av, aok := annotations[prefix+"/auth-sso-roles"]; aok {
		rolesRaw = av
	}

	usersRaw := cfg.AuthSSOUsers
	if av, aok := annotations[prefix+"/auth-sso-users"]; aok {
		usersRaw = av
	}

	idp := cfg.AuthSSOIDP
	if av, aok := annotations[prefix+"/auth-sso-idp"]; aok {
		var parsed int
		if _, err := fmt.Sscanf(av, "%d", &parsed); err == nil && parsed > 0 {
			idp = parsed
		}
	}

	return &Auth{
		SSOEnabled:   true,
		SSORoles:     splitCSV(rolesRaw),
		SSOUsers:     splitCSV(usersRaw),
		AutoLoginIDP: idp,
	}
}

// BuildResource creates an HTTP Resource from an HTTPRoute hostname, annotations, and config.
func BuildResource(routeName, hostname string, annotations map[string]string, cfg *config.Config) Resource {
	if annotations == nil {
		annotations = map[string]string{}
	}
	name := routeName
	ssl := cfg.SSL
	prefix := cfg.AnnotationPrefix

	if v, ok := annotations[prefix+"/name"]; ok && v != "" {
		name = v
	}
	if v, ok := annotations[prefix+"/ssl"]; ok {
		ssl = v == "true" || v == "1"
	}

	return Resource{
		Name:          name,
		Protocol:      "http",
		SSL:           ssl,
		FullDomain:    hostname,
		TLSServerName: hostname,
		Auth:          buildAuth(annotations, cfg),
		Rules:         buildDenyRules(cfg),
		Targets: []Target{
			{
				Site:     cfg.SiteID,
				Hostname: cfg.TargetHostname,
				Method:   cfg.TargetMethod,
				Port:     cfg.TargetPort,
			},
		},
	}
}

// ServicePort holds the resolved information for a single Service port.
type ServicePort struct {
	// Name is the display name in the Pangolin blueprint.
	Name string

	// Protocol is "tcp" or "udp". Ignored in HTTP mode.
	Protocol string
	// ProxyPort is the Pangolin-side port (TCP/UDP mode only).
	ProxyPort int
	// TargetPort is the port on the backend Service.
	TargetPort int
	// TargetHostname is the cluster-internal DNS name of the Service.
	TargetHostname string

	// FullDomain is the public domain Pangolin exposes (e.g. "app.example.com").
	// A non-empty value switches BuildServiceResource into HTTP mode.
	FullDomain string
	// Method is the internal protocol used to reach the Service (http|https|h2c).
	Method string
	// SSL controls whether Pangolin enables SSL on the resource.
	SSL bool
	// Auth is the optional SSO auth block. Only valid in HTTP mode.
	Auth *Auth
}

// BuildServiceResource creates a blueprint Resource for a Service.
//
// When sp.FullDomain is set (HTTP mode), Pangolin exposes the service at the
// given domain over HTTPS. tls-server-name is always set to the full-domain.
// Deny-country rules from cfg.DenyCountries are applied, identical to HTTPRoute
// resources.
//
// When sp.FullDomain is empty (TCP/UDP mode), Pangolin opens a raw TCP or UDP
// port tunnelled directly to the cluster-internal Service DNS name. Rules,
// tls-server-name, and auth are not applicable and are omitted.
func BuildServiceResource(sp ServicePort, cfg *config.Config) Resource {
	if sp.FullDomain != "" {
		// HTTP mode: direct Service, no gateway.
		return Resource{
			Name:          sp.Name,
			Protocol:      "http",
			SSL:           sp.SSL,
			FullDomain:    sp.FullDomain,
			TLSServerName: sp.FullDomain,
			Auth:          sp.Auth,
			Rules:         buildDenyRules(cfg),
			Targets: []Target{
				{
					Site:     cfg.SiteID,
					Hostname: sp.TargetHostname,
					Method:   sp.Method,
					Port:     sp.TargetPort,
				},
			},
		}
	}

	// TCP/UDP mode: raw tunnel. No rules, no tls-server-name, no auth.
	return Resource{
		Name:      sp.Name,
		Protocol:  sp.Protocol,
		ProxyPort: sp.ProxyPort,
		Targets: []Target{
			{
				Site:     cfg.SiteID,
				Hostname: sp.TargetHostname,
				Port:     sp.TargetPort,
			},
		},
	}
}
