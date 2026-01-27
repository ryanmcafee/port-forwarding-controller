package helm_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/helm"
	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/gruntwork-io/terratest/modules/random"
	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestConfig holds configuration for the Helm tests
type TestConfig struct {
	KubeContext   string
	ChartPath     string
	ImageRepo     string
	ImageTag      string
	DefaultValues string
	CustomValues  string
}

// loadTestConfig loads test configuration from environment variables with defaults
func loadTestConfig() *TestConfig {
	config := &TestConfig{
		KubeContext:   getEnvOrDefault("KUBE_CONTEXT", "kind-helm-test"),
		ChartPath:     getEnvOrDefault("CHART_PATH", "../../charts/port-forwarding"),
		ImageRepo:     getEnvOrDefault("TEST_IMAGE_REPO", "port-forwarding-controller"),
		ImageTag:      getEnvOrDefault("TEST_IMAGE_TAG", "test"),
		DefaultValues: getEnvOrDefault("DEFAULT_VALUES_FILE", "testdata/values-default.yaml"),
		CustomValues:  getEnvOrDefault("CUSTOM_VALUES_FILE", "testdata/values-custom.yaml"),
	}
	return config
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// createTestNamespace generates a unique namespace name for test isolation
func createTestNamespace(prefix string) string {
	// Kubernetes namespaces must be lowercase
	return strings.ToLower(fmt.Sprintf("%s-%s", prefix, random.UniqueId()))
}

// buildHelmOptions creates Terratest helm.Options with common settings
func buildHelmOptions(config *TestConfig, namespace string, valuesFiles []string, setValues map[string]string) *helm.Options {
	// Merge image settings with any provided set values
	allSetValues := map[string]string{
		"image.repository": config.ImageRepo,
		"image.tag":        config.ImageTag,
	}
	for k, v := range setValues {
		allSetValues[k] = v
	}

	return &helm.Options{
		KubectlOptions: k8s.NewKubectlOptions(config.KubeContext, "", namespace),
		ValuesFiles:    valuesFiles,
		SetValues:      allSetValues,
	}
}

// getAbsoluteValuesPath returns the absolute path to a values file
func getAbsoluteValuesPath(relativePath string) string {
	absPath, err := filepath.Abs(relativePath)
	if err != nil {
		return relativePath
	}
	return absPath
}

// verifyDeployment checks that a deployment exists and has expected properties
func verifyDeployment(t *testing.T, g *gomega.GomegaWithT, kubectlOptions *k8s.KubectlOptions, deploymentName string, expectedImage string, timeout time.Duration, interval time.Duration) *appsv1.Deployment {
	var deployment *appsv1.Deployment

	g.Eventually(func() error {
		var err error
		deployment, err = k8s.GetDeploymentE(t, kubectlOptions, deploymentName)
		return err
	}, timeout, interval).Should(gomega.Succeed(), "Deployment %s should exist", deploymentName)

	g.Expect(deployment).NotTo(gomega.BeNil())
	g.Expect(deployment.Spec.Template.Spec.Containers).To(gomega.HaveLen(1))

	if expectedImage != "" {
		actualImage := deployment.Spec.Template.Spec.Containers[0].Image
		g.Expect(actualImage).To(gomega.Equal(expectedImage), "Deployment should use expected image")
	}

	return deployment
}

// verifyServiceAccount checks that a service account exists
func verifyServiceAccount(t *testing.T, g *gomega.GomegaWithT, kubectlOptions *k8s.KubectlOptions, saName string, timeout time.Duration, interval time.Duration) *corev1.ServiceAccount {
	var sa *corev1.ServiceAccount

	g.Eventually(func() error {
		var err error
		sa, err = k8s.GetServiceAccountE(t, kubectlOptions, saName)
		return err
	}, timeout, interval).Should(gomega.Succeed(), "ServiceAccount %s should exist", saName)

	g.Expect(sa).NotTo(gomega.BeNil())
	return sa
}

// verifyClusterRole checks that a cluster role exists with expected permissions
func verifyClusterRole(t *testing.T, ctx context.Context, g *gomega.GomegaWithT, kubectlOptions *k8s.KubectlOptions, crName string, timeout time.Duration, interval time.Duration) *rbacv1.ClusterRole {
	clientset, err := k8s.GetKubernetesClientFromOptionsE(t, kubectlOptions)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	var cr *rbacv1.ClusterRole

	g.Eventually(func() error {
		var err error
		cr, err = clientset.RbacV1().ClusterRoles().Get(ctx, crName, metav1.GetOptions{})
		return err
	}, timeout, interval).Should(gomega.Succeed(), "ClusterRole %s should exist", crName)

	g.Expect(cr).NotTo(gomega.BeNil())
	return cr
}

// verifyClusterRoleBinding checks that a cluster role binding exists
func verifyClusterRoleBinding(t *testing.T, ctx context.Context, g *gomega.GomegaWithT, kubectlOptions *k8s.KubectlOptions, crbName string, timeout time.Duration, interval time.Duration) *rbacv1.ClusterRoleBinding {
	clientset, err := k8s.GetKubernetesClientFromOptionsE(t, kubectlOptions)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	var crb *rbacv1.ClusterRoleBinding

	g.Eventually(func() error {
		var err error
		crb, err = clientset.RbacV1().ClusterRoleBindings().Get(ctx, crbName, metav1.GetOptions{})
		return err
	}, timeout, interval).Should(gomega.Succeed(), "ClusterRoleBinding %s should exist", crbName)

	g.Expect(crb).NotTo(gomega.BeNil())
	return crb
}

// verifySecret checks that a secret exists with expected keys
func verifySecret(t *testing.T, g *gomega.GomegaWithT, kubectlOptions *k8s.KubectlOptions, secretName string, expectedKeys []string, timeout time.Duration, interval time.Duration) *corev1.Secret {
	var secret *corev1.Secret

	g.Eventually(func() error {
		var err error
		secret, err = k8s.GetSecretE(t, kubectlOptions, secretName)
		return err
	}, timeout, interval).Should(gomega.Succeed(), "Secret %s should exist", secretName)

	g.Expect(secret).NotTo(gomega.BeNil())

	for _, key := range expectedKeys {
		g.Expect(secret.Data).To(gomega.HaveKey(key), "Secret should contain key %s", key)
	}

	return secret
}

// verifyClusterRoleNotExists checks that a cluster role does not exist
func verifyClusterRoleNotExists(t *testing.T, ctx context.Context, g *gomega.GomegaWithT, kubectlOptions *k8s.KubectlOptions, crName string) {
	clientset, err := k8s.GetKubernetesClientFromOptionsE(t, kubectlOptions)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	_, err = clientset.RbacV1().ClusterRoles().Get(ctx, crName, metav1.GetOptions{})
	g.Expect(err).To(gomega.HaveOccurred(), "ClusterRole %s should not exist", crName)
}

// verifyClusterRoleBindingNotExists checks that a cluster role binding does not exist
func verifyClusterRoleBindingNotExists(t *testing.T, ctx context.Context, g *gomega.GomegaWithT, kubectlOptions *k8s.KubectlOptions, crbName string) {
	clientset, err := k8s.GetKubernetesClientFromOptionsE(t, kubectlOptions)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	_, err = clientset.RbacV1().ClusterRoleBindings().Get(ctx, crbName, metav1.GetOptions{})
	g.Expect(err).To(gomega.HaveOccurred(), "ClusterRoleBinding %s should not exist", crbName)
}

// verifyDeploymentEnvVars checks that a deployment has expected environment variables
func verifyDeploymentEnvVars(g *gomega.GomegaWithT, deployment *appsv1.Deployment, expectedEnvVars []string) {
	container := deployment.Spec.Template.Spec.Containers[0]
	envVarNames := make([]string, 0, len(container.Env))
	for _, env := range container.Env {
		envVarNames = append(envVarNames, env.Name)
	}

	for _, expectedVar := range expectedEnvVars {
		g.Expect(envVarNames).To(gomega.ContainElement(expectedVar), "Deployment should have env var %s", expectedVar)
	}
}

// verifyDeploymentLabels checks that a deployment has expected labels
func verifyDeploymentLabels(g *gomega.GomegaWithT, deployment *appsv1.Deployment, expectedLabels map[string]string) {
	for key, value := range expectedLabels {
		g.Expect(deployment.Labels).To(gomega.HaveKeyWithValue(key, value), "Deployment should have label %s=%s", key, value)
	}
}

// verifyPodStarts checks that a pod starts (even if it crashes later due to missing router)
func verifyPodStarts(t *testing.T, g *gomega.GomegaWithT, kubectlOptions *k8s.KubectlOptions, labelSelector string, timeout time.Duration, interval time.Duration) {
	g.Eventually(func() bool {
		pods := k8s.ListPods(t, kubectlOptions, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if len(pods) == 0 {
			return false
		}
		// Check if the pod has at least been scheduled and container started
		// We don't require Running since it will crash without a real router
		for _, pod := range pods {
			for _, containerStatus := range pod.Status.ContainerStatuses {
				// Container has been started at least once (even if it's restarting)
				if containerStatus.RestartCount > 0 || containerStatus.State.Running != nil {
					return true
				}
			}
			// Also accept pods that are in ContainerCreating or later phases
			if pod.Status.Phase != corev1.PodPending {
				return true
			}
		}
		return false
	}, timeout, interval).Should(gomega.BeTrue(), "Pod should start (image pulled and container created)")
}

// verifyDeploymentResources checks that a deployment has expected resource settings
func verifyDeploymentResources(g *gomega.GomegaWithT, deployment *appsv1.Deployment, expectedCPULimit, expectedMemLimit string) {
	container := deployment.Spec.Template.Spec.Containers[0]

	if expectedCPULimit != "" {
		cpuLimit := container.Resources.Limits.Cpu()
		g.Expect(cpuLimit.String()).To(gomega.Equal(expectedCPULimit), "Deployment should have expected CPU limit")
	}

	if expectedMemLimit != "" {
		memLimit := container.Resources.Limits.Memory()
		g.Expect(memLimit.String()).To(gomega.Equal(expectedMemLimit), "Deployment should have expected memory limit")
	}
}

// verifyDeploymentNodeSelector checks that a deployment has expected node selector
func verifyDeploymentNodeSelector(g *gomega.GomegaWithT, deployment *appsv1.Deployment, expectedNodeSelector map[string]string) {
	g.Expect(deployment.Spec.Template.Spec.NodeSelector).To(gomega.Equal(expectedNodeSelector), "Deployment should have expected node selector")
}

// verifyClusterRolePermissions checks that a cluster role has expected permissions
func verifyClusterRolePermissions(g *gomega.GomegaWithT, cr *rbacv1.ClusterRole, expectedResources []string) {
	var foundResources []string
	for _, rule := range cr.Rules {
		foundResources = append(foundResources, rule.Resources...)
	}

	for _, expected := range expectedResources {
		g.Expect(foundResources).To(gomega.ContainElement(expected), "ClusterRole should have permission for %s", expected)
	}
}
