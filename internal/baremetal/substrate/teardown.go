package substrate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
)

const (
	instanceTerminatedTimeout = 10 * time.Minute
	sgDeleteAttempts          = 6
	sgDeleteDelay             = 10 * time.Second
)

// Teardown removes a cluster's substrate. It discovers resources by tag
// (ManagedBy=ocpctl + ClusterName), deletes the Route53 records, and is
// best-effort and idempotent: a missing resource at any step is not an error.
func Teardown(ctx context.Context, spec TeardownSpec) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(spec.Region))
	if err != nil {
		return fmt.Errorf("substrate load aws config: %w", err)
	}
	c := clients{
		ec2: ec2.NewFromConfig(cfg),
		r53: route53.NewFromConfig(cfg),
	}
	return teardown(ctx, c, time.Sleep, spec)
}

func teardown(ctx context.Context, c clients, sleep func(time.Duration), spec TeardownSpec) error {
	instanceIDs, allocIDs, err := discoverInstances(ctx, c, spec.ClusterName)
	if err != nil {
		return err
	}

	if err := deleteDNS(ctx, c, spec); err != nil {
		return err
	}

	if len(instanceIDs) > 0 {
		if _, err := c.ec2.TerminateInstances(ctx, &ec2.TerminateInstancesInput{InstanceIds: instanceIDs}); err != nil {
			return fmt.Errorf("substrate terminate instances: %w", err)
		}
		waiter := ec2.NewInstanceTerminatedWaiter(c.ec2)
		if err := waiter.Wait(ctx, &ec2.DescribeInstancesInput{InstanceIds: instanceIDs}, instanceTerminatedTimeout); err != nil {
			return fmt.Errorf("substrate wait instances terminated: %w", err)
		}
	}

	for _, allocID := range allocIDs {
		if _, err := c.ec2.ReleaseAddress(ctx, &ec2.ReleaseAddressInput{AllocationId: aws.String(allocID)}); err != nil {
			return fmt.Errorf("substrate release address %s: %w", allocID, err)
		}
	}

	if err := deleteSecurityGroups(ctx, c, sleep, spec.ClusterName); err != nil {
		return err
	}

	return deleteKeypair(ctx, c, spec.ClusterName)
}

// discoverInstances finds the cluster's instances by tag and their associated
// Elastic IP allocation ids.
func discoverInstances(ctx context.Context, c clients, clusterName string) ([]string, []string, error) {
	out, err := c.ec2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: append(tagFilters(clusterName), ec2types.Filter{
			Name:   aws.String("instance-state-name"),
			Values: []string{"pending", "running", "stopping", "stopped"},
		}),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("substrate describe instances: %w", err)
	}
	var instanceIDs []string
	for _, res := range out.Reservations {
		for _, inst := range res.Instances {
			instanceIDs = append(instanceIDs, aws.ToString(inst.InstanceId))
		}
	}

	var allocIDs []string
	for _, id := range instanceIDs {
		addrs, err := c.ec2.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{
			Filters: []ec2types.Filter{{Name: aws.String("instance-id"), Values: []string{id}}},
		})
		if err != nil {
			return nil, nil, fmt.Errorf("substrate describe addresses: %w", err)
		}
		for _, a := range addrs.Addresses {
			if a.AllocationId != nil {
				allocIDs = append(allocIDs, aws.ToString(a.AllocationId))
			}
		}
	}
	return instanceIDs, allocIDs, nil
}

// deleteDNS removes the api/apps A records. A missing zone or missing records is
// not an error.
func deleteDNS(ctx context.Context, c clients, spec TeardownSpec) error {
	zoneID, err := resolveZoneID(ctx, c.r53, spec.ZoneID, spec.BaseDomain)
	if err != nil {
		return nil // no hosted zone => nothing to delete
	}
	out, err := c.r53.ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{HostedZoneId: aws.String(zoneID)})
	if err != nil {
		return fmt.Errorf("substrate list record sets: %w", err)
	}
	targets := map[string]bool{
		apiFQDN(spec.ClusterName, spec.BaseDomain) + ".":  true,
		appsFQDN(spec.ClusterName, spec.BaseDomain) + ".": true,
	}
	var changes []r53types.Change
	for i := range out.ResourceRecordSets {
		rrs := out.ResourceRecordSets[i]
		if rrs.Type != r53types.RRTypeA {
			continue
		}
		if targets[unescapeRecordName(aws.ToString(rrs.Name))] {
			changes = append(changes, r53types.Change{Action: r53types.ChangeActionDelete, ResourceRecordSet: &rrs})
		}
	}
	if len(changes) == 0 {
		return nil
	}
	if _, err := c.r53.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID),
		ChangeBatch:  &r53types.ChangeBatch{Changes: changes},
	}); err != nil {
		return fmt.Errorf("substrate delete dns records: %w", err)
	}
	return nil
}

func deleteSecurityGroups(ctx context.Context, c clients, sleep func(time.Duration), clusterName string) error {
	out, err := c.ec2.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{Filters: tagFilters(clusterName)})
	if err != nil {
		return fmt.Errorf("substrate describe security groups: %w", err)
	}
	for _, sg := range out.SecurityGroups {
		id := aws.ToString(sg.GroupId)
		var lastErr error
		for attempt := 0; attempt < sgDeleteAttempts; attempt++ {
			if attempt > 0 {
				sleep(sgDeleteDelay)
			}
			if _, err := c.ec2.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{GroupId: aws.String(id)}); err != nil {
				lastErr = err
				continue
			}
			lastErr = nil
			break
		}
		if lastErr != nil {
			return fmt.Errorf("substrate delete security group %s: %w", id, lastErr)
		}
	}
	return nil
}

func deleteKeypair(ctx context.Context, c clients, clusterName string) error {
	out, err := c.ec2.DescribeKeyPairs(ctx, &ec2.DescribeKeyPairsInput{Filters: tagFilters(clusterName)})
	if err != nil {
		return fmt.Errorf("substrate describe key pairs: %w", err)
	}
	for _, kp := range out.KeyPairs {
		if _, err := c.ec2.DeleteKeyPair(ctx, &ec2.DeleteKeyPairInput{KeyName: kp.KeyName}); err != nil {
			return fmt.Errorf("substrate delete key pair %s: %w", aws.ToString(kp.KeyName), err)
		}
	}
	return nil
}

func tagFilters(clusterName string) []ec2types.Filter {
	return []ec2types.Filter{
		{Name: aws.String("tag:ManagedBy"), Values: []string{"ocpctl"}},
		{Name: aws.String("tag:ClusterName"), Values: []string{clusterName}},
	}
}

// unescapeRecordName converts Route53's octal escape for a wildcard label
// (\052) back to "*" so fetched record names compare equal to our FQDNs.
func unescapeRecordName(name string) string {
	return strings.ReplaceAll(name, `\052`, "*")
}
