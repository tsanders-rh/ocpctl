package substrate

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

const instanceRunningTimeout = 10 * time.Minute

var (
	basePorts    = []int32{22, 6443, 443, 80}
	hairpinPorts = []int32{6443, 443, 80}
)

// clients bundles the AWS interfaces and the caller-IP detector so tests inject fakes.
type clients struct {
	ec2 ec2API
	r53 route53API
	sts stsAPI
	get httpGetter
}

// Launch provisions the nested-virt EC2 host substrate for a bare-metal cluster:
// keypair, security group, Fedora host instance, Elastic IP, and Route53 records.
// It returns the address, cloud user, and ssh.Signer that host provisioning consumes.
func Launch(ctx context.Context, spec LaunchSpec) (*Substrate, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(spec.Region))
	if err != nil {
		return nil, fmt.Errorf("substrate load aws config: %w", err)
	}
	c := clients{
		ec2: ec2.NewFromConfig(cfg),
		r53: route53.NewFromConfig(cfg),
		sts: sts.NewFromConfig(cfg),
		get: defaultHTTPGet,
	}
	return launch(ctx, c, spec)
}

func launch(ctx context.Context, c clients, spec LaunchSpec) (*Substrate, error) {
	applyDefaults(&spec)

	zoneID, err := preflight(ctx, c, spec)
	if err != nil {
		return nil, err
	}

	pubMaterial, signer, privPEM, err := generateKeypair()
	if err != nil {
		return nil, fmt.Errorf("substrate keypair: %w", err)
	}
	keyName := keypairName(spec.ClusterName)
	tags := buildTags(spec)
	importKey := func() error {
		_, e := c.ec2.ImportKeyPair(ctx, &ec2.ImportKeyPairInput{
			KeyName:           aws.String(keyName),
			PublicKeyMaterial: pubMaterial,
			TagSpecifications: tagSpecs(ec2types.ResourceTypeKeyPair, tags),
		})
		return e
	}
	if err := importKey(); err != nil {
		// A stale key from a prior attempt is useless (each launch mints a fresh
		// ephemeral key), so replace it rather than failing the whole launch.
		if !isDuplicateKeyPair(err) {
			return nil, fmt.Errorf("substrate import keypair: %w", err)
		}
		if _, derr := c.ec2.DeleteKeyPair(ctx, &ec2.DeleteKeyPairInput{KeyName: aws.String(keyName)}); derr != nil {
			return nil, fmt.Errorf("substrate delete stale keypair: %w", derr)
		}
		if err := importKey(); err != nil {
			return nil, fmt.Errorf("substrate import keypair (after replacing stale): %w", err)
		}
	}

	sgID, err := ensureSecurityGroup(ctx, c, spec, tags)
	if err != nil {
		return nil, err
	}

	imageID, rootDev, err := resolveAMI(ctx, c, spec)
	if err != nil {
		return nil, err
	}

	instanceID, err := runHost(ctx, c, spec, imageID, rootDev, keyName, sgID, tags)
	if err != nil {
		return nil, err
	}

	eip, allocID, err := allocateEIP(ctx, c, instanceID, tags)
	if err != nil {
		return nil, err
	}

	if err := openHairpin(ctx, c, sgID, eip); err != nil {
		return nil, err
	}

	if err := upsertDNS(ctx, c, spec, zoneID, eip); err != nil {
		return nil, err
	}

	return &Substrate{
		InstanceID:    instanceID,
		Addr:          eip + ":22",
		User:          spec.User,
		Signer:        signer,
		PrivateKeyPEM: privPEM,
		EIP:           eip,
		AllocID:       allocID,
		SGID:          sgID,
		KeyName:       keyName,
		ZoneID:        zoneID,
		APIFQDN:       apiFQDN(spec.ClusterName, spec.BaseDomain),
		AppsFQDN:      appsFQDN(spec.ClusterName, spec.BaseDomain),
	}, nil
}

// preflight verifies credentials, resolves the hosted zone, and confirms the
// instance type is offered in the region.
func preflight(ctx context.Context, c clients, spec LaunchSpec) (string, error) {
	if _, err := c.sts.GetCallerIdentity(ctx, nil); err != nil {
		return "", fmt.Errorf("substrate caller identity: %w", err)
	}
	zoneID, err := resolveZoneID(ctx, c.r53, spec.ZoneID, spec.BaseDomain)
	if err != nil {
		return "", fmt.Errorf("substrate resolve zone: %w", err)
	}
	out, err := c.ec2.DescribeInstanceTypeOfferings(ctx, &ec2.DescribeInstanceTypeOfferingsInput{
		Filters: []ec2types.Filter{{Name: aws.String("instance-type"), Values: []string{spec.InstanceType}}},
	})
	if err != nil {
		return "", fmt.Errorf("substrate instance-type offerings: %w", err)
	}
	if len(out.InstanceTypeOfferings) == 0 {
		return "", fmt.Errorf("substrate instance type %s not offered in region %s", spec.InstanceType, spec.Region)
	}
	return zoneID, nil
}

func ensureSecurityGroup(ctx context.Context, c clients, spec LaunchSpec, tags map[string]string) (string, error) {
	vpcs, err := c.ec2.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		Filters: []ec2types.Filter{{Name: aws.String("isDefault"), Values: []string{"true"}}},
	})
	if err != nil {
		return "", fmt.Errorf("substrate describe default vpc: %w", err)
	}
	if len(vpcs.Vpcs) == 0 {
		return "", fmt.Errorf("substrate no default VPC in region %s", spec.Region)
	}
	sg, err := c.ec2.CreateSecurityGroup(ctx, &ec2.CreateSecurityGroupInput{
		GroupName:         aws.String(sgName(spec.ClusterName)),
		Description:       aws.String("ocpctl bare-metal host " + spec.ClusterName),
		VpcId:             vpcs.Vpcs[0].VpcId,
		TagSpecifications: tagSpecs(ec2types.ResourceTypeSecurityGroup, tags),
	})
	if err != nil {
		return "", fmt.Errorf("substrate create security group: %w", err)
	}
	sgID := aws.ToString(sg.GroupId)

	cidrs := spec.AllowCIDRs
	if len(cidrs) == 0 {
		cidrs = detectCallerCIDRs(c.get)
	}
	for _, cidr := range cidrs {
		if err := authorizePorts(ctx, c, sgID, basePorts, cidr); err != nil {
			return "", fmt.Errorf("substrate authorize ingress: %w", err)
		}
	}
	return sgID, nil
}

func resolveAMI(ctx context.Context, c clients, spec LaunchSpec) (string, string, error) {
	if spec.AMIOverride != "" {
		out, err := c.ec2.DescribeImages(ctx, &ec2.DescribeImagesInput{ImageIds: []string{spec.AMIOverride}})
		if err != nil {
			return "", "", fmt.Errorf("substrate describe override AMI: %w", err)
		}
		if len(out.Images) == 0 {
			return "", "", fmt.Errorf("substrate override AMI %s not found", spec.AMIOverride)
		}
		return spec.AMIOverride, aws.ToString(out.Images[0].RootDeviceName), nil
	}
	out, err := c.ec2.DescribeImages(ctx, &ec2.DescribeImagesInput{
		Owners: []string{spec.AMIOwner},
		Filters: []ec2types.Filter{
			{Name: aws.String("name"), Values: []string{"Fedora-Cloud-Base-AmazonEC2*-" + spec.FedoraRelease + "-*"}},
			{Name: aws.String("architecture"), Values: []string{"x86_64"}},
			{Name: aws.String("state"), Values: []string{"available"}},
		},
	})
	if err != nil {
		return "", "", fmt.Errorf("substrate describe AMIs: %w", err)
	}
	if len(out.Images) == 0 {
		return "", "", fmt.Errorf("substrate no Fedora %s AMI found for owner %s", spec.FedoraRelease, spec.AMIOwner)
	}
	newest := out.Images[0]
	for _, img := range out.Images[1:] {
		if aws.ToString(img.CreationDate) > aws.ToString(newest.CreationDate) {
			newest = img
		}
	}
	return aws.ToString(newest.ImageId), aws.ToString(newest.RootDeviceName), nil
}

func runHost(ctx context.Context, c clients, spec LaunchSpec, imageID, rootDev, keyName, sgID string, tags map[string]string) (string, error) {
	out, err := c.ec2.RunInstances(ctx, &ec2.RunInstancesInput{
		ImageId:          aws.String(imageID),
		InstanceType:     ec2types.InstanceType(spec.InstanceType),
		KeyName:          aws.String(keyName),
		SecurityGroupIds: []string{sgID},
		MinCount:         aws.Int32(1),
		MaxCount:         aws.Int32(1),
		CpuOptions: &ec2types.CpuOptionsRequest{
			NestedVirtualization: ec2types.NestedVirtualizationSpecificationEnabled,
		},
		BlockDeviceMappings: []ec2types.BlockDeviceMapping{{
			DeviceName: aws.String(rootDev),
			Ebs: &ec2types.EbsBlockDevice{
				VolumeSize:          aws.Int32(int32(spec.HostVolumeGB)),
				VolumeType:          ec2types.VolumeTypeGp3,
				DeleteOnTermination: aws.Bool(true),
			},
		}},
		TagSpecifications: tagSpecs(ec2types.ResourceTypeInstance, tags),
	})
	if err != nil {
		return "", fmt.Errorf("substrate run instance: %w", err)
	}
	if len(out.Instances) == 0 {
		return "", fmt.Errorf("substrate run instance: no instance returned")
	}
	id := aws.ToString(out.Instances[0].InstanceId)
	waiter := ec2.NewInstanceRunningWaiter(c.ec2)
	if err := waiter.Wait(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{id}}, instanceRunningTimeout); err != nil {
		return "", fmt.Errorf("substrate wait instance running: %w", err)
	}
	return id, nil
}

func allocateEIP(ctx context.Context, c clients, instanceID string, tags map[string]string) (string, string, error) {
	alloc, err := c.ec2.AllocateAddress(ctx, &ec2.AllocateAddressInput{
		Domain:            ec2types.DomainTypeVpc,
		TagSpecifications: tagSpecs(ec2types.ResourceTypeElasticIp, tags),
	})
	if err != nil {
		return "", "", fmt.Errorf("substrate allocate address: %w", err)
	}
	if _, err := c.ec2.AssociateAddress(ctx, &ec2.AssociateAddressInput{
		AllocationId: alloc.AllocationId,
		InstanceId:   aws.String(instanceID),
	}); err != nil {
		return "", "", fmt.Errorf("substrate associate address: %w", err)
	}
	return aws.ToString(alloc.PublicIp), aws.ToString(alloc.AllocationId), nil
}

// openHairpin adds ingress rules so the host can reach its own cluster API and
// ingress through the Elastic IP during agent wait-for.
func openHairpin(ctx context.Context, c clients, sgID, eip string) error {
	if err := authorizePorts(ctx, c, sgID, hairpinPorts, eip+"/32"); err != nil {
		return fmt.Errorf("substrate authorize hairpin: %w", err)
	}
	return nil
}

func upsertDNS(ctx context.Context, c clients, spec LaunchSpec, zoneID, eip string) error {
	changes := []r53types.Change{
		aRecordChange(r53types.ChangeActionUpsert, apiFQDN(spec.ClusterName, spec.BaseDomain)+".", eip),
		aRecordChange(r53types.ChangeActionUpsert, appsFQDN(spec.ClusterName, spec.BaseDomain)+".", eip),
	}
	if _, err := c.r53.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID),
		ChangeBatch:  &r53types.ChangeBatch{Changes: changes},
	}); err != nil {
		return fmt.Errorf("substrate upsert dns: %w", err)
	}
	return nil
}

func aRecordChange(action r53types.ChangeAction, name, value string) r53types.Change {
	return r53types.Change{
		Action: action,
		ResourceRecordSet: &r53types.ResourceRecordSet{
			Name:            aws.String(name),
			Type:            r53types.RRTypeA,
			TTL:             aws.Int64(60),
			ResourceRecords: []r53types.ResourceRecord{{Value: aws.String(value)}},
		},
	}
}

func authorizePorts(ctx context.Context, c clients, sgID string, ports []int32, cidr string) error {
	perms := make([]ec2types.IpPermission, 0, len(ports))
	for _, p := range ports {
		perms = append(perms, ec2types.IpPermission{
			IpProtocol: aws.String("tcp"),
			FromPort:   aws.Int32(p),
			ToPort:     aws.Int32(p),
			IpRanges:   []ec2types.IpRange{{CidrIp: aws.String(cidr)}},
		})
	}
	_, err := c.ec2.AuthorizeSecurityGroupIngress(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId:       aws.String(sgID),
		IpPermissions: perms,
	})
	return err
}

// isDuplicateKeyPair reports whether err is EC2's InvalidKeyPair.Duplicate.
func isDuplicateKeyPair(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "InvalidKeyPair.Duplicate"
}

// tagSpecs converts a tag map to a single TagSpecification for the resource type,
// with deterministic key ordering for stable tests.
func tagSpecs(rt ec2types.ResourceType, tags map[string]string) []ec2types.TagSpecification {
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]ec2types.Tag, 0, len(keys))
	for _, k := range keys {
		out = append(out, ec2types.Tag{Key: aws.String(k), Value: aws.String(tags[k])})
	}
	return []ec2types.TagSpecification{{ResourceType: rt, Tags: out}}
}
