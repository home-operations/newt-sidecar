package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/open-policy-agent/cert-controller/pkg/rotator"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/home-operations/newt-sidecar/api/v1alpha1"
	"github.com/home-operations/newt-sidecar/internal/config"
	"github.com/home-operations/newt-sidecar/internal/controller"
	crdprivateresource "github.com/home-operations/newt-sidecar/internal/controller/privateresource"
	crdpublicresource "github.com/home-operations/newt-sidecar/internal/controller/publicresource"
	"github.com/home-operations/newt-sidecar/internal/health"
	"github.com/home-operations/newt-sidecar/internal/resources/httproute"
	"github.com/home-operations/newt-sidecar/internal/resources/service"
	"github.com/home-operations/newt-sidecar/internal/state"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	scheme  = runtime.NewScheme()
	certDir = "/tmp/k8s-webhook-server/serving-certs"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var metricsServiceName string
	var certSecretName string
	var secureMetrics bool
	var enableHTTP2 bool
	var enableLeaderElection bool
	var logLevel string
	var tlsOpts []func(*tls.Config)

	// Register controller flags before config.Load() calls flag.Parse()
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0",
		"The address the metrics endpoint binds to. Use :8443 for HTTPS or :8080 for HTTP, or 0 to disable.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for the controller-runtime manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set, the metrics endpoint is served securely via HTTPS.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics server.")
	flag.StringVar(&metricsServiceName, "metrics-service-name", "newt-sidecar-metrics-service",
		"The service name used as the DNS name in the metrics TLS certificate.")
	flag.StringVar(&certSecretName, "cert-secret-name", "newt-sidecar-metrics-cert",
		"The name of the Secret to store the metrics TLS certificate (used when --metrics-secure=true).")
	flag.StringVar(&logLevel, "log-level", "info",
		"Log level for the controller-runtime manager (info, debug).")

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
		c := controller.New(def, stateManager, informer, clientset, cfg)
		controllers = append(controllers, c)
	}

	if cfg.EnableService || cfg.AutoService {
		def := service.Definition()
		informer := factory.ForResource(def.GVR).Informer()
		c := controller.New(def, stateManager, informer, clientset, cfg)
		controllers = append(controllers, c)
	}

	factory.Start(stopCh)
	for gvr, ok := range factory.WaitForCacheSync(stopCh) {
		if !ok {
			slog.Error("informer cache sync failed", "resource", gvr.Resource)
			os.Exit(1)
		}
	}

	errCh := make(chan error, len(controllers)+1)
	for _, c := range controllers {
		go func(c *controller.Controller) {
			if err := c.Run(ctx); err != nil {
				errCh <- fmt.Errorf("controller (%s): %w", c.GVRString(), err)
			}
		}(c)
	}

	// Configure controller-runtime manager for CRD reconcilers and metrics.
	zapOpts := zap.Options{}
	switch strings.ToLower(logLevel) {
	case "debug":
		zapOpts.Development = true
	default:
		zapOpts.Development = false
	}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOpts)))

	disableHTTP2 := func(c *tls.Config) {
		c.NextProtos = []string{"http/1.1"}
	}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		CertDir:       certDir,
		TLSOpts:       tlsOpts,
	}
	if secureMetrics {
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme:           scheme,
		Metrics:          metricsServerOptions,
		LeaderElection:   enableLeaderElection,
		LeaderElectionID: "newt-sidecar.home-operations.com",
		// Health probes are served by the sidecar's own health endpoint (cfg.HealthPort).
		HealthProbeBindAddress: "",
	})
	if err != nil {
		slog.Error("unable to create controller-runtime manager", "error", err)
		os.Exit(1)
	}

	if secureMetrics {
		controllerNamespace := os.Getenv("CONTROLLER_NAMESPACE")
		if controllerNamespace == "" {
			controllerNamespace = "default"
		}
		dnsName := fmt.Sprintf("%s.%s.svc", metricsServiceName, controllerNamespace)
		certSetupFinished := make(chan struct{})
		if err := rotator.AddRotator(mgr, &rotator.CertRotator{
			SecretKey: types.NamespacedName{
				Namespace: controllerNamespace,
				Name:      certSecretName,
			},
			CertDir:        certDir,
			CAName:         "newt-sidecar-ca",
			CAOrganization: "newt-sidecar",
			DNSName:        dnsName,
			ExtraDNSNames: []string{
				fmt.Sprintf("%s.%s.svc.cluster.local", metricsServiceName, controllerNamespace),
			},
			IsReady:              certSetupFinished,
			EnableReadinessCheck: false,
			Webhooks:             []rotator.WebhookInfo{},
		}); err != nil {
			slog.Error("unable to set up cert rotation", "error", err)
			os.Exit(1)
		}
	}

	if crdInstalled(clientset, v1alpha1.GroupVersion.String()) {
		slog.Info("CRDs detected, registering CRD reconcilers")

		if err := (&crdprivateresource.Reconciler{
			Client:       mgr.GetClient(),
			Scheme:       mgr.GetScheme(),
			StateManager: stateManager,
			SiteID:       cfg.SiteID,
		}).SetupWithManager(mgr); err != nil {
			slog.Error("unable to register PrivateResource reconciler", "error", err)
			os.Exit(1)
		}

		if err := (&crdpublicresource.Reconciler{
			Client:       mgr.GetClient(),
			Scheme:       mgr.GetScheme(),
			StateManager: stateManager,
			SiteID:       cfg.SiteID,
		}).SetupWithManager(mgr); err != nil {
			slog.Error("unable to register PublicResource reconciler", "error", err)
			os.Exit(1)
		}
	} else {
		slog.Info("CRDs not installed, skipping CRD reconcilers (install CRDs and restart the sidecar to enable them)")
	}

	go func() {
		if err := mgr.Start(ctx); err != nil {
			errCh <- fmt.Errorf("controller-runtime manager: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutting down")
	case err := <-errCh:
		slog.Error("controller error", "error", err)
		os.Exit(1)
	}
}

// crdInstalled checks whether the given API group/version is registered in the cluster.
func crdInstalled(clientset kubernetes.Interface, groupVersion string) bool {
	_, err := clientset.Discovery().ServerResourcesForGroupVersion(groupVersion)
	return err == nil
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
