// Code generated by client-gen. DO NOT EDIT.

package v1

import (
	v1 "github.com/kuadrant/kcp-ingress/pkg/apis/kuadrant/v1"
	"github.com/kuadrant/kcp-ingress/pkg/client/kuadrant/clientset/versioned/scheme"
	rest "k8s.io/client-go/rest"
)

type KuadrantV1Interface interface {
	RESTClient() rest.Interface
	DNSRecordsGetter
}

// KuadrantV1Client is used to interact with features provided by the kuadrant.dev group.
type KuadrantV1Client struct {
	restClient rest.Interface
}

func (c *KuadrantV1Client) DNSRecords(namespace string) DNSRecordInterface {
	return newDNSRecords(c, namespace)
}

// NewForConfig creates a new KuadrantV1Client for the given config.
func NewForConfig(c *rest.Config) (*KuadrantV1Client, error) {
	config := *c
	if err := setConfigDefaults(&config); err != nil {
		return nil, err
	}
	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}
	return &KuadrantV1Client{client}, nil
}

// NewForConfigOrDie creates a new KuadrantV1Client for the given config and
// panics if there is an error in the config.
func NewForConfigOrDie(c *rest.Config) *KuadrantV1Client {
	client, err := NewForConfig(c)
	if err != nil {
		panic(err)
	}
	return client
}

// New creates a new KuadrantV1Client for the given RESTClient.
func New(c rest.Interface) *KuadrantV1Client {
	return &KuadrantV1Client{c}
}

func setConfigDefaults(config *rest.Config) error {
	gv := v1.SchemeGroupVersion
	config.GroupVersion = &gv
	config.APIPath = "/apis"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()

	if config.UserAgent == "" {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}

	return nil
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *KuadrantV1Client) RESTClient() rest.Interface {
	if c == nil {
		return nil
	}
	return c.restClient
}
