package substrate

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
)

// resolveZoneID returns the hosted-zone id for baseDomain. An explicit zone id is
// returned as-is. Otherwise it walks up from baseDomain to the closest parent
// zone (e.g. sub.example.com -> example.com), matching rhwa-lab's resolve_r53_zone.
func resolveZoneID(ctx context.Context, r53 route53API, explicitZoneID, baseDomain string) (string, error) {
	if explicitZoneID != "" {
		return explicitZoneID, nil
	}
	try := strings.TrimSuffix(baseDomain, ".")
	for strings.Contains(try, ".") {
		name := try + "."
		out, err := r53.ListHostedZonesByName(ctx, &route53.ListHostedZonesByNameInput{DNSName: aws.String(name)})
		if err != nil {
			return "", fmt.Errorf("list hosted zones for %s: %w", name, err)
		}
		for _, z := range out.HostedZones {
			if aws.ToString(z.Name) == name {
				return strings.TrimPrefix(aws.ToString(z.Id), "/hostedzone/"), nil
			}
		}
		try = try[strings.Index(try, ".")+1:] // drop leftmost label
	}
	return "", fmt.Errorf("no Route53 hosted zone found for %s or any parent domain", baseDomain)
}
