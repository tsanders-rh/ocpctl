package cleanup

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
)

// ec2API is the subset of *ec2.Client used by the VPC cleaner. It is satisfied
// by the concrete client and mocked in tests. It also satisfies
// ec2.DescribeInstancesAPIClient, so it can be handed to the instance-terminated
// waiter directly.
type ec2API interface {
	DescribeVpcEndpoints(context.Context, *ec2.DescribeVpcEndpointsInput, ...func(*ec2.Options)) (*ec2.DescribeVpcEndpointsOutput, error)
	DeleteVpcEndpoints(context.Context, *ec2.DeleteVpcEndpointsInput, ...func(*ec2.Options)) (*ec2.DeleteVpcEndpointsOutput, error)
	DescribeNatGateways(context.Context, *ec2.DescribeNatGatewaysInput, ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error)
	DeleteNatGateway(context.Context, *ec2.DeleteNatGatewayInput, ...func(*ec2.Options)) (*ec2.DeleteNatGatewayOutput, error)
	DescribeInstances(context.Context, *ec2.DescribeInstancesInput, ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	TerminateInstances(context.Context, *ec2.TerminateInstancesInput, ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error)
	DescribeNetworkInterfaces(context.Context, *ec2.DescribeNetworkInterfacesInput, ...func(*ec2.Options)) (*ec2.DescribeNetworkInterfacesOutput, error)
	DetachNetworkInterface(context.Context, *ec2.DetachNetworkInterfaceInput, ...func(*ec2.Options)) (*ec2.DetachNetworkInterfaceOutput, error)
	DeleteNetworkInterface(context.Context, *ec2.DeleteNetworkInterfaceInput, ...func(*ec2.Options)) (*ec2.DeleteNetworkInterfaceOutput, error)
	DescribeEgressOnlyInternetGateways(context.Context, *ec2.DescribeEgressOnlyInternetGatewaysInput, ...func(*ec2.Options)) (*ec2.DescribeEgressOnlyInternetGatewaysOutput, error)
	DeleteEgressOnlyInternetGateway(context.Context, *ec2.DeleteEgressOnlyInternetGatewayInput, ...func(*ec2.Options)) (*ec2.DeleteEgressOnlyInternetGatewayOutput, error)
	DescribeInternetGateways(context.Context, *ec2.DescribeInternetGatewaysInput, ...func(*ec2.Options)) (*ec2.DescribeInternetGatewaysOutput, error)
	DetachInternetGateway(context.Context, *ec2.DetachInternetGatewayInput, ...func(*ec2.Options)) (*ec2.DetachInternetGatewayOutput, error)
	DeleteInternetGateway(context.Context, *ec2.DeleteInternetGatewayInput, ...func(*ec2.Options)) (*ec2.DeleteInternetGatewayOutput, error)
	DescribeNetworkAcls(context.Context, *ec2.DescribeNetworkAclsInput, ...func(*ec2.Options)) (*ec2.DescribeNetworkAclsOutput, error)
	DeleteNetworkAcl(context.Context, *ec2.DeleteNetworkAclInput, ...func(*ec2.Options)) (*ec2.DeleteNetworkAclOutput, error)
	DescribeRouteTables(context.Context, *ec2.DescribeRouteTablesInput, ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error)
	DisassociateRouteTable(context.Context, *ec2.DisassociateRouteTableInput, ...func(*ec2.Options)) (*ec2.DisassociateRouteTableOutput, error)
	DeleteRouteTable(context.Context, *ec2.DeleteRouteTableInput, ...func(*ec2.Options)) (*ec2.DeleteRouteTableOutput, error)
	DescribeSubnets(context.Context, *ec2.DescribeSubnetsInput, ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error)
	DeleteSubnet(context.Context, *ec2.DeleteSubnetInput, ...func(*ec2.Options)) (*ec2.DeleteSubnetOutput, error)
	DescribeSecurityGroups(context.Context, *ec2.DescribeSecurityGroupsInput, ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error)
	RevokeSecurityGroupIngress(context.Context, *ec2.RevokeSecurityGroupIngressInput, ...func(*ec2.Options)) (*ec2.RevokeSecurityGroupIngressOutput, error)
	RevokeSecurityGroupEgress(context.Context, *ec2.RevokeSecurityGroupEgressInput, ...func(*ec2.Options)) (*ec2.RevokeSecurityGroupEgressOutput, error)
	DeleteSecurityGroup(context.Context, *ec2.DeleteSecurityGroupInput, ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error)
	DeleteVpc(context.Context, *ec2.DeleteVpcInput, ...func(*ec2.Options)) (*ec2.DeleteVpcOutput, error)
}

// elbv1API is the subset of the Classic ELB (ELBv1) client used by the cleaner.
type elbv1API interface {
	DescribeLoadBalancers(context.Context, *elasticloadbalancing.DescribeLoadBalancersInput, ...func(*elasticloadbalancing.Options)) (*elasticloadbalancing.DescribeLoadBalancersOutput, error)
	DeleteLoadBalancer(context.Context, *elasticloadbalancing.DeleteLoadBalancerInput, ...func(*elasticloadbalancing.Options)) (*elasticloadbalancing.DeleteLoadBalancerOutput, error)
}

// elbv2API is the subset of the Application/Network ELB (ELBv2) client used by the cleaner.
type elbv2API interface {
	DescribeLoadBalancers(context.Context, *elasticloadbalancingv2.DescribeLoadBalancersInput, ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeLoadBalancersOutput, error)
	DeleteLoadBalancer(context.Context, *elasticloadbalancingv2.DeleteLoadBalancerInput, ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DeleteLoadBalancerOutput, error)
}

// vpcCleaner holds the clients and knobs used to tear down a VPC. Sleeps are
// injectable so tests run instantly; the clients are interfaces so they can be
// mocked.
type vpcCleaner struct {
	ec2    ec2API
	elbv1  elbv1API
	elbv2  elbv2API
	region string
	sleep  func(time.Duration)
}

// DeleteVPCAndDependencies deletes a VPC and all its dependent resources in the correct order
func DeleteVPCAndDependencies(ctx context.Context, ec2Client *ec2.Client, vpcID string) error {
	// Get AWS region from the EC2 client config
	region := ec2Client.Options().Region

	// The ELB clients share the EC2 client's region.
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	c := &vpcCleaner{
		ec2:    ec2Client,
		elbv1:  elasticloadbalancing.NewFromConfig(cfg),
		elbv2:  elasticloadbalancingv2.NewFromConfig(cfg),
		region: region,
		sleep:  time.Sleep,
	}
	return c.run(ctx, vpcID)
}

func (c *vpcCleaner) run(ctx context.Context, vpcID string) error {
	// Deletion order matters - delete higher-level dependencies first
	steps := []struct {
		name string
		fn   func(context.Context, string) error
	}{
		{"VPC endpoints", c.deleteVPCEndpoints},
		{"load balancers", c.deleteLoadBalancers},
		{"NAT gateways", c.deleteNATGateways},
		{"EC2 instances", c.terminateEC2Instances},
		{"network interfaces", c.deleteNetworkInterfaces},
		{"egress-only internet gateways", c.deleteEgressOnlyInternetGateways},
		{"internet gateways", c.detachAndDeleteInternetGateways},
		{"network ACLs", c.deleteNetworkACLs},
		{"non-main route tables", c.deleteRouteTables},
		{"subnets", c.deleteSubnets},
		{"non-default security groups", c.deleteSecurityGroups},
		{"VPC", c.deleteVPC},
	}

	for _, step := range steps {
		log.Printf("==> Deleting %s for VPC %s", step.name, vpcID)
		if err := step.fn(ctx, vpcID); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}

	log.Printf("Successfully deleted VPC %s and all dependencies", vpcID)
	return nil
}

// Helper functions

func isNotFoundOrDependencyViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "InvalidVpcID.NotFound") ||
		strings.Contains(msg, "InvalidSubnetID.NotFound") ||
		strings.Contains(msg, "InvalidRouteTableID.NotFound") ||
		strings.Contains(msg, "InvalidGroup.NotFound") ||
		strings.Contains(msg, "InvalidNetworkAclID.NotFound") ||
		strings.Contains(msg, "DependencyViolation") ||
		strings.Contains(msg, "InvalidInternetGatewayID.NotFound") ||
		strings.Contains(msg, "InvalidNatGatewayID.NotFound") ||
		strings.Contains(msg, "InvalidVpcEndpointId.NotFound") ||
		strings.Contains(msg, "InvalidNetworkInterfaceID.NotFound") ||
		strings.Contains(msg, "InvalidAttachmentID.NotFound") ||
		strings.Contains(msg, "AuthFailure") ||
		strings.Contains(msg, "is currently in use")
}

func ignoreBenignError(err error) error {
	if err == nil || isNotFoundOrDependencyViolation(err) {
		if err != nil {
			log.Printf("Ignored benign AWS error: %v", err)
		}
		return nil
	}
	return err
}

// isServiceManagedENI checks if a network interface is managed by an AWS service
// Service-managed ENIs should not be manually deleted - AWS will clean them up automatically
func isServiceManagedENI(eni *types.NetworkInterface) bool {
	// Check RequesterManaged flag - true means AWS is managing it
	if aws.ToBool(eni.RequesterManaged) {
		return true
	}

	// Check interface type - certain types are always service-managed
	interfaceType := string(eni.InterfaceType)
	serviceManagedTypes := []string{
		"load_balancer",         // Elastic Load Balancer
		"nat_gateway",           // NAT Gateway
		"gateway_load_balancer", // Gateway Load Balancer
		"gateway_load_balancer_endpoint",
		"network_load_balancer", // Network Load Balancer
		"lambda",                // Lambda function
		"efs",                   // Elastic File System
		"api_gateway_managed",   // API Gateway
		"transit_gateway",       // Transit Gateway
		"global_accelerator_managed",
		"aws_codestar_connections_managed",
		"iot_rules_managed",
	}

	for _, managedType := range serviceManagedTypes {
		if interfaceType == managedType {
			return true
		}
	}

	return false
}

func (c *vpcCleaner) deleteVPCEndpoints(ctx context.Context, vpcID string) error {
	out, err := c.ec2.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{
		Filters: []types.Filter{{Name: aws.String("vpc-id"), Values: []string{vpcID}}},
	})
	if err != nil {
		return err
	}
	if len(out.VpcEndpoints) == 0 {
		return nil
	}

	ids := make([]string, 0, len(out.VpcEndpoints))
	for _, ep := range out.VpcEndpoints {
		ids = append(ids, aws.ToString(ep.VpcEndpointId))
	}

	log.Printf("Deleting %d VPC endpoints", len(ids))
	_, err = c.ec2.DeleteVpcEndpoints(ctx, &ec2.DeleteVpcEndpointsInput{VpcEndpointIds: ids})
	return ignoreBenignError(err)
}

func (c *vpcCleaner) deleteNATGateways(ctx context.Context, vpcID string) error {
	out, err := c.ec2.DescribeNatGateways(ctx, &ec2.DescribeNatGatewaysInput{
		Filter: []types.Filter{{Name: aws.String("vpc-id"), Values: []string{vpcID}}},
	})
	if err != nil {
		return err
	}

	for _, ngw := range out.NatGateways {
		id := aws.ToString(ngw.NatGatewayId)
		state := ngw.State
		if state == types.NatGatewayStateDeleted || state == types.NatGatewayStateDeleting {
			continue
		}
		log.Printf("Deleting NAT gateway %s", id)
		_, err := c.ec2.DeleteNatGateway(ctx, &ec2.DeleteNatGatewayInput{NatGatewayId: aws.String(id)})
		if err := ignoreBenignError(err); err != nil {
			return err
		}
	}

	// Wait briefly for NAT gateways to start deleting
	if len(out.NatGateways) > 0 {
		log.Printf("Waiting 20 seconds for NAT gateways to leave active state")
		c.sleep(20 * time.Second)
	}
	return nil
}

func (c *vpcCleaner) terminateEC2Instances(ctx context.Context, vpcID string) error {
	// Find all EC2 instances in this VPC
	out, err := c.ec2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		},
	})
	if err != nil {
		return err
	}

	var instanceIDs []string
	for _, reservation := range out.Reservations {
		for _, instance := range reservation.Instances {
			state := instance.State.Name
			// Skip instances that are already terminated or shutting down
			if state == types.InstanceStateNameTerminated || state == types.InstanceStateNameShuttingDown {
				continue
			}
			instanceID := aws.ToString(instance.InstanceId)
			if instanceID != "" {
				instanceIDs = append(instanceIDs, instanceID)
			}
		}
	}

	if len(instanceIDs) == 0 {
		log.Printf("No EC2 instances found in VPC %s", vpcID)
		return nil
	}

	log.Printf("Terminating %d EC2 instance(s): %v", len(instanceIDs), instanceIDs)
	_, err = c.ec2.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: instanceIDs,
	})
	if err != nil {
		return ignoreBenignError(err)
	}

	// Wait for instances to actually terminate (not just start terminating)
	// This ensures network interfaces are released before we try to delete them
	log.Printf("Waiting for %d instance(s) to terminate (this may take 1-2 minutes)...", len(instanceIDs))
	waiter := ec2.NewInstanceTerminatedWaiter(c.ec2)
	err = waiter.Wait(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: instanceIDs,
	}, 5*time.Minute) // Allow up to 5 minutes for termination
	if err != nil {
		log.Printf("Warning: instance termination wait failed: %v (proceeding anyway)", err)
		// Don't return error - try to continue with cleanup even if wait fails
		c.sleep(30 * time.Second) // Fall back to brief sleep
	} else {
		log.Printf("All instances terminated successfully")
	}

	return nil
}

func (c *vpcCleaner) deleteNetworkInterfaces(ctx context.Context, vpcID string) error {
	out, err := c.ec2.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{
		Filters: []types.Filter{{Name: aws.String("vpc-id"), Values: []string{vpcID}}},
	})
	if err != nil {
		return err
	}

	for _, eni := range out.NetworkInterfaces {
		id := aws.ToString(eni.NetworkInterfaceId)
		if id == "" {
			continue
		}

		// Skip service-managed network interfaces - AWS will clean them up automatically
		if isServiceManagedENI(&eni) {
			log.Printf("Skipping service-managed ENI %s (type: %s, requester-managed: %t, description: %s)",
				id, eni.InterfaceType, aws.ToBool(eni.RequesterManaged), aws.ToString(eni.Description))
			continue
		}

		// Detach if attached
		if eni.Attachment != nil && aws.ToString(eni.Attachment.AttachmentId) != "" {
			attachID := aws.ToString(eni.Attachment.AttachmentId)
			log.Printf("Detaching ENI %s (attachment %s)", id, attachID)
			_, err := c.ec2.DetachNetworkInterface(ctx, &ec2.DetachNetworkInterfaceInput{
				AttachmentId: aws.String(attachID),
				Force:        aws.Bool(true),
			})
			// Skip errors about service-managed attachments (ela-attach, etc.)
			if err != nil && strings.Contains(err.Error(), "OperationNotPermitted") {
				log.Printf("Cannot detach ENI %s - appears to be service-managed: %v", id, err)
				continue
			}
			if err := ignoreBenignError(err); err != nil {
				return err
			}
			c.sleep(5 * time.Second)
		}

		log.Printf("Deleting ENI %s", id)
		_, err := c.ec2.DeleteNetworkInterface(ctx, &ec2.DeleteNetworkInterfaceInput{NetworkInterfaceId: aws.String(id)})
		if err := ignoreBenignError(err); err != nil {
			return err
		}
	}
	return nil
}

func (c *vpcCleaner) deleteEgressOnlyInternetGateways(ctx context.Context, vpcID string) error {
	out, err := c.ec2.DescribeEgressOnlyInternetGateways(ctx, &ec2.DescribeEgressOnlyInternetGatewaysInput{
		Filters: []types.Filter{{Name: aws.String("attachment.vpc-id"), Values: []string{vpcID}}},
	})
	if err != nil {
		return err
	}
	for _, gw := range out.EgressOnlyInternetGateways {
		id := aws.ToString(gw.EgressOnlyInternetGatewayId)
		log.Printf("Deleting egress-only internet gateway %s", id)
		_, err := c.ec2.DeleteEgressOnlyInternetGateway(ctx, &ec2.DeleteEgressOnlyInternetGatewayInput{EgressOnlyInternetGatewayId: aws.String(id)})
		if err := ignoreBenignError(err); err != nil {
			return err
		}
	}
	return nil
}

func (c *vpcCleaner) detachAndDeleteInternetGateways(ctx context.Context, vpcID string) error {
	out, err := c.ec2.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{
		Filters: []types.Filter{{Name: aws.String("attachment.vpc-id"), Values: []string{vpcID}}},
	})
	if err != nil {
		return err
	}
	for _, igw := range out.InternetGateways {
		id := aws.ToString(igw.InternetGatewayId)
		log.Printf("Detaching internet gateway %s", id)
		_, err := c.ec2.DetachInternetGateway(ctx, &ec2.DetachInternetGatewayInput{
			InternetGatewayId: aws.String(id),
			VpcId:             aws.String(vpcID),
		})
		if err := ignoreBenignError(err); err != nil {
			return err
		}
		log.Printf("Deleting internet gateway %s", id)
		_, err = c.ec2.DeleteInternetGateway(ctx, &ec2.DeleteInternetGatewayInput{InternetGatewayId: aws.String(id)})
		if err := ignoreBenignError(err); err != nil {
			return err
		}
	}
	return nil
}

func (c *vpcCleaner) deleteNetworkACLs(ctx context.Context, vpcID string) error {
	out, err := c.ec2.DescribeNetworkAcls(ctx, &ec2.DescribeNetworkAclsInput{
		Filters: []types.Filter{{Name: aws.String("vpc-id"), Values: []string{vpcID}}},
	})
	if err != nil {
		return err
	}
	for _, acl := range out.NetworkAcls {
		if aws.ToBool(acl.IsDefault) {
			continue
		}
		id := aws.ToString(acl.NetworkAclId)
		log.Printf("Deleting network ACL %s", id)
		_, err := c.ec2.DeleteNetworkAcl(ctx, &ec2.DeleteNetworkAclInput{NetworkAclId: aws.String(id)})
		if err := ignoreBenignError(err); err != nil {
			return err
		}
	}
	return nil
}

func (c *vpcCleaner) deleteRouteTables(ctx context.Context, vpcID string) error {
	out, err := c.ec2.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
		Filters: []types.Filter{{Name: aws.String("vpc-id"), Values: []string{vpcID}}},
	})
	if err != nil {
		return err
	}

	for _, rt := range out.RouteTables {
		id := aws.ToString(rt.RouteTableId)
		main := false
		for _, assoc := range rt.Associations {
			if aws.ToBool(assoc.Main) {
				main = true
				break
			}
		}
		if main {
			continue
		}

		// Disassociate first
		for _, assoc := range rt.Associations {
			assocID := aws.ToString(assoc.RouteTableAssociationId)
			if assocID == "" || aws.ToBool(assoc.Main) {
				continue
			}
			log.Printf("Disassociating route table %s (association %s)", id, assocID)
			_, err := c.ec2.DisassociateRouteTable(ctx, &ec2.DisassociateRouteTableInput{AssociationId: aws.String(assocID)})
			if err := ignoreBenignError(err); err != nil {
				return err
			}
		}

		log.Printf("Deleting route table %s", id)
		_, err := c.ec2.DeleteRouteTable(ctx, &ec2.DeleteRouteTableInput{RouteTableId: aws.String(id)})
		if err := ignoreBenignError(err); err != nil {
			return err
		}
	}
	return nil
}

func (c *vpcCleaner) deleteSubnets(ctx context.Context, vpcID string) error {
	out, err := c.ec2.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		Filters: []types.Filter{{Name: aws.String("vpc-id"), Values: []string{vpcID}}},
	})
	if err != nil {
		return err
	}
	for _, subnet := range out.Subnets {
		id := aws.ToString(subnet.SubnetId)
		log.Printf("Deleting subnet %s", id)
		_, err := c.ec2.DeleteSubnet(ctx, &ec2.DeleteSubnetInput{SubnetId: aws.String(id)})
		if err := ignoreBenignError(err); err != nil {
			return err
		}
	}
	return nil
}

func (c *vpcCleaner) deleteSecurityGroups(ctx context.Context, vpcID string) error {
	out, err := c.ec2.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []types.Filter{{Name: aws.String("vpc-id"), Values: []string{vpcID}}},
	})
	if err != nil {
		return err
	}

	// First remove all rules to break circular references
	for _, sg := range out.SecurityGroups {
		id := aws.ToString(sg.GroupId)
		if aws.ToString(sg.GroupName) == "default" {
			continue
		}
		if len(sg.IpPermissions) > 0 {
			log.Printf("Revoking ingress rules for security group %s", id)
			_, err := c.ec2.RevokeSecurityGroupIngress(ctx, &ec2.RevokeSecurityGroupIngressInput{
				GroupId:       aws.String(id),
				IpPermissions: sg.IpPermissions,
			})
			if err := ignoreBenignError(err); err != nil {
				return err
			}
		}
		if len(sg.IpPermissionsEgress) > 0 {
			log.Printf("Revoking egress rules for security group %s", id)
			_, err := c.ec2.RevokeSecurityGroupEgress(ctx, &ec2.RevokeSecurityGroupEgressInput{
				GroupId:       aws.String(id),
				IpPermissions: sg.IpPermissionsEgress,
			})
			if err := ignoreBenignError(err); err != nil {
				return err
			}
		}
	}

	// Now delete the security groups
	for _, sg := range out.SecurityGroups {
		if aws.ToString(sg.GroupName) == "default" {
			continue
		}
		id := aws.ToString(sg.GroupId)
		log.Printf("Deleting security group %s", id)
		_, err := c.ec2.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{GroupId: aws.String(id)})
		if err := ignoreBenignError(err); err != nil {
			return err
		}
	}
	return nil
}

func (c *vpcCleaner) deleteVPC(ctx context.Context, vpcID string) error {
	log.Printf("Deleting VPC %s", vpcID)
	_, err := c.ec2.DeleteVpc(ctx, &ec2.DeleteVpcInput{VpcId: aws.String(vpcID)})
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "DependencyViolation") {
		return fmt.Errorf("VPC has remaining dependencies (likely service-managed network interfaces from deleted load balancers). These are being cleaned up by AWS automatically. Please wait 2-3 minutes and try deleting the VPC again")
	}
	if strings.Contains(err.Error(), "InvalidVpcID.NotFound") {
		log.Printf("VPC %s not found - assuming already deleted", vpcID)
		return nil
	}
	return err
}

func (c *vpcCleaner) deleteLoadBalancers(ctx context.Context, vpcID string) error {
	totalLBsDeleted := 0

	// ===== Check for Classic Load Balancers (ELBv1) =====
	classicLBsResult, err := c.elbv1.DescribeLoadBalancers(ctx, &elasticloadbalancing.DescribeLoadBalancersInput{})
	if err != nil {
		return fmt.Errorf("describe classic load balancers: %w", err)
	}

	// Filter Classic LBs by VPC ID
	var classicLBsToDelete []string
	for _, lb := range classicLBsResult.LoadBalancerDescriptions {
		if aws.ToString(lb.VPCId) == vpcID {
			lbName := aws.ToString(lb.LoadBalancerName)
			classicLBsToDelete = append(classicLBsToDelete, lbName)
			log.Printf("Found Classic Load Balancer %s in VPC %s", lbName, vpcID)
		}
	}

	// Delete Classic Load Balancers
	for _, lbName := range classicLBsToDelete {
		log.Printf("Deleting Classic Load Balancer %s", lbName)
		_, err := c.elbv1.DeleteLoadBalancer(ctx, &elasticloadbalancing.DeleteLoadBalancerInput{
			LoadBalancerName: aws.String(lbName),
		})
		if err != nil {
			if strings.Contains(err.Error(), "LoadBalancerNotFound") {
				log.Printf("Classic Load Balancer %s not found - assuming already deleted", lbName)
				continue
			}
			return fmt.Errorf("delete Classic Load Balancer %s: %w", lbName, err)
		}
		log.Printf("Successfully initiated deletion of Classic Load Balancer %s", lbName)
		totalLBsDeleted++
	}

	// ===== Check for Application/Network Load Balancers (ELBv2) =====
	elbv2Result, err := c.elbv2.DescribeLoadBalancers(ctx, &elasticloadbalancingv2.DescribeLoadBalancersInput{})
	if err != nil {
		return fmt.Errorf("describe application/network load balancers: %w", err)
	}

	// Filter Application/Network LBs by VPC ID
	var elbv2ToDelete []string
	for _, lb := range elbv2Result.LoadBalancers {
		if aws.ToString(lb.VpcId) == vpcID {
			lbArn := aws.ToString(lb.LoadBalancerArn)
			lbName := aws.ToString(lb.LoadBalancerName)
			lbType := lb.Type
			elbv2ToDelete = append(elbv2ToDelete, lbArn)
			log.Printf("Found %s Load Balancer %s (%s) in VPC %s", lbType, lbName, lbArn, vpcID)
		}
	}

	// Delete Application/Network Load Balancers
	for _, lbArn := range elbv2ToDelete {
		log.Printf("Deleting Application/Network Load Balancer %s", lbArn)
		_, err := c.elbv2.DeleteLoadBalancer(ctx, &elasticloadbalancingv2.DeleteLoadBalancerInput{
			LoadBalancerArn: aws.String(lbArn),
		})
		if err != nil {
			if strings.Contains(err.Error(), "LoadBalancerNotFound") {
				log.Printf("Load balancer %s not found - assuming already deleted", lbArn)
				continue
			}
			return fmt.Errorf("delete load balancer %s: %w", lbArn, err)
		}
		log.Printf("Successfully initiated deletion of Application/Network Load Balancer %s", lbArn)
		totalLBsDeleted++
	}

	if totalLBsDeleted == 0 {
		log.Printf("No load balancers found in VPC %s", vpcID)
		return nil
	}

	// Wait for load balancers to start deleting
	// This is critical - service-managed ENIs won't be cleaned up until LBs are deleting
	log.Printf("Waiting 30 seconds for %d load balancer(s) to start deleting and ENIs to be cleaned up", totalLBsDeleted)
	c.sleep(30 * time.Second)

	return nil
}
