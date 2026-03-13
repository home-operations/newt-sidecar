package controller

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"

	"github.com/home-operations/newt-sidecar/internal/config"
	"github.com/home-operations/newt-sidecar/internal/resources"
	"github.com/home-operations/newt-sidecar/internal/state"
)

// Controller watches a single Kubernetes resource type and keeps blueprint state in sync.
type Controller struct {
	resourceKeys  map[string][]string // "namespace/name" → blueprint keys
	gvr           schema.GroupVersionResource
	convert       func(*unstructured.Unstructured) (metav1.Object, error)
	handler       resources.ResourceHandler
	stateManager  *state.Manager
	dynamicClient dynamic.Interface
}

func New(definition *resources.ResourceDefinition, stateManager *state.Manager, dynamicClient dynamic.Interface) *Controller {
	return &Controller{
		resourceKeys:  make(map[string][]string),
		gvr:           definition.GVR,
		convert:       definition.ConvertFunc,
		handler:       resources.NewHandler(definition),
		stateManager:  stateManager,
		dynamicClient: dynamicClient,
	}
}

func (c *Controller) Run(ctx context.Context, cfg *config.Config) error {
	rv, err := c.initialList(ctx, cfg)
	if err != nil {
		return fmt.Errorf("initial list failed for %s: %w", c.gvr.Resource, err)
	}

	for {
		if err := c.watchLoop(ctx, cfg, rv); err != nil {
			slog.Error("watch loop error", "resource", c.gvr.Resource, "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}
}

func (c *Controller) initialList(ctx context.Context, cfg *config.Config) (string, error) {
	list, err := c.dynamicClient.Resource(c.gvr).Namespace(cfg.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("list %s: %w", c.gvr.Resource, err)
	}

	changed := false
	for _, item := range list.Items {
		obj, err := c.convert(&item)
		if err != nil {
			slog.Error("failed to convert resource", "resource", c.gvr.Resource, "error", err)
			continue
		}
		if c.processObject(cfg, obj, false) {
			changed = true
		}
	}

	if changed {
		c.stateManager.ForceWrite()
	}

	return list.GetResourceVersion(), nil
}

func (c *Controller) watchLoop(ctx context.Context, cfg *config.Config, resourceVersion string) error {
	w, err := c.dynamicClient.Resource(c.gvr).Namespace(cfg.Namespace).Watch(ctx, metav1.ListOptions{
		ResourceVersion: resourceVersion,
	})
	if err != nil {
		return fmt.Errorf("watch %s: %w", c.gvr.Resource, err)
	}
	defer w.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case evt, ok := <-w.ResultChan():
			if !ok {
				return fmt.Errorf("watch channel closed")
			}
			c.processEvent(cfg, evt)
		}
	}
}

func (c *Controller) processEvent(cfg *config.Config, evt watch.Event) {
	obj, err := c.convertEvent(evt)
	if err != nil {
		slog.Error("failed to process event", "resource", c.gvr.Resource, "error", err)
		return
	}
	c.handleEvent(cfg, obj, evt.Type)
}

func (c *Controller) convertEvent(evt watch.Event) (metav1.Object, error) {
	unstructuredObj, ok := evt.Object.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("unexpected object type: %T", evt.Object)
	}
	return c.convert(unstructuredObj)
}

func (c *Controller) handleEvent(cfg *config.Config, obj metav1.Object, eventType watch.EventType) {
	objKey := fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetName())

	if eventType == watch.Deleted {
		c.removeObject(objKey)
		return
	}

	c.processObject(cfg, obj, true)
}

// processObject returns true when any change was detected.
func (c *Controller) processObject(cfg *config.Config, obj metav1.Object, write bool) bool {
	objKey := fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetName())

	if !c.handler.ShouldProcess(obj, cfg) {
		c.removeObject(objKey)
		return false
	}

	entries := c.handler.BuildEntries(obj, cfg)
	if len(entries) == 0 {
		c.removeObject(objKey)
		return false
	}

	newKeys := make([]string, 0, len(entries))
	for k := range entries {
		newKeys = append(newKeys, k)
	}

	oldKeys := c.resourceKeys[objKey]
	c.resourceKeys[objKey] = newKeys

	changed := false

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

	for key, resource := range entries {
		if c.stateManager.AddOrUpdate(key, resource, write) {
			slog.Info("updated resource in state",
				"resource", c.gvr.Resource,
				"key", key,
				"object", objKey,
			)
			changed = true
		}
	}

	return changed
}

func (c *Controller) removeObject(objKey string) {
	keys := c.resourceKeys[objKey]
	delete(c.resourceKeys, objKey)

	for _, key := range keys {
		if removed := c.stateManager.Remove(key); removed {
			slog.Info("removed resource from state", "resource", c.gvr.Resource, "key", key)
		}
	}
}
