package privateresource

import (
	"context"
	"fmt"
	"net"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/home-operations/newt-sidecar/api/v1alpha1"
	"github.com/home-operations/newt-sidecar/internal/blueprint"
	"github.com/home-operations/newt-sidecar/internal/state"
)

// +kubebuilder:rbac:groups=newt-sidecar.home-operations.com,resources=privateresources,verbs=get;list;watch

// Reconciler reconciles PrivateResource objects and keeps the blueprint private-resources in sync.
type Reconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	StateManager *state.Manager
	SiteID       string
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var resource v1alpha1.PrivateResource
	if err := r.Get(ctx, req.NamespacedName, &resource); err != nil {
		if apierrors.IsNotFound(err) {
			if removed := r.StateManager.RemovePrivate(req.Name); removed {
				logger.Info("removed private resource from blueprint", "key", req.Name)
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if err := validate(&resource.Spec); err != nil {
		logger.Error(err, "invalid PrivateResource spec, skipping", "name", req.Name)
		return ctrl.Result{}, nil
	}

	pr := blueprint.PrivateResource{
		Name:        resource.Spec.Name,
		Mode:        resource.Spec.Mode,
		Destination: resource.Spec.Destination,
		Site:        r.SiteID,
		TCPPorts:    resource.Spec.TcpPorts,
		UDPPorts:    resource.Spec.UdpPorts,
		DisableICMP: resource.Spec.DisableIcmp,
		Alias:       resource.Spec.Alias,
		Roles:       resource.Spec.Roles,
		Users:       resource.Spec.Users,
		Machines:    resource.Spec.Machines,
	}

	if updated := r.StateManager.AddOrUpdatePrivate(req.Name, pr, true); updated {
		logger.Info("updated private resource in blueprint", "key", req.Name)
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.PrivateResource{}).
		Named("privateresource").
		Complete(r)
}

// validate checks the spec fields that cannot be expressed with kubebuilder markers alone.
func validate(spec *v1alpha1.PrivateResourceSpec) error {
	if spec.Mode == "cidr" {
		if _, _, err := net.ParseCIDR(spec.Destination); err != nil {
			return fmt.Errorf("destination %q is not a valid CIDR: %w", spec.Destination, err)
		}
	}
	if slices.Contains(spec.Roles, "Admin") {
		return fmt.Errorf("roles must not include \"Admin\"")
	}
	return nil
}

// SetupIndexes adds any field indexes required by this controller.
// Call from main before starting the manager.
func SetupIndexes(_ context.Context, _ client.FieldIndexer) error {
	return nil
}
