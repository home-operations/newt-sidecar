package blueprint

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/home-operations/newt-sidecar/internal/config"
)

type Blueprint struct {
	PublicResources  map[string]Resource        `yaml:"public-resources,omitempty"`
	PrivateResources map[string]PrivateResource `yaml:"private-resources,omitempty"`
}

type Header struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

type BasicAuth struct {
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

type HealthCheck struct {
	Hostname          string   `yaml:"hostname"`
	Port              int      `yaml:"port"`
	Enabled           *bool    `yaml:"enabled,omitempty"`
	Path              string   `yaml:"path,omitempty"`
	Scheme            string   `yaml:"scheme,omitempty"`
	Mode              string   `yaml:"mode,omitempty"`
	Interval          int      `yaml:"interval,omitempty"`
	UnhealthyInterval int      `yaml:"unhealthy-interval,omitempty"`
	Timeout           int      `yaml:"timeout,omitempty"`
	Headers           []Header `yaml:"headers,omitempty"`
	FollowRedirects   *bool    `yaml:"follow-redirects,omitempty"`
	Method            string   `yaml:"method,omitempty"`
	Status            int      `yaml:"status,omitempty"`
}

type Maintenance struct {
	Enabled       bool   `yaml:"enabled"`
	Type          string `yaml:"type,omitempty"`
	Title         string `yaml:"title,omitempty"`
	Message       string `yaml:"message,omitempty"`
	EstimatedTime string `yaml:"estimated-time,omitempty"`
}

type PrivateResource struct {
	Name        string   `yaml:"name"`
	Mode        string   `yaml:"mode"`
	Destination string   `yaml:"destination"`
	Site        string   `yaml:"site"`
	TCPPorts    string   `yaml:"tcp-ports,omitempty"`
	UDPPorts    string   `yaml:"udp-ports,omitempty"`
	DisableICMP bool     `yaml:"disable-icmp,omitempty"`
	Alias       string   `yaml:"alias,omitempty"`
	Roles       []string `yaml:"roles,omitempty"`
	Users       []string `yaml:"users,omitempty"`
	Machines    []string `yaml:"machines,omitempty"`
}

type Resource struct {
	Name          string       `yaml:"name,omitempty"`
	Protocol      string       `yaml:"protocol,omitempty"`
	Enabled       *bool        `yaml:"enabled,omitempty"`
	SSL           bool         `yaml:"ssl,omitempty"`
	FullDomain    string       `yaml:"full-domain,omitempty"`
	HostHeader    string       `yaml:"host-header,omitempty"`
	TLSServerName string       `yaml:"tls-server-name,omitempty"`
	ProxyPort     int          `yaml:"proxy-port,omitempty"`
	Headers       []Header     `yaml:"headers,omitempty"`
	Auth          *Auth        `yaml:"auth,omitempty"`
	Maintenance   *Maintenance `yaml:"maintenance,omitempty"`
	Rules         []Rule       `yaml:"rules,omitempty"`
	Targets       []Target     `yaml:"targets"`
}

// Auth maps to the Pangolin blueprint auth block.
// Sensitive values (Pincode, Password, BasicAuth) come from a K8s Secret, never from annotations.
type Auth struct {
	Pincode        int        `yaml:"pincode,omitempty"`
	Password       string     `yaml:"password,omitempty"`
	BasicAuth      *BasicAuth `yaml:"basic-auth,omitempty"`
	SSOEnabled     bool       `yaml:"sso-enabled"`
	SSORoles       []string   `yaml:"sso-roles,omitempty"`
	SSOUsers       []string   `yaml:"sso-users,omitempty"`
	WhitelistUsers []string   `yaml:"whitelist-users,omitempty"`
	AutoLoginIDP   int        `yaml:"auto-login-idp,omitempty"`
}

type Rule struct {
	Action   string `yaml:"action"`
	Match    string `yaml:"match"`
	Value    string `yaml:"value"`
	Priority int    `yaml:"priority,omitempty"`
}

type Target struct {
	Site         string       `yaml:"site,omitempty"`
	Hostname     string       `yaml:"hostname"`
	Enabled      *bool        `yaml:"enabled,omitempty"`
	Method       string       `yaml:"method,omitempty"`
	Port         int          `yaml:"port"`
	InternalPort int          `yaml:"internal-port,omitempty"`
	Path         string       `yaml:"path,omitempty"`
	PathMatch    string       `yaml:"path-match,omitempty"`
	RewritePath  string       `yaml:"rewrite-path,omitempty"`
	RewriteMatch string       `yaml:"rewrite-match,omitempty"`
	Priority     int          `yaml:"priority,omitempty"`
	HealthCheck  *HealthCheck `yaml:"healthcheck,omitempty"`
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
	for country := range strings.SplitSeq(cfg.DenyCountries, ",") {
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

// BuildRules parses custom rules from the {prefix}/rules annotation and merges
// with deny-country rules from cfg.DenyCountries.
//
// The {prefix}/rules annotation is a JSON array of Rule objects:
//
//	[{"action":"deny","match":"ip","value":"10.0.0.0/8"},{"action":"allow","match":"path","value":"/admin","priority":10}]
//
// Valid actions: allow, deny, pass
// Valid matches: cidr, ip, path, country
// Priority is optional (zero means not specified). When present, must be 1–1000.
//
// Annotation rules are returned first, followed by deny-country rules from cfg.
// Invalid rules are silently dropped. Returns nil if no valid rules exist.
func BuildRules(annotations map[string]string, prefix string, cfg *config.Config) []Rule {
	var rules []Rule

	// Parse annotation rules first
	if raw, ok := annotations[prefix+"/rules"]; ok && raw != "" {
		var annotationRules []Rule
		if err := json.Unmarshal([]byte(raw), &annotationRules); err != nil {
			slog.Warn("failed to parse rules annotation", "annotation", prefix+"/rules", "error", err)
		} else {
			for _, r := range annotationRules {
				if isValidRule(r) {
					rules = append(rules, r)
				}
			}
		}
	}

	// Append deny-country rules after annotation rules
	denyRules := buildDenyRules(cfg)
	rules = append(rules, denyRules...)

	if len(rules) == 0 {
		return nil
	}
	return rules
}

// isValidRule checks if a Rule has valid action, match, and non-empty value.
// Priority is optional (zero = not specified). When non-zero it must be 1–1000.
func isValidRule(r Rule) bool {
	validActions := map[string]bool{"allow": true, "deny": true, "pass": true}
	validMatches := map[string]bool{"cidr": true, "ip": true, "path": true, "country": true}

	if !validActions[r.Action] {
		return false
	}
	if !validMatches[r.Match] {
		return false
	}
	if r.Value == "" {
		return false
	}
	if r.Priority != 0 && (r.Priority < 1 || r.Priority > 1000) {
		return false
	}
	return true
}

func isTruthy(v string) bool { return v == "true" || v == "1" }

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for v := range strings.SplitSeq(s, ",") {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// BuildHeaders parses the {prefix}/headers annotation as a JSON array of Header objects.
// Returns nil on missing annotation or parse error.
func BuildHeaders(annotations map[string]string, prefix string) []Header {
	raw, ok := annotations[prefix+"/headers"]
	if !ok || raw == "" {
		return nil
	}
	var headers []Header
	if err := json.Unmarshal([]byte(raw), &headers); err != nil {
		slog.Warn("failed to parse headers annotation", "annotation", prefix+"/headers", "error", err)
		return nil
	}
	return headers
}

// BuildMaintenance builds a Maintenance block from annotations.
// Returns nil when {prefix}/maintenance-enabled is absent or not exactly "true" or "1".
func BuildMaintenance(annotations map[string]string, prefix string) *Maintenance {
	v, ok := annotations[prefix+"/maintenance-enabled"]
	if !ok || !isTruthy(v) {
		return nil
	}
	return &Maintenance{
		Enabled:       true,
		Type:          annotations[prefix+"/maintenance-type"],
		Title:         annotations[prefix+"/maintenance-title"],
		Message:       annotations[prefix+"/maintenance-message"],
		EstimatedTime: annotations[prefix+"/maintenance-estimated-time"],
	}
}

// BuildTargetExtras enriches a base Target with optional target-level annotations.
//
// Recognised annotations (relative to prefix):
//
//	/target-path            — path prefix, exact path, or regex pattern
//	/target-path-match      — matching type: prefix, exact, or regex
//	/target-rewrite-path    — path to rewrite the request to
//	/target-rewrite-match   — rewrite match type: exact, prefix, regex, or stripPrefix
//	/target-priority        — load-balancing priority (1–1000)
//	/target-internal-port   — internal port mapping (1–65535)
//	/target-healthcheck     — JSON HealthCheck object
//	/target-enabled         — enable/disable the target: "true"/"1" or "false"/"0"
func BuildTargetExtras(base Target, annotations map[string]string, prefix string) Target {
	t := base

	if v := strings.TrimSpace(annotations[prefix+"/target-path"]); v != "" {
		t.Path = v
	}
	if v := strings.TrimSpace(annotations[prefix+"/target-path-match"]); v != "" {
		switch v {
		case "prefix", "exact", "regex":
			t.PathMatch = v
		}
	}
	if v := strings.TrimSpace(annotations[prefix+"/target-rewrite-path"]); v != "" {
		t.RewritePath = v
	}
	if v := strings.TrimSpace(annotations[prefix+"/target-rewrite-match"]); v != "" {
		switch v {
		case "exact", "prefix", "regex", "stripPrefix":
			t.RewriteMatch = v
		}
	}
	if v := strings.TrimSpace(annotations[prefix+"/target-priority"]); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 1 && parsed <= 1000 {
			t.Priority = parsed
		}
	}
	if v := strings.TrimSpace(annotations[prefix+"/target-internal-port"]); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 1 && parsed <= 65535 {
			t.InternalPort = parsed
		}
	}
	if v := annotations[prefix+"/target-healthcheck"]; v != "" {
		var hc HealthCheck
		if err := json.Unmarshal([]byte(v), &hc); err != nil {
			slog.Warn("failed to parse target-healthcheck annotation", "annotation", prefix+"/target-healthcheck", "error", err)
		} else {
			t.HealthCheck = &hc
		}
	}
	if v := strings.TrimSpace(annotations[prefix+"/target-enabled"]); v != "" {
		enabled := isTruthy(v)
		t.Enabled = &enabled
	}

	return t
}

// BuildAuth constructs an Auth block from annotations, secretData, and global config defaults.
// secretData contains values resolved from the K8s Secret named by the {prefix}/auth-secret
// annotation. Keys: "pincode", "password", "basic-auth-user", "basic-auth-password".
// Returns nil when all auth fields are zero/empty and no secretData values are set.
func BuildAuth(annotations map[string]string, secretData map[string]string, cfg *config.Config) *Auth {
	prefix := cfg.AnnotationPrefix

	ssoEnabled := false
	if v, ok := annotations[prefix+"/auth-sso"]; ok && isTruthy(v) {
		ssoEnabled = true
	}

	var ssoRoles, ssoUsers []string
	idp := 0
	if ssoEnabled {
		rolesRaw := cfg.AuthSSORoles
		if av, aok := annotations[prefix+"/auth-sso-roles"]; aok {
			rolesRaw = av
		}
		ssoRoles = splitCSV(rolesRaw)

		usersRaw := cfg.AuthSSOUsers
		if av, aok := annotations[prefix+"/auth-sso-users"]; aok {
			usersRaw = av
		}
		ssoUsers = splitCSV(usersRaw)

		idp = cfg.AuthSSOIDP
		if av, aok := annotations[prefix+"/auth-sso-idp"]; aok {
			if parsed, err := strconv.Atoi(av); err == nil && parsed > 0 {
				idp = parsed
			}
		}
	}

	whitelistRaw := cfg.AuthWhitelistUsers
	if av, aok := annotations[prefix+"/auth-whitelist-users"]; aok {
		whitelistRaw = av
	}
	whitelistUsers := splitCSV(whitelistRaw)

	var pincode int
	var password string
	var basicAuth *BasicAuth
	if secretData != nil {
		if v, ok := secretData["pincode"]; ok && v != "" {
			if parsed, err := strconv.Atoi(v); err == nil {
				pincode = parsed
			}
		}
		if v, ok := secretData["password"]; ok {
			password = v
		}
		baUser := secretData["basic-auth-user"]
		baPass := secretData["basic-auth-password"]
		if baUser != "" || baPass != "" {
			basicAuth = &BasicAuth{User: baUser, Password: baPass}
		}
	}

	if !ssoEnabled && len(whitelistUsers) == 0 && pincode == 0 && password == "" && basicAuth == nil {
		return nil
	}

	return &Auth{
		Pincode:        pincode,
		Password:       password,
		BasicAuth:      basicAuth,
		SSOEnabled:     ssoEnabled,
		SSORoles:       ssoRoles,
		SSOUsers:       ssoUsers,
		WhitelistUsers: whitelistUsers,
		AutoLoginIDP:   idp,
	}
}

// BuildResource creates an HTTP Resource from an HTTPRoute hostname, annotations, and config.
// secretData contains values resolved from the K8s Secret named by the {prefix}/auth-secret annotation.
func BuildResource(routeName, hostname string, annotations map[string]string, secretData map[string]string, cfg *config.Config) Resource {
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
		ssl = isTruthy(v)
	}

	var enabled *bool
	if v, ok := annotations[prefix+"/enabled"]; ok {
		b := isTruthy(v)
		enabled = &b
	}

	tlsServerName := hostname
	if v := strings.TrimSpace(annotations[prefix+"/tls-server-name"]); v != "" {
		tlsServerName = v
	}

	return Resource{
		Name:          name,
		Protocol:      "http",
		Enabled:       enabled,
		SSL:           ssl,
		FullDomain:    hostname,
		HostHeader:    annotations[prefix+"/host-header"],
		TLSServerName: tlsServerName,
		Headers:       BuildHeaders(annotations, prefix),
		Auth:          BuildAuth(annotations, secretData, cfg),
		Maintenance:   BuildMaintenance(annotations, prefix),
		Rules:         BuildRules(annotations, prefix, cfg),
		Targets: []Target{
			BuildTargetExtras(Target{
				Site:     cfg.SiteID,
				Hostname: cfg.TargetHostname,
				Method:   cfg.TargetMethod,
				Port:     cfg.TargetPort,
			}, annotations, prefix),
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
	// TLSServerName overrides the SNI name for the backend TLS connection.
	// Falls back to FullDomain when empty.
	TLSServerName string
	// Method is the internal protocol used to reach the Service (http|https|h2c).
	Method string
	// SSL controls whether Pangolin enables SSL on the resource.
	SSL bool
	// HostHeader is an optional custom Host header for the resource.
	HostHeader string
	// Headers are optional extra headers for the resource.
	Headers []Header
	// Maintenance is the optional maintenance page configuration.
	Maintenance *Maintenance
	// Auth is the optional auth block. Only valid in HTTP mode.
	Auth *Auth
	// Rules are optional access control rules. Only valid in HTTP mode.
	Rules []Rule

	// Target-level extras (HTTP mode only). Applied to the single target.
	TargetEnabled      *bool
	TargetPath         string
	TargetPathMatch    string
	TargetRewritePath  string
	TargetRewriteMatch string
	TargetPriority     int
	TargetInternalPort int
	TargetHealthCheck  *HealthCheck
}

// BuildServiceResource creates a blueprint Resource for a Service.
//
// When sp.FullDomain is set (HTTP mode), Pangolin exposes the service at the
// given domain. tls-server-name defaults to FullDomain but can be overridden
// via sp.TLSServerName. Host-header, headers, maintenance, rules, and all
// target-level extras are forwarded from the ServicePort.
//
// When sp.FullDomain is empty (TCP/UDP mode), Pangolin opens a raw TCP or UDP
// port tunnelled directly to the cluster-internal Service DNS name. Rules,
// tls-server-name, and auth are not applicable and are omitted.
func BuildServiceResource(sp ServicePort, cfg *config.Config) Resource {
	if sp.FullDomain != "" {
		tlsServerName := sp.FullDomain
		if sp.TLSServerName != "" {
			tlsServerName = sp.TLSServerName
		}
		return Resource{
			Name:          sp.Name,
			Protocol:      "http",
			SSL:           sp.SSL,
			FullDomain:    sp.FullDomain,
			HostHeader:    sp.HostHeader,
			TLSServerName: tlsServerName,
			Headers:       sp.Headers,
			Auth:          sp.Auth,
			Maintenance:   sp.Maintenance,
			Rules:         sp.Rules,
			Targets: []Target{
				{
					Site:         cfg.SiteID,
					Hostname:     sp.TargetHostname,
					Enabled:      sp.TargetEnabled,
					Method:       sp.Method,
					Port:         sp.TargetPort,
					Path:         sp.TargetPath,
					PathMatch:    sp.TargetPathMatch,
					RewritePath:  sp.TargetRewritePath,
					RewriteMatch: sp.TargetRewriteMatch,
					Priority:     sp.TargetPriority,
					InternalPort: sp.TargetInternalPort,
					HealthCheck:  sp.TargetHealthCheck,
				},
			},
		}
	}

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
