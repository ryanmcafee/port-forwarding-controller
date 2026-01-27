# Image URL to use all building/pushing image targets
IMG ?= ghcr.io/ryanmcafee/port-forwarding-controller
IMG_TAG ?= latest

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.31.0

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate RBAC objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role paths="./..." output:rbac:artifacts:config=config/rbac

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v /e2e | grep -v /test/helm) -coverprofile cover.out

.PHONY: test-e2e
test-e2e: ## Run the e2e tests against a Kind k8s instance.
	go test ./test/e2e/ -v -ginkgo.v

##@ Helm

.PHONY: helm-test
helm-test: helm-lint helm-template ## Test Helm chart (lint and template validation).

.PHONY: helm-lint
helm-lint: ## Lint Helm chart.
	helm lint ./charts/port-forwarding --set router.url=https://test.local --set router.username=admin --set router.password=secret

.PHONY: helm-template
helm-template: ## Render Helm templates to verify syntax.
	helm template port-forwarding ./charts/port-forwarding --set router.url=https://test.local --set router.username=admin --set router.password=secret --debug

# Helm E2E test configuration
HELM_TEST_CLUSTER ?= helm-test
HELM_TEST_IMAGE ?= port-forwarding-controller:test

.PHONY: helm-e2e
helm-e2e: helm-e2e-setup helm-e2e-run helm-e2e-cleanup ## Run full Helm E2E test cycle.

.PHONY: helm-e2e-setup
helm-e2e-setup: ## Set up Kind cluster and build test image.
	kind create cluster --name $(HELM_TEST_CLUSTER) --config test/helm/kind-config.yaml || true
	goreleaser build --snapshot --clean
	mkdir -p linux/amd64 && cp dist/manager_linux_amd64_v1/manager linux/amd64/manager
	docker build -t $(HELM_TEST_IMAGE) --build-arg TARGETPLATFORM=linux/amd64 .
	kind load docker-image $(HELM_TEST_IMAGE) --name $(HELM_TEST_CLUSTER)

.PHONY: helm-e2e-run
helm-e2e-run: ## Run Helm tests (assumes cluster exists).
	KUBE_CONTEXT=kind-$(HELM_TEST_CLUSTER) \
	TEST_IMAGE_REPO=port-forwarding-controller \
	TEST_IMAGE_TAG=test \
	go test -v -timeout 15m ./test/helm/...

.PHONY: helm-e2e-cleanup
helm-e2e-cleanup: ## Delete Kind cluster.
	kind delete cluster --name $(HELM_TEST_CLUSTER) || true

.PHONY: helm-e2e-quick
helm-e2e-quick: ## Run tests without rebuild (fast iteration).
	KUBE_CONTEXT=kind-$(HELM_TEST_CLUSTER) \
	TEST_IMAGE_REPO=port-forwarding-controller \
	TEST_IMAGE_TAG=test \
	go test -v -timeout 15m ./test/helm/...

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager ./cmd/manager

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/manager/main.go

.PHONY: docker-build
docker-build: ## Build docker image using GoReleaser.
	goreleaser build --snapshot --clean

##@ Deployment

.PHONY: install
install: manifests kustomize ## Install RBAC into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/rbac | kubectl apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall RBAC from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/rbac | kubectl delete --ignore-not-found -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found -f -

##@ Release

.PHONY: release
release: ## Build and release using GoReleaser.
	goreleaser release --clean

.PHONY: release-snapshot
release-snapshot: ## Local release test.
	goreleaser release --snapshot --clean

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint

## Tool Versions
KUSTOMIZE_VERSION ?= v5.5.0
CONTROLLER_TOOLS_VERSION ?= v0.17.2
ENVTEST_VERSION ?= release-0.19
GOLANGCI_LINT_VERSION ?= v1.64.5

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

# go-install-tool will 'go install' any package with custom target and target name.
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef
