package service

import (
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/home-operations/newt-sidecar/internal/blueprint"
	"github.com/home-operations/newt-sidecar/internal/config"
	"github.com/home-operations/newt-sidecar/internal/resources"
)

// Definition returns a ResourceDefinition for Service resources.
func Definition() *resources.ResourceDefinition {
	return &resources.ResourceDefinition{
		GVR: schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "services",
		},
		ConvertFunc:   resources.CreateConvertFunc(reflect.TypeOf(corev1.Service{})),
		ShouldProcess: shouldProcess,
		BuildEntries:  buildEntries,
	}
}

// shouldProcess implements opt-in (annotation-mode) and opt-out (auto-mode) logic.
func shouldProcess(obj metav1.Object, cfg *config.Config) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	enabledVal, hasEnabled := annotations[cfg.AnnotationPrefix+"/enabled"]

	if !cfg.AutoService {
		// Annotation-mode: only process when explicitly opted in.
		return hasEnabled && (enabledVal == "true" || enabledVal == "1")
	}

	// Auto-mode: process everything unless explicitly opted out.
	return !(hasEnabled && (enabledVal == "false" || enabledVal == "0"))
}

// buildEntries builds blueprint entries for a Service.
//
// all-ports mode is active when either:
//   - the newt-sidecar/all-ports annotation is set to "true" or "1", or
//   - the global --all-ports flag is set (and the annotation does not
//     explicitly disable it with "false" or "0").
//
// In all-ports mode every TCP/UDP port gets its own blueprint entry.
// Services that define the same port number for both TCP and UDP each
// receive their own entry, keyed by namespace-name-port-protocol.
// HTTP mode (full-domain) is not supported in all-ports mode.
// Otherwise the standard single-port selection logic applies, which also
// supports HTTP mode via the full-domain annotation.
func buildEntries(obj metav1.Object, cfg *config.Config) map[string]blueprint.Resource {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		return nil
	}

	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	svcKey := fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)
	clusterHostname := fmt.Sprintf("%s.%s.svc.cluster.local", svc.Name, svc.Namespace)

	if resolveAllPorts(annotations, cfg) {
		return buildAllPortEntries(svc, svcKey, clusterHostname, cfg)
	}

	return buildSinglePortEntry(svc, annotations, cfg, svcKey, clusterHostname)
}

// resolveAllPorts determines whether all-ports mode should be active.
//
// The annotation takes precedence over the global flag:
//   - annotation "true"/"1"  → all-ports on  (regardless of global flag)
//   - annotation "false"/"0" → all-ports off (regardless of global flag)
//   - annotation absent      → fall back to global --all-ports flag
func resolveAllPorts(annotations map[string]string, cfg *config.Config) bool {
	if v, ok := annotations[cfg.AnnotationPrefix+"/all-ports"]; ok {
		return v == "true" || v == "1"
	}
	return cfg.AllPorts
}

// buildAllPortEntries creates one blueprint entry per TCP/UDP port.
// The protocol is read directly from the ServicePort spec.
// When the same port number appears with different protocols (e.g. 7777/TCP
// and 7777/UDP) each combination receives its own entry; the blueprint key
// includes the protocol to prevent collisions.
//
// The display name uses the format "<svcName>-<portName>" without spaces.
// Port names are unique within a Service, so no protocol suffix is needed.
// If a port has no name, the port number is used instead.
func buildAllPortEntries(svc *corev1.Service, svcKey, clusterHostname string, cfg *config.Config) map[string]blueprint.Resource {
	if len(svc.Spec.Ports) == 0 {
		slog.Warn("service has no ports, skipping", "service", svcKey)
		return nil
	}

	entries := make(map[string]blueprint.Resource, len(svc.Spec.Ports))

	for i := range svc.Spec.Ports {
		p := &svc.Spec.Ports[i]

		portName := p.Name
		if portName == "" {
			portName = strconv.Itoa(int(p.Port))
		}
		proto := serviceProtocol(p.Protocol)
		displayName := fmt.Sprintf("%s-%s", svc.Name, portName)
		key := blueprint.ServiceToKey(svc.Namespace, svc.Name, strconv.Itoa(int(p.Port)), proto)

		entries[key] = blueprint.BuildServiceResource(blueprint.ServicePort{
			Name:           displayName,
			Protocol:       proto,
			ProxyPort:      int(p.Port),
			TargetPort:     int(p.Port),
			TargetHostname: clusterHostname,
		}, cfg)
	}

	return entries
}

// buildSinglePortEntry applies the standard port selection logic and supports
// both TCP/UDP mode and HTTP mode (via full-domain annotation).
func buildSinglePortEntry(svc *corev1.Service, annotations map[string]string, cfg *config.Config, svcKey, clusterHostname string) map[string]blueprint.Resource {
	sp, ok := resolvePort(svc, annotations, cfg.AnnotationPrefix, svcKey, clusterHostname, cfg)
	if !ok {
		return nil
	}

	var key string
	if sp.FullDomain != "" {
		key = blueprint.HostnameToKey(sp.FullDomain)
	} else {
		key = blueprint.ServiceToKey(svc.Namespace, svc.Name, strconv.Itoa(sp.ProxyPort), sp.Protocol)
	}

	return map[string]blueprint.Resource{
		key: blueprint.BuildServiceResource(sp, cfg),
	}
}

// resolvePort selects the Service port to expose and builds a ServicePort.
//
// Mode selection:
//
//	newt-sidecar/full-domain set → HTTP mode.
//	newt-sidecar/full-domain absent → TCP/UDP mode.
//
// Port selection:
//
//  1. newt-sidecar/port annotation → match by number or name.
//  2. Service has exactly one port → use it.
//  3. Service has a port named "http" → use it.
//  4. Otherwise skip with a warning.
//
// Protocol (TCP/UDP mode only):
//
//	Read from the ServicePort spec. Can be overridden via newt-sidecar/protocol.
func resolvePort(svc *corev1.Service, annotations map[string]string, prefix, svcKey, clusterHostname string, cfg *config.Config) (blueprint.ServicePort, bool) {
	// Mode detection.
	fullDomain := strings.TrimSpace(annotations[prefix+"/full-domain"])
	httpMode := fullDomain != ""

	// Port selection.
	var selected *corev1.ServicePort

	if v, ok := annotations[prefix+"/port"]; ok {
		v = strings.TrimSpace(v)
		for i := range svc.Spec.Ports {
			p := &svc.Spec.Ports[i]
			if strconv.Itoa(int(p.Port)) == v || p.Name == v {
				selected = p
				break
			}
		}
		if selected == nil {
			slog.Warn("newt-sidecar/port annotation does not match any port, skipping service",
				"service", svcKey, "value", v)
			return blueprint.ServicePort{}, false
		}
	} else if len(svc.Spec.Ports) == 1 {
		selected = &svc.Spec.Ports[0]
	} else {
		for i := range svc.Spec.Ports {
			if svc.Spec.Ports[i].Name == "http" {
				selected = &svc.Spec.Ports[i]
				break
			}
		}
		if selected == nil {
			slog.Warn("service has multiple ports and none is named 'http'; set newt-sidecar/port to select explicitly, skipping service",
				"service", svcKey)
			return blueprint.ServicePort{}, false
		}
	}

	// Display name.
	portName := selected.Name
	if portName == "" {
		portName = strconv.Itoa(int(selected.Port))
	}
	displayName := fmt.Sprintf("%s %s", svc.Name, portName)
	if v, ok := annotations[prefix+"/name"]; ok && v != "" {
		displayName = v
	}

	// HTTP mode.
	if httpMode {
		method := "http"
		if v, ok := annotations[prefix+"/method"]; ok {
			v = strings.ToLower(strings.TrimSpace(v))
			if v == "https" || v == "h2c" {
				method = v
			}
		}
		ssl := cfg.SSL
		if v, ok := annotations[prefix+"/ssl"]; ok {
			ssl = v == "true" || v == "1"
		}
		return blueprint.ServicePort{
			Name:           displayName,
			FullDomain:     fullDomain,
			Method:         method,
			SSL:            ssl,
			TargetPort:     int(selected.Port),
			TargetHostname: clusterHostname,
		}, true
	}

	// TCP/UDP mode: protocol from ServicePort spec, annotation can override.
	protocol := serviceProtocol(selected.Protocol)
	if v, ok := annotations[prefix+"/protocol"]; ok {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "tcp" || v == "udp" {
			protocol = v
		}
	}

	return blueprint.ServicePort{
		Name:           displayName,
		Protocol:       protocol,
		ProxyPort:      int(selected.Port),
		TargetPort:     int(selected.Port),
		TargetHostname: clusterHostname,
	}, true
}

// serviceProtocol converts a corev1.Protocol to the lowercase string used in
// the Pangolin blueprint. Defaults to "tcp" for unknown/unset protocols.
func serviceProtocol(p corev1.Protocol) string {
	switch p {
	case corev1.ProtocolUDP:
		return "udp"
	default:
		return "tcp"
	}
}
