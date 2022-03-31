
SHELL := /usr/bin/env bash

NUM_CLUSTERS := 2
KCP_BRANCH := release-prototype-2

IMAGE_TAG_BASE ?= quay.io/kuadrant/kcp-glbc
IMAGE_TAG ?= latest
IMG ?= $(IMAGE_TAG_BASE):$(IMAGE_TAG)

KUBECONFIG ?= $(shell pwd)/.kcp/admin.kubeconfig
CLUSTERS_KUBECONFIG_DIR ?= $(shell pwd)/tmp

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: clean
clean: ## Clean up temporary files.
	-rm -rf ./.kcp
	-rm -f ./bin/*
	-rm -rf ./tmp

.PHONY: generate
generate: generate-deepcopy generate-crd generate-client ## Generate code containing DeepCopy method implementations, CustomResourceDefinition objects and Clients.

.PHONY: generate-deepcopy
generate-deepcopy: controller-gen
	cd pkg/apis/kuadrant && $(CONTROLLER_GEN) paths="./..." object

.PHONY: generate-deepcopy
generate-crd: controller-gen
	cd pkg/apis/kuadrant && $(CONTROLLER_GEN) crd paths=./... output:crd:artifacts:config=../../../config/crd output:crd:dir=../../../config/crd/bases crd:crdVersions=v1 && rm -rf ./config

.PHONY: generate-client
generate-client:
	./scripts/gen_client.sh

.PHONY: vendor
vendor: ## Vendor the dependencies.
	go mod tidy
	go mod vendor

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint against code.
	golangci-lint run ./...

.PHONY: test
test: generate ## Run tests.
	go test -v ./... -coverprofile=cover.out

.PHONY: e2e
e2e: build
	KUBECONFIG="$(KUBECONFIG)" CLUSTERS_KUBECONFIG_DIR="$(CLUSTERS_KUBECONFIG_DIR)" \
	go test -timeout 60m -v ./e2e -tags=e2e

##@ CI

#Note, these targets are expected to run in a clean CI environment.

.PHONY: verify-generate
verify-generate: generate ## Verify generate update.
	git diff --exit-code

##@ Build

.PHONY: build
build: ## Build the project.
	go build -o bin ./cmd/...

.PHONY: docker-build
docker-build: ## Build docker image.
	docker build -t ${IMG} .

##@ Deployment

.PHONY: install
install: generate-crd kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: generate-crd kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

.PHONY: deploy
deploy: generate-crd kustomize deploy-glbc-config ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete -f -

KCP_KUBECONFIG ?= .kcp/admin.kubeconfig
GLBC_KUBECONFIG ?= ./tmp/kcp-cluster-glbc-control.kubeconfig.internal
HCG_LE_EMAIL ?= kuadrant-dev@redhat.com
GLBC_TLS_PROVIDED ?= true
GLBC_TLS_PROVIDER ?= le-staging
GLBC_ENABLE_CUSTOM_HOSTS ?= false
GLBC_DOMAIN ?= dev.hcpapps.net
GLBC_DNS_PROVIDER ?= aws
AWS_DNS_PUBLIC_ZONE_ID ?= Z08652651232L9P84LRSB
.PHONY: deploy-glbc-config
deploy-glbc-config: ## Deploy glbc secrets and config to K8s cluster specified in ~/.kube/config.
	kubectl create ns kcp-glbc | true
	kubectl -n kcp-glbc create secret generic kcp-kubeconfig --from-file=kubeconfig=$(KCP_KUBECONFIG) | true
	kubectl -n kcp-glbc create secret generic glbc-kubeconfig --from-file=kubeconfig=$(GLBC_KUBECONFIG) | true
	kubectl -n kcp-glbc create secret generic aws-credentials \
			--from-literal=AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID} \
			--from-literal=AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY} | true
	kubectl -n kcp-glbc create configmap glbc-config \
			--from-literal=GLBC_TLS_PROVIDED=$(GLBC_TLS_PROVIDED) \
			--from-literal=GLBC_TLS_PROVIDER=$(GLBC_TLS_PROVIDER) \
			--from-literal=GLBC_DOMAIN=$(GLBC_DOMAIN) \
			--from-literal=GLBC_ENABLE_CUSTOM_HOSTS=$(GLBC_ENABLE_CUSTOM_HOSTS) \
			--from-literal=GLBC_DNS_PROVIDER=$(GLBC_DNS_PROVIDER) \
			--from-literal=AWS_DNS_PUBLIC_ZONE_ID=$(AWS_DNS_PUBLIC_ZONE_ID) \
			--from-literal=HCG_LE_EMAIL=$(HCG_LE_EMAIL) | true

.PHONY: local-setup
local-setup: clean build kind kcp ## Setup kcp locally using kind.
	./utils/local-setup.sh -c ${NUM_CLUSTERS}

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN): ## Ensure that the directory exists
	mkdir -p $(LOCALBIN)

## Tool Binaries
KCP ?= $(LOCALBIN)/kcp
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
KIND ?= $(LOCALBIN)/kind

## Tool Versions
KUSTOMIZE_VERSION ?= v3.8.7
CONTROLLER_TOOLS_VERSION ?= v0.8.0
KIND_VERSION ?= v0.11.1

.PHONY: kcp
kcp: $(KCP) ## Download kcp locally if necessary.
$(KCP):
	rm -rf ./tmp/kcp
	git clone --depth=1 --branch ${KCP_BRANCH} https://github.com/kuadrant/kcp ./tmp/kcp
	cd ./tmp/kcp && make
	cp ./tmp/kcp/bin/cluster-controller $(LOCALBIN)
	cp ./tmp/kcp/bin/compat $(LOCALBIN)
	cp ./tmp/kcp/bin/crd-puller $(LOCALBIN)
	cp ./tmp/kcp/bin/deployment-splitter $(LOCALBIN)
	cp ./tmp/kcp/bin/kcp $(LOCALBIN)
	cp ./tmp/kcp/bin/kubectl-kcp $(LOCALBIN)
	cp ./tmp/kcp/bin/shard-proxy $(LOCALBIN)
	cp ./tmp/kcp/bin/syncer $(LOCALBIN)
	cp ./tmp/kcp/bin/virtual-workspaces $(LOCALBIN)
	rm -rf ./tmp/kcp

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN):
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: kind
kind: $(KIND) ## Download kind locally if necessary.
$(KIND):
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/kind@$(KIND_VERSION)

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE):
	curl -s $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN)
