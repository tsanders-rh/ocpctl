// Package substrate manages the AWS substrate for a bare-metal cluster: it
// launches the nested-virt EC2 host (keypair, security group, AMI, instance,
// EIP, Route53) and tears it all down by tag.
package substrate

import (
	"context"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	octypes "github.com/tsanders-rh/ocpctl/pkg/types"
	"golang.org/x/crypto/ssh"
)

const (
	defaultInstanceType  = "m8i.12xlarge"
	defaultAMIOwner      = "125523088429" // Fedora Project
	defaultFedoraRelease = "44"
	defaultHostVolumeGB  = 1000
	defaultUser          = "fedora"
)

// LaunchSpec is built by the caller from the profile's BareMetalConfig plus the
// shared Region/BaseDomain blocks.
type LaunchSpec struct {
	ClusterID     string
	ClusterName   string
	Region        string
	BaseDomain    string
	ZoneID        string
	InstanceType  string
	AMIOwner      string
	FedoraRelease string
	AMIOverride   string
	HostVolumeGB  int
	User          string
	AllowCIDRs    []string
	CreatedAt     time.Time
}

// TeardownSpec identifies a cluster's substrate for removal.
type TeardownSpec struct {
	Region      string
	ClusterName string
	BaseDomain  string
	ZoneID      string
}

// Substrate is what piece 2 (internal/baremetal/host) consumes.
type Substrate struct {
	InstanceID    string
	Addr          string // "<eip>:22"
	User          string
	Signer        ssh.Signer
	PrivateKeyPEM []byte // ephemeral private key (OpenSSH PEM); for host->node ssh + debugging
	EIP           string
	AllocID       string
	SGID          string
	KeyName       string
	ZoneID        string
	APIFQDN       string
	AppsFQDN      string
}

// ec2API is the subset of *ec2.Client this package uses.
type ec2API interface {
	DescribeInstanceTypeOfferings(context.Context, *ec2.DescribeInstanceTypeOfferingsInput, ...func(*ec2.Options)) (*ec2.DescribeInstanceTypeOfferingsOutput, error)
	DescribeVpcs(context.Context, *ec2.DescribeVpcsInput, ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error)
	CreateSecurityGroup(context.Context, *ec2.CreateSecurityGroupInput, ...func(*ec2.Options)) (*ec2.CreateSecurityGroupOutput, error)
	AuthorizeSecurityGroupIngress(context.Context, *ec2.AuthorizeSecurityGroupIngressInput, ...func(*ec2.Options)) (*ec2.AuthorizeSecurityGroupIngressOutput, error)
	DescribeSecurityGroups(context.Context, *ec2.DescribeSecurityGroupsInput, ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error)
	DeleteSecurityGroup(context.Context, *ec2.DeleteSecurityGroupInput, ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error)
	ImportKeyPair(context.Context, *ec2.ImportKeyPairInput, ...func(*ec2.Options)) (*ec2.ImportKeyPairOutput, error)
	DescribeKeyPairs(context.Context, *ec2.DescribeKeyPairsInput, ...func(*ec2.Options)) (*ec2.DescribeKeyPairsOutput, error)
	DeleteKeyPair(context.Context, *ec2.DeleteKeyPairInput, ...func(*ec2.Options)) (*ec2.DeleteKeyPairOutput, error)
	DescribeImages(context.Context, *ec2.DescribeImagesInput, ...func(*ec2.Options)) (*ec2.DescribeImagesOutput, error)
	RunInstances(context.Context, *ec2.RunInstancesInput, ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error)
	DescribeInstances(context.Context, *ec2.DescribeInstancesInput, ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	TerminateInstances(context.Context, *ec2.TerminateInstancesInput, ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error)
	AllocateAddress(context.Context, *ec2.AllocateAddressInput, ...func(*ec2.Options)) (*ec2.AllocateAddressOutput, error)
	AssociateAddress(context.Context, *ec2.AssociateAddressInput, ...func(*ec2.Options)) (*ec2.AssociateAddressOutput, error)
	DescribeAddresses(context.Context, *ec2.DescribeAddressesInput, ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error)
	ReleaseAddress(context.Context, *ec2.ReleaseAddressInput, ...func(*ec2.Options)) (*ec2.ReleaseAddressOutput, error)
}

// route53API is the subset of *route53.Client this package uses.
type route53API interface {
	ListHostedZonesByName(context.Context, *route53.ListHostedZonesByNameInput, ...func(*route53.Options)) (*route53.ListHostedZonesByNameOutput, error)
	ListResourceRecordSets(context.Context, *route53.ListResourceRecordSetsInput, ...func(*route53.Options)) (*route53.ListResourceRecordSetsOutput, error)
	ChangeResourceRecordSets(context.Context, *route53.ChangeResourceRecordSetsInput, ...func(*route53.Options)) (*route53.ChangeResourceRecordSetsOutput, error)
}

// stsAPI is the subset of *sts.Client this package uses.
type stsAPI interface {
	GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

func applyDefaults(s *LaunchSpec) {
	if s.InstanceType == "" {
		s.InstanceType = defaultInstanceType
	}
	if s.AMIOwner == "" {
		s.AMIOwner = defaultAMIOwner
	}
	if s.FedoraRelease == "" {
		s.FedoraRelease = defaultFedoraRelease
	}
	if s.HostVolumeGB == 0 {
		s.HostVolumeGB = defaultHostVolumeGB
	}
	if s.User == "" {
		s.User = defaultUser
	}
}

func buildTags(s LaunchSpec) map[string]string {
	tags := octypes.ProvenanceTags(s.ClusterID, s.ClusterName, s.CreatedAt)
	tags["ManagedBy"] = "ocpctl"
	tags["ClusterName"] = s.ClusterName
	tags["Name"] = s.ClusterName
	return tags
}

func apiFQDN(cluster, base string) string {
	return "api." + cluster + "." + strings.TrimSuffix(base, ".")
}
func appsFQDN(cluster, base string) string {
	return "*.apps." + cluster + "." + strings.TrimSuffix(base, ".")
}
func keypairName(cluster string) string { return cluster + "-key" }
func sgName(cluster string) string      { return cluster + "-sg" }
