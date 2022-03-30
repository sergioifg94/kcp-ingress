
SHELL := /usr/bin/env bash

NUM_CLUSTERS := 2
KCP_BRANCH := release-prototype-2

IMAGE_TAG_BASE ?= quay.io/kuadrant/kcp-glbc
IMAGE_TAG ?= latest
IMG ?= $(IMAGE_TAG_BASE):$(IMAGE_TAG)

GO_VERSION = $(shell go version | awk '{print $$3}')
GO_GET_INSTALL = go get
ifeq ($(GO_VERSION),go1.18)
	GO_GET_INSTALL = go install
endif

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

generate: generate-deepcopy generate-crd generate-client ## Generate code containing DeepCopy method implementations, CustomResourceDefinition objects and Clients.

generate-deepcopy: controller-gen
	cd pkg/apis/kuadrant && $(CONTROLLER_GEN) paths="./..." object

generate-crd: controller-gen
	cd pkg/apis/kuadrant && $(CONTROLLER_GEN) crd paths=./... output:crd:artifacts:config=../../../config/crd output:crd:dir=../../../config/crd/bases crd:crdVersions=v1 && rm -rf ./config

generate-client:
	./scripts/gen_client.sh

vendor: ## Vendor the dependencies.
	go mod tidy
	go mod vendor
.PHONY: vendor

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

lint: ## Run golangci-lint against code.
	golangci-lint run ./...
.PHONY: lint

.PHONY: test
test: generate ## Run tests.
	go test -v ./... -coverprofile=cover.out

e2e: build
	KUBECONFIG="$(KUBECONFIG)" CLUSTERS_KUBECONFIG_DIR="$(CLUSTERS_KUBECONFIG_DIR)" \
	go test -timeout 60m -v ./e2e -tags=e2e

##@ CI

#Note, these targets are expected to run in a clean CI environment.

.PHONY: verify-generate
verify-generate: generate ## Verify generate update.
	git diff --exit-code

##@ Build

build: ## Build the project.
	go build -o bin ./cmd/...
.PHONY: build

.PHONY: docker-build
docker-build: ## Build docker image.
	docker build -t ${IMG} .

##@ Deployment

install: generate-crd kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

uninstall: generate-crd kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

deploy: generate-crd kustomize deploy-glbc-config ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

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

KCP = $(shell pwd)/bin/kcp
kcp: ## Download kcp locally.
	rm -rf ./tmp/kcp
	git clone --depth=1 --branch ${KCP_BRANCH} https://github.com/kuadrant/kcp ./tmp/kcp
	cd ./tmp/kcp && make
	cp ./tmp/kcp/bin/cluster-controller $(shell pwd)/bin
	cp ./tmp/kcp/bin/compat $(shell pwd)/bin
	cp ./tmp/kcp/bin/crd-puller $(shell pwd)/bin
	cp ./tmp/kcp/bin/deployment-splitter $(shell pwd)/bin
	cp ./tmp/kcp/bin/kcp $(shell pwd)/bin
	cp ./tmp/kcp/bin/kubectl-kcp $(shell pwd)/bin
	cp ./tmp/kcp/bin/shard-proxy $(shell pwd)/bin
	cp ./tmp/kcp/bin/syncer $(shell pwd)/bin
	cp ./tmp/kcp/bin/virtual-workspaces $(shell pwd)/bin
	rm -rf ./tmp/kcp

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.8.0)

KIND = $(shell pwd)/bin/kind
kind: ## Download kind locally if necessary.
	$(call go-get-tool,$(KIND),sigs.k8s.io/kind@v0.11.1)

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin $(GO_GET_INSTALL) $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef
