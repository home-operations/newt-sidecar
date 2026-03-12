package controller

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"

	"github.com/home-operations/newt-sidecar/internal/blueprint"
	"github.com/home-operations/newt-sidecar/internal/config"
	"github.com/home-operations/newt-sidecar/internal/state"
)

var serviceGVR = schema.GroupVersionResource{
	Group:    "",
	Version:  "v1",
	Resource: "services",
}

// ServiceController watches Service resources and writes blueprint entries.
//
// Two modes per Service (selected via annotations):
//
//	HTTP mode  — newt-sidecar/full-domain is set.
//	             Pangolin exposes the Service at the given domain over HTTPS.
//	             The internal target is the cluster-internal Service DNS name
//	             reached via newt-sidecar/method (default "http"). No Envoy hop.
//
//	TCP/UDP mode — newt-sidecar/full-domain is absent.
//	             Pangolin opens a raw TCP (or UDP) port tunnelled directly to
//	             the cluster-internal Service DNS name.
//
// In both modes Services always route directly — no gateway hop is involved.
type ServiceController struct {
	mu sync.Mutex
	// serviceKeys maps "namespace/name" → blueprint resource key registered for
	// that Service. Used to clean up stale entries on change or deletion.
	serviceKeys   map[string]string
	stateManager  *state.Manager
	dynamicClient dynamic.Interface
}

// NewServiceController creates a new ServiceController.
func NewServiceController(stateManager *state.Manager, dynamicClient dynamic.Interface) *ServiceController {
	return &ServiceController{
		serviceKeys:   make(map[string]string),
		stateManager:  stateManager,
		dynamicClient: dynamicClient,
	}
}

// Run performs the initial list then enters the watch loop.
func (sc *ServiceController) Run(ctx context.Context, cfg *config.Config) error {
	if err := sc.initialList(ctx, cfg); err != nil {
		return fmt.Errorf("service initial list failed: %w", err)
	}

	for {
		if err := sc.watchLoop(ctx, cfg); err != nil {
			slog.Error("service watch loop error", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}
}

func (sc *ServiceController) initialList(ctx context.Context, cfg *config.Config) error {
	list, err := sc.dynamicClient.Resource(serviceGVR).Namespace(cfg.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list services: %w", err)
	}

	changed := false
	for _, item := range list.Items {
		svc, err := convertToService(&item)
		if err != nil {
			slog.Error("failed to convert service", "name", item.GetName(), "error", err)
			continue
		}
		if sc.processService(cfg, svc, false) {
			changed = true
		}
	}

	if changed {
		sc.stateManager.ForceWrite()
	}

	return nil
}

func (sc *ServiceController) watchLoop(ctx context.Context, cfg *config.Config) error {
	w, err := sc.dynamicClient.Resource(serviceGVR).Namespace(cfg.Namespace).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("watch services: %w", err)
	}
	defer w.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case evt, ok := <-w.ResultChan():
			if !ok {
				return fmt.Errorf("service watch channel closed")
			}
			sc.handleServiceEvent(cfg, evt)
		}
	}
}

func (sc *ServiceController) handleServiceEvent(cfg *config.Config, evt watch.Event) {
	unstructuredObj, ok := evt.Object.(*unstructured.Unstructured)
	if !ok {
		slog.Error("unexpected service event object type", "type", fmt.Sprintf("%T", evt.Object))
		return
	}

	svc, err := convertToService(unstructuredObj)
	if err != nil {
		slog.Error("failed to convert service", "error", err)
		return
	}

	svcKey := fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)

	if evt.Type == watch.Deleted {
		sc.removeService(svcKey)
		return
	}

	sc.processService(cfg, svc, true)
}

// processService processes a Service and updates state.
// Returns true if any change was detected.
//
// Opt-in/out is controlled exclusively via annotations, consistent with the
// HTTPRoute controller. In annotation-mode (default) a Service must carry
// newt-sidecar/enabled: "true" to be processed. In auto-mode every Service is
// processed unless newt-sidecar/enabled is explicitly "false" or "0".
func (sc *ServiceController) processService(cfg *config.Config, svc *corev1.Service, write bool) bool {
	svcKey := fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)
	annotations := svc.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	prefix := cfg.AnnotationPrefix

	enabledVal, hasEnabled := annotations[prefix+"/enabled"]

	if !cfg.AutoService {
		// Annotation-mode: only process when explicitly enabled.
		if !hasEnabled || (enabledVal != "true" && enabledVal != "1") {
			sc.removeService(svcKey)
			return false
		}
	} else {
		// Auto-mode: process everything unless explicitly disabled.
		if hasEnabled && (enabledVal == "false" || enabledVal == "0") {
			sc.removeService(svcKey)
			return false
		}
	}

	// Resolve port and mode (HTTP vs TCP/UDP).
	sp, ok := sc.resolvePort(svc, annotations, cfg)
	if !ok {
		sc.removeService(svcKey)
		return false
	}

	// Derive the blueprint key. HTTP mode uses the domain as key (consistent
	// with HTTPRoute); TCP/UDP mode uses namespace-name-port.
	var key string
	if sp.FullDomain != "" {
		key = blueprint.HostnameToKey(sp.FullDomain)
	} else {
		key = blueprint.ServiceToKey(svc.Namespace, svc.Name, strconv.Itoa(sp.ProxyPort))
	}

	// Swap old key → new key.
	sc.mu.Lock()
	oldKey := sc.serviceKeys[svcKey]
	sc.serviceKeys[svcKey] = key
	sc.mu.Unlock()

	changed := false

	// Remove the old key if it differs (e.g. mode or port changed).
	if oldKey != "" && oldKey != key {
		if sc.stateManager.Remove(oldKey) {
			changed = true
		}
	}

	// Add or update the current entry.
	resource := blueprint.BuildServiceResource(sp, cfg)
	if sc.stateManager.AddOrUpdate(key, resource, write) {
		slog.Info("updated service resource in state",
			"key", key,
			"service", svcKey,
			"mode", func() string {
				if sp.FullDomain != "" {
					return "http"
				}
				return sp.Protocol
			}(),
		)
		changed = true
	}

	return changed
}

// resolvePort selects the Service port to expose and determines the mode.
//
// Mode selection:
//
//	newt-sidecar/full-domain is set → HTTP mode.
//	  The Service is exposed at the given public domain via Pangolin HTTPS.
//	  newt-sidecar/method controls the internal protocol (default "http").
//	  newt-sidecar/ssl controls SSL on the Pangolin resource (default: cfg.SSL).
//
//	newt-sidecar/full-domain is absent → TCP/UDP mode.
//	  newt-sidecar/protocol selects "tcp" (default) or "udp".
//
// Port selection (both modes):
//
//	1. newt-sidecar/port annotation → match by port number or name.
//	2. Service has exactly one port → use it automatically.
//	3. Service has a port named "http" → use it.
//	4. None of the above → skip and log a warning.
func (sc *ServiceController) resolvePort(svc *corev1.Service, annotations map[string]string, cfg *config.Config) (blueprint.ServicePort, bool) {
	prefix := cfg.AnnotationPrefix
	clusterHostname := fmt.Sprintf("%s.%s.svc.cluster.local", svc.Name, svc.Namespace)
	svcKey := fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)

	// --- Mode detection ---
	fullDomain := strings.TrimSpace(annotations[prefix+"/full-domain"])
	httpMode := fullDomain != ""

	// --- Port selection ---
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
			slog.Warn("service has multiple ports but none is named 'http'; set newt-sidecar/port to select a port explicitly, skipping service",
				"service", svcKey)
			return blueprint.ServicePort{}, false
		}
	}

	// --- Display name ---
	portName := selected.Name
	if portName == "" {
		portName = strconv.Itoa(int(selected.Port))
	}
	displayName := fmt.Sprintf("%s %s", svc.Name, portName)
	if v, ok := annotations[prefix+"/name"]; ok && v != "" {
		displayName = v
	}

	// --- Build ServicePort ---
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

func (sc *ServiceController) removeService(svcKey string) {
	sc.mu.Lock()
	key := sc.serviceKeys[svcKey]
	delete(sc.serviceKeys, svcKey)
	sc.mu.Unlock()

	if key != "" {
		if removed := sc.stateManager.Remove(key); removed {
			slog.Info("removed service resource from state", "key", key)
		}
	}
}

func convertToService(u *unstructured.Unstructured) (*corev1.Service, error) {
	var svc corev1.Service
	if err := k8sruntime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &svc); err != nil {
		return nil, fmt.Errorf("failed to convert to Service: %w", err)
	}
	return &svc, nil
}
