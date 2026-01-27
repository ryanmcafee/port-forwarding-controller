package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/ryanmcafee/port-forwarding-controller/pkg/forwarding"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const AnnotationPrefix = "port-forwarding.ryanmcafee.com"

//go:generate counterfeiter . PortForwardingReconciler
type PortForwardingReconciler interface {
	CreateAddresses([]forwarding.Address) error
	DeleteAddresses([]forwarding.Address) error
}

// Add creates a new Service Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager, pfr PortForwardingReconciler) error {
	return add(mgr, NewReconciler(mgr, pfr))
}

func AddWithReconciler(mgr manager.Manager, r reconcile.Reconciler) error {
	return add(mgr, r)
}

// NewReconciler returns a new reconcile.Reconciler
func NewReconciler(mgr manager.Manager, pfr PortForwardingReconciler) reconcile.Reconciler {
	return &ReconcileService{
		Client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
		pfr:    pfr,
		log:    logf.Log.WithName("service-controller"),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	skipValidation := true
	c, err := controller.New("service-controller", mgr, controller.Options{
		Reconciler:         r,
		SkipNameValidation: &skipValidation, // Allow multiple managers in tests
	})
	if err != nil {
		return err
	}

	err = c.Watch(source.Kind(mgr.GetCache(), &corev1.Service{}, &handler.TypedEnqueueRequestForObject[*corev1.Service]{}))
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileService{}

// ReconcileService reconciles a Service object
type ReconcileService struct {
	client.Client
	scheme *runtime.Scheme
	pfr    PortForwardingReconciler
	log    logr.Logger
}

// Reconcile reads that state of the cluster for a Service object and makes changes based on the state read
// and what is in the Service.Spec
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
func (r *ReconcileService) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// Fetch the Service instance
	instance := &corev1.Service{}
	err := r.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if isAnnotatedLB(instance) {
		return r.processLB(instance)
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileService) processLB(instance *corev1.Service) (reconcile.Result, error) {
	addresses := []forwarding.Address{}
	targetIP := ipFromService(instance)
	sourceRange := sourceRangeFromService(instance)
	options := optionsFromService(instance)

	for _, port := range instance.Spec.Ports {
		addresses = append(addresses, forwarding.Address{
			Name:        fmt.Sprintf("%s-%s", instance.Namespace, instance.Name),
			Port:        int(port.Port),
			IP:          targetIP,
			SourceRange: sourceRange,
			Options:     options,
		})
	}

	finalizerName := fmt.Sprintf("finalizer.%s/v1", AnnotationPrefix)

	if instance.DeletionTimestamp.IsZero() {
		err := r.pfr.CreateAddresses(addresses)
		if err != nil {
			return reconcile.Result{}, err
		}
		if !containsString(instance.Finalizers, finalizerName) {
			instance.Finalizers = append(instance.Finalizers, finalizerName)
			if err = r.Update(context.Background(), instance); err != nil {
				return reconcile.Result{}, err
			}
		}
	} else {
		if containsString(instance.Finalizers, finalizerName) {
			err := r.pfr.DeleteAddresses(addresses)
			if err != nil {
				return reconcile.Result{}, err
			}

			instance.Finalizers = removeString(instance.Finalizers, finalizerName)
			if err = r.Update(context.Background(), instance); err != nil {
				return reconcile.Result{}, err
			}
		}
	}
	return reconcile.Result{}, nil
}

func isAnnotatedLB(instance *corev1.Service) bool {
	if instance.Spec.Type == "LoadBalancer" || len(instance.Spec.ExternalIPs) > 0 {
		for key, value := range instance.Annotations {
			if key == fmt.Sprintf("%s/enable", AnnotationPrefix) && value == "true" {
				return true
			}
		}
	}
	return false
}

func ipFromService(svc *corev1.Service) string {
	if svc.Spec.Type == "LoadBalancer" {
		// First check Status.LoadBalancer.Ingress (modern LB controllers like Cilium, MetalLB, cloud providers)
		if len(svc.Status.LoadBalancer.Ingress) > 0 {
			ingress := svc.Status.LoadBalancer.Ingress[0]
			if ingress.IP != "" {
				return ingress.IP
			}
		}
		// Fall back to Spec.LoadBalancerIP (deprecated but still used by some implementations)
		return svc.Spec.LoadBalancerIP
	}
	if len(svc.Spec.ExternalIPs) > 0 {
		return svc.Spec.ExternalIPs[0]
	}
	return ""
}

func sourceRangeFromService(svc *corev1.Service) string {
	if svc.Spec.Type == "LoadBalancer" && len(svc.Spec.LoadBalancerSourceRanges) > 0 {
		return svc.Spec.LoadBalancerSourceRanges[0]
	}
	return ""
}

func optionsFromService(svc *corev1.Service) map[string]string {
	options := map[string]string{}
	for k, v := range svc.Annotations {
		if strings.HasPrefix(k, AnnotationPrefix) && !strings.HasSuffix(k, "/enable") {
			options[strings.TrimPrefix(k, fmt.Sprintf("%s/", AnnotationPrefix))] = v
		}
	}
	return options
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}
