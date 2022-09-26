# GLBC Webhooks

The GLBC leverages KCP ability to use admission webhooks. This document explains
what webhooks are in place, as well as how to configure GLBC to act as a 
webhook server and reconcile webhook configuration resources

## Webhook server

The GLBC has the ability to run as a webhook server alongside the different
reconcilers. This option is not included by default, so in order to run the
webhook server, the `--webhook-port` flag  or the `GLBC_WEBHOOKS_PORT` 
must be set (it is enabled by default)

## Webhook reconciliation

In addition to the webhook server. The GLBC has the ability to reconcile
`ValidatingWebhookConfiguration` resources that rely on the webhook server
to valididate `DomainVerification` resources. 

This resource is reconciled as part of the Ingress reconciliation loop, when the
Ingress being reconciled is [the Ingress that exposes the webhook server of the
controller](../../config/webhooks/ingress.yaml)

In order to enable the reconciliation of the webhook resource, the 
`DomainVerificationWebhookEnabled` option must be set to `true` (defaults to `false`),
either with the `--domain-verification-webhook-enabled` flag or the
`DOMAIN_VERIFICATION_WEBHOOK_ENABLED` environment variable.

## Deploying webhook resources

In order to make the webhook server reachable, a Service and an Ingress must be
deployed. These resources are included in the `config` folder. In order to deploy GLBC with these resources,
ensure the following sections are included:

* [config/manager/kustomization.yaml](../../config/manager/kustomization.yaml)
    ```yaml
    patchesStrategicMerge:
      - patches/set_port.yaml
    ```
* [config/default/kustomization.yaml](../../config/default/kustomization.yaml)
    ```yaml
    - ../webhooks
    ```