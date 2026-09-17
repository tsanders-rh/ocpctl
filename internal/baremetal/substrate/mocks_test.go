package substrate

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

var (
	_ ec2API     = (*fakeEC2)(nil)
	_ route53API = (*fakeRoute53)(nil)
	_ stsAPI     = (*fakeSTS)(nil)
)

// fakeEC2 implements ec2API. Each method calls its matching func field when set,
// otherwise returns a minimal success output. Inputs later tests assert on are
// recorded.
type fakeEC2 struct {
	describeOfferings func(*ec2.DescribeInstanceTypeOfferingsInput) (*ec2.DescribeInstanceTypeOfferingsOutput, error)
	describeVpcs      func(*ec2.DescribeVpcsInput) (*ec2.DescribeVpcsOutput, error)
	createSG          func(*ec2.CreateSecurityGroupInput) (*ec2.CreateSecurityGroupOutput, error)
	authorizeIngress  func(*ec2.AuthorizeSecurityGroupIngressInput) (*ec2.AuthorizeSecurityGroupIngressOutput, error)
	describeSGs       func(*ec2.DescribeSecurityGroupsInput) (*ec2.DescribeSecurityGroupsOutput, error)
	deleteSG          func(*ec2.DeleteSecurityGroupInput) (*ec2.DeleteSecurityGroupOutput, error)
	importKeyPair     func(*ec2.ImportKeyPairInput) (*ec2.ImportKeyPairOutput, error)
	describeKeyPairs  func(*ec2.DescribeKeyPairsInput) (*ec2.DescribeKeyPairsOutput, error)
	deleteKeyPair     func(*ec2.DeleteKeyPairInput) (*ec2.DeleteKeyPairOutput, error)
	describeImages    func(*ec2.DescribeImagesInput) (*ec2.DescribeImagesOutput, error)
	runInstances      func(*ec2.RunInstancesInput) (*ec2.RunInstancesOutput, error)
	describeInstances func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error)
	terminate         func(*ec2.TerminateInstancesInput) (*ec2.TerminateInstancesOutput, error)
	allocateAddress   func(*ec2.AllocateAddressInput) (*ec2.AllocateAddressOutput, error)
	associateAddress  func(*ec2.AssociateAddressInput) (*ec2.AssociateAddressOutput, error)
	describeAddresses func(*ec2.DescribeAddressesInput) (*ec2.DescribeAddressesOutput, error)
	releaseAddress    func(*ec2.ReleaseAddressInput) (*ec2.ReleaseAddressOutput, error)

	authorizeCalls []*ec2.AuthorizeSecurityGroupIngressInput
	runInput       *ec2.RunInstancesInput
	importInput    *ec2.ImportKeyPairInput
	deleteSGCalls  int
}

func (f *fakeEC2) DescribeInstanceTypeOfferings(_ context.Context, in *ec2.DescribeInstanceTypeOfferingsInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstanceTypeOfferingsOutput, error) {
	if f.describeOfferings != nil {
		return f.describeOfferings(in)
	}
	return &ec2.DescribeInstanceTypeOfferingsOutput{}, nil
}

func (f *fakeEC2) DescribeVpcs(_ context.Context, in *ec2.DescribeVpcsInput, _ ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
	if f.describeVpcs != nil {
		return f.describeVpcs(in)
	}
	return &ec2.DescribeVpcsOutput{}, nil
}

func (f *fakeEC2) CreateSecurityGroup(_ context.Context, in *ec2.CreateSecurityGroupInput, _ ...func(*ec2.Options)) (*ec2.CreateSecurityGroupOutput, error) {
	if f.createSG != nil {
		return f.createSG(in)
	}
	return &ec2.CreateSecurityGroupOutput{}, nil
}

func (f *fakeEC2) AuthorizeSecurityGroupIngress(_ context.Context, in *ec2.AuthorizeSecurityGroupIngressInput, _ ...func(*ec2.Options)) (*ec2.AuthorizeSecurityGroupIngressOutput, error) {
	f.authorizeCalls = append(f.authorizeCalls, in)
	if f.authorizeIngress != nil {
		return f.authorizeIngress(in)
	}
	return &ec2.AuthorizeSecurityGroupIngressOutput{}, nil
}

func (f *fakeEC2) DescribeSecurityGroups(_ context.Context, in *ec2.DescribeSecurityGroupsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
	if f.describeSGs != nil {
		return f.describeSGs(in)
	}
	return &ec2.DescribeSecurityGroupsOutput{}, nil
}

func (f *fakeEC2) DeleteSecurityGroup(_ context.Context, in *ec2.DeleteSecurityGroupInput, _ ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
	f.deleteSGCalls++
	if f.deleteSG != nil {
		return f.deleteSG(in)
	}
	return &ec2.DeleteSecurityGroupOutput{}, nil
}

func (f *fakeEC2) ImportKeyPair(_ context.Context, in *ec2.ImportKeyPairInput, _ ...func(*ec2.Options)) (*ec2.ImportKeyPairOutput, error) {
	f.importInput = in
	if f.importKeyPair != nil {
		return f.importKeyPair(in)
	}
	return &ec2.ImportKeyPairOutput{KeyName: in.KeyName}, nil
}

func (f *fakeEC2) DescribeKeyPairs(_ context.Context, in *ec2.DescribeKeyPairsInput, _ ...func(*ec2.Options)) (*ec2.DescribeKeyPairsOutput, error) {
	if f.describeKeyPairs != nil {
		return f.describeKeyPairs(in)
	}
	return &ec2.DescribeKeyPairsOutput{}, nil
}

func (f *fakeEC2) DeleteKeyPair(_ context.Context, in *ec2.DeleteKeyPairInput, _ ...func(*ec2.Options)) (*ec2.DeleteKeyPairOutput, error) {
	if f.deleteKeyPair != nil {
		return f.deleteKeyPair(in)
	}
	return &ec2.DeleteKeyPairOutput{}, nil
}

func (f *fakeEC2) DescribeImages(_ context.Context, in *ec2.DescribeImagesInput, _ ...func(*ec2.Options)) (*ec2.DescribeImagesOutput, error) {
	if f.describeImages != nil {
		return f.describeImages(in)
	}
	return &ec2.DescribeImagesOutput{}, nil
}

func (f *fakeEC2) RunInstances(_ context.Context, in *ec2.RunInstancesInput, _ ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error) {
	f.runInput = in
	if f.runInstances != nil {
		return f.runInstances(in)
	}
	return &ec2.RunInstancesOutput{}, nil
}

func (f *fakeEC2) DescribeInstances(_ context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	if f.describeInstances != nil {
		return f.describeInstances(in)
	}
	return &ec2.DescribeInstancesOutput{}, nil
}

func (f *fakeEC2) TerminateInstances(_ context.Context, in *ec2.TerminateInstancesInput, _ ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error) {
	if f.terminate != nil {
		return f.terminate(in)
	}
	return &ec2.TerminateInstancesOutput{}, nil
}

func (f *fakeEC2) AllocateAddress(_ context.Context, in *ec2.AllocateAddressInput, _ ...func(*ec2.Options)) (*ec2.AllocateAddressOutput, error) {
	if f.allocateAddress != nil {
		return f.allocateAddress(in)
	}
	return &ec2.AllocateAddressOutput{}, nil
}

func (f *fakeEC2) AssociateAddress(_ context.Context, in *ec2.AssociateAddressInput, _ ...func(*ec2.Options)) (*ec2.AssociateAddressOutput, error) {
	if f.associateAddress != nil {
		return f.associateAddress(in)
	}
	return &ec2.AssociateAddressOutput{}, nil
}

func (f *fakeEC2) DescribeAddresses(_ context.Context, in *ec2.DescribeAddressesInput, _ ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
	if f.describeAddresses != nil {
		return f.describeAddresses(in)
	}
	return &ec2.DescribeAddressesOutput{}, nil
}

func (f *fakeEC2) ReleaseAddress(_ context.Context, in *ec2.ReleaseAddressInput, _ ...func(*ec2.Options)) (*ec2.ReleaseAddressOutput, error) {
	if f.releaseAddress != nil {
		return f.releaseAddress(in)
	}
	return &ec2.ReleaseAddressOutput{}, nil
}

// fakeRoute53 implements route53API.
type fakeRoute53 struct {
	listZones     func(*route53.ListHostedZonesByNameInput) (*route53.ListHostedZonesByNameOutput, error)
	listRecords   func(*route53.ListResourceRecordSetsInput) (*route53.ListResourceRecordSetsOutput, error)
	changeRecords func(*route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error)

	changeCalls []*route53.ChangeResourceRecordSetsInput
}

func (f *fakeRoute53) ListHostedZonesByName(_ context.Context, in *route53.ListHostedZonesByNameInput, _ ...func(*route53.Options)) (*route53.ListHostedZonesByNameOutput, error) {
	if f.listZones != nil {
		return f.listZones(in)
	}
	return &route53.ListHostedZonesByNameOutput{}, nil
}

func (f *fakeRoute53) ListResourceRecordSets(_ context.Context, in *route53.ListResourceRecordSetsInput, _ ...func(*route53.Options)) (*route53.ListResourceRecordSetsOutput, error) {
	if f.listRecords != nil {
		return f.listRecords(in)
	}
	return &route53.ListResourceRecordSetsOutput{}, nil
}

func (f *fakeRoute53) ChangeResourceRecordSets(_ context.Context, in *route53.ChangeResourceRecordSetsInput, _ ...func(*route53.Options)) (*route53.ChangeResourceRecordSetsOutput, error) {
	f.changeCalls = append(f.changeCalls, in)
	if f.changeRecords != nil {
		return f.changeRecords(in)
	}
	return &route53.ChangeResourceRecordSetsOutput{}, nil
}

// fakeSTS implements stsAPI.
type fakeSTS struct {
	getIdentity func(*sts.GetCallerIdentityInput) (*sts.GetCallerIdentityOutput, error)
}

func (f *fakeSTS) GetCallerIdentity(_ context.Context, in *sts.GetCallerIdentityInput, _ ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	if f.getIdentity != nil {
		return f.getIdentity(in)
	}
	return &sts.GetCallerIdentityOutput{}, nil
}
