# Health Check reconciliation

The GLB Controller has the ability to reconcile health checks for the DNS records
that it maintains. Currently it supports reconciliation of Route 53 health checks,
where a health check is created for each Route 53 record. For example, given the
following endpoints in the `DNSRecord` CR:

```yaml
endpoints:
  - dnsName: c92nein5runjgpioik5g.sf.hcpapps.net
    providerSpecific:
    - name: aws/weight
      value: "100"
    recordTTL: 60
    recordType: A
    setIdentifier: 3.230.19.134
    targets:
    - 3.230.19.134
  - dnsName: c92nein5runjgpioik5g.sf.hcpapps.net
    providerSpecific:
    - name: aws/weight
      value: "100"
    recordTTL: 60
    recordType: A
    setIdentifier: 52.1.106.34
    targets:
    - 52.1.106.34
  - dnsName: c92nein5runjgpioik5g.sf.hcpapps.net
    providerSpecific:
    - name: aws/weight
      value: "100"
    recordTTL: 60
    recordType: A
    setIdentifier: 34.148.111.106
    targets:
    - 34.148.111.106
```

3 health checks will be created pointing to the endpoint address (the `setIdentifier` value)
using the `dnsName` value as the `Host` header.

In order to enable health check reconciliation. Add the `kuadrant.experimental/health-endpoint`
annotation to the Ingress. The value is the path of the health endpoint of the service.

Other configuration values can be set as annotations:

| Annotation | Description | Default value |
| ---------- | ----------- | ------------- |
| `kuadrant.experimental/health-endpoint` |  Path of the health endpoint for the target service | _Required_ |
| `kuadrant.experimental/health-port` |  Port where the health checks will be performed | 80 |
| `kuadrant.experimental/health-protocol` |  Protocol to be used by the health checks to request the endpoint | `HTTP` |
| `kuadrant.experimental/health-failure-threshold` | Number of consecutive health checks that the endpoint can fail in order to be considered unhealthy | 3 |

## Failover

> ⚠️ Note that all endpoints must be accessible to the AWS Health Checkers. If
> they are not, they will be marked as unhealthy and unavailable to DNS requests


The health checks will be associated to each Route 53 weighted record. In the event
of an unhealthy endpoint, Route 53 will stop serving that address to DNS clients