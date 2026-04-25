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

// DeleteVPCAndDependencies deletes a VPC and all its dependent resources in the correct order
func DeleteVPCAndDependencies(ctx context.Context, ec2Client *ec2.Client, vpcID string) error {
	// Get AWS region from the EC2 client config
	region := ec2Client.Options().Region

	// Deletion order matters - delete higher-level dependencies first
	steps := []struct {
		name string
		fn   func(context.Context, *ec2.Client, string) error
	}{
		{"VPC endpoints", deleteVPCEndpoints},
		{"load balancers", func(ctx context.Context, _ *ec2.Client, vpcID string) error {
			return deleteLoadBalancers(ctx, region, vpcID)
		}},
		{"NAT gateways", deleteNATGateways},
		{"network interfaces", deleteNetworkInterfaces},
		{"egress-only internet gateways", deleteEgressOnlyInternetGateways},
		{"internet gateways", detachAndDeleteInternetGateways},
		{"network ACLs", deleteNetworkACLs},
		{"non-main route tables", deleteRouteTables},
		{"subnets", deleteSubnets},
		{"non-default security groups", deleteSecurityGroups},
		{"VPC", deleteVPC},
	}

	for _, step := range steps {
		log.Printf("==> Deleting %s for VPC %s", step.name, vpcID)
		if err := step.fn(ctx, ec2Client, vpcID); err != nil {
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

func deleteVPCEndpoints(ctx context.Context, ec2Client *ec2.Client, vpcID string) error {
	out, err := ec2Client.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{
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
	_, err = ec2Client.DeleteVpcEndpoints(ctx, &ec2.DeleteVpcEndpointsInput{VpcEndpointIds: ids})
	return ignoreBenignError(err)
}

func deleteNATGateways(ctx context.Context, ec2Client *ec2.Client, vpcID string) error {
	out, err := ec2Client.DescribeNatGateways(ctx, &ec2.DescribeNatGatewaysInput{
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
		_, err := ec2Client.DeleteNatGateway(ctx, &ec2.DeleteNatGatewayInput{NatGatewayId: aws.String(id)})
		if err := ignoreBenignError(err); err != nil {
			return err
		}
	}

	// Wait briefly for NAT gateways to start deleting
	if len(out.NatGateways) > 0 {
		log.Printf("Waiting 20 seconds for NAT gateways to leave active state")
		time.Sleep(20 * time.Second)
	}
	return nil
}

func deleteNetworkInterfaces(ctx context.Context, ec2Client *ec2.Client, vpcID string) error {
	out, err := ec2Client.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{
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

		// Detach if attached
		if eni.Attachment != nil && aws.ToString(eni.Attachment.AttachmentId) != "" {
			attachID := aws.ToString(eni.Attachment.AttachmentId)
			log.Printf("Detaching ENI %s (attachment %s)", id, attachID)
			_, err := ec2Client.DetachNetworkInterface(ctx, &ec2.DetachNetworkInterfaceInput{
				AttachmentId: aws.String(attachID),
				Force:        aws.Bool(true),
			})
			if err := ignoreBenignError(err); err != nil {
				return err
			}
			time.Sleep(5 * time.Second)
		}

		log.Printf("Deleting ENI %s", id)
		_, err := ec2Client.DeleteNetworkInterface(ctx, &ec2.DeleteNetworkInterfaceInput{NetworkInterfaceId: aws.String(id)})
		if err := ignoreBenignError(err); err != nil {
			return err
		}
	}
	return nil
}

func deleteEgressOnlyInternetGateways(ctx context.Context, ec2Client *ec2.Client, vpcID string) error {
	out, err := ec2Client.DescribeEgressOnlyInternetGateways(ctx, &ec2.DescribeEgressOnlyInternetGatewaysInput{
		Filters: []types.Filter{{Name: aws.String("attachment.vpc-id"), Values: []string{vpcID}}},
	})
	if err != nil {
		return err
	}
	for _, gw := range out.EgressOnlyInternetGateways {
		id := aws.ToString(gw.EgressOnlyInternetGatewayId)
		log.Printf("Deleting egress-only internet gateway %s", id)
		_, err := ec2Client.DeleteEgressOnlyInternetGateway(ctx, &ec2.DeleteEgressOnlyInternetGatewayInput{EgressOnlyInternetGatewayId: aws.String(id)})
		if err := ignoreBenignError(err); err != nil {
			return err
		}
	}
	return nil
}

func detachAndDeleteInternetGateways(ctx context.Context, ec2Client *ec2.Client, vpcID string) error {
	out, err := ec2Client.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{
		Filters: []types.Filter{{Name: aws.String("attachment.vpc-id"), Values: []string{vpcID}}},
	})
	if err != nil {
		return err
	}
	for _, igw := range out.InternetGateways {
		id := aws.ToString(igw.InternetGatewayId)
		log.Printf("Detaching internet gateway %s", id)
		_, err := ec2Client.DetachInternetGateway(ctx, &ec2.DetachInternetGatewayInput{
			InternetGatewayId: aws.String(id),
			VpcId:             aws.String(vpcID),
		})
		if err := ignoreBenignError(err); err != nil {
			return err
		}
		log.Printf("Deleting internet gateway %s", id)
		_, err = ec2Client.DeleteInternetGateway(ctx, &ec2.DeleteInternetGatewayInput{InternetGatewayId: aws.String(id)})
		if err := ignoreBenignError(err); err != nil {
			return err
		}
	}
	return nil
}

func deleteNetworkACLs(ctx context.Context, ec2Client *ec2.Client, vpcID string) error {
	out, err := ec2Client.DescribeNetworkAcls(ctx, &ec2.DescribeNetworkAclsInput{
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
		_, err := ec2Client.DeleteNetworkAcl(ctx, &ec2.DeleteNetworkAclInput{NetworkAclId: aws.String(id)})
		if err := ignoreBenignError(err); err != nil {
			return err
		}
	}
	return nil
}

func deleteRouteTables(ctx context.Context, ec2Client *ec2.Client, vpcID string) error {
	out, err := ec2Client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
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
			_, err := ec2Client.DisassociateRouteTable(ctx, &ec2.DisassociateRouteTableInput{AssociationId: aws.String(assocID)})
			if err := ignoreBenignError(err); err != nil {
				return err
			}
		}

		log.Printf("Deleting route table %s", id)
		_, err := ec2Client.DeleteRouteTable(ctx, &ec2.DeleteRouteTableInput{RouteTableId: aws.String(id)})
		if err := ignoreBenignError(err); err != nil {
			return err
		}
	}
	return nil
}

func deleteSubnets(ctx context.Context, ec2Client *ec2.Client, vpcID string) error {
	out, err := ec2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		Filters: []types.Filter{{Name: aws.String("vpc-id"), Values: []string{vpcID}}},
	})
	if err != nil {
		return err
	}
	for _, subnet := range out.Subnets {
		id := aws.ToString(subnet.SubnetId)
		log.Printf("Deleting subnet %s", id)
		_, err := ec2Client.DeleteSubnet(ctx, &ec2.DeleteSubnetInput{SubnetId: aws.String(id)})
		if err := ignoreBenignError(err); err != nil {
			return err
		}
	}
	return nil
}

func deleteSecurityGroups(ctx context.Context, ec2Client *ec2.Client, vpcID string) error {
	out, err := ec2Client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
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
			_, err := ec2Client.RevokeSecurityGroupIngress(ctx, &ec2.RevokeSecurityGroupIngressInput{
				GroupId:       aws.String(id),
				IpPermissions: sg.IpPermissions,
			})
			if err := ignoreBenignError(err); err != nil {
				return err
			}
		}
		if len(sg.IpPermissionsEgress) > 0 {
			log.Printf("Revoking egress rules for security group %s", id)
			_, err := ec2Client.RevokeSecurityGroupEgress(ctx, &ec2.RevokeSecurityGroupEgressInput{
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
		_, err := ec2Client.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{GroupId: aws.String(id)})
		if err := ignoreBenignError(err); err != nil {
			return err
		}
	}
	return nil
}

func deleteVPC(ctx context.Context, ec2Client *ec2.Client, vpcID string) error {
	log.Printf("Deleting VPC %s", vpcID)
	_, err := ec2Client.DeleteVpc(ctx, &ec2.DeleteVpcInput{VpcId: aws.String(vpcID)})
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

func deleteLoadBalancers(ctx context.Context, region, vpcID string) error {
	// Load AWS config for the ELB clients
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	totalLBsDeleted := 0

	// ===== Check for Classic Load Balancers (ELBv1) =====
	elbv1Client := elasticloadbalancing.NewFromConfig(cfg)
	classicLBsResult, err := elbv1Client.DescribeLoadBalancers(ctx, &elasticloadbalancing.DescribeLoadBalancersInput{})
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
		_, err := elbv1Client.DeleteLoadBalancer(ctx, &elasticloadbalancing.DeleteLoadBalancerInput{
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
	elbv2Client := elasticloadbalancingv2.NewFromConfig(cfg)
	elbv2Result, err := elbv2Client.DescribeLoadBalancers(ctx, &elasticloadbalancingv2.DescribeLoadBalancersInput{})
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
		_, err := elbv2Client.DeleteLoadBalancer(ctx, &elasticloadbalancingv2.DeleteLoadBalancerInput{
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
	time.Sleep(30 * time.Second)

	return nil
}
