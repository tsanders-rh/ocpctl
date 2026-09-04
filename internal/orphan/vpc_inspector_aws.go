package orphan

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
)

// awsVPCInspector implements VPCInspector using the AWS SDK. Credentials and
// region are resolved per call from the resource's region.
type awsVPCInspector struct{}

// NewAWSVPCInspector returns a VPCInspector backed by the AWS SDK.
func NewAWSVPCInspector() VPCInspector { return awsVPCInspector{} }

// InspectVPC counts live infrastructure inside a VPC: running/pending EC2
// instances, pending/available NAT gateways, and ELBv2 load balancers attached
// to the VPC. Any nonzero count means the VPC is not safe to tear down.
func (awsVPCInspector) InspectVPC(ctx context.Context, vpcID, region string) (VPCLiveCounts, error) {
	var counts VPCLiveCounts

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return counts, fmt.Errorf("load AWS config: %w", err)
	}

	ec2Client := ec2.NewFromConfig(cfg)

	// Running/pending EC2 instances in the VPC.
	instPaginator := ec2.NewDescribeInstancesPaginator(ec2Client, &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
			{Name: aws.String("instance-state-name"), Values: []string{"pending", "running"}},
		},
	})
	for instPaginator.HasMorePages() {
		page, err := instPaginator.NextPage(ctx)
		if err != nil {
			return counts, fmt.Errorf("describe instances: %w", err)
		}
		for _, r := range page.Reservations {
			counts.RunningInstances += len(r.Instances)
		}
	}

	// Pending/available NAT gateways in the VPC.
	natPaginator := ec2.NewDescribeNatGatewaysPaginator(ec2Client, &ec2.DescribeNatGatewaysInput{
		Filter: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
			{Name: aws.String("state"), Values: []string{"pending", "available"}},
		},
	})
	for natPaginator.HasMorePages() {
		page, err := natPaginator.NextPage(ctx)
		if err != nil {
			return counts, fmt.Errorf("describe nat gateways: %w", err)
		}
		counts.NATGateways += len(page.NatGateways)
	}

	// ELBv2 load balancers attached to the VPC. There is no server-side VPC
	// filter for DescribeLoadBalancers, so we list and match client-side.
	elbClient := elasticloadbalancingv2.NewFromConfig(cfg)
	lbPaginator := elasticloadbalancingv2.NewDescribeLoadBalancersPaginator(elbClient, &elasticloadbalancingv2.DescribeLoadBalancersInput{})
	for lbPaginator.HasMorePages() {
		page, err := lbPaginator.NextPage(ctx)
		if err != nil {
			return counts, fmt.Errorf("describe load balancers: %w", err)
		}
		for _, lb := range page.LoadBalancers {
			if lb.VpcId != nil && *lb.VpcId == vpcID {
				counts.LoadBalancers++
			}
		}
	}

	return counts, nil
}
