package traffic

import (
	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	"github.com/kuadrant/kcp-glbc/pkg/dns"
	"github.com/kuadrant/kcp-glbc/test/support"

	"github.com/kuadrant/kcp-glbc/pkg/traffic"
)

func Endpoints(t support.Test, ingress traffic.Interface, res dns.HostResolver) []types.GomegaMatcher {
	host := ingress.GetAnnotations()[traffic.ANNOTATION_HCG_HOST]
	targets, err := ingress.GetDNSTargets()
	t.Expect(err).NotTo(gomega.HaveOccurred())
	matchers := []types.GomegaMatcher{}
	for _, target := range targets {
		matchers = append(matchers, support.MatchFieldsP(IgnoreExtras,
			Fields{
				"DNSName":          Equal(host),
				"Targets":          ConsistOf(target.Value),
				"RecordType":       Equal("A"),
				"RecordTTL":        Equal(kuadrantv1.TTL(60)),
				"SetIdentifier":    Equal(target.Value),
				"ProviderSpecific": ConsistOf(kuadrantv1.ProviderSpecific{{Name: "aws/weight", Value: "120"}}),
			}))

	}
	return matchers

}
