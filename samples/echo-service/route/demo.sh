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

export KUBECONFIG=./.kcp/admin.kubeconfig
GLBC_WORKSPACE=root:kuadrant
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"


echo
echo "before running this script, ensure that you have a CRC synctarget created and functional"
read -p "Press enter to create initial route with unverified custom domain"

kubectl create -f ${SCRIPT_DIR}/dns-mock.yaml
kubectl create -f ${SCRIPT_DIR}/echo-route.yaml

sleep 10

echo
echo "dig "$(kubectl get route echo -o yaml | yq eval ".spec.host" -)
echo "curl -k https://"$(kubectl get route echo -o yaml | yq eval ".spec.host" -)

#update CNAME for custom domain
echo "update CNAME for test.pb-custom.hcpapps.net in route53 to: "$(kubectl get route echo -o yaml | yq eval ".spec.host" -)" now"

read -p "press enter to continue to domain verification"

#mock domain verification
kubectl apply -f ${SCRIPT_DIR}/dns-mock-approved-domain.yaml

sleep 10

#show route using custom domain
echo
echo "dig test.pb-custom.hcpapps.net"
echo "kubectl get routes"
echo "echo host: "$(kubectl get route echo -o yaml | yq eval ".spec.host" -)
echo "echo-shadow host: "$(kubectl get route echo-shadow -o yaml | yq eval ".spec.host" -)
echo "curl -k https://test.pb-custom.hcpapps.net"
echo "curl -k https://"$(kubectl get route echo-shadow -o yaml | yq eval ".spec.host" -)

read -p "press enter to continue to clean up"
echo "existing resources..."
echo "---> routes"
kubectl get routes
echo "---> dnsrecords"
kubectl get dnsrecords
echo

read -p "press enter to begin deletion"
#delete route
kubectl delete -f ${SCRIPT_DIR}/echo-route.yaml
kubectl delete -f ${SCRIPT_DIR}/dns-mock.yaml

sleep 10

echo
echo "existing resources..."
echo "---> routes"
kubectl get routes
echo "---> dnsrecords"
kubectl get dnsrecords
echo