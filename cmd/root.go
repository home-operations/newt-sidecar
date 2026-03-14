package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/home-operations/newt-sidecar/internal/config"
	"github.com/home-operations/newt-sidecar/internal/controller"
	"github.com/home-operations/newt-sidecar/internal/resources/httproute"
	"github.com/home-operations/newt-sidecar/internal/resources/service"
	"github.com/home-operations/newt-sidecar/internal/state"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	cfg := config.Load()
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

	// errCh collects fatal errors from controllers so main can exit cleanly.
	errCh := make(chan error, 2)

	// HTTPRoute controller — only started when a gateway is configured.
	if cfg.GatewayName != "" {
		ctrl := controller.New(httproute.Definition(), stateManager, dc, clientset)
		go func() {
			if err := ctrl.Run(ctx, cfg); err != nil {
				errCh <- fmt.Errorf("httproute controller: %w", err)
			}
		}()
	}

	// Service controller — started when --enable-service or --auto-service is set.
	if cfg.EnableService || cfg.AutoService {
		ctrl := controller.New(service.Definition(), stateManager, dc, clientset)
		go func() {
			if err := ctrl.Run(ctx, cfg); err != nil {
				errCh <- fmt.Errorf("service controller: %w", err)
			}
		}()
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
