package service

import (
	"context"
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

const (
	annotationTrue = "true"
	annotationOne  = "1"
)

func Definition() *resources.ResourceDefinition {
	return &resources.ResourceDefinition{
		GVR: schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "services",
		},
		ConvertFunc:    resources.CreateConvertFunc(reflect.TypeOf(corev1.Service{})),
		AutoConfigFunc: func(cfg *config.Config) bool { return cfg.AutoService },
		BuildEntries:   buildEntries,
	}
}

func buildEntries(ctx context.Context, obj metav1.Object, secretData map[string]string, cfg *config.Config) map[string]blueprint.Resource {
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

	return buildSinglePortEntry(svc, annotations, secretData, cfg, svcKey, clusterHostname)
}

// resolveAllPorts returns true when all-ports mode is active.
// Annotation takes precedence over the global --all-ports flag.
func resolveAllPorts(annotations map[string]string, cfg *config.Config) bool {
	if v, ok := annotations[cfg.AnnotationPrefix+"/all-ports"]; ok {
		return v == annotationTrue || v == annotationOne
	}
	return cfg.AllPorts
}

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

func buildSinglePortEntry(svc *corev1.Service, annotations map[string]string, secretData map[string]string, cfg *config.Config, svcKey, clusterHostname string) map[string]blueprint.Resource {
	sp, ok := resolvePort(svc, annotations, secretData, cfg.AnnotationPrefix, svcKey, clusterHostname, cfg)
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

// resolvePort selects the port to expose and builds a ServicePort.
// full-domain annotation -> HTTP mode; absent -> TCP/UDP mode.
// Port selection: /port annotation > single port > port named "http".
// Protocol in TCP/UDP mode: ServicePort spec, overridable via /protocol annotation.
func resolvePort(svc *corev1.Service, annotations map[string]string, secretData map[string]string, prefix, svcKey, clusterHostname string, cfg *config.Config) (blueprint.ServicePort, bool) {
	fullDomain := strings.TrimSpace(annotations[prefix+"/full-domain"])
	httpMode := fullDomain != ""

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

	portName := selected.Name
	if portName == "" {
		portName = strconv.Itoa(int(selected.Port))
	}
	displayName := fmt.Sprintf("%s %s", svc.Name, portName)
	if v, ok := annotations[prefix+"/name"]; ok && v != "" {
		displayName = v
	}

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
			ssl = v == annotationTrue || v == annotationOne
		}
		extras := blueprint.BuildTargetExtras(blueprint.Target{}, annotations, prefix)
		return blueprint.ServicePort{
			Name:               displayName,
			FullDomain:         fullDomain,
			TLSServerName:      strings.TrimSpace(annotations[prefix+"/tls-server-name"]),
			Method:             method,
			SSL:                ssl,
			HostHeader:         annotations[prefix+"/host-header"],
			Headers:            blueprint.BuildHeaders(annotations, prefix),
			Maintenance:        blueprint.BuildMaintenance(annotations, prefix),
			Rules:              blueprint.BuildRules(annotations, prefix, cfg),
			TargetPort:         int(selected.Port),
			TargetHostname:     clusterHostname,
			Auth:               blueprint.BuildAuth(annotations, secretData, cfg),
			TargetEnabled:      extras.Enabled,
			TargetPath:         extras.Path,
			TargetPathMatch:    extras.PathMatch,
			TargetRewritePath:  extras.RewritePath,
			TargetRewriteMatch: extras.RewriteMatch,
			TargetPriority:     extras.Priority,
			TargetInternalPort: extras.InternalPort,
			TargetHealthCheck:  extras.HealthCheck,
		}, true
	}

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

func serviceProtocol(p corev1.Protocol) string {
	switch p {
	case corev1.ProtocolUDP:
		return "udp"
	default:
		return "tcp"
	}
}
