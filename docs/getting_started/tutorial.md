# GLBC

The Global Load Balancer Controller (GLBC) leverages [`kcp`](https://github.com/Kuadrant/kcp) to provide DNS-based global load balancing and transparent multi-cluster ingress. The main API for the GLBC is the Kubernetes Ingress object. GLBC watches Ingress objects and transforms them adding in the GLBC managed host and TLS certificate.

For more information on the architecture of GLBC and how the various component work, refer to the [overview documentation](https://github.com/Kuadrant/kcp-glbc/blob/bb8e43639691568b594720244a0c94a23470a587/docs/getting_started/overview.md).

Use this tutorial to perform the following actions:

* Install the kcp-glbc instance and verify installation.
* Follow the demo and have GLBC running and working with an AWS domain. You can then deploy the sample service to view how GLBC allows access to services  in a multi-cluster ingress scenario.

<br>

**Tutorial Contents:**
* [Prerequisites](#prerequisites)
* [Install](#installation)
* [Provide GLBC credentials](#provide-glbc-with-aws-credentials-and-configuration)
* [Run GLBC](#run-glbc)
* [Deploy the sample service](#deploy-the-sample-service)
* [Verify sample service deployment](#verify-sample-service-deployment)
* [Demo: **Providing ingress in a multi-cluster ingress scenario**](#main-use-case)

---

## Prerequisites
- Install [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl).
- Install Go 1.18 or higher. This is the version used in kcp-glbc as indicated in the [`go.mod`](https://github.com/Kuadrant/kcp-glbc/blob/main/go.mod) file.
- Install the [yq](https://github.com/mikefarah/yq) command-line YAML processor.
- Have an AWS account, a DNS Zone, and a subdomain of the domain being used. You will need this in order to instruct GLBC to make use of your AWS credentials and configuration.
- Add the `kcp-glbc/bin` directory to your `$PATH`

## Installation

Clone the repo and run the following command:

```bash
make local-setup
```
> NOTE: If errors are encountered during the local-setup, refer to the Troubleshooting Installation document.

This script performs the following actions: 
* Builds all the binaries
* Deploys three Kubernetes 1.22 clusters locally using [kind](https://kind.sigs.k8s.io/)
* Deploys and configures the ingress controllers in each cluster
* Downloads kcp at the latest version integrated with GLBC
* Starts the kcp server
* Creates kcp workspaces for GLBC and user resources:
    * `kcp-glbc`
    * `kcp-glbc-compute`
    * `kcp-glbc-user`
    * `kcp-glbc-user-compute`
* Add workload clusters to the `*-compute` workspaces
    * `kcp-glbc-compute`: 1x kind cluster
    * `kcp-glbc-user-compute`: 2x kind clusters
* Deploy GLBC dependencies (`cert-manager`) into the `kcp-glbc` workspace.

-----

After `local-setup` has successfully completed, it will indicate that kcp is now running. However, at this point, GLBC is not yet running. You will be presented in the terminal with two options to deploy GLBC:

1. [Local-deployment](https://github.com/Kuadrant/kcp-glbc/blob/main/docs/local_deployment.md): this option is good for testing purposes by using a local kcp instance and kind clusters.

1. [Deploy latest in kcp](https://github.com/Kuadrant/kcp-glbc/blob/main/docs/deployment.md) with monitoring enabled: this will deploy GLBC to your target kcp instance. This will enable you to view observability in Prometheus and Grafana.

For the demo, before deploying GLBC, we will want to provide it with your AWS credentials and configuration.

<br>

### Provide GLBC with AWS credentials and configuration

The easiest way to do this is to perform the following steps:

1. Open the `kcp-glbc` project in your IDE.
1. Navigate to the `./config/deploy/local/aws-credentials.env` environment file.
1. Enter your `AWS access key ID` and `AWS Secret Access Key` as indicated in the example below:
  
     ```bash
      AWS_ACCESS_KEY_ID=EXAMPLEID2DJ3rSA3E
      AWS_SECRET_ACCESS_KEY=EXAMPLEKEYIEI034+fETFDS34QFAD0IAO
      ```
1. Navigate to `./config/deploy/local/controller-config.env` and change the fields to resemble something similar to following:

   ```bash
   AWS_DNS_PUBLIC_ZONE_ID=Z0485348LD348SDHJSR0
   GLBC_DNS_PROVIDER=aws
   GLBC_DOMAIN=cz.hcpapps.net
   GLBC_ENABLE_CUSTOM_HOSTS=false
   GLBC_LOGICAL_CLUSTER_TARGET=*
   GLBC_TLS_PROVIDED=true
   GLBC_TLS_PROVIDER=glbc-ca
   HCG_LE_EMAIL=kuadrant-dev@redhat.com
   NAMESPACE=kcp-glbc
   GLBC_WORKSPACE=root:default:kcp-glbc
   ```

   The fields that might need to be edited include:
     - Replace `<AWS_DNS_PUBLIC_ZONE_ID>` with your own hosted zone ID
     - Replace `<GLBC_DNS_PROVIDER>` with `aws`
     - Replace `<GLBC_DOMAIN>` with your specified subdomain

<br>

### Run GLBC

After all the above is configured correctly, for the demo, we can run the first command under _Option 1_ to change to the directory where the repo is located. The commands are similar to the following (run them in a new tab):

   ```bash
   Run Option 1 (Local):
          cd to/the/repo
   ```

Using the same tab in the terminal, run the following command to run GLBC and use `controller-config.env` and `aws-credentials.env`. We will be able to curl the domain in the tutorial and visualize how the workload migrates from `cluster-1` to `cluster-2`.

   ```bash
   (export $(cat ./config/deploy/local/controller-config.env | xargs) && export $(cat ./config/deploy/local/aws-credentials.env | xargs) && KUBECONFIG=./tmp/kcp.kubeconfig ./bin/kcp-glbc)
   ```

<br>

### Deploy the sample service

Now we will attempt to deploy the sample service. You can do this by running the following command in a new tab in the terminal:

   ```bash
   ./samples/location-api/sample.sh
   ```
After the sample service has been deployed, we are presented with the following output of what was done, and some useful commands:

![Screenshot from 2022-08-02 12-22-17](https://user-images.githubusercontent.com/73656840/182363020-6aa61b73-c2a7-4570-ada7-aae97ad9db00.png)


The sample script will remain paused until we press the enter key to migrate the workload from one cluster to the other. However, we will not perform this action just yet.

<br>

### Verify sample service deployment

1. In a new terminal, verify that the ingress was created after deploying the sample service:
   ```bash
   export KUBECONFIG=.kcp/admin.kubeconfig                                         
   ./bin/kubectl-kcp workspace use root:default:kcp-glbc-user
   kubectl get ingress
   ```
   ```bash
   NAME                AGE
   ingress-nondomain   81s
   ```

1. Verify that the DNS record was created:
   ```bash
   export KUBECONFIG=.kcp/admin.kubeconfig                                         
   ./bin/kubectl-kcp workspace use root:default:kcp-glbc-user
   kubectl get dnsrecords ingress-nondomain -o yaml
   ```
   We might not get an output just yet until the DNS record exists in `route53`. This may take several minutes.

1. Alternatively, you can also view if the DNS record was created, by  in your AWS domain .

   ![Screenshot from 2022-08-02 12-26-19](https://user-images.githubusercontent.com/73656840/182363808-558f8a40-4ed6-4e08-9c02-1d74b6209b46.png)

------

## Main Use Case

### Demo: Providing ingress in a multi-cluster ingress scenario

This section will show how GLBC is used to provide ingress in a multi-cluster ingress scenario.

For this tutorial, after following along the with the [Installation](#installation) section of this document, we should already have `kcp` and GLBC running, and also have had deployed the sample service which would have created a placement resource, an ingress named *"ingress-nondomain"* and a DNS record. Note, the "default" namespace is where we are putting all the sample resources at the moment.

<br>

#### Viewing the "default" namespace

We will run the following commands in a new tab:

   ```bash
   export KUBECONFIG=.kcp/admin.kubeconfig                                         
   ./bin/kubectl-kcp workspace use root:default:kcp-glbc-user
   kubectl get ns default -o yaml
   ```
As we can see, there is a label named: `*state.internal.workload.kcp.dev/kcp-cluster-1: Sync*`:

   ![Screenshot from 2022-08-02 12-32-06](https://user-images.githubusercontent.com/73656840/182365628-22f04bb5-0818-46a3-8a12-3abc2e8451f3.png)

GLBC is telling `kcp` where to sync all of the work resources in the namespace. Meaning, since the namespace is is set to `kcp-cluster-1` , the ingress will also have `kcp-cluster-1` set to it. 

<br>

#### Watching the ingress and the DNS record

We can run the watch command in a new tab to start watching the ingress:

   ```bash
   export KUBECONFIG=.kcp/admin.kubeconfig                                         
   ./bin/kubectl-kcp workspace use root:default:kcp-glbc-user
   watch -n1 -d 'kubectl get ingress ingress-nondomain -o yaml | yq eval ".metadata" - | grep -v "kubernetes.io"'
   ```

As we can see in the first annotation, the load balancer for `kcp-cluster-1` will have an IP address, after the DNS record is created:

   ![Screenshot from 2022-08-02 12-40-48](https://user-images.githubusercontent.com/73656840/182366116-aa2f32ce-a603-49bb-b974-e9356c71c6fc.png)


Alternatively, we can also run the following command in another tab to start watching the DNS record in real-time:

   ```bash
   export KUBECONFIG=.kcp/admin.kubeconfig                                         
   ./bin/kubectl-kcp workspace use root:default:kcp-glbc-user
   watch -n1 'kubectl get dnsrecords ingress-nondomain -o yaml | yq eval ".spec" -'
   ```
   
<br>

#### Curl the running domain

Now that the DNS record has been successfully created, in a new tab in the terminal, we can curl the domain to view it. To do this, we will run the following watch command that is outputs to our termial and will look similar to the following:

   ```bash
   watch -n1 "curl -k https://cbkgg75kjgmah1mbpvsg.cz.hcpapps.net"
   ```

   This will curl the domain which will give an output similar to the following:

   ![Screenshot from 2022-08-02 12-44-15](https://user-images.githubusercontent.com/73656840/182368772-8a08a197-66d9-4d9c-9747-74ddaad0e4d7.png)

This means that `kcp-cluster-1` is up and running correctly.

<br>

#### Migrating workload from `kcp-cluster-1` to `kcp-cluster-2`

As we continue with the following steps, we will want to be observing the tab where we are watching our domain. This way, we will notice that during the workload migration, there is no interruptions and no down time.

To proceed with the workload migration, we will go to the tab where we deployed the sample service, and press the enter key to "trigger migration from `kcp-cluster-1` to `kcp-cluster-2`. This deletes `placement-1` and creates `placement-2` which points to `kcp-cluster-2`. This will also change the label in the "default" namespace mentioned before: `*state.internal.workload.kcp.dev/kcp-cluster-1: Sync*`, and change it from `kcp-cluster-1` to `kcp-cluster-2`.

   ![Screenshot from 2022-08-02 12-48-05](https://user-images.githubusercontent.com/73656840/182367670-dc6c243d-aea7-44e9-bebf-99685391d931.png)


In the tab where we are watching the ingress, we can observe that the label in the ingress has changed from `kcp-cluster-1` to `kcp-cluster-2`. KCP has propagated that label down from the namespace to everything in it. Everything in the namespace gets the same label. Because of that label, `kcp` is syncing it to `kcp-cluster-2`.

   ![Screenshot from 2022-08-02 12-51-37](https://user-images.githubusercontent.com/73656840/182367915-5de8acef-4c77-4c09-a1b9-049c2605ce12.png)


Moreover, in the annotations we also have a status there for `kcp-cluster-2` and it has an IP address in it meaning that it has indeed synced to `kcp-cluster-2`. We will also find another annotation named "*deletion.internal.workload.kcp.dev/kcp-cluster-1*", which is code coming from the GLBC which is saying "Don't remove this work from `kcp-cluster-1` until the DNS has propagated."

For that reason we can also observe another annotation named "*finalizers.workload.kcp.dev/kcp-cluster-1: kuadrant.dev/glbc-migration*" which remains there because GLBC is saying to `kcp` "Don't get rid of this yet, as we're waiting for it to come up to another cluster before you remove it from `kcp-cluster-1`. After it has completely migrated and sufficient time has been allowed for DNS propagation, the finalizer for `kcp-cluster-1` will no longer be there and the workload will be deleted from `kcp-cluster-1`. Account for the DNS propagation time of the TTL (usually 60 seconds) * 2.

   ![Screenshot from 2022-08-02 12-49-21](https://user-images.githubusercontent.com/73656840/182368360-0bb65282-1751-44ea-a9da-7cfbe508e084.png)


We will notice that in the tab where we are curling the domain, we will always be getting a response back because the graceful migration is active. Meaning, even after the workload has been migrated, and the DNS record is updated, we will keep receiving a response without any interruption even after `kcp-cluster-1` has been deleted. This can be observed in our curl:

   ![Screenshot from 2022-08-02 12-55-24](https://user-images.githubusercontent.com/73656840/182368597-1ec0ade2-9849-4414-849f-ac342680d11b.png)

This shows that the workload has successfully migrated from `cluster-1` to `cluster-2` without any interruption.

<br>

#### Clean-up

After finishing with this tutorial, we can go back to our tab where we deployed the sample service, and press the enter key to reset and undo everything that was done from running the sample.

   ![Screenshot from 2022-08-02 13-04-27](https://user-images.githubusercontent.com/73656840/182370379-4e5af83b-6ad9-4b2d-9b11-8be18edff290.png)