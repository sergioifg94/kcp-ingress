SELF_DIR := $(dir $(lastword $(MAKEFILE_LIST)))

DHALL ?= $(LOCALBIN)/dhall
DHALL_TO_YAML ?= $(LOCALBIN)/dhall-to-yaml
DHALL_TO_JSON ?= $(LOCALBIN)/dhall-to-json
DHALL_TO_YAML_BINARY_LINUX ?= "https://github.com/dhall-lang/dhall-haskell/releases/download/1.41.1/dhall-json-1.7.10-x86_64-linux.tar.bz2"
DHALL_TO_YAML_BINARY_MACOS ?= "https://github.com/dhall-lang/dhall-haskell/releases/download/1.41.1/dhall-json-1.7.10-x86_64-macos.tar.bz2"
DHALL_BINARY_LINUX ?= "https://github.com/dhall-lang/dhall-haskell/releases/download/1.41.1/dhall-1.41.1-x86_64-linux.tar.bz2"
DHALL_BINARY_MACOS ?= "https://github.com/dhall-lang/dhall-haskell/releases/download/1.41.1/dhall-1.41.1-x86_64-macos.tar.bz2"
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Linux)
	DHALL_TO_YAML_BINARY=$(DHALL_TO_YAML_BINARY_LINUX)
	DHALL_BINARY=$(DHALL_BINARY_LINUX)
endif
ifeq ($(UNAME_S),Darwin)
	DHALL_TO_YAML_BINARY=$(DHALL_TO_YAML_BINARY_MACOS)
	DHALL_BINARY=$(DHALL_BINARY_MACOS)
endif

.PHONY: dhall
dhall: $(DHALL) ## Download dhall locally if necessary.
$(DHALL):
	curl -L $(DHALL_TO_YAML_BINARY) | tar --extract --bzip2
	curl -L $(DHALL_BINARY) | tar --exclude='./share' --extract --bzip2

DHALL_SOURCE_DIR := config/observability/monitoring_resources
DHALL_SOURCE_FILES=$(shell find $(DHALL_SOURCE_DIR) -type f -name "*.dhall")
DHALL_COMMON_SOURCE_DIR := $(DHALL_SOURCE_DIR)/common
DHALL_K8S_SOURCE_DIR := $(DHALL_SOURCE_DIR)/kubernetes
DHALL_OPENSHIFT_SOURCE_DIR := $(DHALL_SOURCE_DIR)/openshift
DHALL_K8S_TARGET_DIR := config/observability/kubernetes/monitoring_resources
DHALL_OPENSHIFT_TARGET_DIR := config/observability/openshift/monitoring_resources
DHALL_K8S_TARGETS := $(addprefix $(DHALL_K8S_TARGET_DIR)/,$(patsubst %.dhall,%.yaml,$(shell ls $(DHALL_COMMON_SOURCE_DIR)/*.dhall | xargs -n 1 basename))) $(addprefix $(DHALL_K8S_TARGET_DIR)/,$(patsubst %.dhall,%.yaml,$(shell ls $(DHALL_K8S_SOURCE_DIR)/*.dhall | xargs -n 1 basename)))
DHALL_OPENSHIFT_TARGETS := $(addprefix $(DHALL_OPENSHIFT_TARGET_DIR)/,$(patsubst %.dhall,%.yaml,$(shell ls $(DHALL_COMMON_SOURCE_DIR)/*.dhall | xargs -n 1 basename))) $(addprefix $(DHALL_OPENSHIFT_TARGET_DIR)/,$(patsubst %.dhall,%.yaml,$(shell ls $(DHALL_OPENSHIFT_SOURCE_DIR)/*.dhall | xargs -n 1 basename)))

.PHONY: dhall-format
dhall-format: dhall
	$(DHALL) lint $(DHALL_SOURCE_FILES)
	$(DHALL) format $(DHALL_SOURCE_FILES)

define GENERATE_DHALL
	$(DHALL_TO_YAML) --generated-comment --file $< --output $@
endef

$(DHALL_K8S_TARGET_DIR)/%.yaml: $(DHALL_COMMON_SOURCE_DIR)/%.dhall
	$(GENERATE_DHALL)

$(DHALL_OPENSHIFT_TARGET_DIR)/%.yaml: $(DHALL_COMMON_SOURCE_DIR)/%.dhall
	$(GENERATE_DHALL)

$(DHALL_K8S_TARGET_DIR)/%.yaml: $(DHALL_K8S_SOURCE_DIR)/%.dhall
	$(GENERATE_DHALL)

$(DHALL_OPENSHIFT_TARGET_DIR)/%.yaml: $(DHALL_OPENSHIFT_SOURCE_DIR)/%.dhall
	$(GENERATE_DHALL)

$(DHALL_K8S_TARGETS): | $(DHALL_K8S_TARGET_DIR)

$(DHALL_OPENSHIFT_TARGETS): | $(DHALL_OPENSHIFT_TARGET_DIR)

$(DHALL_K8S_TARGET_DIR):
	mkdir $(DHALL_K8S_TARGET_DIR)

$(DHALL_OPENSHIFT_TARGET_DIR):
	mkdir $(DHALL_OPENSHIFT_TARGET_DIR)

# TODO: detect when dashboard json file has been modified, which is used by dashboard dhall files
# TODO: regen prometheusrule crs when rules files have changed. 
.PHONY: touch-monitoring-files
touch-monitoring-files:
	touch config/observability/monitoring_resources/kubernetes/*.dhall config/observability/monitoring_resources/openshift/*.dhall
	touch config/observability/monitoring_resources/common/*.dhall

# Generate monitoring resources for prometheus etc... 
.PHONY: gen-monitoring-resources
gen-monitoring-resources: touch-monitoring-files dhall dhall-format ${DHALL_K8S_TARGETS} ${DHALL_OPENSHIFT_TARGETS}

# Ensure the generated monitoring resources are the latest
.PHONY: verify-gen-monitoring-resources
verify-gen-monitoring-resources: gen-monitoring-resources
	git diff --exit-code

# Run all prometheus (alert) rules unit tests
.PHONY: prometheus-rules-unit-test
prometheus-rules-unit-test:
	docker run --rm -t \
    -v $(shell pwd)/config/observability/kubernetes/monitoring_resources:/prometheus:z --entrypoint=/bin/sh \
$(PROMTOOL_IMAGE) -c 'promtool test rules /prometheus/rules_unit_tests/*'