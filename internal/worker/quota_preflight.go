package worker

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/servicequotas"
	"github.com/tsanders-rh/ocpctl/internal/profile"
)

// AWS Service Quotas codes for the resources every OpenShift IPI cluster consumes.
// See https://docs.aws.amazon.com/general/latest/gr/aws_service_limits.html
const (
	quotaServiceVPC = "vpc"
	quotaServiceEC2 = "ec2"

	// Gateway VPC endpoints per Region (each cluster VPC gets one S3 gateway endpoint).
	quotaCodeGatewayVPCEndpoints = "L-1B52E74A"
	// VPCs per Region.
	quotaCodeVPCsPerRegion = "L-F678F1CE"
	// EC2-VPC Elastic IPs per Region (NAT gateways consume one each).
	quotaCodeElasticIPs = "L-0263D0A3"
	// Running On-Demand Standard (A, C, D, H, I, M, R, T, Z) instances, measured in vCPUs.
	quotaCodeStandardVCPUs = "L-1216C47A"
)

// ec2QuotaAPI is the subset of the EC2 client used by the quota preflight. It is
// satisfied by *ec2.Client and mocked in tests.
type ec2QuotaAPI interface {
	DescribeVpcEndpoints(context.Context, *ec2.DescribeVpcEndpointsInput, ...func(*ec2.Options)) (*ec2.DescribeVpcEndpointsOutput, error)
	DescribeVpcs(context.Context, *ec2.DescribeVpcsInput, ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error)
	DescribeAddresses(context.Context, *ec2.DescribeAddressesInput, ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error)
	DescribeInstances(context.Context, *ec2.DescribeInstancesInput, ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	DescribeInstanceTypes(context.Context, *ec2.DescribeInstanceTypesInput, ...func(*ec2.Options)) (*ec2.DescribeInstanceTypesOutput, error)
}

// quotasAPI is the subset of the Service Quotas client used by the quota
// preflight. Satisfied by *servicequotas.Client and mocked in tests.
type quotasAPI interface {
	GetServiceQuota(context.Context, *servicequotas.GetServiceQuotaInput, ...func(*servicequotas.Options)) (*servicequotas.GetServiceQuotaOutput, error)
}

// standardVCPUFamilies are the instance-family first letters counted by the
// "Running On-Demand Standard instances" quota (L-1216C47A).
var standardVCPUFamilies = map[byte]bool{
	'a': true, 'c': true, 'd': true, 'h': true,
	'i': true, 'm': true, 'r': true, 't': true, 'z': true,
}

// isStandardFamily reports whether an instance type belongs to a Standard family.
func isStandardFamily(instanceType string) bool {
	if instanceType == "" {
		return false
	}
	return standardVCPUFamilies[instanceType[0]]
}

// quotaRequirement describes one quota to validate: how many units this cluster
// needs and how to count what is already in use.
type quotaRequirement struct {
	label       string
	serviceCode string
	quotaCode   string
	needed      int
	getUsage    func(context.Context) (int, error)
}

// CheckQuotas validates that the target region has enough headroom in the AWS
// service quotas every cluster consumes (gateway VPC endpoints, VPCs, Elastic
// IPs, and Standard vCPUs) before provisioning begins.
//
// It fails ONLY on a confirmed shortfall. Any lookup that errors (missing
// permission, throttling, an unrecognized quota) is logged and skipped so the
// preflight can never become a new source of outages.
func (c *AWSPreflightChecker) CheckQuotas(ctx context.Context, prof *profile.Profile) error {
	if c.quotasClient == nil {
		log.Printf("Pre-flight quota check: no Service Quotas client configured, skipping")
		return nil
	}
	return runQuotaChecks(ctx, c.region, c.ec2Client, c.quotasClient, prof)
}

// runQuotaChecks is the testable core of CheckQuotas.
func runQuotaChecks(ctx context.Context, region string, ec2c ec2QuotaAPI, q quotasAPI, prof *profile.Profile) error {
	reqs := buildQuotaRequirements(ctx, ec2c, prof)

	var shortfalls []string
	for _, r := range reqs {
		applied, err := getAppliedQuota(ctx, q, r.serviceCode, r.quotaCode)
		if err != nil {
			log.Printf("Pre-flight quota check: skipping %q (quota lookup failed: %v)", r.label, err)
			continue
		}
		usage, err := r.getUsage(ctx)
		if err != nil {
			log.Printf("Pre-flight quota check: skipping %q (usage lookup failed: %v)", r.label, err)
			continue
		}

		free := applied - usage
		log.Printf("Pre-flight quota check: %s — usage %d / quota %d, need %d (%d free)",
			r.label, usage, applied, r.needed, free)

		if r.needed > free {
			shortfalls = append(shortfalls, fmt.Sprintf(
				"  • %s: %d/%d used, need %d free (only %d available)",
				r.label, usage, applied, r.needed, max(free, 0)))
		}
	}

	if len(shortfalls) > 0 {
		return formatQuotaError(region, shortfalls)
	}
	return nil
}

// buildQuotaRequirements assembles the list of quotas to check for this profile.
// The vCPU requirement is only added when it can be computed for Standard-family
// instance types; otherwise it is skipped with a warning.
func buildQuotaRequirements(ctx context.Context, ec2c ec2QuotaAPI, prof *profile.Profile) []quotaRequirement {
	reqs := []quotaRequirement{
		{
			label:       "Gateway VPC endpoints per Region",
			serviceCode: quotaServiceVPC,
			quotaCode:   quotaCodeGatewayVPCEndpoints,
			needed:      1, // one S3 gateway endpoint per cluster VPC
			getUsage:    func(ctx context.Context) (int, error) { return countGatewayVPCEndpoints(ctx, ec2c) },
		},
		{
			label:       "VPCs per Region",
			serviceCode: quotaServiceVPC,
			quotaCode:   quotaCodeVPCsPerRegion,
			needed:      1,
			getUsage:    func(ctx context.Context) (int, error) { return countVPCs(ctx, ec2c) },
		},
		{
			label:       "Elastic IPs per Region",
			serviceCode: quotaServiceEC2,
			quotaCode:   quotaCodeElasticIPs,
			needed:      estimateAZCount(prof), // one EIP per NAT gateway (one per AZ)
			getUsage:    func(ctx context.Context) (int, error) { return countElasticIPs(ctx, ec2c) },
		},
	}

	if need, ok := computeVCPUNeed(ctx, ec2c, prof); ok {
		reqs = append(reqs, quotaRequirement{
			label:       "Running On-Demand Standard vCPUs",
			serviceCode: quotaServiceEC2,
			quotaCode:   quotaCodeStandardVCPUs,
			needed:      need,
			getUsage:    func(ctx context.Context) (int, error) { return countStandardVCPUsInUse(ctx, ec2c) },
		})
	} else {
		log.Printf("Pre-flight quota check: skipping vCPU check (non-Standard instance family or lookup unavailable)")
	}

	return reqs
}

// getAppliedQuota returns the currently applied quota value, rounded down.
func getAppliedQuota(ctx context.Context, q quotasAPI, serviceCode, quotaCode string) (int, error) {
	out, err := q.GetServiceQuota(ctx, &servicequotas.GetServiceQuotaInput{
		ServiceCode: aws.String(serviceCode),
		QuotaCode:   aws.String(quotaCode),
	})
	if err != nil {
		return 0, fmt.Errorf("get service quota %s/%s: %w", serviceCode, quotaCode, err)
	}
	if out.Quota == nil || out.Quota.Value == nil {
		return 0, fmt.Errorf("get service quota %s/%s: empty value", serviceCode, quotaCode)
	}
	return int(math.Floor(*out.Quota.Value)), nil
}

// countGatewayVPCEndpoints counts gateway-type VPC endpoints in the region.
func countGatewayVPCEndpoints(ctx context.Context, ec2c ec2QuotaAPI) (int, error) {
	count := 0
	var token *string
	for {
		out, err := ec2c.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{
			Filters: []ec2types.Filter{{
				Name:   aws.String("vpc-endpoint-type"),
				Values: []string{string(ec2types.VpcEndpointTypeGateway)},
			}},
			NextToken: token,
		})
		if err != nil {
			return 0, err
		}
		count += len(out.VpcEndpoints)
		if out.NextToken == nil || *out.NextToken == "" {
			return count, nil
		}
		token = out.NextToken
	}
}

// countVPCs counts all VPCs in the region (the quota includes the default VPC).
func countVPCs(ctx context.Context, ec2c ec2QuotaAPI) (int, error) {
	count := 0
	var token *string
	for {
		out, err := ec2c.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{NextToken: token})
		if err != nil {
			return 0, err
		}
		count += len(out.Vpcs)
		if out.NextToken == nil || *out.NextToken == "" {
			return count, nil
		}
		token = out.NextToken
	}
}

// countElasticIPs counts allocated Elastic IPs in the region.
func countElasticIPs(ctx context.Context, ec2c ec2QuotaAPI) (int, error) {
	out, err := ec2c.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{})
	if err != nil {
		return 0, err
	}
	return len(out.Addresses), nil
}

// countStandardVCPUsInUse sums the vCPUs of running/pending Standard-family
// instances in the region.
func countStandardVCPUsInUse(ctx context.Context, ec2c ec2QuotaAPI) (int, error) {
	typeCounts := map[string]int{}
	var token *string
	for {
		out, err := ec2c.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			Filters: []ec2types.Filter{{
				Name:   aws.String("instance-state-name"),
				Values: []string{"running", "pending"},
			}},
			NextToken: token,
		})
		if err != nil {
			return 0, err
		}
		for _, res := range out.Reservations {
			for _, inst := range res.Instances {
				it := string(inst.InstanceType)
				if isStandardFamily(it) {
					typeCounts[it]++
				}
			}
		}
		if out.NextToken == nil || *out.NextToken == "" {
			break
		}
		token = out.NextToken
	}

	if len(typeCounts) == 0 {
		return 0, nil
	}

	vcpus, err := getVCPUsPerType(ctx, ec2c, mapKeys(typeCounts))
	if err != nil {
		return 0, err
	}
	total := 0
	for it, n := range typeCounts {
		total += vcpus[it] * n
	}
	return total, nil
}

// computeVCPUNeed returns the vCPUs this cluster will consume (control plane +
// workers + one bootstrap node). The second return is false when the check does
// not apply — a non-Standard instance family, or an instance-type lookup error.
func computeVCPUNeed(ctx context.Context, ec2c ec2QuotaAPI, prof *profile.Profile) (int, bool) {
	cpType, cpReplicas := "", 0
	if prof.Compute.ControlPlane != nil {
		cpType = prof.Compute.ControlPlane.InstanceType
		cpReplicas = prof.Compute.ControlPlane.Replicas
	}
	workerType, workerReplicas := "", 0
	if prof.Compute.Workers != nil {
		workerType = prof.Compute.Workers.InstanceType
		workerReplicas = prof.Compute.Workers.Replicas
	}

	// The Standard vCPU quota only counts Standard-family instances. If any
	// requested type is outside that group we cannot compute usage against the
	// same quota, so skip rather than risk a wrong verdict.
	for _, t := range []string{cpType, workerType} {
		if t != "" && !isStandardFamily(t) {
			return 0, false
		}
	}
	if cpType == "" && workerType == "" {
		return 0, false
	}

	typeSet := map[string]bool{}
	if cpType != "" {
		typeSet[cpType] = true
	}
	if workerType != "" {
		typeSet[workerType] = true
	}
	types := make([]string, 0, len(typeSet))
	for t := range typeSet {
		types = append(types, t)
	}
	vcpus, err := getVCPUsPerType(ctx, ec2c, types)
	if err != nil {
		return 0, false
	}

	need := 0
	if cpType != "" {
		need += vcpus[cpType] * cpReplicas
		need += vcpus[cpType] // bootstrap node uses a control-plane-sized instance
	}
	if workerType != "" {
		need += vcpus[workerType] * workerReplicas
	}
	if need == 0 {
		return 0, false
	}
	return need, true
}

// getVCPUsPerType returns the default vCPU count for each instance type.
func getVCPUsPerType(ctx context.Context, ec2c ec2QuotaAPI, types []string) (map[string]int, error) {
	result := map[string]int{}
	if len(types) == 0 {
		return result, nil
	}
	sort.Strings(types)

	instanceTypes := make([]ec2types.InstanceType, 0, len(types))
	for _, t := range types {
		instanceTypes = append(instanceTypes, ec2types.InstanceType(t))
	}

	out, err := ec2c.DescribeInstanceTypes(ctx, &ec2.DescribeInstanceTypesInput{
		InstanceTypes: instanceTypes,
	})
	if err != nil {
		return nil, err
	}
	for _, it := range out.InstanceTypes {
		if it.VCpuInfo != nil && it.VCpuInfo.DefaultVCpus != nil {
			result[string(it.InstanceType)] = int(*it.VCpuInfo.DefaultVCpus)
		}
	}
	return result, nil
}

// estimateAZCount estimates how many availability zones (and therefore NAT
// gateways / Elastic IPs) the cluster will use: one for single-node, otherwise
// the standard three.
func estimateAZCount(prof *profile.Profile) int {
	cp := 3
	if prof.Compute.ControlPlane != nil && prof.Compute.ControlPlane.Replicas > 0 {
		cp = prof.Compute.ControlPlane.Replicas
	}
	if cp > 3 {
		cp = 3
	}
	if cp < 1 {
		cp = 1
	}
	return cp
}

// formatQuotaError builds a user-facing error listing the quotas without headroom.
func formatQuotaError(region string, shortfalls []string) error {
	var msg strings.Builder
	msg.WriteString("❌ AWS Quota Pre-flight Check Failed\n\n")
	msg.WriteString(fmt.Sprintf("The region %s does not have enough quota headroom to create this cluster:\n\n", region))
	msg.WriteString(strings.Join(shortfalls, "\n"))
	msg.WriteString("\n\nRecommendations:\n")
	msg.WriteString("  1. Create the cluster in a different region (each region has its own quotas)\n")
	msg.WriteString("  2. Free capacity by destroying unused clusters or cleaning up orphaned resources\n")
	msg.WriteString("  3. Request a Service Quotas increase for the limits above\n")
	return fmt.Errorf("%s", msg.String())
}

// mapKeys returns the keys of a string-keyed map.
func mapKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
