# nos version. Used by release workflow, do not move this line.
NOS_VERSION ?= 0.0.1

DOCKER_REGISTRY ?= ghcr.io/saguirregaray1

# Image URLs to build/push Docker image targets
GPU_PARTITIONER_IMG ?= $(DOCKER_REGISTRY)/nos-gpu-partitioner:$(NOS_VERSION)
MIG_AGENT_IMG ?= $(DOCKER_REGISTRY)/nos-mig-agent:$(NOS_VERSION)
GPU_AGENT_IMG ?= $(DOCKER_REGISTRY)/nos-gpu-agent:$(NOS_VERSION)
METRICS_EXPORTER_IMG ?= $(DOCKER_REGISTRY)/nos-metrics-exporter:$(NOS_VERSION)

# Helm chart URL to push Helm charts
HELM_CHART_REGISTRY ?= oci://ghcr.io/nebuly-ai/helm-charts

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.24.2

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

GOCACHE ?= $(CURDIR)/.gocache
export GOCACHE

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: gpu-partitioner-manifests ## Generate manifests for the gpu-partitioner (ClusterRole, etc.).
gpu-partitioner-manifests: controller-gen
	$(CONTROLLER_GEN) paths="./internal/controllers/gpupartitioner" \
	rbac:roleName=gpu-partitioner-role \
	output:rbac:artifacts:config=config/gpupartitioner/rbac

.PHONY: mig-agent-manifests ## Generate manifests for the mig-agent (ClusterRole, etc.).
mig-agent-manifests: controller-gen
	$(CONTROLLER_GEN) paths="./internal/controllers/migagent/..." \
	rbac:roleName=mig-agent-role \
	output:rbac:artifacts:config=config/migagent/rbac

.PHONY: gpu-agent-manifests	## Generate manifests for the gpu-agent (ClusterRole, etc.).
gpu-agent-manifests: controller-gen
	$(CONTROLLER_GEN) paths="./internal/controllers/gpuagent/..." \
	rbac:roleName=gpu-agent-role \
	output:rbac:artifacts:config=config/gpuagent/rbac


.PHONY: manifests
manifests: \
	mig-agent-manifests \
	gpu-partitioner-manifests \
	gpu-agent-manifests

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate/license.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test -tags integration ./... -coverprofile cover.out -covermode=count

.PHONY: lint
lint: vet golangci-lint ## Run Go linter.
	$(GOLANGCI_LINT) run ./... -v

.PHONY: license-check
license-check: license-eye ## Check all files have the license header
	$(LICENSE_EYE) header check

.PHONY: license-fix
license-fix: license-eye ## Add license header to files that still don't have it
	$(LICENSE_EYE) header fix

.PHONY: helm-doc
helm-doc: helm-docs-bin ## Generate Helm charts documentation
	$(HELM_DOCS) --chart-search-root ./helm-charts --document-dependency-values ; sh hack/generate-helm-docs.sh

##@ Build

.PHONY: cluster
cluster: kind ## Create a KIND cluster for development
	kind create cluster --config hack/kind/cluster.yaml

.PHONY: build
build: generate fmt vet ## Build manager binary.
	go build -o bin/manager main.go

.PHONY: docker-build-gpu-partitioner
docker-build-gpu-partitioner: ## Build docker image with the gpu-partitioner.
	docker build -t ${GPU_PARTITIONER_IMG} -f build/gpupartitioner/Dockerfile .

.PHONY: docker-build-mig-agent
docker-build-mig-agent: ## Build docker image with the mig-agent.
	docker build -t ${MIG_AGENT_IMG} -f build/migagent/Dockerfile .

.PHONY: docker-build-gpu-agent
docker-build-gpu-agent: ## Build docker image with the gpu-agent.
	docker build -t ${GPU_AGENT_IMG} -f build/gpuagent/Dockerfile .

.PHONY: docker-build-metrics-exporter
docker-build-metrics-exporter: ## Build docker image with the metrics-exporter.
	docker build -t ${METRICS_EXPORTER_IMG} -f build/metricsexporter/Dockerfile .

.PHONY: docker-push-mig-agent
docker-push-mig-agent: ## Push docker image with the mig-agent.
	docker push ${MIG_AGENT_IMG}

.PHONY: docker-push-gpu-agent
docker-push-gpu-agent: ## Push docker image with the gpu-agent.
	docker push ${GPU_AGENT_IMG}

.PHONY: docker-push-gpu-partitioner
docker-push-gpu-partitioner: ## Push docker image with the gpu-partitioner.
	docker push ${GPU_PARTITIONER_IMG}

.PHONY: docker-push-metrics-exporter
docker-push-metrics-exporter: ## Push docker image with the metrics-exporter.
	docker push ${METRICS_EXPORTER_IMG}

.PHONY: docker-build
docker-build: docker-build-mig-agent \
	docker-build-gpu-agent \
	docker-build-gpu-partitioner \
	docker-build-metrics-exporter

.PHONY: docker-push
docker-push: docker-push-mig-agent \
	docker-push-gpu-agent \
	docker-push-gpu-partitioner \
	docker-push-metrics-exporter

.PHONY: helm-push-nos
helm-push-nos: ## Push the nos Helm chart to the Helm repository.
	helm package helm-charts/nos --destination /tmp
	helm push /tmp/nos-*.tgz $(HELM_CHART_REGISTRY)
	rm /tmp/nos-*.tgz

.PHONY: helm-push
helm-push: helm-push-nos ## Push the all the Helm charts to the Helm repository.

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = true
endif

.PHONY: install-cert-manager
install-cert-manager: ## Deploy cert-manager on the K8s cluster specified in ~/.kube/config
	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/$(CERT_MANAGER_VERSION)/cert-manager.yaml

.PHONY: deploy-mig-agent
deploy-mig-agent: kustomize ## Deploy the MIG Agent to the K8s cluster specified in ~/.kube/config.
	cd config/migagent/manager && $(KUSTOMIZE) edit set image mig-agent=${MIG_AGENT_IMG}
	$(KUSTOMIZE) build config/migagent/default | kubectl apply -f -

.PHONY: deploy-gpu-agent
deploy-gpu-agent: kustomize ## Deploy the GPU Agent to the K8s cluster specified in ~/.kube/config.
	cd config/gpuagent/manager && $(KUSTOMIZE) edit set image gpu-agent=${GPU_AGENT_IMG}
	$(KUSTOMIZE) build config/gpuagent/default | kubectl apply -f -

.PHONY: deploy-gpu-partitioner
deploy-gpu-partitioner: kustomize deploy-mig-agent deploy-gpu-agent ## Deploy the GPU Partitioner to the K8s cluster specified in ~/.kube/config.
	cd config/gpupartitioner/manager && $(KUSTOMIZE) edit set image gpu-partitioner=${GPU_PARTITIONER_IMG}
	$(KUSTOMIZE) build config/gpupartitioner/default | kubectl apply -f -

.PHONY: deploy
deploy: deploy-gpu-partitioner deploy-gpu-agent deploy-mig-agent ## Deploy the MIG-related components to the K8s cluster specified in ~/.kube/config.

.PHONY: undeploy-mig-agent
undeploy-mig-agent: ## Undeploy the MIG agent from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/migagent/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: undeploy-gpu-agent
undeploy-gpu-agent: ## Undeploy the GPU agent from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/gpuagent/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: undeploy-gpu-partitioner
undeploy-gpu-partitioner: ## Undeploy the GPU Partitioner from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/gpupartitioner/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: undeploy
undeploy: undeploy-gpu-partitioner undeploy-gpu-agent undeploy-mig-agent ## Undeploy the MIG-related components from the K8s cluster specified in ~/.kube/config.

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
DEFAULTER_GEN ?= $(LOCALBIN)/defaulter-gen
CONVERSION_GEN ?= $(LOCALBIN)/conversion-gen
CODE_GEN ?= $(LOCALBIN)/code-generator
ENVTEST ?= $(LOCALBIN)/setup-envtest
KIND ?= $(LOCALBIN)/kind
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint
LICENSE_EYE ?= $(LOCALBIN)/license-eye
HELM_DOCS ?= $(LOCALBIN)/helm-docs

## Tool Versions
KUSTOMIZE_VERSION ?= latest
CONTROLLER_TOOLS_VERSION ?= latest
CODE_GENERATOR_VERSION ?= latest
GOLANGCI_LINT_VERSION ?= latest
HELM_DOCS_VERSION ?= latest
CERT_MANAGER_VERSION ?= latest

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	test -s $(LOCALBIN)/kustomize || { curl -s $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: defaulter-gen
defaulter-gen: $(DEFAULTER_GEN) ## Download defaulter-gen locally if necessary
$(DEFAULTER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/defaulter-gen || GOBIN=$(LOCALBIN) go install k8s.io/code-generator/cmd/defaulter-gen@$(CODE_GENERATOR_VERSION)

.PHONY: conversion-gen
conversion-gen: $(CONVERSION_GEN) ## Download defaulter-gen locally if necessary
$(CONVERSION_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/conversion-gen || GOBIN=$(LOCALBIN) go install k8s.io/code-generator/cmd/conversion-gen@$(CODE_GENERATOR_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: kind ## Download Kind if necessary
kind: $(KIND)
$(KIND): $(LOCALBIN)
	test -s $(LOCALBIN)/kind || GOBIN=$(LOCALBIN) go install sigs.k8s.io/kind@latest

.PHONY: golangci-lint ## Download golanci-lint if necessary
golangci-lint: $(GOLANGCI_LINT)
$(GOLANGCI_LINT): $(LOCALBIN)
	test -s $(LOCALBIN)/golangci-lint || GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/cmd/golangci-lint@v${GOLANGCI_LINT_VERSION}

.PHONY: license-eye ## Download license-eye if necessary
license-eye: $(LICENSE_EYE)
$(LICENSE_EYE): $(LOCALBIN)
	test -s $(LOCALBIN)/license-eye || GOBIN=$(LOCALBIN) go install github.com/apache/skywalking-eyes/cmd/license-eye@latest

.PHONY: helm-docs-bin ## Download helm-docs if necessary
helm-docs-bin: $(HELM_DOCS)
$(HELM_DOCS): $(LOCALBIN)
	test -s $(LOCALBIN)/helm-docs || GOBIN=$(LOCALBIN) go install github.com/norwoodj/helm-docs/cmd/helm-docs@$(HELM_DOCS_VERSION)
