package substrate

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func happyClients() (*fakeEC2, *fakeRoute53, *fakeSTS, clients) {
	e := &fakeEC2{
		describeVpcs: func(*ec2.DescribeVpcsInput) (*ec2.DescribeVpcsOutput, error) {
			return &ec2.DescribeVpcsOutput{Vpcs: []ec2types.Vpc{{VpcId: aws.String("vpc-1")}}}, nil
		},
		describeOfferings: func(*ec2.DescribeInstanceTypeOfferingsInput) (*ec2.DescribeInstanceTypeOfferingsOutput, error) {
			return &ec2.DescribeInstanceTypeOfferingsOutput{InstanceTypeOfferings: []ec2types.InstanceTypeOffering{{InstanceType: ec2types.InstanceType("m8i.12xlarge")}}}, nil
		},
		createSG: func(*ec2.CreateSecurityGroupInput) (*ec2.CreateSecurityGroupOutput, error) {
			return &ec2.CreateSecurityGroupOutput{GroupId: aws.String("sg-1")}, nil
		},
		describeImages: func(*ec2.DescribeImagesInput) (*ec2.DescribeImagesOutput, error) {
			return &ec2.DescribeImagesOutput{Images: []ec2types.Image{
				{ImageId: aws.String("ami-old"), CreationDate: aws.String("2026-01-01T00:00:00.000Z"), RootDeviceName: aws.String("/dev/sda1")},
				{ImageId: aws.String("ami-new"), CreationDate: aws.String("2026-06-01T00:00:00.000Z"), RootDeviceName: aws.String("/dev/sda1")},
			}}, nil
		},
		importKeyPair: func(in *ec2.ImportKeyPairInput) (*ec2.ImportKeyPairOutput, error) {
			return &ec2.ImportKeyPairOutput{KeyName: in.KeyName}, nil
		},
		runInstances: func(*ec2.RunInstancesInput) (*ec2.RunInstancesOutput, error) {
			return &ec2.RunInstancesOutput{Instances: []ec2types.Instance{{InstanceId: aws.String("i-1")}}}, nil
		},
		describeInstances: func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
			// Satisfy the running-waiter immediately.
			return &ec2.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{{
				InstanceId: aws.String("i-1"),
				State:      &ec2types.InstanceState{Name: ec2types.InstanceStateNameRunning},
			}}}}}, nil
		},
		allocateAddress: func(*ec2.AllocateAddressInput) (*ec2.AllocateAddressOutput, error) {
			return &ec2.AllocateAddressOutput{AllocationId: aws.String("eipalloc-1"), PublicIp: aws.String("198.51.100.9")}, nil
		},
	}
	r := &fakeRoute53{
		listZones: func(*route53.ListHostedZonesByNameInput) (*route53.ListHostedZonesByNameOutput, error) {
			return &route53.ListHostedZonesByNameOutput{HostedZones: []r53types.HostedZone{
				{Id: aws.String("/hostedzone/ZONE1"), Name: aws.String("example.com.")},
			}}, nil
		},
	}
	s := &fakeSTS{
		getIdentity: func(*sts.GetCallerIdentityInput) (*sts.GetCallerIdentityOutput, error) {
			return &sts.GetCallerIdentityOutput{Account: aws.String("111122223333")}, nil
		},
	}
	get := func(string) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("203.0.113.7\n"))}, nil
	}
	return e, r, s, clients{ec2: e, r53: r, sts: s, get: get}
}

func testSpec() LaunchSpec {
	return LaunchSpec{
		ClusterID: "id-1", ClusterName: "mycluster", Region: "us-east-1",
		BaseDomain: "example.com", CreatedAt: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC),
	}
}

func TestLaunch_HappyPath(t *testing.T) {
	e, r, _, c := happyClients()
	sub, err := launch(context.Background(), c, testSpec())
	require.NoError(t, err)

	assert.Equal(t, "i-1", sub.InstanceID)
	assert.Equal(t, "198.51.100.9:22", sub.Addr)
	assert.Equal(t, "fedora", sub.User)
	assert.Equal(t, "198.51.100.9", sub.EIP)
	assert.Equal(t, "eipalloc-1", sub.AllocID)
	assert.Equal(t, "sg-1", sub.SGID)
	assert.Equal(t, "mycluster-key", sub.KeyName)
	assert.Equal(t, "ZONE1", sub.ZoneID)
	assert.Equal(t, "api.mycluster.example.com", sub.APIFQDN)
	assert.Equal(t, "*.apps.mycluster.example.com", sub.AppsFQDN)
	require.NotNil(t, sub.Signer)

	// Newest AMI chosen.
	assert.Equal(t, "ami-new", aws.ToString(e.runInput.ImageId))
	// gp3 root volume at HostVolumeGB default.
	require.Len(t, e.runInput.BlockDeviceMappings, 1)
	assert.Equal(t, "/dev/sda1", aws.ToString(e.runInput.BlockDeviceMappings[0].DeviceName))
	assert.Equal(t, ec2types.VolumeTypeGp3, e.runInput.BlockDeviceMappings[0].Ebs.VolumeType)
	assert.Equal(t, int32(1000), aws.ToInt32(e.runInput.BlockDeviceMappings[0].Ebs.VolumeSize))
	// Nested virtualization requested.
	require.NotNil(t, e.runInput.CpuOptions)
	assert.Equal(t, ec2types.NestedVirtualizationSpecificationEnabled, e.runInput.CpuOptions.NestedVirtualization)
	// Instance tagged with provenance + Name.
	assert.True(t, hasTag(e.runInput.TagSpecifications, "Name", "mycluster"))
	assert.True(t, hasTag(e.runInput.TagSpecifications, "ManagedBy", "ocpctl"))

	// Keypair imported with a valid OpenSSH public key matching the returned signer.
	require.NotNil(t, e.importInput)
	parsed, _, _, _, perr := ssh.ParseAuthorizedKey(e.importInput.PublicKeyMaterial)
	require.NoError(t, perr)
	assert.Equal(t, parsed.Marshal(), sub.Signer.PublicKey().Marshal())

	// Base ingress (22/6443/443/80 from caller) + hairpin (6443/443/80 from EIP).
	assert.True(t, authorized(e.authorizeCalls, 22, "203.0.113.7/32"))
	assert.True(t, authorized(e.authorizeCalls, 6443, "203.0.113.7/32"))
	assert.True(t, authorized(e.authorizeCalls, 443, "203.0.113.7/32"))
	assert.True(t, authorized(e.authorizeCalls, 80, "203.0.113.7/32"))
	assert.True(t, authorized(e.authorizeCalls, 6443, "198.51.100.9/32"))
	assert.True(t, authorized(e.authorizeCalls, 443, "198.51.100.9/32"))
	assert.True(t, authorized(e.authorizeCalls, 80, "198.51.100.9/32"))
	// Hairpin does not open SSH to the EIP.
	assert.False(t, authorized(e.authorizeCalls, 22, "198.51.100.9/32"))

	// DNS UPSERT of both records to the EIP.
	require.Len(t, r.changeCalls, 1)
	names := recordNamesAndValues(r.changeCalls[0])
	assert.Equal(t, "198.51.100.9", names["api.mycluster.example.com."])
	assert.Equal(t, "198.51.100.9", names["*.apps.mycluster.example.com."])
}

func TestLaunch_InstanceTypeNotOffered(t *testing.T) {
	e, r, s, c := happyClients()
	e.describeOfferings = func(*ec2.DescribeInstanceTypeOfferingsInput) (*ec2.DescribeInstanceTypeOfferingsOutput, error) {
		return &ec2.DescribeInstanceTypeOfferingsOutput{}, nil
	}
	_ = r
	_ = s
	_, err := launch(context.Background(), c, testSpec())
	require.Error(t, err)
}

// hasTag reports whether any TagSpecification carries the (key, value) tag.
func hasTag(specs []ec2types.TagSpecification, k, v string) bool {
	for _, ts := range specs {
		for _, tag := range ts.Tags {
			if aws.ToString(tag.Key) == k && aws.ToString(tag.Value) == v {
				return true
			}
		}
	}
	return false
}

// authorized scans every recorded ingress call for an IpPermission covering the
// given port from the given CIDR.
func authorized(calls []*ec2.AuthorizeSecurityGroupIngressInput, port int32, cidr string) bool {
	for _, call := range calls {
		for _, perm := range call.IpPermissions {
			if aws.ToInt32(perm.FromPort) != port || aws.ToInt32(perm.ToPort) != port {
				continue
			}
			for _, r := range perm.IpRanges {
				if aws.ToString(r.CidrIp) == cidr {
					return true
				}
			}
		}
	}
	return false
}

// recordNamesAndValues maps each UPSERT record name to its first record value.
func recordNamesAndValues(in *route53.ChangeResourceRecordSetsInput) map[string]string {
	out := map[string]string{}
	for _, ch := range in.ChangeBatch.Changes {
		rrs := ch.ResourceRecordSet
		if rrs == nil || rrs.Name == nil {
			continue
		}
		val := ""
		if len(rrs.ResourceRecords) > 0 {
			val = aws.ToString(rrs.ResourceRecords[0].Value)
		}
		out[aws.ToString(rrs.Name)] = val
	}
	return out
}
