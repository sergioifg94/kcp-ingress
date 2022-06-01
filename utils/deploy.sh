#!/bin/bash

#
# Copyright 2022 Red Hat, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

DEPLOY_SCRIPT_DIR="$(dirname "${BASH_SOURCE[0]}")"
KCP_GLBC_DIR="${DEPLOY_SCRIPT_DIR}/.."
source "${DEPLOY_SCRIPT_DIR}"/.setupEnv
source "${DEPLOY_SCRIPT_DIR}"/.startUtils

ORG_WORKSPACE=root:default
GLBC_WORKSPACE=kcp-glbc
GLBC_NAMESPACE=kcp-glbc
GLBC_WORKLOAD_CLUSTER=glbc
DEPLOY_COMPONENTS=glbc,cert-manager
GLBC_KUSTOMIZATION=${KCP_GLBC_DIR}/config/deploy/local

KCP_VERSION="release-0.4"
KCP_SYNCER_IMAGE="ghcr.io/kcp-dev/kcp/syncer:${KCP_VERSION}"

############################################################
# Help                                                     #
############################################################
help()
{
   # Display Help
   echo "Prepares a KCP workspace and deploys GLBC and its dependant components into it."
   echo
   echo "Syntax: deploy.sh [-n|h|w|W]"
   echo "options:"
   echo "c     Components to deploy (default: ${DEPLOY_COMPONENTS})"
   echo "k     GLBC deployment kustomization directory (default: ${GLBC_KUSTOMIZATION})"
   echo "n     Namespace glbc is being deployed into (default: ${GLBC_NAMESPACE})"
   echo "h     Print this Help."
   echo "w     Workspace to create and use for deployment (default: ${GLBC_WORKSPACE})."
   echo "W     Organisation workspace (default: ${ORG_WORKSPACE})."
   echo
}

create_ns() {
  echo "Creating namespace '${1}'"
  # Create a namespace and force it to target the GLBC workload cluster
  kubectl create namespace ${1} --dry-run=client -o yaml | kubectl apply -f -
  kubectl label --overwrite namespace ${1} workloads.kcp.dev/cluster=${GLBC_WORKLOAD_CLUSTER}
}

create_glbc_workload_cluster() {
  echo "Creating GLBC workload cluster '${GLBC_WORKLOAD_CLUSTER}'"
  ${KUBECTL_KCP_BIN} workload sync ${GLBC_WORKLOAD_CLUSTER} --kcp-namespace kcp-syncer --syncer-image=${KCP_SYNCER_IMAGE} --resources=ingresses.networking.k8s.io,services > ${GLBC_KUSTOMIZATION}/${GLBC_WORKLOAD_CLUSTER}-syncer.yaml
  echo "Apply the following syncer config to the intended GLBC physical cluster."
  echo ""
  echo "   kubectl apply -f ${GLBC_KUSTOMIZATION}/${GLBC_WORKLOAD_CLUSTER}-syncer.yaml"
  echo ""
  echo "This script will automatically continue once the cluster is synced!"
}

deploy_cert_manager() {
  echo "Deploying Cert Manager"
  create_ns "cert-manager"
  kubectl apply -f ${KCP_GLBC_DIR}/config/cert-manager/cert-manager.yaml
  echo "Waiting for Cert Manager deployments to be ready..."
  kubectl -n cert-manager wait --timeout=300s --for=condition=Available deployments --all
}

deploy_glbc() {
  echo "Deploying GLBC"
  create_ns ${GLBC_NAMESPACE}
  ## Create cluster scoped service account
  # ToDo Allow adding -o ${GLBC_KUSTOMIZATION}/kcp.kubeconfig for non local deployments
  ${DEPLOY_SCRIPT_DIR}/create_glbc_ns.sh -n "default" -c ${GLBC_WORKSPACE} -C
  ${KUSTOMIZE_BIN} build ${GLBC_KUSTOMIZATION} | kubectl apply -f -
  echo "Waiting for GLBC deployments to be ready..."
  kubectl -n ${GLBC_NAMESPACE} wait --timeout=300s --for=condition=Available deployments --all
}

deploy_glbc_observability() {
    echo "Deploying GLBC Observability"
    create_ns "kcp-glbc-observability"
    ## Deploy Grafana
    wait_for "${KUSTOMIZE_BIN} build config/observability/kubernetes/grafana/ | kubectl apply -f -" "grafana" "1m" "5"
    echo "Waiting for Observability deployments to be ready..."
    kubectl -n kcp-glbc-observability wait --timeout=300s --for=condition=Available deployments --all
    ## Deploy Pod Monitor for kcp-glbc
    ${KUSTOMIZE_BIN} build config/prometheus/ | kubectl -n ${GLBC_NAMESPACE} apply -f -
}

############################################################
# Script Start                                             #
############################################################

while getopts "c:k:n:hw:W:" arg; do
  case "${arg}" in
    c)
      DEPLOY_COMPONENTS=${OPTARG}
      ;;
    k)
      GLBC_KUSTOMIZATION=${OPTARG}
      ;;
    n)
      GLBC_NAMESPACE=${OPTARG}
      ;;
    h)
      help
      exit 0
      ;;
    w)
      GLBC_WORKSPACE=${OPTARG}
      ;;
    W)
      ORG_WORKSPACE=${OPTARG}
      ;;
    *)
      help
      exit 1
      ;;
  esac
done
shift $((OPTIND-1))

set -e pipefail

## Check we are targeting a kcp instance
${KUBECTL_KCP_BIN} workspace list > /dev/null || (echo "You must be targeting a KCP API Server, check your current KUBECONIFG and context before continuing!" && exit 1)

## Create target workspace if it doesn't already exist
${KUBECTL_KCP_BIN} workspace use ${ORG_WORKSPACE}
${KUBECTL_KCP_BIN} workspace create ${GLBC_WORKSPACE} --enter || ${KUBECTL_KCP_BIN} workspace use ${GLBC_WORKSPACE}

## Create GLBC workload cluster
kubectl create namespace kcp-syncer --dry-run=client -o yaml | kubectl apply -f -
kubectl get workloadclusters ${GLBC_WORKLOAD_CLUSTER} || create_glbc_workload_cluster

## Register K8s v1 APIs
kubectl apply -f ${KCP_GLBC_DIR}/utils/kcp-contrib/crds

## Register GLBC APIs
kubectl apply -f ${KCP_GLBC_DIR}/config/crd/bases
kubectl apply -f ${KCP_GLBC_DIR}/utils/kcp-contrib/apiresourceschema.yaml
kubectl apply -f ${KCP_GLBC_DIR}/utils/kcp-contrib/apiexport.yaml

## Register CertManager APIs
kubectl apply -f ${KCP_GLBC_DIR}/config/cert-manager/certificates-apiresourceschema.yaml
kubectl apply -f ${KCP_GLBC_DIR}/config/cert-manager/cert-manager-apiexport.yaml
#ToDO apibinding target needs to change based on target namespace
kubectl apply -f ${KCP_GLBC_DIR}/config/cert-manager/cert-manager-apibinding.yaml

## Deploy components
if [[ $DEPLOY_COMPONENTS =~ "cert-manager" ]]; then
  deploy_cert_manager
fi

if [[ $DEPLOY_COMPONENTS =~ "glbc" ]]; then
  deploy_glbc
  if [[ $DEPLOY_COMPONENTS =~ "observability" ]]; then
    deploy_glbc_observability
  fi

  echo ""
  echo "GLBC is now running."
  echo ""
  echo "Try deploying the sample service:"
  echo ""
  echo "     kubectl apply -f samples/echo-service/echo.yaml"
  echo ""
fi
