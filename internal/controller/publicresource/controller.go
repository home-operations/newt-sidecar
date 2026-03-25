package publicresource

import (
	"context"
	"fmt"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"

	v1alpha1 "github.com/home-operations/newt-sidecar/api/v1alpha1"
	"github.com/home-operations/newt-sidecar/internal/blueprint"
	"github.com/home-operations/newt-sidecar/internal/state"
)

// +kubebuilder:rbac:groups=newt-sidecar.home-operations.com,resources=publicresources,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconciler reconciles PublicResource objects and keeps the blueprint public-resources in sync.
type Reconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	StateManager *state.Manager
	SiteID       string
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var resource v1alpha1.PublicResource
	if err := r.Get(ctx, req.NamespacedName, &resource); err != nil {
		if apierrors.IsNotFound(err) {
			if removed := r.StateManager.Remove(req.Name); removed {
				logger.Info("removed resource from blueprint", "key", req.Name)
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if err := validate(&resource.Spec); err != nil {
		logger.Error(err, "invalid PublicResource spec, skipping", "name", req.Name)
		return ctrl.Result{}, nil
	}

	secretData := r.resolveAuthSecret(ctx, &resource)
	res := buildResource(&resource.Spec, secretData, r.SiteID)

	if updated := r.StateManager.AddOrUpdate(req.Name, res, true); updated {
		logger.Info("updated resource in blueprint", "key", req.Name)
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.PublicResource{}).
		Named("publicresource").
		Complete(r)
}

// resolveAuthSecret reads the Kubernetes Secret referenced in spec.auth.authSecretRef.
func (r *Reconciler) resolveAuthSecret(ctx context.Context, resource *v1alpha1.PublicResource) map[string]string {
	if resource.Spec.Auth == nil || resource.Spec.Auth.AuthSecretRef == "" {
		return nil
	}
	logger := log.FromContext(ctx)
	secretName := resource.Spec.Auth.AuthSecretRef
	ns := resource.Namespace

	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: secretName}, &secret); err != nil {
		logger.Error(err, "failed to resolve auth secret", "secret", secretName, "namespace", ns)
		return nil
	}

	result := make(map[string]string, len(secret.Data))
	for k, v := range secret.Data {
		result[k] = string(v)
	}
	return result
}

// buildResource converts a PublicResourceSpec into a blueprint.Resource.
func buildResource(spec *v1alpha1.PublicResourceSpec, secretData map[string]string, siteID string) blueprint.Resource {
	res := blueprint.Resource{
		Name:          spec.Name,
		Protocol:      spec.Protocol,
		Enabled:       spec.Enabled,
		SSL:           spec.Ssl,
		FullDomain:    spec.FullDomain,
		HostHeader:    spec.HostHeader,
		TLSServerName: spec.TlsServerName,
		ProxyPort:     spec.ProxyPort,
		Headers:       convertHeaders(spec.Headers),
		Rules:         convertRules(spec.Rules),
		Maintenance:   convertMaintenance(spec.Maintenance),
		Auth:          buildAuth(spec.Auth, secretData),
		Targets:       convertTargets(spec.Targets, siteID),
	}

	// Default TLSServerName to FullDomain when not set (http mode)
	if res.TLSServerName == "" && res.FullDomain != "" {
		res.TLSServerName = res.FullDomain
	}

	return res
}

func convertHeaders(headers []v1alpha1.PublicHeaderSpec) []blueprint.Header {
	if len(headers) == 0 {
		return nil
	}
	out := make([]blueprint.Header, len(headers))
	for i, h := range headers {
		out[i] = blueprint.Header{Name: h.Name, Value: h.Value}
	}
	return out
}

func convertRules(rules []v1alpha1.PublicRuleSpec) []blueprint.Rule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]blueprint.Rule, len(rules))
	for i, r := range rules {
		out[i] = blueprint.Rule{
			Action:   r.Action,
			Match:    r.Match,
			Value:    r.Value,
			Priority: r.Priority,
		}
	}
	return out
}

func convertMaintenance(m *v1alpha1.PublicMaintenanceSpec) *blueprint.Maintenance {
	if m == nil {
		return nil
	}
	return &blueprint.Maintenance{
		Enabled:       m.Enabled,
		Type:          m.Type,
		Title:         m.Title,
		Message:       m.Message,
		EstimatedTime: m.EstimatedTime,
	}
}

func buildAuth(spec *v1alpha1.PublicAuthSpec, secretData map[string]string) *blueprint.Auth {
	if spec == nil && len(secretData) == 0 {
		return nil
	}

	auth := &blueprint.Auth{}

	if spec != nil {
		auth.SSOEnabled = spec.SsoEnabled
		auth.SSORoles = spec.SsoRoles
		auth.SSOUsers = spec.SsoUsers
		auth.WhitelistUsers = spec.WhitelistUsers
		auth.AutoLoginIDP = spec.AutoLoginIdp
	}

	if secretData != nil {
		if v, ok := secretData["pincode"]; ok && v != "" {
			if parsed, err := strconv.Atoi(v); err == nil {
				auth.Pincode = parsed
			}
		}
		if v, ok := secretData["password"]; ok {
			auth.Password = v
		}
		baUser := secretData["basic-auth-user"]
		baPass := secretData["basic-auth-password"]
		if baUser != "" || baPass != "" {
			auth.BasicAuth = &blueprint.BasicAuth{User: baUser, Password: baPass}
		}
	}

	// Return nil if nothing was set
	if !auth.SSOEnabled &&
		len(auth.SSORoles) == 0 &&
		len(auth.SSOUsers) == 0 &&
		len(auth.WhitelistUsers) == 0 &&
		auth.AutoLoginIDP == 0 &&
		auth.Pincode == 0 &&
		auth.Password == "" &&
		auth.BasicAuth == nil {
		return nil
	}

	return auth
}

func convertTargets(targets []v1alpha1.PublicTargetSpec, siteID string) []blueprint.Target {
	out := make([]blueprint.Target, len(targets))
	for i, t := range targets {
		out[i] = blueprint.Target{
			Site:         siteID,
			Hostname:     t.Hostname,
			Port:         t.Port,
			Method:       t.Method,
			Enabled:      t.Enabled,
			InternalPort: t.InternalPort,
			Path:         t.Path,
			PathMatch:    t.PathMatch,
			RewritePath:  t.RewritePath,
			RewriteMatch: t.RewriteMatch,
			Priority:     t.Priority,
			HealthCheck:  convertHealthCheck(t.Healthcheck),
		}
	}
	return out
}

func convertHealthCheck(hc *v1alpha1.PublicHealthCheckSpec) *blueprint.HealthCheck {
	if hc == nil {
		return nil
	}
	return &blueprint.HealthCheck{
		Hostname:          hc.Hostname,
		Port:              hc.Port,
		Enabled:           hc.Enabled,
		Path:              hc.Path,
		Scheme:            hc.Scheme,
		Mode:              hc.Mode,
		Interval:          hc.Interval,
		UnhealthyInterval: hc.UnhealthyInterval,
		Timeout:           hc.Timeout,
		Headers:           convertHeaders(hc.Headers),
		FollowRedirects:   hc.FollowRedirects,
		Method:            hc.Method,
		Status:            hc.Status,
	}
}

// validate checks cross-field constraints that cannot be expressed with kubebuilder markers alone.
func validate(spec *v1alpha1.PublicResourceSpec) error {
	switch spec.Protocol {
	case "http":
		if spec.FullDomain == "" {
			return fmt.Errorf("fullDomain is required when protocol is \"http\"")
		}
	case "tcp", "udp":
		if spec.ProxyPort == 0 {
			return fmt.Errorf("proxyPort is required when protocol is %q", spec.Protocol)
		}
	}
	return nil
}
