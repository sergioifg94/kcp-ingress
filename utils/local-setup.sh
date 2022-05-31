#!/bin/bash

#
# Copyright 2021 Red Hat, Inc.
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

LOCAL_SETUP_DIR="$(dirname "${BASH_SOURCE[0]}")"
KCP_GLBC_DIR="${LOCAL_SETUP_DIR}/.."
source "${LOCAL_SETUP_DIR}"/.setupEnv
source "${LOCAL_SETUP_DIR}"/.startUtils

DO_BREW="false"

usage() { echo "usage: ./local-setup.sh -c <number of clusters>" 1>&2; exit 1; }
while getopts ":bc:" arg; do
  case "${arg}" in
    c)
      NUM_CLUSTERS=${OPTARG}
      ;;
    b)
      DO_BREW="true"
      ;;
    *)
      usage
      ;;
  esac
done
shift $((OPTIND-1))

if [[ "$DO_BREW" == "true" ]]; then
  if [[ "${OSTYPE}" =~ ^darwin.* ]]; then
    ${SCRIPT_DIR}/macos/required_brew_packages.sh
  fi
else
  echo "skipping brew"
fi

if [ -z "${NUM_CLUSTERS}" ]; then
    usage
fi

set -e pipefail

trap cleanup EXIT 1 2 3 6 15

cleanup() {
  echo "Killing KCP"
  kill "$KCP_PID"
}

TEMP_DIR="./tmp"
KCP_LOG_FILE="${TEMP_DIR}"/kcp.log

KIND_CLUSTER_PREFIX="kcp-cluster-"
KCP_GLBC_CLUSTER_NAME="${KIND_CLUSTER_PREFIX}glbc-control"

: ${KCP_VERSION:="release-0.4"}
KCP_SYNCER_IMAGE="ghcr.io/kcp-dev/kcp/syncer:${KCP_VERSION}"

for ((i=1;i<=$NUM_CLUSTERS;i++))
do
	CLUSTERS="${CLUSTERS}${KIND_CLUSTER_PREFIX}${i} "
done

mkdir -p ${TEMP_DIR}

[[ ! -z "$KUBECONFIG" ]] && KUBECONFIG="$KUBECONFIG" || KUBECONFIG="$HOME/.kube/config"

createCluster() {
  cluster=$1;
  port80=$2;
  port443=$3;
  cat <<EOF | ${KIND_BIN} create cluster --name ${cluster} --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  image: kindest/node:v1.22.7@sha256:1dfd72d193bf7da64765fd2f2898f78663b9ba366c2aa74be1fd7498a1873166
  kubeadmConfigPatches:
  - |
    kind: InitConfiguration
    nodeRegistration:
      kubeletExtraArgs:
        node-labels: "ingress-ready=true"
  extraPortMappings:
  - containerPort: 80
    hostPort: ${port80}
    protocol: TCP
  - containerPort: 443
    hostPort: ${port443}
    protocol: TCP
EOF

  ${KIND_BIN} get kubeconfig --name=${cluster} > ${TEMP_DIR}/${cluster}.kubeconfig
  ${KIND_BIN} get kubeconfig --internal --name=${cluster} > ${TEMP_DIR}/${cluster}.kubeconfig.internal
}

createKCPWorkloadCluster() {
  [[ ! -z "$4" ]] && clusterName=${4} ||  clusterName=${1}
  echo "Creating KCP WorkloadCluster (${clusterName})"
  createCluster $1 $2 $3

  kubectl config use-context kind-${1}

  echo "Deploying Ingress controller to ${1}"
  VERSION=$(curl https://raw.githubusercontent.com/kubernetes/ingress-nginx/master/stable.txt)
  curl https://raw.githubusercontent.com/kubernetes/ingress-nginx/"${VERSION}"/deploy/static/provider/kind/deploy.yaml | sed "s/--publish-status-address=localhost/--report-node-internal-ip-address/g" | kubectl apply -f -
  kubectl annotate ingressclass nginx "ingressclass.kubernetes.io/is-default-class=true"
  echo "Waiting for deployments to be ready ..."
  kubectl -n ingress-nginx wait --timeout=300s --for=condition=Available deployments --all

  echo "Deploying kcp syncer to ${1}"
  kubectl --kubeconfig=.kcp/admin.kubeconfig create namespace kcp-syncer --dry-run=client -o yaml | kubectl --kubeconfig=.kcp/admin.kubeconfig apply -f -
  KUBECONFIG=.kcp/admin.kubeconfig ${KUBECTL_KCP_BIN} workload sync ${clusterName} --kcp-namespace kcp-syncer --syncer-image=${KCP_SYNCER_IMAGE} --resources=ingresses.networking.k8s.io,services >${TEMP_DIR}/${clusterName}-syncer.yaml
  kubectl apply -f ${TEMP_DIR}/${clusterName}-syncer.yaml
}

createKCPWorkspace() {
  echo "Creating KCP Workspace (${1})"
  KUBECONFIG=.kcp/admin.kubeconfig ${KUBECTL_KCP_BIN} workspace use root:default
  KUBECONFIG=.kcp/admin.kubeconfig ${KUBECTL_KCP_BIN} workspace create ${1} --enter
}

deployGLBC() {
  echo "Deploying GLBC"
  # Note: This doesn't actually deploy glbc, we call the deploy script here to setup the KCP workspace, GLBC ApiBindings and to install cert manager.
  # This allows devs to continue to run the controller locally for dev/test purposes.
  KUBECONFIG=.kcp/admin.kubeconfig ${SCRIPT_DIR}/deploy.sh -c cert-manager
}

#Delete existing kind clusters
clusterCount=$(${KIND_BIN} get clusters | grep ${KIND_CLUSTER_PREFIX} | wc -l)
if ! [[ $clusterCount =~ "0" ]] ; then
  echo "Deleting previous kind clusters."
  ${KIND_BIN} get clusters | grep ${KIND_CLUSTER_PREFIX} | xargs ${KIND_BIN} delete clusters
fi

#1. Start KCP
echo "Starting KCP, sending logs to ${KCP_LOG_FILE}"
${KCP_BIN} start --discovery-poll-interval 3s --run-controllers > ${KCP_LOG_FILE} 2>&1 &
KCP_PID=$!

if ! ps -p ${KCP_PID}; then
  echo "####"
  echo "---> KCP failed to start, see ${KCP_LOG_FILE} for info."
  echo "####"
  exit 1 #this will trigger cleanup function
fi

echo "Waiting for KCP server to be ready..."
wait_for "grep 'Ready to start controllers' ${KCP_LOG_FILE}" "kcp" "1m" "5"

(cd ${KCP_GLBC_DIR} && make generate-ld-config)

#2. Create GLBC workspace (kcp-glbc)
createKCPWorkspace "kcp-glbc"
#3. Create GLBC workload cluster
createKCPWorkloadCluster $KCP_GLBC_CLUSTER_NAME 8081 8444 "glbc"
#4. Deploy GLBC and CertManager and register there APIs
deployGLBC
## ToDo Enable user workspace
# Note: This is temporarily removed until we move to a version of KCP that supports using multiple workspaces (0.5.0).
# Until then we only really need the 1 kind cluster for testing since the placement/locations API's are not currently available.
##5. Create User workspace (kcp-glbc-user)
#createKCPWorkspace "kcp-glbc-user"
##6. Create User workload clusters
#echo "Creating $NUM_CLUSTERS kcp workload cluster(s)"
#port80=8082
#port443=8445
#for cluster in $CLUSTERS; do
#  createKCPWorkloadCluster "$cluster" $port80 $port443
#  port80=$((port80+1))
#  port443=$((port443+1))
#done

echo ""
echo "KCP PID          : ${KCP_PID}"
echo ""
echo "The kind k8s clusters have been registered, and KCP is running, now you should run kcp-glbc."
echo ""
echo "Run Option 1 (Local):"
echo ""
echo "       ./bin/kcp-glbc --kubeconfig .kcp/admin.kubeconfig --context system:admin --glbc-kubeconfig .kcp/admin.kubeconfig"
echo ""
echo "Run Option 2 (Deploy latest in KCP with monitoring enabled):"
echo ""
echo "       KUBECONFIG=${PWD}/.kcp/admin.kubeconfig ./utils/deploy.sh"
echo "       KUBECONFIG=${KUBECONFIG} ./utils/deploy-observability.sh"
echo ""
echo "When glbc is running, try deploying the sample service:"
echo ""
echo "       kubectl apply -f samples/echo-service/echo.yaml"
echo ""
echo "Don't forget to export the proper KUBECONFIG to create objects against KCP:"
echo "export KUBECONFIG=${PWD}/.kcp/admin.kubeconfig"
echo ""
read -p "Press enter to exit -> It will kill the KCP process running in background"
