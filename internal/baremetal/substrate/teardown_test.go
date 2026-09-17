package substrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTeardown_FullPath(t *testing.T) {
	terminated := false
	released := false
	deletedKey := false
	sgDeleteAttempts := 0

	e := &fakeEC2{
		describeInstances: func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
			st := ec2types.InstanceStateNameTerminated
			if !terminated {
				st = ec2types.InstanceStateNameRunning
			}
			return &ec2.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{{
				InstanceId: aws.String("i-1"), State: &ec2types.InstanceState{Name: st},
			}}}}}, nil
		},
		describeAddresses: func(*ec2.DescribeAddressesInput) (*ec2.DescribeAddressesOutput, error) {
			return &ec2.DescribeAddressesOutput{Addresses: []ec2types.Address{{AllocationId: aws.String("eipalloc-1"), PublicIp: aws.String("198.51.100.9")}}}, nil
		},
		terminate: func(*ec2.TerminateInstancesInput) (*ec2.TerminateInstancesOutput, error) {
			terminated = true
			return &ec2.TerminateInstancesOutput{}, nil
		},
		releaseAddress: func(*ec2.ReleaseAddressInput) (*ec2.ReleaseAddressOutput, error) {
			released = true
			return &ec2.ReleaseAddressOutput{}, nil
		},
		describeSGs: func(*ec2.DescribeSecurityGroupsInput) (*ec2.DescribeSecurityGroupsOutput, error) {
			return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: []ec2types.SecurityGroup{{GroupId: aws.String("sg-1")}}}, nil
		},
		deleteSG: func(*ec2.DeleteSecurityGroupInput) (*ec2.DeleteSecurityGroupOutput, error) {
			sgDeleteAttempts++
			if sgDeleteAttempts < 2 { // first attempt fails (deps linger)
				return nil, errors.New("DependencyViolation")
			}
			return &ec2.DeleteSecurityGroupOutput{}, nil
		},
		describeKeyPairs: func(*ec2.DescribeKeyPairsInput) (*ec2.DescribeKeyPairsOutput, error) {
			return &ec2.DescribeKeyPairsOutput{KeyPairs: []ec2types.KeyPairInfo{{KeyName: aws.String("mycluster-key")}}}, nil
		},
		deleteKeyPair: func(*ec2.DeleteKeyPairInput) (*ec2.DeleteKeyPairOutput, error) {
			deletedKey = true
			return &ec2.DeleteKeyPairOutput{}, nil
		},
	}
	r := &fakeRoute53{
		listZones: func(*route53.ListHostedZonesByNameInput) (*route53.ListHostedZonesByNameOutput, error) {
			return &route53.ListHostedZonesByNameOutput{HostedZones: []r53types.HostedZone{{Id: aws.String("/hostedzone/ZONE1"), Name: aws.String("example.com.")}}}, nil
		},
		listRecords: func(*route53.ListResourceRecordSetsInput) (*route53.ListResourceRecordSetsOutput, error) {
			return &route53.ListResourceRecordSetsOutput{ResourceRecordSets: []r53types.ResourceRecordSet{
				{Name: aws.String("api.mycluster.example.com."), Type: r53types.RRTypeA, TTL: aws.Int64(60), ResourceRecords: []r53types.ResourceRecord{{Value: aws.String("198.51.100.9")}}},
				{Name: aws.String("\\052.apps.mycluster.example.com."), Type: r53types.RRTypeA, TTL: aws.Int64(60), ResourceRecords: []r53types.ResourceRecord{{Value: aws.String("198.51.100.9")}}},
			}}, nil
		},
	}
	c := clients{ec2: e, r53: r}
	err := teardown(context.Background(), c, func(time.Duration) {}, TeardownSpec{
		Region: "us-east-1", ClusterName: "mycluster", BaseDomain: "example.com",
	})
	require.NoError(t, err)
	assert.True(t, terminated)
	assert.True(t, released)
	assert.True(t, deletedKey)
	assert.GreaterOrEqual(t, sgDeleteAttempts, 2, "SG delete retried past first failure")
	require.Len(t, r.changeCalls, 1)
	assert.Equal(t, r53types.ChangeActionDelete, r.changeCalls[0].ChangeBatch.Changes[0].Action)
	// Both A records queued for deletion.
	assert.Len(t, r.changeCalls[0].ChangeBatch.Changes, 2)
}

func TestTeardown_NothingFound(t *testing.T) {
	// Empty describes everywhere => no error (idempotent re-run).
	c := clients{ec2: &fakeEC2{}, r53: &fakeRoute53{}}
	err := teardown(context.Background(), c, func(time.Duration) {}, TeardownSpec{
		Region: "us-east-1", ClusterName: "gone", BaseDomain: "example.com",
	})
	require.NoError(t, err)
}
