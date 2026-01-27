package helm_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/helm"
	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/onsi/gomega"
)

const (
	defaultTimeout  = 5 * time.Minute
	defaultInterval = 5 * time.Second
)

func TestHelmDeployDefault(t *testing.T) {
	t.Parallel()
	g := gomega.NewGomegaWithT(t)
	ctx := context.Background()

	config := loadTestConfig()
	namespace := createTestNamespace("helm-default")
	releaseName := "test-default"

	// Build helm options with default values
	valuesFiles := []string{getAbsoluteValuesPath(config.DefaultValues)}
	helmOptions := buildHelmOptions(config, namespace, valuesFiles, nil)

	// Create namespace
	k8s.CreateNamespace(t, helmOptions.KubectlOptions, namespace)
	defer k8s.DeleteNamespace(t, helmOptions.KubectlOptions, namespace)

	// Deploy the chart
	defer helm.Delete(t, helmOptions, releaseName, true)
	helm.Install(t, helmOptions, config.ChartPath, releaseName)

	// Derive expected names based on Helm naming conventions
	fullName := fmt.Sprintf("%s-port-forwarding", releaseName)
	expectedImage := fmt.Sprintf("%s:%s", config.ImageRepo, config.ImageTag)

	// Verify Deployment
	deployment := verifyDeployment(t, g, helmOptions.KubectlOptions, fullName, expectedImage, defaultTimeout, defaultInterval)

	// Verify expected environment variables
	expectedEnvVars := []string{
		"ROUTER_URL",
		"ROUTER_USERNAME",
		"ROUTER_PASSWORD",
		"ROUTER_RULE_PREFIX",
		"ROUTER_SKIP_TLS_VERIFICATION",
		"POD_NAMESPACE",
	}
	verifyDeploymentEnvVars(g, deployment, expectedEnvVars)

	// Verify expected labels
	expectedLabels := map[string]string{
		"app":     "port-forwarding",
		"release": releaseName,
	}
	verifyDeploymentLabels(g, deployment, expectedLabels)

	// Verify ServiceAccount
	verifyServiceAccount(t, g, helmOptions.KubectlOptions, fullName, defaultTimeout, defaultInterval)

	// Verify ClusterRole with expected permissions
	cr := verifyClusterRole(t, ctx, g, helmOptions.KubectlOptions, fullName, defaultTimeout, defaultInterval)
	expectedResources := []string{"services", "secrets", "mutatingwebhookconfigurations", "validatingwebhookconfigurations"}
	verifyClusterRolePermissions(g, cr, expectedResources)

	// Verify ClusterRoleBinding
	crb := verifyClusterRoleBinding(t, ctx, g, helmOptions.KubectlOptions, fullName, defaultTimeout, defaultInterval)
	g.Expect(crb.RoleRef.Name).To(gomega.Equal(fullName))
	g.Expect(crb.Subjects).To(gomega.HaveLen(1))
	g.Expect(crb.Subjects[0].Name).To(gomega.Equal(fullName))
	g.Expect(crb.Subjects[0].Namespace).To(gomega.Equal(namespace))

	// Verify Secret with expected keys
	expectedSecretKeys := []string{
		"ROUTER_URL",
		"ROUTER_USERNAME",
		"ROUTER_PASSWORD",
		"ROUTER_RULE_PREFIX",
		"ROUTER_SKIP_TLS_VERIFICATION",
	}
	verifySecret(t, g, helmOptions.KubectlOptions, fullName, expectedSecretKeys, defaultTimeout, defaultInterval)

	// Verify Pod starts (image pulled successfully - will CrashLoop due to mock router)
	labelSelector := fmt.Sprintf("app=port-forwarding,release=%s", releaseName)
	verifyPodStarts(t, g, helmOptions.KubectlOptions, labelSelector, defaultTimeout, defaultInterval)
}

func TestHelmDeployCustomValues(t *testing.T) {
	t.Parallel()
	g := gomega.NewGomegaWithT(t)
	ctx := context.Background()

	config := loadTestConfig()
	namespace := createTestNamespace("helm-custom")
	releaseName := "test-custom"

	// Build helm options with custom values
	valuesFiles := []string{getAbsoluteValuesPath(config.CustomValues)}
	helmOptions := buildHelmOptions(config, namespace, valuesFiles, nil)

	// Create namespace
	k8s.CreateNamespace(t, helmOptions.KubectlOptions, namespace)
	defer k8s.DeleteNamespace(t, helmOptions.KubectlOptions, namespace)

	// Deploy the chart
	defer helm.Delete(t, helmOptions, releaseName, true)
	helm.Install(t, helmOptions, config.ChartPath, releaseName)

	fullName := fmt.Sprintf("%s-port-forwarding", releaseName)
	expectedImage := fmt.Sprintf("%s:%s", config.ImageRepo, config.ImageTag)

	// Verify Deployment with custom image
	deployment := verifyDeployment(t, g, helmOptions.KubectlOptions, fullName, expectedImage, defaultTimeout, defaultInterval)

	// Verify custom resource limits
	verifyDeploymentResources(g, deployment, "200m", "128Mi")

	// Verify custom node selector
	expectedNodeSelector := map[string]string{
		"node-role.kubernetes.io/control-plane": "",
	}
	verifyDeploymentNodeSelector(g, deployment, expectedNodeSelector)

	// Verify RBAC resources are created by default
	verifyClusterRole(t, ctx, g, helmOptions.KubectlOptions, fullName, defaultTimeout, defaultInterval)
	verifyClusterRoleBinding(t, ctx, g, helmOptions.KubectlOptions, fullName, defaultTimeout, defaultInterval)

	// Verify ServiceAccount is created by default
	verifyServiceAccount(t, g, helmOptions.KubectlOptions, fullName, defaultTimeout, defaultInterval)

	// Verify Secret is created by default
	verifySecret(t, g, helmOptions.KubectlOptions, fullName, []string{"ROUTER_URL"}, defaultTimeout, defaultInterval)
}

func TestHelmRBACDisabled(t *testing.T) {
	t.Parallel()
	g := gomega.NewGomegaWithT(t)
	ctx := context.Background()

	config := loadTestConfig()
	namespace := createTestNamespace("helm-no-rbac")
	releaseName := "test-no-rbac"

	// Build helm options with RBAC disabled
	valuesFiles := []string{getAbsoluteValuesPath(config.DefaultValues)}
	setValues := map[string]string{
		"rbac.create": "false",
	}
	helmOptions := buildHelmOptions(config, namespace, valuesFiles, setValues)

	// Create namespace
	k8s.CreateNamespace(t, helmOptions.KubectlOptions, namespace)
	defer k8s.DeleteNamespace(t, helmOptions.KubectlOptions, namespace)

	// Deploy the chart
	defer helm.Delete(t, helmOptions, releaseName, true)
	helm.Install(t, helmOptions, config.ChartPath, releaseName)

	fullName := fmt.Sprintf("%s-port-forwarding", releaseName)

	// Verify Deployment still exists
	verifyDeployment(t, g, helmOptions.KubectlOptions, fullName, "", defaultTimeout, defaultInterval)

	// Verify ClusterRole does NOT exist
	verifyClusterRoleNotExists(t, ctx, g, helmOptions.KubectlOptions, fullName)

	// Verify ClusterRoleBinding does NOT exist
	verifyClusterRoleBindingNotExists(t, ctx, g, helmOptions.KubectlOptions, fullName)
}

func TestHelmServiceAccountDisabled(t *testing.T) {
	t.Parallel()
	g := gomega.NewGomegaWithT(t)

	config := loadTestConfig()
	namespace := createTestNamespace("helm-no-sa")
	releaseName := "test-no-sa"

	// Build helm options with ServiceAccount disabled
	valuesFiles := []string{getAbsoluteValuesPath(config.DefaultValues)}
	setValues := map[string]string{
		"serviceAccount.create": "false",
	}
	helmOptions := buildHelmOptions(config, namespace, valuesFiles, setValues)

	// Create namespace
	k8s.CreateNamespace(t, helmOptions.KubectlOptions, namespace)
	defer k8s.DeleteNamespace(t, helmOptions.KubectlOptions, namespace)

	// Deploy the chart
	defer helm.Delete(t, helmOptions, releaseName, true)
	helm.Install(t, helmOptions, config.ChartPath, releaseName)

	fullName := fmt.Sprintf("%s-port-forwarding", releaseName)

	// Verify Deployment still exists
	verifyDeployment(t, g, helmOptions.KubectlOptions, fullName, "", defaultTimeout, defaultInterval)

	// Verify ServiceAccount does NOT exist (the custom one)
	_, err := k8s.GetServiceAccountE(t, helmOptions.KubectlOptions, fullName)
	g.Expect(err).To(gomega.HaveOccurred(), "ServiceAccount %s should not exist", fullName)
}

func TestHelmSecretDisabled(t *testing.T) {
	t.Parallel()
	g := gomega.NewGomegaWithT(t)

	config := loadTestConfig()
	namespace := createTestNamespace("helm-no-secret")
	releaseName := "test-no-secret"

	// Build helm options with Secret disabled
	valuesFiles := []string{getAbsoluteValuesPath(config.DefaultValues)}
	setValues := map[string]string{
		"router.createSecret": "false",
	}
	helmOptions := buildHelmOptions(config, namespace, valuesFiles, setValues)

	// Create namespace
	k8s.CreateNamespace(t, helmOptions.KubectlOptions, namespace)
	defer k8s.DeleteNamespace(t, helmOptions.KubectlOptions, namespace)

	// Deploy the chart
	defer helm.Delete(t, helmOptions, releaseName, true)
	helm.Install(t, helmOptions, config.ChartPath, releaseName)

	fullName := fmt.Sprintf("%s-port-forwarding", releaseName)

	// Note: Deployment will exist but pod won't be able to start without the secret
	// This is expected behavior - user must provide their own secret

	// Verify Secret does NOT exist
	_, err := k8s.GetSecretE(t, helmOptions.KubectlOptions, fullName)
	g.Expect(err).To(gomega.HaveOccurred(), "Secret %s should not exist when router.createSecret=false", fullName)
}

// TestHelmTemplateValidation validates that the chart templates render correctly
// This is a lighter-weight test that doesn't require a cluster
func TestHelmTemplateValidation(t *testing.T) {
	t.Parallel()

	config := loadTestConfig()
	namespace := "template-validation"
	releaseName := "test-template"

	// Build helm options
	valuesFiles := []string{getAbsoluteValuesPath(config.DefaultValues)}
	helmOptions := buildHelmOptions(config, namespace, valuesFiles, nil)

	// Render templates (doesn't require a cluster)
	output := helm.RenderTemplate(t, helmOptions, config.ChartPath, releaseName, []string{
		"templates/deployment.yml",
		"templates/serviceaccount.yml",
		"templates/clusterrole.yml",
		"templates/clusterrolebinding.yml",
		"templates/secret.yml",
	})

	// Basic validation that templates rendered
	g := gomega.NewGomegaWithT(t)
	g.Expect(output).To(gomega.ContainSubstring("kind: Deployment"))
	g.Expect(output).To(gomega.ContainSubstring("kind: ServiceAccount"))
	g.Expect(output).To(gomega.ContainSubstring("kind: ClusterRole"))
	g.Expect(output).To(gomega.ContainSubstring("kind: ClusterRoleBinding"))
	g.Expect(output).To(gomega.ContainSubstring("kind: Secret"))
}
