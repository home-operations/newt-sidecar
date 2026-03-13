package resources

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/home-operations/newt-sidecar/internal/blueprint"
	"github.com/home-operations/newt-sidecar/internal/config"
)

type ResourceHandler interface {
	ShouldProcess(obj v1.Object, cfg *config.Config) bool
	BuildEntries(obj v1.Object, cfg *config.Config) map[string]blueprint.Resource
}

type Handler struct {
	definition *ResourceDefinition
}

var _ ResourceHandler = (*Handler)(nil)

func NewHandler(definition *ResourceDefinition) *Handler {
	return &Handler{definition: definition}
}

func (h *Handler) ShouldProcess(obj v1.Object, cfg *config.Config) bool {
	if isOptOut(obj, cfg) {
		return false
	}

	if h.definition.FilterFunc != nil && !h.definition.FilterFunc(obj, cfg) {
		return false
	}

	if h.definition.AutoConfigFunc == nil {
		return true
	}

	if h.definition.AutoConfigFunc(cfg) {
		return true
	}

	return HasRequiredAnnotations(obj, cfg)
}

func (h *Handler) BuildEntries(obj v1.Object, cfg *config.Config) map[string]blueprint.Resource {
	if h.definition.BuildEntries == nil {
		return nil
	}
	return h.definition.BuildEntries(obj, cfg)
}

func isOptOut(obj v1.Object, cfg *config.Config) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}

	val, ok := annotations[cfg.AnnotationPrefix+"/enabled"]
	if !ok {
		return false
	}

	return val == "false" || val == "0"
}

func HasRequiredAnnotations(obj v1.Object, cfg *config.Config) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}

	val, ok := annotations[cfg.AnnotationPrefix+"/enabled"]
	return ok && (val == "true" || val == "1")
}
