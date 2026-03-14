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
	"k8s.io/client-go/kubernetes"

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
	clientset     kubernetes.Interface
}

func New(definition *resources.ResourceDefinition, stateManager *state.Manager, dynamicClient dynamic.Interface, clientset kubernetes.Interface) *Controller {
	return &Controller{
		resourceKeys:  make(map[string][]string),
		gvr:           definition.GVR,
		convert:       definition.ConvertFunc,
		handler:       resources.NewHandler(definition),
		stateManager:  stateManager,
		dynamicClient: dynamicClient,
		clientset:     clientset,
	}
}

func (c *Controller) Run(ctx context.Context, cfg *config.Config) error {
	if err := c.initialList(ctx, cfg); err != nil {
		return fmt.Errorf("initial list failed for %s: %w", c.gvr.Resource, err)
	}

	for {
		if err := c.watchLoop(ctx, cfg); err != nil {
			slog.Error("watch loop error", "resource", c.gvr.Resource, "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}
}

func (c *Controller) initialList(ctx context.Context, cfg *config.Config) error {
	list, err := c.dynamicClient.Resource(c.gvr).Namespace(cfg.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list %s: %w", c.gvr.Resource, err)
	}

	changed := false
	for _, item := range list.Items {
		obj, err := c.convert(&item)
		if err != nil {
			slog.Error("failed to convert resource", "resource", c.gvr.Resource, "error", err)
			continue
		}
		if c.processObject(ctx, cfg, obj, false) {
			changed = true
		}
	}

	if changed {
		c.stateManager.ForceWrite()
	}

	return nil
}

func (c *Controller) watchLoop(ctx context.Context, cfg *config.Config) error {
	w, err := c.dynamicClient.Resource(c.gvr).Namespace(cfg.Namespace).Watch(ctx, metav1.ListOptions{})
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
			c.processEvent(ctx, cfg, evt)
		}
	}
}

func (c *Controller) processEvent(ctx context.Context, cfg *config.Config, evt watch.Event) {
	if evt.Type == watch.Bookmark {
		return
	}
	obj, err := c.convertEvent(evt)
	if err != nil {
		slog.Error("failed to process event", "resource", c.gvr.Resource, "error", err)
		return
	}
	c.handleEvent(ctx, cfg, obj, evt.Type)
}

func (c *Controller) convertEvent(evt watch.Event) (metav1.Object, error) {
	unstructuredObj, ok := evt.Object.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("unexpected object type: %T", evt.Object)
	}
	return c.convert(unstructuredObj)
}

func (c *Controller) handleEvent(ctx context.Context, cfg *config.Config, obj metav1.Object, eventType watch.EventType) {
	objKey := fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetName())

	if eventType == watch.Deleted {
		c.removeObject(objKey)
		return
	}

	c.processObject(ctx, cfg, obj, true)
}

// resolveAuthSecret fetches the K8s Secret named by the {prefix}/auth-secret annotation.
// Returns nil when the annotation is absent or the Secret cannot be retrieved.
func (c *Controller) resolveAuthSecret(ctx context.Context, obj metav1.Object, cfg *config.Config) map[string]string {
	secretName := obj.GetAnnotations()[cfg.AnnotationPrefix+"/auth-secret"]
	if secretName == "" {
		return nil
	}
	ns := obj.GetNamespace()
	secret, err := c.clientset.CoreV1().Secrets(ns).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		slog.Warn("failed to resolve auth-secret", "secret", secretName, "namespace", ns, "error", err)
		return nil
	}
	result := make(map[string]string, len(secret.Data))
	for k, v := range secret.Data {
		result[k] = string(v)
	}
	return result
}

// processObject returns true when any change was detected.
func (c *Controller) processObject(ctx context.Context, cfg *config.Config, obj metav1.Object, write bool) bool {
	objKey := fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetName())

	if !c.handler.ShouldProcess(obj, cfg) {
		c.removeObject(objKey)
		return false
	}

	secretData := c.resolveAuthSecret(ctx, obj, cfg)
	entries := c.handler.BuildEntries(ctx, obj, secretData, cfg)
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
