package controller

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"

	"github.com/home-operations/newt-sidecar/internal/config"
	"github.com/home-operations/newt-sidecar/internal/resources"
	"github.com/home-operations/newt-sidecar/internal/state"
)

// Controller is a generic Kubernetes resource watcher that builds blueprint
// entries via a ResourceDefinition. Modelled after the gatus-sidecar pattern:
// adding a new resource type only requires a new ResourceDefinition — no new
// controller code.
type Controller struct {
	mu sync.Mutex
	// resourceKeys maps "namespace/name" → []blueprint keys registered for that
	// object. A single object may produce multiple keys (e.g. one per HTTPRoute
	// hostname).
	resourceKeys  map[string][]string
	definition    *resources.ResourceDefinition
	stateManager  *state.Manager
	dynamicClient dynamic.Interface
}

// New creates a Controller for the given ResourceDefinition.
func New(definition *resources.ResourceDefinition, stateManager *state.Manager, dynamicClient dynamic.Interface) *Controller {
	return &Controller{
		resourceKeys:  make(map[string][]string),
		definition:    definition,
		stateManager:  stateManager,
		dynamicClient: dynamicClient,
	}
}

// Run performs the initial list then enters the watch loop.
func (c *Controller) Run(ctx context.Context, cfg *config.Config) error {
	if err := c.initialList(ctx, cfg); err != nil {
		return fmt.Errorf("%s initial list failed: %w", c.definition.GVR.Resource, err)
	}

	for {
		if err := c.watchLoop(ctx, cfg); err != nil {
			slog.Error("watch loop error", "resource", c.definition.GVR.Resource, "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}
}

func (c *Controller) initialList(ctx context.Context, cfg *config.Config) error {
	list, err := c.dynamicClient.Resource(c.definition.GVR).Namespace(cfg.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list %s: %w", c.definition.GVR.Resource, err)
	}

	changed := false
	for _, item := range list.Items {
		obj, err := c.definition.ConvertFunc(&item)
		if err != nil {
			slog.Error("failed to convert resource", "resource", c.definition.GVR.Resource, "name", item.GetName(), "error", err)
			continue
		}
		if c.processObject(cfg, obj, false) {
			changed = true
		}
	}

	if changed {
		c.stateManager.ForceWrite()
	}

	return nil
}

func (c *Controller) watchLoop(ctx context.Context, cfg *config.Config) error {
	w, err := c.dynamicClient.Resource(c.definition.GVR).Namespace(cfg.Namespace).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("watch %s: %w", c.definition.GVR.Resource, err)
	}
	defer w.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case evt, ok := <-w.ResultChan():
			if !ok {
				return fmt.Errorf("%s watch channel closed", c.definition.GVR.Resource)
			}
			c.handleEvent(cfg, evt)
		}
	}
}

func (c *Controller) handleEvent(cfg *config.Config, evt watch.Event) {
	unstructuredObj, ok := evt.Object.(*unstructured.Unstructured)
	if !ok {
		slog.Error("unexpected event object type", "resource", c.definition.GVR.Resource, "type", fmt.Sprintf("%T", evt.Object))
		return
	}

	obj, err := c.definition.ConvertFunc(unstructuredObj)
	if err != nil {
		slog.Error("failed to convert resource", "resource", c.definition.GVR.Resource, "error", err)
		return
	}

	objKey := fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetName())

	if evt.Type == watch.Deleted {
		c.removeObject(objKey)
		return
	}

	c.processObject(cfg, obj, true)
}

// processObject processes a single Kubernetes object and updates state.
// Returns true when any change was detected.
func (c *Controller) processObject(cfg *config.Config, obj metav1.Object, write bool) bool {
	objKey := fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetName())

	if !c.definition.ShouldProcess(obj, cfg) {
		c.removeObject(objKey)
		return false
	}

	entries := c.definition.BuildEntries(obj, cfg)
	if len(entries) == 0 {
		c.removeObject(objKey)
		return false
	}

	// Compute new key set.
	newKeys := make([]string, 0, len(entries))
	for k := range entries {
		newKeys = append(newKeys, k)
	}

	// Swap old keys → new keys.
	c.mu.Lock()
	oldKeys := c.resourceKeys[objKey]
	c.resourceKeys[objKey] = newKeys
	c.mu.Unlock()

	changed := false

	// Remove keys no longer produced by this object.
	newSet := make(map[string]bool, len(newKeys))
	for _, k := range newKeys {
		newSet[k] = true
	}
	for _, old := range oldKeys {
		if !newSet[old] {
			if c.stateManager.Remove(old) {
				changed = true
			}
		}
	}

	// Add or update current entries.
	for key, resource := range entries {
		if c.stateManager.AddOrUpdate(key, resource, write) {
			slog.Info("updated resource in state",
				"resource", c.definition.GVR.Resource,
				"key", key,
				"object", objKey,
			)
			changed = true
		}
	}

	return changed
}

func (c *Controller) removeObject(objKey string) {
	c.mu.Lock()
	keys := c.resourceKeys[objKey]
	delete(c.resourceKeys, objKey)
	c.mu.Unlock()

	for _, key := range keys {
		if removed := c.stateManager.Remove(key); removed {
			slog.Info("removed resource from state", "resource", c.definition.GVR.Resource, "key", key)
		}
	}
}
