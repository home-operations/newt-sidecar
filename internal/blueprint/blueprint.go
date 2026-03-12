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
	Rules         []Rule   `yaml:"rules,omitempty"`
	Targets       []Target `yaml:"targets"`
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
// Example: "home.example.com" → "home-example-com"
func HostnameToKey(hostname string) string {
	return strings.ReplaceAll(hostname, ".", "-")
}

// ServiceToKey converts a namespace/name/port/protocol tuple to a stable resource map key.
// The protocol is included to correctly handle Services that expose the same port number
// for both TCP and UDP (e.g. game servers).
// Example: "default", "gameserver", "7777", "tcp" → "default-gameserver-7777-tcp"
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

// BuildResource creates an HTTP Resource from an HTTPRoute hostname, annotations, and config.
func BuildResource(routeName, hostname string, annotations map[string]string, cfg *config.Config) Resource {
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

	// --- TCP/UDP mode ---

	// Protocol is "tcp" or "udp". Ignored in HTTP mode.
	Protocol string
	// ProxyPort is the Pangolin-side port (TCP/UDP mode only).
	ProxyPort int
	// TargetPort is the port on the backend Service.
	TargetPort int
	// TargetHostname is the cluster-internal DNS name of the Service.
	TargetHostname string

	// --- HTTP mode (set when FullDomain is non-empty) ---

	// FullDomain is the public domain Pangolin exposes (e.g. "app.example.com").
	// A non-empty value switches BuildServiceResource into HTTP mode.
	FullDomain string
	// Method is the internal protocol used to reach the Service (http|https|h2c).
	Method string
	// SSL controls whether Pangolin enables SSL on the resource.
	SSL bool
}

// BuildServiceResource creates a blueprint Resource for a Service.
//
// HTTP mode — when sp.FullDomain is set:
//
//	Pangolin exposes the service at the given domain over HTTPS.
//	tls-server-name is always set to the full-domain — Pangolin needs
//	it for every HTTP resource regardless of the internal method.
//	deny-country rules from cfg.DenyCountries are applied, identical
//	to HTTPRoute resources.
//
// TCP/UDP mode — when sp.FullDomain is empty:
//
//	Pangolin opens a raw TCP or UDP port tunnelled directly to
//	the cluster-internal Service DNS name. Rules and tls-server-name
//	are not applicable for TCP/UDP and are therefore omitted.
func BuildServiceResource(sp ServicePort, cfg *config.Config) Resource {
	if sp.FullDomain != "" {
		// HTTP mode: direct Service, no gateway.
		// tls-server-name is always equal to full-domain for HTTP resources.
		return Resource{
			Name:          sp.Name,
			Protocol:      "http",
			SSL:           sp.SSL,
			FullDomain:    sp.FullDomain,
			TLSServerName: sp.FullDomain,
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

	// TCP/UDP mode: direct Service, raw tunnel. No rules, no tls-server-name.
	return Resource{
		Name:      sp.Name,
		Protocol:  sp.Protocol,
		ProxyPort: sp.ProxyPort,
		Targets: []Target{
			{
				Site:     cfg.SiteID,
				Hostname: sp.TargetHostname,
				Port:     sp.TargetTarget,
			},
		},
	}
}
