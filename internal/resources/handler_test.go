package resources_test

import (
	"testing"

	"github.com/home-operations/newt-sidecar/internal/config"
	"github.com/home-operations/newt-sidecar/internal/resources"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHandler_ShouldProcess_OptOutAnnotation(t *testing.T) {
	cfg := &config.Config{AnnotationPrefix: "newt-sidecar"}
	def := &resources.ResourceDefinition{}
	handler := resources.NewHandler(def)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
			Annotations: map[string]string{
				"newt-sidecar/enabled": "false",
			},
		},
	}

	if handler.ShouldProcess(svc, cfg) {
		t.Error("ShouldProcess should return false for explicit opt-out")
	}
}

func TestHandler_ShouldProcess_AutoMode(t *testing.T) {
	cfg := &config.Config{
		AnnotationPrefix: "newt-sidecar",
		AutoService:      true,
	}
	def := &resources.ResourceDefinition{
		AutoConfigFunc: func(cfg *config.Config) bool { return cfg.AutoService },
	}
	handler := resources.NewHandler(def)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
	}

	if !handler.ShouldProcess(svc, cfg) {
		t.Error("ShouldProcess should return true in auto-mode")
	}
}

func TestHandler_ShouldProcess_AnnotationMode(t *testing.T) {
	cfg := &config.Config{AnnotationPrefix: "newt-sidecar"}
	def := &resources.ResourceDefinition{
		AutoConfigFunc: func(cfg *config.Config) bool { return false },
	}
	handler := resources.NewHandler(def)

	t.Run("without annotation should be skipped", func(t *testing.T) {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
		}
		if handler.ShouldProcess(svc, cfg) {
			t.Error("ShouldProcess should return false without required annotation")
		}
	})

	t.Run("with annotation should be processed", func(t *testing.T) {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
				Annotations: map[string]string{
					"newt-sidecar/enabled": "true",
				},
			},
		}
		if !handler.ShouldProcess(svc, cfg) {
			t.Error("ShouldProcess should return true with enabled annotation")
		}
	})
}

func TestHandler_ShouldProcess_FilterFunc(t *testing.T) {
	cfg := &config.Config{
		AnnotationPrefix: "newt-sidecar",
		GatewayName:      "my-gateway",
	}
	def := &resources.ResourceDefinition{
		FilterFunc: func(obj metav1.Object, cfg *config.Config) bool {
			return false
		},
	}
	handler := resources.NewHandler(def)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
	}

	if handler.ShouldProcess(svc, cfg) {
		t.Error("ShouldProcess should return false when FilterFunc returns false")
	}
}

func TestHandler_ShouldProcess_OptOutMode(t *testing.T) {
	cfg := &config.Config{AnnotationPrefix: "newt-sidecar"}
	def := &resources.ResourceDefinition{
		AutoConfigFunc: nil,
		FilterFunc: func(obj metav1.Object, cfg *config.Config) bool {
			return true
		},
	}
	handler := resources.NewHandler(def)

	t.Run("no annotations should be processed (opt-out mode)", func(t *testing.T) {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
		}
		if !handler.ShouldProcess(svc, cfg) {
			t.Error("ShouldProcess should return true in opt-out mode when filter passes")
		}
	})

	t.Run("opt-out annotation should skip", func(t *testing.T) {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
				Annotations: map[string]string{
					"newt-sidecar/enabled": "false",
				},
			},
		}
		if handler.ShouldProcess(svc, cfg) {
			t.Error("ShouldProcess should return false when opted out")
		}
	})

	t.Run("filter rejection should skip", func(t *testing.T) {
		defReject := &resources.ResourceDefinition{
			AutoConfigFunc: nil,
			FilterFunc: func(obj metav1.Object, cfg *config.Config) bool {
				return false
			},
		}
		handlerReject := resources.NewHandler(defReject)
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
		}
		if handlerReject.ShouldProcess(svc, cfg) {
			t.Error("ShouldProcess should return false when filter rejects")
		}
	})
}
