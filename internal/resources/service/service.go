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

// buildEntries resolves the port, determines the mode (HTTP vs TCP/UDP) and
// returns a single blueprint entry for this Service.
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

	sp, ok := resolvePort(svc, annotations, cfg.AnnotationPrefix, svcKey, cfg)
	if !ok {
		return nil
	}

	var key string
	if sp.FullDomain != "" {
		key = blueprint.HostnameToKey(sp.FullDomain)
	} else {
		key = blueprint.ServiceToKey(svc.Namespace, svc.Name, strconv.Itoa(sp.ProxyPort))
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
func resolvePort(svc *corev1.Service, annotations map[string]string, prefix, svcKey string, cfg *config.Config) (blueprint.ServicePort, bool) {
	clusterHostname := fmt.Sprintf("%s.%s.svc.cluster.local", svc.Name, svc.Namespace)

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

	// TCP/UDP mode.
	protocol := "tcp"
	if v, ok := annotations[prefix+"/protocol"]; ok {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "udp" {
			protocol = "udp"
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
