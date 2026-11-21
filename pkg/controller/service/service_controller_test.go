package service_test

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"github.com/ryanmcafee/port-forwarding-controller/pkg/controller/service"
	"github.com/ryanmcafee/port-forwarding-controller/pkg/controller/service/servicefakes"
	"github.com/ryanmcafee/port-forwarding-controller/pkg/forwarding"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var c client.Client

const timeout = time.Second * 5

func startManager(g *GomegaWithT, pfr service.PortForwardingReconciler) (chan reconcile.Request, func()) {
	gracePeriod := time.Second * 1
	mgr, err := manager.New(cfg, manager.Options{
		Metrics:                 metricsserver.Options{BindAddress: "0"}, // Disable metrics server in tests
		GracefulShutdownTimeout: &gracePeriod,
	})
	g.Expect(err).NotTo(HaveOccurred())
	c = mgr.GetClient()

	reconciler := service.NewReconciler(mgr, pfr)
	recFn, requests := SetupTestReconcile(reconciler)
	g.Expect(service.AddWithReconciler(mgr, recFn)).NotTo(HaveOccurred())

	cancelFunc, mgrStopped := StartTestManager(mgr, g)

	return requests, func() {
		cancelFunc()
		mgrStopped.Wait()
	}
}

func createServiceAndWait(g *GomegaWithT, svc *corev1.Service, requests chan reconcile.Request) {
	err := c.Create(context.Background(), svc)
	g.Expect(err).NotTo(HaveOccurred())

	expectedRequest := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      svc.Name,
			Namespace: "default",
		},
	}
	g.Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
	// annotated LBs get a finalizer added which will trigger an update
	if len(svc.Annotations) > 0 {
		g.Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
	}
}

func deleteServiceAndWait(g *GomegaWithT, svc *corev1.Service, requests chan reconcile.Request) {
	err := c.Delete(context.Background(), svc)
	g.Expect(err).NotTo(HaveOccurred())

	expectedRequest := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      svc.Name,
			Namespace: "default",
		},
	}
	g.Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
}

func TestReconcileWithLoadBalancer(t *testing.T) {
	g := NewGomegaWithT(t)

	fakePFR := &servicefakes.FakePortForwardingReconciler{}
	requests, shutdown := startManager(g, fakePFR)
	defer shutdown()

	instance := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-loadbalancer",
			Namespace: "default",
			Annotations: map[string]string{
				"port-forwarding.ryanmcafee.com/enable": "true",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:           "LoadBalancer",
			LoadBalancerIP: "1.2.3.4",
			Ports: []corev1.ServicePort{
				{
					Port: 80,
				},
			},
		},
	}
	createServiceAndWait(g, instance, requests)
	defer func() { _ = c.Delete(context.Background(), instance) }()

	// Create may get called a second time after finalizer is added
	g.Expect(fakePFR.CreateAddressesCallCount()).To(BeNumerically(">=", 1))
	g.Expect(fakePFR.CreateAddressesArgsForCall(0)).To(Equal([]forwarding.Address{
		{
			Name:    "default-svc-loadbalancer",
			Port:    80,
			IP:      "1.2.3.4",
			Options: map[string]string{},
		},
	}))
}

func TestReconcileWithLoadBalancerSourceRange(t *testing.T) {
	g := NewGomegaWithT(t)

	fakePFR := &servicefakes.FakePortForwardingReconciler{}
	requests, shutdown := startManager(g, fakePFR)
	defer shutdown()

	instance := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-sourcerange",
			Namespace: "default",
			Annotations: map[string]string{
				"port-forwarding.ryanmcafee.com/enable": "true",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:                     "LoadBalancer",
			LoadBalancerIP:           "1.2.3.4",
			LoadBalancerSourceRanges: []string{"10.0.0.0/16"},
			Ports: []corev1.ServicePort{
				{
					Port: 80,
				},
			},
		},
	}
	createServiceAndWait(g, instance, requests)
	defer func() { _ = c.Delete(context.Background(), instance) }()

	// Create may get called a second time after finalizer is added
	g.Expect(fakePFR.CreateAddressesCallCount()).To(BeNumerically(">=", 1))
	g.Expect(fakePFR.CreateAddressesArgsForCall(0)).To(Equal([]forwarding.Address{
		{
			Name:        "default-svc-sourcerange",
			Port:        80,
			IP:          "1.2.3.4",
			SourceRange: "10.0.0.0/16",
			Options:     map[string]string{},
		},
	}))
}

func TestReconcileWithExternalIP(t *testing.T) {
	g := NewGomegaWithT(t)

	fakePFR := &servicefakes.FakePortForwardingReconciler{}
	requests, shutdown := startManager(g, fakePFR)
	defer shutdown()

	instance := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-externalip",
			Namespace: "default",
			Annotations: map[string]string{
				"port-forwarding.ryanmcafee.com/enable": "true",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:        "ClusterIP",
			ExternalIPs: []string{"1.2.3.4"},
			Ports: []corev1.ServicePort{
				{
					Port: 80,
				},
			},
		},
	}
	createServiceAndWait(g, instance, requests)
	defer func() { _ = c.Delete(context.Background(), instance) }()

	// Create may get called a second time after finalizer is added
	g.Expect(fakePFR.CreateAddressesCallCount()).To(BeNumerically(">=", 1))
	g.Expect(fakePFR.CreateAddressesArgsForCall(0)).To(Equal([]forwarding.Address{
		{
			Name:    "default-svc-externalip",
			Port:    80,
			IP:      "1.2.3.4",
			Options: map[string]string{},
		},
	}))
}

func TestReconcileWithOptionAnnotations(t *testing.T) {
	g := NewGomegaWithT(t)

	fakePFR := &servicefakes.FakePortForwardingReconciler{}
	requests, shutdown := startManager(g, fakePFR)
	defer shutdown()

	instance := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-options",
			Namespace: "default",
			Annotations: map[string]string{
				"port-forwarding.ryanmcafee.com/enable":     "true",
				"port-forwarding.ryanmcafee.com/unifi-site": "some-site",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:           "LoadBalancer",
			LoadBalancerIP: "1.2.3.4",
			Ports: []corev1.ServicePort{
				{
					Port: 80,
				},
			},
		},
	}
	createServiceAndWait(g, instance, requests)
	defer func() { _ = c.Delete(context.Background(), instance) }()

	// Create may get called a second time after finalizer is added
	g.Expect(fakePFR.CreateAddressesCallCount()).To(BeNumerically(">=", 1))
	g.Expect(fakePFR.CreateAddressesArgsForCall(0)).To(Equal([]forwarding.Address{
		{
			Name: "default-svc-options",
			Port: 80,
			IP:   "1.2.3.4",
			Options: map[string]string{
				"unifi-site": "some-site",
			},
		},
	}))
}

func TestReconcileWithNoAnnotation(t *testing.T) {
	g := NewGomegaWithT(t)

	fakePFR := &servicefakes.FakePortForwardingReconciler{}
	requests, shutdown := startManager(g, fakePFR)
	defer shutdown()

	nonAnnotatedInstance := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "svc-noannotation",
			Namespace:   "default",
			Annotations: map[string]string{},
		},
		Spec: corev1.ServiceSpec{
			Type:           "LoadBalancer",
			LoadBalancerIP: "5.6.7.8",
			Ports: []corev1.ServicePort{
				{
					Port: 80,
				},
			},
		},
	}
	createServiceAndWait(g, nonAnnotatedInstance, requests)
	deleteServiceAndWait(g, nonAnnotatedInstance, requests)

	g.Expect(fakePFR.CreateAddressesCallCount()).To(Equal(0))
	g.Expect(deleteCallsFor(fakePFR, "default-svc-noannotation")()).To(BeEmpty())
}

func TestReconcileWithDelete(t *testing.T) {
	g := NewGomegaWithT(t)

	fakePFR := &servicefakes.FakePortForwardingReconciler{}
	requests, shutdown := startManager(g, fakePFR)
	defer shutdown()

	instance := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-delete",
			Namespace: "default",
			Annotations: map[string]string{
				"port-forwarding.ryanmcafee.com/enable": "true",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:           "LoadBalancer",
			LoadBalancerIP: "1.2.3.4",
			Ports: []corev1.ServicePort{
				{
					Port: 80,
				},
			},
		},
	}
	createServiceAndWait(g, instance, requests)

	deleteServiceAndWait(g, instance, requests)

	g.Expect(deleteCallsFor(fakePFR, "default-svc-delete")()).To(Equal([][]forwarding.Address{{
		{
			Name:    "default-svc-delete",
			Port:    80,
			IP:      "1.2.3.4",
			Options: map[string]string{},
		},
	}}))
}

func TestReconcileWithLoadBalancerStatusIP(t *testing.T) {
	g := NewGomegaWithT(t)

	fakePFR := &servicefakes.FakePortForwardingReconciler{}
	requests, shutdown := startManager(g, fakePFR)
	defer shutdown()

	// Create service without Spec.LoadBalancerIP (modern LB controllers don't use it)
	instance := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-status-ip",
			Namespace: "default",
			Annotations: map[string]string{
				"port-forwarding.ryanmcafee.com/enable": "true",
			},
		},
		Spec: corev1.ServiceSpec{
			Type: "LoadBalancer",
			Ports: []corev1.ServicePort{
				{
					Port: 443,
				},
			},
		},
	}
	err := c.Create(context.Background(), instance)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = c.Delete(context.Background(), instance) }()

	// Wait for initial reconciliation
	expectedRequest := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      instance.Name,
			Namespace: "default",
		},
	}
	g.Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))

	// Re-fetch the service to get the latest resourceVersion (controller may have added finalizer)
	err = c.Get(context.Background(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, instance)
	g.Expect(err).NotTo(HaveOccurred())

	// Update status with LoadBalancer ingress IP (simulating what a real LB controller does)
	instance.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
		{IP: "10.20.30.40"},
	}
	err = c.Status().Update(context.Background(), instance)
	g.Expect(err).NotTo(HaveOccurred())

	// Wait for status update to trigger reconciliation
	g.Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
	// May get additional reconciliation from finalizer
	g.Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))

	// Verify the IP was extracted from Status.LoadBalancer.Ingress
	g.Eventually(func() string {
		if fakePFR.CreateAddressesCallCount() == 0 {
			return ""
		}
		addresses := fakePFR.CreateAddressesArgsForCall(fakePFR.CreateAddressesCallCount() - 1)
		if len(addresses) == 0 {
			return ""
		}
		return addresses[0].IP
	}, timeout).Should(Equal("10.20.30.40"))
}

func TestReconcileWithLoadBalancerStatusIPTakesPrecedence(t *testing.T) {
	g := NewGomegaWithT(t)

	fakePFR := &servicefakes.FakePortForwardingReconciler{}
	requests, shutdown := startManager(g, fakePFR)
	defer shutdown()

	// Create service with both Spec.LoadBalancerIP and Status.LoadBalancer.Ingress
	// Status should take precedence
	instance := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-both-ips",
			Namespace: "default",
			Annotations: map[string]string{
				"port-forwarding.ryanmcafee.com/enable": "true",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:           "LoadBalancer",
			LoadBalancerIP: "1.1.1.1", // Legacy spec IP
			Ports: []corev1.ServicePort{
				{
					Port: 8080,
				},
			},
		},
	}
	err := c.Create(context.Background(), instance)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = c.Delete(context.Background(), instance) }()

	// Wait for initial reconciliation
	expectedRequest := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      instance.Name,
			Namespace: "default",
		},
	}
	g.Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))

	// Re-fetch the service to get the latest resourceVersion (controller may have added finalizer)
	err = c.Get(context.Background(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, instance)
	g.Expect(err).NotTo(HaveOccurred())

	// Update status with different LoadBalancer ingress IP
	instance.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
		{IP: "2.2.2.2"}, // Status IP should take precedence
	}
	err = c.Status().Update(context.Background(), instance)
	g.Expect(err).NotTo(HaveOccurred())

	// Wait for status update to trigger reconciliation
	g.Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
	g.Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))

	// Verify the IP was extracted from Status (2.2.2.2) not Spec (1.1.1.1)
	g.Eventually(func() string {
		if fakePFR.CreateAddressesCallCount() == 0 {
			return ""
		}
		addresses := fakePFR.CreateAddressesArgsForCall(fakePFR.CreateAddressesCallCount() - 1)
		if len(addresses) == 0 {
			return ""
		}
		return addresses[0].IP
	}, timeout).Should(Equal("2.2.2.2"))
}

const finalizerName = "finalizer.port-forwarding.ryanmcafee.com/v1"

// deleteCallsFor ignores calls for Services that earlier tests left terminating.
func deleteCallsFor(fake *servicefakes.FakePortForwardingReconciler, ruleName string) func() [][]forwarding.Address {
	return func() [][]forwarding.Address {
		calls := [][]forwarding.Address{}
		for i := 0; i < fake.DeleteAddressesCallCount(); i++ {
			addresses := fake.DeleteAddressesArgsForCall(i)
			if len(addresses) > 0 && addresses[0].Name == ruleName {
				calls = append(calls, addresses)
			}
		}
		return calls
	}
}

// drain keeps the single reconcile worker from blocking on the unbuffered requests channel.
func drain(requests chan reconcile.Request) {
	for range requests {
	}
}

func serviceFinalizers(g *GomegaWithT, svc *corev1.Service) func() []string {
	return func() []string {
		current := &corev1.Service{}
		g.Expect(c.Get(context.Background(), client.ObjectKeyFromObject(svc), current)).To(Succeed())
		return current.Finalizers
	}
}

func TestReconcileWithAnnotationRemoved(t *testing.T) {
	g := NewGomegaWithT(t)

	fakePFR := &servicefakes.FakePortForwardingReconciler{}
	requests, shutdown := startManager(g, fakePFR)
	defer shutdown()

	instance := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-annotation-removed",
			Namespace: "default",
			Annotations: map[string]string{
				"port-forwarding.ryanmcafee.com/enable": "true",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:           "LoadBalancer",
			LoadBalancerIP: "192.0.2.10",
			Ports:          []corev1.ServicePort{{Name: "http", Port: 80}, {Name: "https", Port: 443}},
		},
	}
	createServiceAndWait(g, instance, requests)
	defer func() { _ = c.Delete(context.Background(), instance) }()
	go drain(requests)
	g.Eventually(serviceFinalizers(g, instance), timeout).Should(ContainElement(finalizerName))

	g.Expect(c.Get(context.Background(), client.ObjectKeyFromObject(instance), instance)).To(Succeed())
	delete(instance.Annotations, "port-forwarding.ryanmcafee.com/enable")
	g.Expect(c.Update(context.Background(), instance)).To(Succeed())

	g.Eventually(deleteCallsFor(fakePFR, "default-svc-annotation-removed"), timeout).Should(Equal([][]forwarding.Address{{
		{Name: "default-svc-annotation-removed", Port: 80, IP: "192.0.2.10", Options: map[string]string{}},
		{Name: "default-svc-annotation-removed", Port: 443, IP: "192.0.2.10", Options: map[string]string{}},
	}}))
	g.Eventually(serviceFinalizers(g, instance), timeout).ShouldNot(ContainElement(finalizerName))
}

func TestReconcileWithAnnotationRemovedWhileDeleting(t *testing.T) {
	g := NewGomegaWithT(t)

	directClient, err := client.New(cfg, client.Options{})
	g.Expect(err).NotTo(HaveOccurred())

	instance := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "svc-annotation-removed-deleting",
			Namespace:  "default",
			Finalizers: []string{finalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:  "ClusterIP",
			Ports: []corev1.ServicePort{{Port: 80}},
		},
	}
	g.Expect(directClient.Create(context.Background(), instance)).To(Succeed())
	g.Expect(directClient.Delete(context.Background(), instance)).To(Succeed())

	fakePFR := &servicefakes.FakePortForwardingReconciler{}
	requests, shutdown := startManager(g, fakePFR)
	defer shutdown()
	go drain(requests)

	g.Eventually(deleteCallsFor(fakePFR, "default-svc-annotation-removed-deleting"), timeout).Should(Equal([][]forwarding.Address{{
		{Name: "default-svc-annotation-removed-deleting", Port: 80, Options: map[string]string{}},
	}}))
	g.Eventually(func() bool {
		err := directClient.Get(context.Background(), client.ObjectKeyFromObject(instance), &corev1.Service{})
		return apierrors.IsNotFound(err)
	}, timeout).Should(BeTrue())
}
