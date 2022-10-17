package dns

import (
	"fmt"

	dnsAWS "github.com/kuadrant/kcp-glbc/pkg/dns/aws"
)

func DNSProvider(dnsProviderName string) (Provider, error) {
	var dnsProvider Provider
	var dnsError error
	switch dnsProviderName {
	case "aws":
		dnsProvider, dnsError = newAWSDNSProvider()
	default:
		dnsProvider = &FakeProvider{}
	}
	return dnsProvider, dnsError
}

func newAWSDNSProvider() (Provider, error) {
	var dnsProvider Provider
	provider, err := dnsAWS.NewProvider(dnsAWS.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS DNS manager: %v", err)
	}
	dnsProvider = provider

	return dnsProvider, nil
}
