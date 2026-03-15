package controller

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/home-operations/newt-sidecar/internal/config"
	"github.com/home-operations/newt-sidecar/internal/resources"
	"github.com/home-operations/newt-sidecar/internal/state"
)

// Controller watches a single Kubernetes resource type and keeps blueprint state in sync.
type Controller struct {
	resourceKeys map[string][]string // "namespace/name" → blueprint keys
	gvr          schema.GroupVersionResource
	convert      func(*unstructured.Unstructured) (metav1.Object, error)
	handler      resources.ResourceHandler
	stateManager *state.Manager
	informer     cache.SharedIndexInformer
	clientset    kubernetes.Interface
	cfg          *config.Config

	ctx    atomic.Value // stores context.Context; context.Background() until Run() sets it
	synced atomic.Bool  // false during initial cache fill; true after ForceWrite()
}

func New(definition *resources.ResourceDefinition, stateManager *state.Manager, informer cache.SharedIndexInformer, clientset kubernetes.Interface, cfg *config.Config) *Controller {
	c := &Controller{
		resourceKeys: make(map[string][]string),
		gvr:          definition.GVR,
		convert:      definition.ConvertFunc,
		handler:      resources.NewHandler(definition),
		stateManager: stateManager,
		informer:     informer,
		clientset:    clientset,
		cfg:          cfg,
	}
	c.ctx.Store(context.Background())

	if _, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onAdd,
		UpdateFunc: c.onUpdate,
		DeleteFunc: c.onDelete,
	}); err != nil {
		slog.Error("failed to register informer event handler", "resource", definition.GVR.Resource, "error", err)
	}

	return c
}

func (c *Controller) Run(ctx context.Context) error {
	c.ctx.Store(ctx)

	if !cache.WaitForCacheSync(ctx.Done(), c.informer.HasSynced) {
		return fmt.Errorf("cache sync timed out for %s", c.gvr.Resource)
	}

	c.stateManager.ForceWrite()
	c.synced.Store(true)

	<-ctx.Done()
	return nil
}

func (c *Controller) onAdd(obj interface{}) {
	metaObj, err := c.toMetaObj(obj)
	if err != nil {
		slog.Error("onAdd: failed to convert object", "resource", c.gvr.Resource, "error", err)
		return
	}
	ctx := c.ctx.Load().(context.Context)
	c.processObject(ctx, metaObj, c.synced.Load())
}

func (c *Controller) onUpdate(_ interface{}, newObj interface{}) {
	metaObj, err := c.toMetaObj(newObj)
	if err != nil {
		slog.Error("onUpdate: failed to convert object", "resource", c.gvr.Resource, "error", err)
		return
	}
	ctx := c.ctx.Load().(context.Context)
	c.processObject(ctx, metaObj, c.synced.Load())
}

func (c *Controller) onDelete(obj interface{}) {
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tombstone.Obj
	}
	metaObj, err := c.toMetaObj(obj)
	if err != nil {
		slog.Error("onDelete: failed to convert object", "resource", c.gvr.Resource, "error", err)
		return
	}
	objKey := fmt.Sprintf("%s/%s", metaObj.GetNamespace(), metaObj.GetName())
	c.removeObject(objKey)
}

func (c *Controller) toMetaObj(obj interface{}) (metav1.Object, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("unexpected object type: %T", obj)
	}
	return c.convert(u)
}

func (c *Controller) resolveAuthSecret(ctx context.Context, obj metav1.Object) map[string]string {
	secretName := obj.GetAnnotations()[c.cfg.AnnotationPrefix+"/auth-secret"]
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
func (c *Controller) processObject(ctx context.Context, obj metav1.Object, write bool) bool {
	objKey := fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetName())

	if !c.handler.ShouldProcess(obj, c.cfg) {
		c.removeObject(objKey)
		return false
	}

	secretData := c.resolveAuthSecret(ctx, obj)
	entries := c.handler.BuildEntries(ctx, obj, secretData, c.cfg)
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

func (c *Controller) GVRString() string {
	return c.gvr.Resource
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
