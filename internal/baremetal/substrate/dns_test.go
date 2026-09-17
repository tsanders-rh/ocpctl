package substrate

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveZoneID_Explicit(t *testing.T) {
	id, err := resolveZoneID(context.Background(), &fakeRoute53{}, "Z123ABC", "anything.example.com")
	require.NoError(t, err)
	assert.Equal(t, "Z123ABC", id)
}

func TestResolveZoneID_WalksUp(t *testing.T) {
	r53 := &fakeRoute53{
		listZones: func(in *route53.ListHostedZonesByNameInput) (*route53.ListHostedZonesByNameOutput, error) {
			// No zone for sub.example.com; a zone exists for example.com.
			if aws.ToString(in.DNSName) == "example.com." {
				return &route53.ListHostedZonesByNameOutput{HostedZones: []r53types.HostedZone{
					{Id: aws.String("/hostedzone/ZEXAMPLE"), Name: aws.String("example.com.")},
				}}, nil
			}
			return &route53.ListHostedZonesByNameOutput{}, nil
		},
	}
	id, err := resolveZoneID(context.Background(), r53, "", "sub.example.com")
	require.NoError(t, err)
	assert.Equal(t, "ZEXAMPLE", id) // "/hostedzone/" prefix stripped
}

func TestResolveZoneID_NotFound(t *testing.T) {
	_, err := resolveZoneID(context.Background(), &fakeRoute53{}, "", "nozone.example.com")
	require.Error(t, err)
}
