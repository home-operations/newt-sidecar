package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/home-operations/newt-sidecar/internal/config"
	"github.com/home-operations/newt-sidecar/internal/controller"
	"github.com/home-operations/newt-sidecar/internal/health"
	"github.com/home-operations/newt-sidecar/internal/resources/httproute"
	"github.com/home-operations/newt-sidecar/internal/resources/service"
	"github.com/home-operations/newt-sidecar/internal/state"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	restCfg, err := getKubeConfig()
	if err != nil {
		slog.Error("get kubernetes config", "error", err)
		os.Exit(1)
	}

	dc, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		slog.Error("create dynamic client", "error", err)
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		slog.Error("create kubernetes clientset", "error", err)
		os.Exit(1)
	}

	stateManager := state.NewManager(cfg.Output)

	if cfg.HealthPort != 0 {
		go health.Serve(cfg.HealthPort, stateManager)
	}

	stopCh := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(stopCh)
	}()

	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dc, 5*time.Minute, cfg.Namespace, nil)

	var controllers []*controller.Controller

	if cfg.GatewayName != "" {
		def := httproute.Definition()
		informer := factory.ForResource(def.GVR).Informer()
		ctrl := controller.New(def, stateManager, informer, clientset, cfg)
		controllers = append(controllers, ctrl)
	}

	if cfg.EnableService || cfg.AutoService {
		def := service.Definition()
		informer := factory.ForResource(def.GVR).Informer()
		ctrl := controller.New(def, stateManager, informer, clientset, cfg)
		controllers = append(controllers, ctrl)
	}

	factory.Start(stopCh)
	for gvr, ok := range factory.WaitForCacheSync(stopCh) {
		if !ok {
			slog.Error("informer cache sync failed", "resource", gvr.Resource)
			os.Exit(1)
		}
	}

	errCh := make(chan error, len(controllers))
	for _, ctrl := range controllers {
		go func(c *controller.Controller) {
			if err := c.Run(ctx); err != nil {
				errCh <- fmt.Errorf("controller (%s): %w", c.GVRString(), err)
			}
		}(ctrl)
	}

	select {
	case <-ctx.Done():
		slog.Info("shutting down")
	case err := <-errCh:
		slog.Error("controller error", "error", err)
		os.Exit(1)
	}
}

func getKubeConfig() (*rest.Config, error) {
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		cfg, err := rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("in-cluster config: %w", err)
		}
		slog.Info("using in-cluster kubernetes config")
		return cfg, nil
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	cfg, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("kubeconfig: %w", err)
	}

	slog.Info("using kubeconfig")
	return cfg, nil
}
