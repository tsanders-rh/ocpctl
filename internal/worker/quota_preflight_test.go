package worker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/servicequotas"
	sqtypes "github.com/aws/aws-sdk-go-v2/service/servicequotas/types"
	"github.com/tsanders-rh/ocpctl/internal/profile"
)

// --- mocks ---

type mockEC2 struct {
	endpoints  int
	vpcs       int
	eips       int
	instances  map[string]int   // instance type -> running count
	vcpuByType map[string]int32 // instance type -> default vCPUs

	endpointsErr error
	vpcsErr      error
	eipsErr      error
	instancesErr error
	instTypesErr error
}

func (m *mockEC2) DescribeVpcEndpoints(_ context.Context, _ *ec2.DescribeVpcEndpointsInput, _ ...func(*ec2.Options)) (*ec2.DescribeVpcEndpointsOutput, error) {
	if m.endpointsErr != nil {
		return nil, m.endpointsErr
	}
	return &ec2.DescribeVpcEndpointsOutput{VpcEndpoints: make([]ec2types.VpcEndpoint, m.endpoints)}, nil
}

func (m *mockEC2) DescribeVpcs(_ context.Context, _ *ec2.DescribeVpcsInput, _ ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
	if m.vpcsErr != nil {
		return nil, m.vpcsErr
	}
	return &ec2.DescribeVpcsOutput{Vpcs: make([]ec2types.Vpc, m.vpcs)}, nil
}

func (m *mockEC2) DescribeAddresses(_ context.Context, _ *ec2.DescribeAddressesInput, _ ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
	if m.eipsErr != nil {
		return nil, m.eipsErr
	}
	return &ec2.DescribeAddressesOutput{Addresses: make([]ec2types.Address, m.eips)}, nil
}

func (m *mockEC2) DescribeInstances(_ context.Context, _ *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	if m.instancesErr != nil {
		return nil, m.instancesErr
	}
	var insts []ec2types.Instance
	for t, n := range m.instances {
		for i := 0; i < n; i++ {
			insts = append(insts, ec2types.Instance{InstanceType: ec2types.InstanceType(t)})
		}
	}
	return &ec2.DescribeInstancesOutput{
		Reservations: []ec2types.Reservation{{Instances: insts}},
	}, nil
}

func (m *mockEC2) DescribeInstanceTypes(_ context.Context, in *ec2.DescribeInstanceTypesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstanceTypesOutput, error) {
	if m.instTypesErr != nil {
		return nil, m.instTypesErr
	}
	var out ec2.DescribeInstanceTypesOutput
	for _, it := range in.InstanceTypes {
		v, ok := m.vcpuByType[string(it)]
		if !ok {
			continue
		}
		out.InstanceTypes = append(out.InstanceTypes, ec2types.InstanceTypeInfo{
			InstanceType: it,
			VCpuInfo:     &ec2types.VCpuInfo{DefaultVCpus: aws.Int32(v)},
		})
	}
	return &out, nil
}

type mockQuotas struct {
	values map[string]float64 // "service/code" -> applied value
	err    error
}

func (m *mockQuotas) GetServiceQuota(_ context.Context, in *servicequotas.GetServiceQuotaInput, _ ...func(*servicequotas.Options)) (*servicequotas.GetServiceQuotaOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	key := aws.ToString(in.ServiceCode) + "/" + aws.ToString(in.QuotaCode)
	v, ok := m.values[key]
	if !ok {
		return nil, errors.New("quota not found: " + key)
	}
	return &servicequotas.GetServiceQuotaOutput{
		Quota: &sqtypes.ServiceQuota{Value: aws.Float64(v)},
	}, nil
}

// generous quotas so nothing is the bottleneck unless a test overrides it.
func generousQuotas() map[string]float64 {
	return map[string]float64{
		quotaServiceVPC + "/" + quotaCodeGatewayVPCEndpoints: 50,
		quotaServiceVPC + "/" + quotaCodeVPCsPerRegion:       50,
		quotaServiceEC2 + "/" + quotaCodeElasticIPs:          50,
		quotaServiceEC2 + "/" + quotaCodeStandardVCPUs:       1000,
	}
}

func snoProfile() *profile.Profile {
	return &profile.Profile{
		Compute: profile.ComputeConfig{
			ControlPlane: &profile.ControlPlaneConfig{Replicas: 1, InstanceType: "m6i.2xlarge"},
			Workers:      &profile.WorkersConfig{Replicas: 0},
		},
	}
}

func standardProfile() *profile.Profile {
	return &profile.Profile{
		Compute: profile.ComputeConfig{
			ControlPlane: &profile.ControlPlaneConfig{Replicas: 3, InstanceType: "m6i.xlarge"},
			Workers:      &profile.WorkersConfig{Replicas: 3, InstanceType: "m6i.2xlarge"},
		},
	}
}

// --- tests ---

func TestRunQuotaChecks(t *testing.T) {
	vcpu := map[string]int32{"m6i.xlarge": 4, "m6i.2xlarge": 8}

	tests := []struct {
		name      string
		ec2       *mockEC2
		quotas    *mockQuotas
		prof      *profile.Profile
		wantErr   bool
		errSubstr string
	}{
		{
			name:   "plenty of headroom passes",
			ec2:    &mockEC2{endpoints: 5, vpcs: 5, eips: 2, vcpuByType: vcpu},
			quotas: &mockQuotas{values: generousQuotas()},
			prof:   snoProfile(),
		},
		{
			name:      "gateway endpoint exhaustion blocks",
			ec2:       &mockEC2{endpoints: 20, vpcs: 5, eips: 2, vcpuByType: vcpu},
			quotas:    &mockQuotas{values: map[string]float64{quotaServiceVPC + "/" + quotaCodeGatewayVPCEndpoints: 20, quotaServiceVPC + "/" + quotaCodeVPCsPerRegion: 50, quotaServiceEC2 + "/" + quotaCodeElasticIPs: 50, quotaServiceEC2 + "/" + quotaCodeStandardVCPUs: 1000}},
			prof:      snoProfile(),
			wantErr:   true,
			errSubstr: "Gateway VPC endpoints",
		},
		{
			name:      "vcpu shortfall blocks for standard family",
			ec2:       &mockEC2{endpoints: 1, vpcs: 1, eips: 0, instances: map[string]int{"m6i.2xlarge": 3}, vcpuByType: vcpu},
			quotas:    &mockQuotas{values: map[string]float64{quotaServiceVPC + "/" + quotaCodeGatewayVPCEndpoints: 50, quotaServiceVPC + "/" + quotaCodeVPCsPerRegion: 50, quotaServiceEC2 + "/" + quotaCodeElasticIPs: 50, quotaServiceEC2 + "/" + quotaCodeStandardVCPUs: 40}},
			prof:      standardProfile(), // need = 4*3 + 4 (bootstrap) + 8*3 = 40; usage = 24 -> 64 > 40
			wantErr:   true,
			errSubstr: "Standard vCPUs",
		},
		{
			name:   "quota lookup failure degrades gracefully (no block)",
			ec2:    &mockEC2{endpoints: 99, vpcs: 99, eips: 99, vcpuByType: vcpu},
			quotas: &mockQuotas{err: errors.New("AccessDenied")},
			prof:   snoProfile(),
		},
		{
			name:   "usage lookup failure degrades gracefully (no block)",
			ec2:    &mockEC2{endpointsErr: errors.New("throttled"), vpcs: 1, eips: 0, vcpuByType: vcpu},
			quotas: &mockQuotas{values: generousQuotas()},
			prof:   snoProfile(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runQuotaChecks(context.Background(), "us-east-1", tt.ec2, tt.quotas, tt.prof)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}

func TestVCPUCheckSkippedForNonStandardFamily(t *testing.T) {
	// Worker uses a GPU family (g5) -> vCPU check must be skipped even though the
	// Standard vCPU quota is set impossibly low.
	prof := &profile.Profile{
		Compute: profile.ComputeConfig{
			ControlPlane: &profile.ControlPlaneConfig{Replicas: 3, InstanceType: "m6i.xlarge"},
			Workers:      &profile.WorkersConfig{Replicas: 2, InstanceType: "g5.xlarge"},
		},
	}
	ec2m := &mockEC2{endpoints: 1, vpcs: 1, eips: 0, instances: map[string]int{"m6i.2xlarge": 100}, vcpuByType: map[string]int32{"m6i.xlarge": 4}}
	q := &mockQuotas{values: map[string]float64{
		quotaServiceVPC + "/" + quotaCodeGatewayVPCEndpoints: 50,
		quotaServiceVPC + "/" + quotaCodeVPCsPerRegion:       50,
		quotaServiceEC2 + "/" + quotaCodeElasticIPs:          50,
		quotaServiceEC2 + "/" + quotaCodeStandardVCPUs:       1, // would fail if checked
	}}

	if err := runQuotaChecks(context.Background(), "us-east-1", ec2m, q, prof); err != nil {
		t.Fatalf("vCPU check should have been skipped for non-Standard family, got: %v", err)
	}
}

func TestComputeVCPUNeed(t *testing.T) {
	ec2m := &mockEC2{vcpuByType: map[string]int32{"m6i.xlarge": 4, "m6i.2xlarge": 8}}

	need, ok := computeVCPUNeed(context.Background(), ec2m, standardProfile())
	if !ok {
		t.Fatal("expected vCPU need to be computable for Standard families")
	}
	// 4*3 (control plane) + 4 (bootstrap) + 8*3 (workers) = 40
	if need != 40 {
		t.Fatalf("expected need 40, got %d", need)
	}

	// Non-standard family -> not applicable.
	nonStd := &profile.Profile{Compute: profile.ComputeConfig{
		ControlPlane: &profile.ControlPlaneConfig{Replicas: 3, InstanceType: "g5.xlarge"},
	}}
	if _, ok := computeVCPUNeed(context.Background(), ec2m, nonStd); ok {
		t.Fatal("expected vCPU need to be inapplicable for non-Standard family")
	}
}

func TestEstimateAZCount(t *testing.T) {
	if got := estimateAZCount(snoProfile()); got != 1 {
		t.Fatalf("SNO: expected 1 AZ, got %d", got)
	}
	if got := estimateAZCount(standardProfile()); got != 3 {
		t.Fatalf("standard: expected 3 AZs, got %d", got)
	}
}

func TestIsStandardFamily(t *testing.T) {
	cases := map[string]bool{
		"m6i.xlarge":   true,
		"c5.2xlarge":   true,
		"t3.medium":    true,
		"r6g.large":    true,
		"g5.xlarge":    false, // GPU
		"p4d.24xlarge": false, // GPU
		"":             false,
	}
	for it, want := range cases {
		if got := isStandardFamily(it); got != want {
			t.Errorf("isStandardFamily(%q) = %v, want %v", it, got, want)
		}
	}
}
