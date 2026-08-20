package cleanup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ectypes "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbv1types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

// ---- mocks -----------------------------------------------------------------

// mockEC2 implements ec2API. Every method records its call; describe methods
// return an optional hooked output (empty by default), delete/modify methods
// return an optional hooked error (nil by default). This lets each test wire up
// only the calls it cares about.
type mockEC2 struct {
	calls []string

	vpcEndpoints []ectypes.VpcEndpoint
	natGateways  []ectypes.NatGateway
	reservations []ectypes.Reservation
	enis         []ectypes.NetworkInterface
	eigws        []ectypes.EgressOnlyInternetGateway
	igws         []ectypes.InternetGateway
	nacls        []ectypes.NetworkAcl
	routeTables  []ectypes.RouteTable
	subnets      []ectypes.Subnet
	sgs          []ectypes.SecurityGroup

	// describeInstances is called first by the cleaner, then repeatedly by the
	// termination waiter. describeInstancesFn lets a test vary output per call.
	describeInstancesFn func(n int) (*ec2.DescribeInstancesOutput, error)
	descInstancesCalls  int

	// errs maps a method name to an error it should return.
	errs map[string]error
}

func (m *mockEC2) rec(name string) { m.calls = append(m.calls, name) }
func (m *mockEC2) err(name string) error {
	if m.errs == nil {
		return nil
	}
	return m.errs[name]
}

func (m *mockEC2) DescribeVpcEndpoints(_ context.Context, _ *ec2.DescribeVpcEndpointsInput, _ ...func(*ec2.Options)) (*ec2.DescribeVpcEndpointsOutput, error) {
	m.rec("DescribeVpcEndpoints")
	return &ec2.DescribeVpcEndpointsOutput{VpcEndpoints: m.vpcEndpoints}, m.err("DescribeVpcEndpoints")
}
func (m *mockEC2) DeleteVpcEndpoints(_ context.Context, in *ec2.DeleteVpcEndpointsInput, _ ...func(*ec2.Options)) (*ec2.DeleteVpcEndpointsOutput, error) {
	m.rec("DeleteVpcEndpoints:" + strings.Join(in.VpcEndpointIds, ","))
	return nil, m.err("DeleteVpcEndpoints")
}
func (m *mockEC2) DescribeNatGateways(_ context.Context, _ *ec2.DescribeNatGatewaysInput, _ ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
	m.rec("DescribeNatGateways")
	return &ec2.DescribeNatGatewaysOutput{NatGateways: m.natGateways}, m.err("DescribeNatGateways")
}
func (m *mockEC2) DeleteNatGateway(_ context.Context, in *ec2.DeleteNatGatewayInput, _ ...func(*ec2.Options)) (*ec2.DeleteNatGatewayOutput, error) {
	m.rec("DeleteNatGateway:" + aws.ToString(in.NatGatewayId))
	return nil, m.err("DeleteNatGateway")
}
func (m *mockEC2) DescribeInstances(_ context.Context, _ *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	m.rec("DescribeInstances")
	m.descInstancesCalls++
	if m.describeInstancesFn != nil {
		return m.describeInstancesFn(m.descInstancesCalls)
	}
	return &ec2.DescribeInstancesOutput{Reservations: m.reservations}, m.err("DescribeInstances")
}
func (m *mockEC2) TerminateInstances(_ context.Context, in *ec2.TerminateInstancesInput, _ ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error) {
	m.rec("TerminateInstances:" + strings.Join(in.InstanceIds, ","))
	return nil, m.err("TerminateInstances")
}
func (m *mockEC2) DescribeNetworkInterfaces(_ context.Context, _ *ec2.DescribeNetworkInterfacesInput, _ ...func(*ec2.Options)) (*ec2.DescribeNetworkInterfacesOutput, error) {
	m.rec("DescribeNetworkInterfaces")
	return &ec2.DescribeNetworkInterfacesOutput{NetworkInterfaces: m.enis}, m.err("DescribeNetworkInterfaces")
}
func (m *mockEC2) DetachNetworkInterface(_ context.Context, in *ec2.DetachNetworkInterfaceInput, _ ...func(*ec2.Options)) (*ec2.DetachNetworkInterfaceOutput, error) {
	m.rec("DetachNetworkInterface:" + aws.ToString(in.AttachmentId))
	return nil, m.err("DetachNetworkInterface")
}
func (m *mockEC2) DeleteNetworkInterface(_ context.Context, in *ec2.DeleteNetworkInterfaceInput, _ ...func(*ec2.Options)) (*ec2.DeleteNetworkInterfaceOutput, error) {
	m.rec("DeleteNetworkInterface:" + aws.ToString(in.NetworkInterfaceId))
	return nil, m.err("DeleteNetworkInterface")
}
func (m *mockEC2) DescribeEgressOnlyInternetGateways(_ context.Context, _ *ec2.DescribeEgressOnlyInternetGatewaysInput, _ ...func(*ec2.Options)) (*ec2.DescribeEgressOnlyInternetGatewaysOutput, error) {
	m.rec("DescribeEgressOnlyInternetGateways")
	return &ec2.DescribeEgressOnlyInternetGatewaysOutput{EgressOnlyInternetGateways: m.eigws}, m.err("DescribeEgressOnlyInternetGateways")
}
func (m *mockEC2) DeleteEgressOnlyInternetGateway(_ context.Context, in *ec2.DeleteEgressOnlyInternetGatewayInput, _ ...func(*ec2.Options)) (*ec2.DeleteEgressOnlyInternetGatewayOutput, error) {
	m.rec("DeleteEgressOnlyInternetGateway:" + aws.ToString(in.EgressOnlyInternetGatewayId))
	return nil, m.err("DeleteEgressOnlyInternetGateway")
}
func (m *mockEC2) DescribeInternetGateways(_ context.Context, _ *ec2.DescribeInternetGatewaysInput, _ ...func(*ec2.Options)) (*ec2.DescribeInternetGatewaysOutput, error) {
	m.rec("DescribeInternetGateways")
	return &ec2.DescribeInternetGatewaysOutput{InternetGateways: m.igws}, m.err("DescribeInternetGateways")
}
func (m *mockEC2) DetachInternetGateway(_ context.Context, in *ec2.DetachInternetGatewayInput, _ ...func(*ec2.Options)) (*ec2.DetachInternetGatewayOutput, error) {
	m.rec("DetachInternetGateway:" + aws.ToString(in.InternetGatewayId))
	return nil, m.err("DetachInternetGateway")
}
func (m *mockEC2) DeleteInternetGateway(_ context.Context, in *ec2.DeleteInternetGatewayInput, _ ...func(*ec2.Options)) (*ec2.DeleteInternetGatewayOutput, error) {
	m.rec("DeleteInternetGateway:" + aws.ToString(in.InternetGatewayId))
	return nil, m.err("DeleteInternetGateway")
}
func (m *mockEC2) DescribeNetworkAcls(_ context.Context, _ *ec2.DescribeNetworkAclsInput, _ ...func(*ec2.Options)) (*ec2.DescribeNetworkAclsOutput, error) {
	m.rec("DescribeNetworkAcls")
	return &ec2.DescribeNetworkAclsOutput{NetworkAcls: m.nacls}, m.err("DescribeNetworkAcls")
}
func (m *mockEC2) DeleteNetworkAcl(_ context.Context, in *ec2.DeleteNetworkAclInput, _ ...func(*ec2.Options)) (*ec2.DeleteNetworkAclOutput, error) {
	m.rec("DeleteNetworkAcl:" + aws.ToString(in.NetworkAclId))
	return nil, m.err("DeleteNetworkAcl")
}
func (m *mockEC2) DescribeRouteTables(_ context.Context, _ *ec2.DescribeRouteTablesInput, _ ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error) {
	m.rec("DescribeRouteTables")
	return &ec2.DescribeRouteTablesOutput{RouteTables: m.routeTables}, m.err("DescribeRouteTables")
}
func (m *mockEC2) DisassociateRouteTable(_ context.Context, in *ec2.DisassociateRouteTableInput, _ ...func(*ec2.Options)) (*ec2.DisassociateRouteTableOutput, error) {
	m.rec("DisassociateRouteTable:" + aws.ToString(in.AssociationId))
	return nil, m.err("DisassociateRouteTable")
}
func (m *mockEC2) DeleteRouteTable(_ context.Context, in *ec2.DeleteRouteTableInput, _ ...func(*ec2.Options)) (*ec2.DeleteRouteTableOutput, error) {
	m.rec("DeleteRouteTable:" + aws.ToString(in.RouteTableId))
	return nil, m.err("DeleteRouteTable")
}
func (m *mockEC2) DescribeSubnets(_ context.Context, _ *ec2.DescribeSubnetsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	m.rec("DescribeSubnets")
	return &ec2.DescribeSubnetsOutput{Subnets: m.subnets}, m.err("DescribeSubnets")
}
func (m *mockEC2) DeleteSubnet(_ context.Context, in *ec2.DeleteSubnetInput, _ ...func(*ec2.Options)) (*ec2.DeleteSubnetOutput, error) {
	m.rec("DeleteSubnet:" + aws.ToString(in.SubnetId))
	return nil, m.err("DeleteSubnet")
}
func (m *mockEC2) DescribeSecurityGroups(_ context.Context, _ *ec2.DescribeSecurityGroupsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
	m.rec("DescribeSecurityGroups")
	return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: m.sgs}, m.err("DescribeSecurityGroups")
}
func (m *mockEC2) RevokeSecurityGroupIngress(_ context.Context, in *ec2.RevokeSecurityGroupIngressInput, _ ...func(*ec2.Options)) (*ec2.RevokeSecurityGroupIngressOutput, error) {
	m.rec("RevokeSecurityGroupIngress:" + aws.ToString(in.GroupId))
	return nil, m.err("RevokeSecurityGroupIngress")
}
func (m *mockEC2) RevokeSecurityGroupEgress(_ context.Context, in *ec2.RevokeSecurityGroupEgressInput, _ ...func(*ec2.Options)) (*ec2.RevokeSecurityGroupEgressOutput, error) {
	m.rec("RevokeSecurityGroupEgress:" + aws.ToString(in.GroupId))
	return nil, m.err("RevokeSecurityGroupEgress")
}
func (m *mockEC2) DeleteSecurityGroup(_ context.Context, in *ec2.DeleteSecurityGroupInput, _ ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
	m.rec("DeleteSecurityGroup:" + aws.ToString(in.GroupId))
	return nil, m.err("DeleteSecurityGroup")
}
func (m *mockEC2) DeleteVpc(_ context.Context, in *ec2.DeleteVpcInput, _ ...func(*ec2.Options)) (*ec2.DeleteVpcOutput, error) {
	m.rec("DeleteVpc:" + aws.ToString(in.VpcId))
	return nil, m.err("DeleteVpc")
}

type mockELBv1 struct {
	lbs  []elbv1types.LoadBalancerDescription
	errs map[string]error
}

func (m *mockELBv1) DescribeLoadBalancers(_ context.Context, _ *elasticloadbalancing.DescribeLoadBalancersInput, _ ...func(*elasticloadbalancing.Options)) (*elasticloadbalancing.DescribeLoadBalancersOutput, error) {
	return &elasticloadbalancing.DescribeLoadBalancersOutput{LoadBalancerDescriptions: m.lbs}, m.errs["Describe"]
}
func (m *mockELBv1) DeleteLoadBalancer(_ context.Context, _ *elasticloadbalancing.DeleteLoadBalancerInput, _ ...func(*elasticloadbalancing.Options)) (*elasticloadbalancing.DeleteLoadBalancerOutput, error) {
	return nil, m.errs["Delete"]
}

type mockELBv2 struct {
	lbs  []elbv2types.LoadBalancer
	errs map[string]error
}

func (m *mockELBv2) DescribeLoadBalancers(_ context.Context, _ *elasticloadbalancingv2.DescribeLoadBalancersInput, _ ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeLoadBalancersOutput, error) {
	return &elasticloadbalancingv2.DescribeLoadBalancersOutput{LoadBalancers: m.lbs}, m.errs["Describe"]
}
func (m *mockELBv2) DeleteLoadBalancer(_ context.Context, _ *elasticloadbalancingv2.DeleteLoadBalancerInput, _ ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DeleteLoadBalancerOutput, error) {
	return nil, m.errs["Delete"]
}

// newCleaner wires a cleaner with no-op sleep so tests never block on the
// real 20s/30s waits.
func newCleaner(ec2m *mockEC2, v1 *mockELBv1, v2 *mockELBv2) *vpcCleaner {
	if v1 == nil {
		v1 = &mockELBv1{}
	}
	if v2 == nil {
		v2 = &mockELBv2{}
	}
	return &vpcCleaner{
		ec2:    ec2m,
		elbv1:  v1,
		elbv2:  v2,
		region: "us-east-1",
		sleep:  func(time.Duration) {},
	}
}

func hasCall(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}

// ---- pure helpers ----------------------------------------------------------

func TestIsNotFoundOrDependencyViolation(t *testing.T) {
	benign := []string{
		"InvalidVpcID.NotFound: no such vpc",
		"operation error: DependencyViolation: still in use",
		"InvalidSubnetID.NotFound",
		"InvalidGroup.NotFound",
		"AuthFailure",
		"resource is currently in use by something",
	}
	for _, s := range benign {
		if !isNotFoundOrDependencyViolation(errors.New(s)) {
			t.Errorf("expected %q to be treated as benign", s)
		}
	}

	if isNotFoundOrDependencyViolation(nil) {
		t.Error("nil error must not be benign")
	}
	if isNotFoundOrDependencyViolation(errors.New("UnauthorizedOperation: nope")) {
		t.Error("a real error must not be treated as benign")
	}
}

func TestIgnoreBenignError(t *testing.T) {
	if err := ignoreBenignError(nil); err != nil {
		t.Errorf("nil in -> nil out, got %v", err)
	}
	if err := ignoreBenignError(errors.New("DependencyViolation")); err != nil {
		t.Errorf("benign error should be swallowed, got %v", err)
	}
	real := errors.New("RequestLimitExceeded")
	if err := ignoreBenignError(real); err != real {
		t.Errorf("real error should pass through unchanged, got %v", err)
	}
}

func TestIsServiceManagedENI(t *testing.T) {
	tests := []struct {
		name string
		eni  ectypes.NetworkInterface
		want bool
	}{
		{"requester-managed flag", ectypes.NetworkInterface{RequesterManaged: aws.Bool(true)}, true},
		{"nat_gateway type", ectypes.NetworkInterface{InterfaceType: ectypes.NetworkInterfaceType("nat_gateway")}, true},
		{"load_balancer type", ectypes.NetworkInterface{InterfaceType: ectypes.NetworkInterfaceType("load_balancer")}, true},
		{"lambda type", ectypes.NetworkInterface{InterfaceType: ectypes.NetworkInterfaceType("lambda")}, true},
		{"plain interface", ectypes.NetworkInterface{InterfaceType: ectypes.NetworkInterfaceType("interface")}, false},
		{"empty", ectypes.NetworkInterface{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eni := tt.eni
			if got := isServiceManagedENI(&eni); got != tt.want {
				t.Errorf("isServiceManagedENI = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---- run() ordering & happy path ------------------------------------------

func TestRun_EmptyVPC_DeletesInOrder(t *testing.T) {
	m := &mockEC2{}
	c := newCleaner(m, nil, nil)

	if err := c.run(context.Background(), "vpc-123"); err != nil {
		t.Fatalf("run: %v", err)
	}

	// The describe calls establish the teardown ordering: endpoints -> LBs (no
	// EC2 describe) -> NAT -> instances -> ENIs -> ... -> subnets -> SGs -> VPC.
	wantOrder := []string{
		"DescribeVpcEndpoints",
		"DescribeNatGateways",
		"DescribeInstances",
		"DescribeNetworkInterfaces",
		"DescribeEgressOnlyInternetGateways",
		"DescribeInternetGateways",
		"DescribeNetworkAcls",
		"DescribeRouteTables",
		"DescribeSubnets",
		"DescribeSecurityGroups",
		"DeleteVpc:vpc-123",
	}
	// Filter m.calls down to the ones we assert on, preserving order.
	var got []string
	wanted := map[string]bool{}
	for _, w := range wantOrder {
		wanted[w] = true
	}
	for _, c := range m.calls {
		if wanted[c] {
			got = append(got, c)
		}
	}
	if len(got) != len(wantOrder) {
		t.Fatalf("got calls %v, want %v", got, wantOrder)
	}
	for i := range wantOrder {
		if got[i] != wantOrder[i] {
			t.Fatalf("call[%d] = %s, want %s (full: %v)", i, got[i], wantOrder[i], got)
		}
	}
}

// ---- safety: skip protected/default resources -----------------------------

func TestDeleteNetworkInterfaces_SkipsServiceManaged(t *testing.T) {
	m := &mockEC2{
		enis: []ectypes.NetworkInterface{
			{NetworkInterfaceId: aws.String("eni-managed"), RequesterManaged: aws.Bool(true)},
			{NetworkInterfaceId: aws.String("eni-user")},
		},
	}
	c := newCleaner(m, nil, nil)
	if err := c.deleteNetworkInterfaces(context.Background(), "vpc-1"); err != nil {
		t.Fatalf("deleteNetworkInterfaces: %v", err)
	}
	if hasCall(m.calls, "DeleteNetworkInterface:eni-managed") {
		t.Error("service-managed ENI must NOT be deleted")
	}
	if !hasCall(m.calls, "DeleteNetworkInterface:eni-user") {
		t.Error("user ENI should be deleted")
	}
}

func TestDeleteNetworkInterfaces_DetachesAttached(t *testing.T) {
	m := &mockEC2{
		enis: []ectypes.NetworkInterface{
			{
				NetworkInterfaceId: aws.String("eni-att"),
				Attachment:         &ectypes.NetworkInterfaceAttachment{AttachmentId: aws.String("attach-1")},
			},
		},
	}
	c := newCleaner(m, nil, nil)
	if err := c.deleteNetworkInterfaces(context.Background(), "vpc-1"); err != nil {
		t.Fatalf("deleteNetworkInterfaces: %v", err)
	}
	if !hasCall(m.calls, "DetachNetworkInterface:attach-1") {
		t.Error("attached ENI should be detached first")
	}
	if !hasCall(m.calls, "DeleteNetworkInterface:eni-att") {
		t.Error("attached ENI should then be deleted")
	}
}

func TestDeleteSecurityGroups_SkipsDefault(t *testing.T) {
	m := &mockEC2{
		sgs: []ectypes.SecurityGroup{
			{GroupId: aws.String("sg-default"), GroupName: aws.String("default")},
			{GroupId: aws.String("sg-custom"), GroupName: aws.String("web"),
				IpPermissions: []ectypes.IpPermission{{}}},
		},
	}
	c := newCleaner(m, nil, nil)
	if err := c.deleteSecurityGroups(context.Background(), "vpc-1"); err != nil {
		t.Fatalf("deleteSecurityGroups: %v", err)
	}
	if hasCall(m.calls, "DeleteSecurityGroup:sg-default") {
		t.Error("default security group must NOT be deleted")
	}
	if !hasCall(m.calls, "DeleteSecurityGroup:sg-custom") {
		t.Error("custom security group should be deleted")
	}
	if !hasCall(m.calls, "RevokeSecurityGroupIngress:sg-custom") {
		t.Error("custom SG ingress rules should be revoked to break circular refs")
	}
}

func TestDeleteNetworkACLs_SkipsDefault(t *testing.T) {
	m := &mockEC2{
		nacls: []ectypes.NetworkAcl{
			{NetworkAclId: aws.String("acl-default"), IsDefault: aws.Bool(true)},
			{NetworkAclId: aws.String("acl-custom"), IsDefault: aws.Bool(false)},
		},
	}
	c := newCleaner(m, nil, nil)
	if err := c.deleteNetworkACLs(context.Background(), "vpc-1"); err != nil {
		t.Fatalf("deleteNetworkACLs: %v", err)
	}
	if hasCall(m.calls, "DeleteNetworkAcl:acl-default") {
		t.Error("default network ACL must NOT be deleted")
	}
	if !hasCall(m.calls, "DeleteNetworkAcl:acl-custom") {
		t.Error("custom network ACL should be deleted")
	}
}

func TestDeleteRouteTables_SkipsMainAndDisassociates(t *testing.T) {
	m := &mockEC2{
		routeTables: []ectypes.RouteTable{
			{
				RouteTableId: aws.String("rtb-main"),
				Associations: []ectypes.RouteTableAssociation{{Main: aws.Bool(true)}},
			},
			{
				RouteTableId: aws.String("rtb-custom"),
				Associations: []ectypes.RouteTableAssociation{
					{Main: aws.Bool(false), RouteTableAssociationId: aws.String("rtbassoc-1")},
				},
			},
		},
	}
	c := newCleaner(m, nil, nil)
	if err := c.deleteRouteTables(context.Background(), "vpc-1"); err != nil {
		t.Fatalf("deleteRouteTables: %v", err)
	}
	if hasCall(m.calls, "DeleteRouteTable:rtb-main") {
		t.Error("main route table must NOT be deleted")
	}
	if !hasCall(m.calls, "DisassociateRouteTable:rtbassoc-1") {
		t.Error("custom route table association should be disassociated first")
	}
	if !hasCall(m.calls, "DeleteRouteTable:rtb-custom") {
		t.Error("custom route table should be deleted")
	}
}

func TestDeleteNATGateways_SkipsAlreadyDeleting(t *testing.T) {
	m := &mockEC2{
		natGateways: []ectypes.NatGateway{
			{NatGatewayId: aws.String("nat-active"), State: ectypes.NatGatewayStateAvailable},
			{NatGatewayId: aws.String("nat-gone"), State: ectypes.NatGatewayStateDeleting},
		},
	}
	c := newCleaner(m, nil, nil)
	if err := c.deleteNATGateways(context.Background(), "vpc-1"); err != nil {
		t.Fatalf("deleteNATGateways: %v", err)
	}
	if !hasCall(m.calls, "DeleteNatGateway:nat-active") {
		t.Error("active NAT gateway should be deleted")
	}
	if hasCall(m.calls, "DeleteNatGateway:nat-gone") {
		t.Error("already-deleting NAT gateway must NOT be deleted again")
	}
}

// ---- instances / waiter ----------------------------------------------------

func TestTerminateEC2Instances_SkipsTerminated(t *testing.T) {
	m := &mockEC2{
		reservations: []ectypes.Reservation{{
			Instances: []ectypes.Instance{
				{InstanceId: aws.String("i-gone"), State: &ectypes.InstanceState{Name: ectypes.InstanceStateNameTerminated}},
			},
		}},
	}
	c := newCleaner(m, nil, nil)
	if err := c.terminateEC2Instances(context.Background(), "vpc-1"); err != nil {
		t.Fatalf("terminateEC2Instances: %v", err)
	}
	for _, call := range m.calls {
		if strings.HasPrefix(call, "TerminateInstances") {
			t.Errorf("already-terminated instances must not trigger TerminateInstances, got %q", call)
		}
	}
}

func TestTerminateEC2Instances_TerminatesRunning(t *testing.T) {
	// First DescribeInstances (the cleaner's own listing) returns a running
	// instance; the waiter's subsequent polls report it terminated so Wait
	// returns immediately without any real sleep.
	m := &mockEC2{
		describeInstancesFn: func(n int) (*ec2.DescribeInstancesOutput, error) {
			state := ectypes.InstanceStateNameRunning
			if n > 1 {
				state = ectypes.InstanceStateNameTerminated
			}
			return &ec2.DescribeInstancesOutput{Reservations: []ectypes.Reservation{{
				Instances: []ectypes.Instance{
					{InstanceId: aws.String("i-run"), State: &ectypes.InstanceState{Name: state}},
				},
			}}}, nil
		},
	}
	c := newCleaner(m, nil, nil)
	if err := c.terminateEC2Instances(context.Background(), "vpc-1"); err != nil {
		t.Fatalf("terminateEC2Instances: %v", err)
	}
	if !hasCall(m.calls, "TerminateInstances:i-run") {
		t.Errorf("running instance should be terminated, calls=%v", m.calls)
	}
}

// ---- load balancers --------------------------------------------------------

func TestDeleteLoadBalancers_FiltersByVPCAndTolerates(t *testing.T) {
	m := &mockEC2{}
	v1 := &mockELBv1{lbs: []elbv1types.LoadBalancerDescription{
		{LoadBalancerName: aws.String("classic-here"), VPCId: aws.String("vpc-1")},
		{LoadBalancerName: aws.String("classic-other"), VPCId: aws.String("vpc-2")},
	}}
	v2 := &mockELBv2{lbs: []elbv2types.LoadBalancer{
		{LoadBalancerArn: aws.String("arn-here"), LoadBalancerName: aws.String("alb"), VpcId: aws.String("vpc-1")},
		{LoadBalancerArn: aws.String("arn-other"), LoadBalancerName: aws.String("alb2"), VpcId: aws.String("vpc-9")},
	}}
	c := newCleaner(m, v1, v2)
	if err := c.deleteLoadBalancers(context.Background(), "vpc-1"); err != nil {
		t.Fatalf("deleteLoadBalancers: %v", err)
	}
}

func TestDeleteLoadBalancers_NotFoundTolerated(t *testing.T) {
	v1 := &mockELBv1{
		lbs:  []elbv1types.LoadBalancerDescription{{LoadBalancerName: aws.String("lb"), VPCId: aws.String("vpc-1")}},
		errs: map[string]error{"Delete": errors.New("LoadBalancerNotFound: already gone")},
	}
	c := newCleaner(&mockEC2{}, v1, nil)
	if err := c.deleteLoadBalancers(context.Background(), "vpc-1"); err != nil {
		t.Fatalf("LoadBalancerNotFound on delete should be tolerated, got %v", err)
	}
}

func TestDeleteLoadBalancers_DescribeErrorPropagates(t *testing.T) {
	v1 := &mockELBv1{errs: map[string]error{"Describe": errors.New("AccessDenied")}}
	c := newCleaner(&mockEC2{}, v1, nil)
	err := c.deleteLoadBalancers(context.Background(), "vpc-1")
	if err == nil || !strings.Contains(err.Error(), "describe classic load balancers") {
		t.Fatalf("expected describe error to propagate, got %v", err)
	}
}

// ---- error handling in run() ----------------------------------------------

func TestRun_BenignDeleteErrorTolerated(t *testing.T) {
	m := &mockEC2{
		subnets: []ectypes.Subnet{{SubnetId: aws.String("subnet-1")}},
		errs:    map[string]error{"DeleteSubnet": errors.New("DependencyViolation: not yet")},
	}
	c := newCleaner(m, nil, nil)
	if err := c.run(context.Background(), "vpc-1"); err != nil {
		t.Fatalf("benign delete error should not fail the run, got %v", err)
	}
}

func TestRun_RealDescribeErrorFailsStep(t *testing.T) {
	m := &mockEC2{errs: map[string]error{"DescribeSubnets": errors.New("RequestLimitExceeded")}}
	c := newCleaner(m, nil, nil)
	err := c.run(context.Background(), "vpc-1")
	if err == nil || !strings.Contains(err.Error(), "subnets:") {
		t.Fatalf("expected subnets step error, got %v", err)
	}
}

func TestDeleteVPC_DependencyViolationGivesGuidance(t *testing.T) {
	m := &mockEC2{errs: map[string]error{"DeleteVpc": errors.New("DependencyViolation: ENIs remain")}}
	c := newCleaner(m, nil, nil)
	err := c.deleteVPC(context.Background(), "vpc-1")
	if err == nil || !strings.Contains(err.Error(), "remaining dependencies") {
		t.Fatalf("expected dependency guidance error, got %v", err)
	}
}

func TestDeleteVPC_NotFoundIsSuccess(t *testing.T) {
	m := &mockEC2{errs: map[string]error{"DeleteVpc": errors.New("InvalidVpcID.NotFound")}}
	c := newCleaner(m, nil, nil)
	if err := c.deleteVPC(context.Background(), "vpc-1"); err != nil {
		t.Fatalf("NotFound on VPC delete should be treated as success, got %v", err)
	}
}
