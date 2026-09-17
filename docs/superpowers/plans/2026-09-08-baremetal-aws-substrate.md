# Bare-metal AWS Substrate Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A self-contained, unit-tested Go package that launches and tears down the nested-virt EC2 host substrate (instance, ephemeral keypair, security group, Elastic IP, Route53 records) for a bare-metal/agent OpenShift cluster, using the AWS Go SDK.

**Architecture:** New package `internal/baremetal/substrate`, sibling to `internal/baremetal/host` (piece 2). `Launch(ctx, LaunchSpec) (*Substrate, error)` runs rhwa-lab's substrate steps via aws-sdk-go-v2; `Teardown(ctx, TeardownSpec) error` discovers resources by tag and removes them. All SDK access is behind narrow `ec2API`/`route53API`/`stsAPI` interfaces, satisfied by the real clients and by hand-written mocks in tests, so the package is fully unit-testable with no live AWS. An ephemeral ed25519 keypair is generated in-process; its private half becomes the `ssh.Signer` piece 2 consumes.

**Tech Stack:** Go 1.25; `aws-sdk-go-v2` (`config`, `service/ec2`, `service/ec2/types`, `service/route53`, `service/route53/types`, `service/sts`); `golang.org/x/crypto/ssh`; stdlib `crypto/ed25519`, `crypto/rand`, `net/http`; `testify` for tests.

**Spec:** `docs/superpowers/specs/2026-09-08-baremetal-aws-substrate-design.md`

## Global Constraints

- **ocpctl as orchestrator, rhwa-lab as reference only** — no runtime dependency on the rhwa-lab repo/binary.
- **SDK, not CLI:** aws-sdk-go-v2, clients via `config.LoadDefaultConfig(ctx, config.WithRegion(region))` + `<svc>.NewFromConfig(cfg)`, default credential chain (no AssumeRole). Match `internal/worker/preflight_aws.go:26-37`.
- **Testability:** narrow per-package interfaces = exactly the SDK methods used, satisfied by the real client and hand-written mocks in `_test.go`; inject an internal `sleep func(time.Duration)` for retries. No live AWS in unit tests. `testify` (`require`/`assert`).
- **Tagging:** every taggable resource carries `types.ProvenanceTags(clusterID, clusterName, createdAt)` (`pkg/types/cluster.go`) merged with `ManagedBy=ocpctl`, `ClusterName=<name>`, `Name=<name>` so the janitor/orphan-detector recognises it.
- **Comment style:** follow the surrounding ocpctl code — no inline comments other providers lack, no decorative banners. Doc comments on exported identifiers only, terse.
- **Scope:** pure library. NO worker-dispatch wiring and NO removal of the existing `rhwa-lab` shell-out handlers — that is piece 4.
- **Ephemeral keypair:** ed25519, generated in-process, private key never written to disk.
- **Package name:** `substrate` (not `aws` — avoids shadowing the SDK `aws` package).

---

### Task 1: Core types, profile field, tag & defaults helpers

**Files:**
- Modify: `internal/profile/types.go` (add one field to `BareMetalConfig`, ~line 168-179)
- Create: `internal/baremetal/substrate/types.go`
- Test: `internal/baremetal/substrate/types_test.go`

**Interfaces:**
- Consumes: `github.com/tsanders-rh/ocpctl/pkg/types` (`ProvenanceTags`).
- Produces: `LaunchSpec`, `TeardownSpec`, `Substrate`, `ec2API`, `route53API`, `stsAPI` (for Tasks 3-5); `buildTags(LaunchSpec) map[string]string`; `applyDefaults(*LaunchSpec)`; the exported defaults constants.

- [ ] **Step 1: Add the profile field**

In `internal/profile/types.go`, inside `BareMetalConfig`, add after `SushyPort`:

```go
	HostVolumeGB int `yaml:"hostVolumeGB,omitempty"` // host root disk in GB (holds all VM qcow2s); default 1000
```

- [ ] **Step 2: Write the failing test for types.go helpers**

Create `internal/baremetal/substrate/types_test.go`:

```go
package substrate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	octypes "github.com/tsanders-rh/ocpctl/pkg/types"
)

func TestApplyDefaults(t *testing.T) {
	s := LaunchSpec{ClusterName: "c1"}
	applyDefaults(&s)
	assert.Equal(t, defaultInstanceType, s.InstanceType)
	assert.Equal(t, defaultAMIOwner, s.AMIOwner)
	assert.Equal(t, defaultFedoraRelease, s.FedoraRelease)
	assert.Equal(t, defaultHostVolumeGB, s.HostVolumeGB)
	assert.Equal(t, defaultUser, s.User)

	// Explicit values are preserved.
	s2 := LaunchSpec{ClusterName: "c1", InstanceType: "m8i.24xlarge", HostVolumeGB: 500, User: "cloud"}
	applyDefaults(&s2)
	assert.Equal(t, "m8i.24xlarge", s2.InstanceType)
	assert.Equal(t, 500, s2.HostVolumeGB)
	assert.Equal(t, "cloud", s2.User)
}

func TestBuildTags(t *testing.T) {
	when := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	spec := LaunchSpec{ClusterID: "id-123", ClusterName: "mycluster", CreatedAt: when}
	tags := buildTags(spec)

	// Provenance tags present.
	for k, v := range octypes.ProvenanceTags("id-123", "mycluster", when) {
		assert.Equal(t, v, tags[k], "provenance tag %s", k)
	}
	// ocpctl janitor/orphan tags present.
	assert.Equal(t, "ocpctl", tags["ManagedBy"])
	assert.Equal(t, "mycluster", tags["ClusterName"])
	assert.Equal(t, "mycluster", tags["Name"])
}

func TestFQDNs(t *testing.T) {
	assert.Equal(t, "api.mycluster.example.com", apiFQDN("mycluster", "example.com"))
	assert.Equal(t, "*.apps.mycluster.example.com", appsFQDN("mycluster", "example.com"))
	// Trailing dot on base domain tolerated.
	assert.Equal(t, "api.mycluster.example.com", apiFQDN("mycluster", "example.com."))
	require.NotEmpty(t, keypairName("mycluster"))
	assert.Equal(t, "mycluster-key", keypairName("mycluster"))
	assert.Equal(t, "mycluster-sg", sgName("mycluster"))
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/baremetal/substrate/ -run 'TestApplyDefaults|TestBuildTags|TestFQDNs' -v`
Expected: FAIL — package/identifiers do not exist.

- [ ] **Step 4: Write types.go**

Create `internal/baremetal/substrate/types.go`:

```go
package substrate

import (
	"context"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"golang.org/x/crypto/ssh"
	octypes "github.com/tsanders-rh/ocpctl/pkg/types"
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
	InstanceID string
	Addr       string // "<eip>:22"
	User       string
	Signer     ssh.Signer
	EIP        string
	AllocID    string
	SGID       string
	KeyName    string
	ZoneID     string
	APIFQDN    string
	AppsFQDN   string
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

func apiFQDN(cluster, base string) string  { return "api." + cluster + "." + strings.TrimSuffix(base, ".") }
func appsFQDN(cluster, base string) string { return "*.apps." + cluster + "." + strings.TrimSuffix(base, ".") }
func keypairName(cluster string) string    { return cluster + "-key" }
func sgName(cluster string) string         { return cluster + "-sg" }
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/baremetal/substrate/ -run 'TestApplyDefaults|TestBuildTags|TestFQDNs' -v` and `go build ./...`
Expected: PASS; build clean.

- [ ] **Step 6: Commit**

```bash
git add internal/profile/types.go internal/baremetal/substrate/types.go internal/baremetal/substrate/types_test.go
git commit -m "substrate: core types, profile HostVolumeGB, tag/defaults helpers"
```

---

### Task 2: Ephemeral ed25519 keypair

**Files:**
- Create: `internal/baremetal/substrate/keypair.go`
- Test: `internal/baremetal/substrate/keypair_test.go`

**Interfaces:**
- Produces: `generateKeypair() (pubMaterial []byte, signer ssh.Signer, err error)` — `pubMaterial` is OpenSSH authorized-keys format for `ec2.ImportKeyPairInput.PublicKeyMaterial`; `signer` wraps the private key.

- [ ] **Step 1: Write the failing test**

Create `internal/baremetal/substrate/keypair_test.go`:

```go
package substrate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestGenerateKeypair(t *testing.T) {
	pubMaterial, signer, err := generateKeypair()
	require.NoError(t, err)
	require.NotNil(t, signer)

	// pubMaterial parses as an authorized_keys line...
	parsed, _, _, _, err := ssh.ParseAuthorizedKey(pubMaterial)
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519", parsed.Type())

	// ...and matches the signer's public key.
	assert.Equal(t, parsed.Marshal(), signer.PublicKey().Marshal())

	// Two calls produce distinct keys.
	pub2, _, err := generateKeypair()
	require.NoError(t, err)
	assert.NotEqual(t, pubMaterial, pub2)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/baremetal/substrate/ -run TestGenerateKeypair -v`
Expected: FAIL — `generateKeypair` undefined.

- [ ] **Step 3: Write keypair.go**

Create `internal/baremetal/substrate/keypair.go`:

```go
package substrate

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// generateKeypair creates a fresh ed25519 keypair. It returns the public key in
// OpenSSH authorized-keys format (for ec2.ImportKeyPair) and an ssh.Signer over
// the private key. The private key never leaves the process.
func generateKeypair() ([]byte, ssh.Signer, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate ed25519 key: %w", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, nil, fmt.Errorf("wrap public key: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, nil, fmt.Errorf("build signer: %w", err)
	}
	return ssh.MarshalAuthorizedKey(sshPub), signer, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/baremetal/substrate/ -run TestGenerateKeypair -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/baremetal/substrate/keypair.go internal/baremetal/substrate/keypair_test.go
git commit -m "substrate: ephemeral ed25519 keypair with ssh.Signer"
```

---

### Task 3: Shared AWS mocks, zone resolution & caller-IP detection

**Files:**
- Create: `internal/baremetal/substrate/dns.go`
- Create: `internal/baremetal/substrate/callerip.go`
- Create: `internal/baremetal/substrate/mocks_test.go` (shared fakes for Tasks 3-5)
- Test: `internal/baremetal/substrate/dns_test.go`, `internal/baremetal/substrate/callerip_test.go`

**Interfaces:**
- Consumes: `route53API` (Task 1).
- Produces: `resolveZoneID(ctx, r53 route53API, explicitZoneID, baseDomain string) (string, error)` (walks up from `<baseDomain>` to closest parent hosted zone; returns bare zone id without `/hostedzone/`); `detectCallerCIDRs(get httpGetter) []string` with fallback `["0.0.0.0/32"]`; `type httpGetter func(url string) (*http.Response, error)`; and the shared `fakeEC2`/`fakeRoute53`/`fakeSTS` mock types used by later tasks.

- [ ] **Step 1: Write the shared mocks (test helper file)**

Create `internal/baremetal/substrate/mocks_test.go`. Each fake stores per-method function fields (nil => a sensible zero-value success) and records calls. Keep it minimal but cover every `ec2API`/`route53API`/`stsAPI` method. Example shape (implement all methods of all three interfaces the same way):

```go
package substrate

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

type fakeEC2 struct {
	runInstances       func(*ec2.RunInstancesInput) (*ec2.RunInstancesOutput, error)
	describeImages     func(*ec2.DescribeImagesInput) (*ec2.DescribeImagesOutput, error)
	describeVpcs       func(*ec2.DescribeVpcsInput) (*ec2.DescribeVpcsOutput, error)
	createSG           func(*ec2.CreateSecurityGroupInput) (*ec2.CreateSecurityGroupOutput, error)
	authorizeIngress   func(*ec2.AuthorizeSecurityGroupIngressInput) (*ec2.AuthorizeSecurityGroupIngressOutput, error)
	describeSGs        func(*ec2.DescribeSecurityGroupsInput) (*ec2.DescribeSecurityGroupsOutput, error)
	deleteSG           func(*ec2.DeleteSecurityGroupInput) (*ec2.DeleteSecurityGroupOutput, error)
	importKeyPair      func(*ec2.ImportKeyPairInput) (*ec2.ImportKeyPairOutput, error)
	describeKeyPairs   func(*ec2.DescribeKeyPairsInput) (*ec2.DescribeKeyPairsOutput, error)
	deleteKeyPair      func(*ec2.DeleteKeyPairInput) (*ec2.DeleteKeyPairOutput, error)
	describeOfferings  func(*ec2.DescribeInstanceTypeOfferingsInput) (*ec2.DescribeInstanceTypeOfferingsOutput, error)
	describeInstances  func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error)
	terminate          func(*ec2.TerminateInstancesInput) (*ec2.TerminateInstancesOutput, error)
	allocateAddress    func(*ec2.AllocateAddressInput) (*ec2.AllocateAddressOutput, error)
	associateAddress   func(*ec2.AssociateAddressInput) (*ec2.AssociateAddressOutput, error)
	describeAddresses  func(*ec2.DescribeAddressesInput) (*ec2.DescribeAddressesOutput, error)
	releaseAddress     func(*ec2.ReleaseAddressInput) (*ec2.ReleaseAddressOutput, error)

	authorizeCalls []*ec2.AuthorizeSecurityGroupIngressInput
	runInput       *ec2.RunInstancesInput
	importInput    *ec2.ImportKeyPairInput
	deleteSGCalls  int
}

// Implement each ec2API method: if the matching func field is non-nil, call it;
// else return a minimal non-nil success output (&ec2.XxxOutput{}). Record inputs
// where later tests assert on them (authorizeCalls, runInput, importInput,
// deleteSGCalls). Example:
func (f *fakeEC2) AuthorizeSecurityGroupIngress(ctx context.Context, in *ec2.AuthorizeSecurityGroupIngressInput, _ ...func(*ec2.Options)) (*ec2.AuthorizeSecurityGroupIngressOutput, error) {
	f.authorizeCalls = append(f.authorizeCalls, in)
	if f.authorizeIngress != nil {
		return f.authorizeIngress(in)
	}
	return &ec2.AuthorizeSecurityGroupIngressOutput{}, nil
}
// ... implement the remaining ec2API methods identically ...

type fakeRoute53 struct {
	listZones    func(*route53.ListHostedZonesByNameInput) (*route53.ListHostedZonesByNameOutput, error)
	listRecords  func(*route53.ListResourceRecordSetsInput) (*route53.ListResourceRecordSetsOutput, error)
	changeRecords func(*route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error)
	changeCalls  []*route53.ChangeResourceRecordSetsInput
}
// ... implement route53API methods, recording changeCalls ...

type fakeSTS struct {
	getIdentity func(*sts.GetCallerIdentityInput) (*sts.GetCallerIdentityOutput, error)
}
// ... implement GetCallerIdentity ...
```

The implementer completes every interface method. `var _ ec2API = (*fakeEC2)(nil)` (and for the other two) at file scope to guarantee coverage at compile time.

- [ ] **Step 2: Write the failing tests for dns.go and callerip.go**

Create `internal/baremetal/substrate/dns_test.go`:

```go
package substrate

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveZoneID_Explicit(t *testing.T) {
	id, err := resolveZoneID(context.Background(), &fakeRoute53{}, "Z123ABC", "anything.example.com")
	require.NoError(t, err)
	assert.Equal(t, "Z123ABC", id)
}

func TestResolveZoneID_WalksUp(t *testing.T) {
	r53 := &fakeRoute53{
		listZones: func(in *route53.ListHostedZonesByNameInput) (*route53.ListHostedZonesByNameOutput, error) {
			// No zone for sub.example.com; a zone exists for example.com.
			if aws.ToString(in.DNSName) == "example.com." {
				return &route53.ListHostedZonesByNameOutput{HostedZones: []r53types.HostedZone{
					{Id: aws.String("/hostedzone/ZEXAMPLE"), Name: aws.String("example.com.")},
				}}, nil
			}
			return &route53.ListHostedZonesByNameOutput{}, nil
		},
	}
	id, err := resolveZoneID(context.Background(), r53, "", "sub.example.com")
	require.NoError(t, err)
	assert.Equal(t, "ZEXAMPLE", id) // "/hostedzone/" prefix stripped
}

func TestResolveZoneID_NotFound(t *testing.T) {
	_, err := resolveZoneID(context.Background(), &fakeRoute53{}, "", "nozone.example.com")
	require.Error(t, err)
}
```

Create `internal/baremetal/substrate/callerip_test.go`:

```go
package substrate

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectCallerCIDRs_OK(t *testing.T) {
	get := func(string) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("203.0.113.7\n")),
		}, nil
	}
	assert.Equal(t, []string{"203.0.113.7/32"}, detectCallerCIDRs(get))
}

func TestDetectCallerCIDRs_FailsSafe(t *testing.T) {
	get := func(string) (*http.Response, error) { return nil, errors.New("network down") }
	assert.Equal(t, []string{"0.0.0.0/32"}, detectCallerCIDRs(get))
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/baremetal/substrate/ -run 'TestResolveZoneID|TestDetectCallerCIDRs' -v`
Expected: FAIL — functions undefined.

- [ ] **Step 4: Write dns.go**

Create `internal/baremetal/substrate/dns.go`:

```go
package substrate

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
)

// resolveZoneID returns the hosted-zone id for baseDomain. An explicit zone id is
// returned as-is. Otherwise it walks up from baseDomain to the closest parent
// zone (e.g. sub.example.com -> example.com), matching rhwa-lab's resolve_r53_zone.
func resolveZoneID(ctx context.Context, r53 route53API, explicitZoneID, baseDomain string) (string, error) {
	if explicitZoneID != "" {
		return explicitZoneID, nil
	}
	try := strings.TrimSuffix(baseDomain, ".")
	for strings.Contains(try, ".") {
		name := try + "."
		out, err := r53.ListHostedZonesByName(ctx, &route53.ListHostedZonesByNameInput{DNSName: aws.String(name)})
		if err != nil {
			return "", fmt.Errorf("list hosted zones for %s: %w", name, err)
		}
		for _, z := range out.HostedZones {
			if aws.ToString(z.Name) == name {
				return strings.TrimPrefix(aws.ToString(z.Id), "/hostedzone/"), nil
			}
		}
		try = try[strings.Index(try, ".")+1:] // drop leftmost label
	}
	return "", fmt.Errorf("no Route53 hosted zone found for %s or any parent domain", baseDomain)
}
```

- [ ] **Step 5: Write callerip.go**

Create `internal/baremetal/substrate/callerip.go`:

```go
package substrate

import (
	"io"
	"net/http"
	"strings"
	"time"
)

type httpGetter func(url string) (*http.Response, error)

const checkIPURL = "https://checkip.amazonaws.com"

// detectCallerCIDRs returns the caller's public IP as a /32 CIDR. On any failure
// it returns 0.0.0.0/32, which fails safe (opens nothing) rather than opening the
// security group to the world.
func detectCallerCIDRs(get httpGetter) []string {
	resp, err := get(checkIPURL)
	if err != nil {
		return []string{"0.0.0.0/32"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return []string{"0.0.0.0/32"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return []string{"0.0.0.0/32"}
	}
	ip := strings.TrimSpace(string(body))
	if ip == "" {
		return []string{"0.0.0.0/32"}
	}
	return []string{ip + "/32"}
}

// defaultHTTPGet is the production httpGetter (short timeout).
func defaultHTTPGet(url string) (*http.Response, error) {
	c := &http.Client{Timeout: 5 * time.Second}
	return c.Get(url)
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/baremetal/substrate/ -run 'TestResolveZoneID|TestDetectCallerCIDRs' -v` and `go build ./...`
Expected: PASS; build clean (the mocks compile against the interfaces via the `var _ ec2API` assertions).

- [ ] **Step 7: Commit**

```bash
git add internal/baremetal/substrate/dns.go internal/baremetal/substrate/callerip.go internal/baremetal/substrate/mocks_test.go internal/baremetal/substrate/dns_test.go internal/baremetal/substrate/callerip_test.go
git commit -m "substrate: zone resolution, caller-IP detection, shared AWS mocks"
```

---

### Task 4: Launch orchestration

**Files:**
- Create: `internal/baremetal/substrate/launch.go`
- Test: `internal/baremetal/substrate/launch_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1-3 (`LaunchSpec`, `Substrate`, the three interfaces, `generateKeypair`, `resolveZoneID`, `detectCallerCIDRs`, `buildTags`, `applyDefaults`, name/FQDN helpers).
- Produces: `Launch(ctx, LaunchSpec) (*Substrate, error)`; an internal `clients` struct bundling `ec2 ec2API; r53 route53API; sts stsAPI; get httpGetter` so tests inject fakes; an internal `launch(ctx, clients, LaunchSpec) (*Substrate, error)` doing the work (public `Launch` builds real clients then delegates).

**Design notes for the implementer:**
- `Launch` builds real clients: `cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(spec.Region))`; `clients{ec2: ec2.NewFromConfig(cfg), r53: route53.NewFromConfig(cfg), sts: sts.NewFromConfig(cfg), get: defaultHTTPGet}`; then `return launch(ctx, c, spec)`.
- Instance-running wait: `ec2.NewInstanceRunningWaiter(c.ec2).Wait(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{id}}, 10*time.Minute)`. `c.ec2` (type `ec2API`) satisfies `ec2.DescribeInstancesAPIClient` because it has `DescribeInstances`.
- Tag specifications: convert `buildTags(spec)` to `[]ec2types.Tag` (sorted keys for deterministic tests) and wrap in `ec2types.TagSpecification{ResourceType: <type>, Tags: tags}` for instance, keypair, security-group, and elastic-ip resource types.
- Nested virt (verified against pinned ec2 v1.296.0): set `CpuOptions: &ec2types.CpuOptionsRequest{NestedVirtualization: ec2types.NestedVirtualizationSpecificationEnabled}` on `RunInstancesInput`. This is rhwa-lab's `--cpu-options NestedVirtualization=enabled`. (The enum lives at `ec2/types/enums.go`; `NestedVirtualizationSpecificationEnabled = "enabled"`.)
- Block device: read the AMI's `RootDeviceName` from the selected image; map `ec2types.BlockDeviceMapping{DeviceName: rootDev, Ebs: &ec2types.EbsBlockDevice{VolumeSize: aws.Int32(int32(spec.HostVolumeGB)), VolumeType: ec2types.VolumeTypeGp3, DeleteOnTermination: aws.Bool(true)}}`.
- AMI selection: if `spec.AMIOverride != ""` use it; else `DescribeImages` with owners `[spec.AMIOwner]` and filters `name=Fedora-Cloud-Base-AmazonEC2*-<release>-*`, `architecture=x86_64`, `state=available`; pick the image with the newest `CreationDate` (RFC3339 string compare is safe for that format).
- SG ingress: base ports `[]int32{22, 6443, 443, 80}` from each caller CIDR; hairpin ports `[]int32{6443, 443, 80}` from `<eip>/32`. One `AuthorizeSecurityGroupIngress` per (cidr) with an `IpPermissions` slice, or per port — either is fine; tests assert the resulting permissions cover the required (port, cidr) pairs.
- DNS: one `ChangeResourceRecordSets` UPSERT batch with both A records (`api...` and `*.apps...`), TTL 60, value = EIP.
- Order and error wrapping per spec Section 2 (`fmt.Errorf("substrate <step>: %w", err)`).

- [ ] **Step 1: Write the failing test**

Create `internal/baremetal/substrate/launch_test.go`. Build a fully-wired happy-path `clients` with fakes returning canned IDs, run `launch`, and assert the `Substrate` and the recorded AWS inputs:

```go
package substrate

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func happyClients() (*fakeEC2, *fakeRoute53, *fakeSTS, clients) {
	e := &fakeEC2{
		describeVpcs: func(*ec2.DescribeVpcsInput) (*ec2.DescribeVpcsOutput, error) {
			return &ec2.DescribeVpcsOutput{Vpcs: []ec2types.Vpc{{VpcId: aws.String("vpc-1")}}}, nil
		},
		describeOfferings: func(*ec2.DescribeInstanceTypeOfferingsInput) (*ec2.DescribeInstanceTypeOfferingsOutput, error) {
			return &ec2.DescribeInstanceTypeOfferingsOutput{InstanceTypeOfferings: []ec2types.InstanceTypeOffering{{InstanceType: ec2types.InstanceType("m8i.12xlarge")}}}, nil
		},
		createSG: func(*ec2.CreateSecurityGroupInput) (*ec2.CreateSecurityGroupOutput, error) {
			return &ec2.CreateSecurityGroupOutput{GroupId: aws.String("sg-1")}, nil
		},
		describeImages: func(*ec2.DescribeImagesInput) (*ec2.DescribeImagesOutput, error) {
			return &ec2.DescribeImagesOutput{Images: []ec2types.Image{
				{ImageId: aws.String("ami-old"), CreationDate: aws.String("2026-01-01T00:00:00.000Z"), RootDeviceName: aws.String("/dev/sda1")},
				{ImageId: aws.String("ami-new"), CreationDate: aws.String("2026-06-01T00:00:00.000Z"), RootDeviceName: aws.String("/dev/sda1")},
			}}, nil
		},
		importKeyPair: func(in *ec2.ImportKeyPairInput) (*ec2.ImportKeyPairOutput, error) {
			return &ec2.ImportKeyPairOutput{KeyName: in.KeyName}, nil
		},
		runInstances: func(*ec2.RunInstancesInput) (*ec2.RunInstancesOutput, error) {
			return &ec2.RunInstancesOutput{Instances: []ec2types.Instance{{InstanceId: aws.String("i-1")}}}, nil
		},
		describeInstances: func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
			// Satisfy the running-waiter immediately.
			return &ec2.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{{
				InstanceId: aws.String("i-1"),
				State:      &ec2types.InstanceState{Name: ec2types.InstanceStateNameRunning},
			}}}}}, nil
		},
		allocateAddress: func(*ec2.AllocateAddressInput) (*ec2.AllocateAddressOutput, error) {
			return &ec2.AllocateAddressOutput{AllocationId: aws.String("eipalloc-1"), PublicIp: aws.String("198.51.100.9")}, nil
		},
	}
	r := &fakeRoute53{
		listZones: func(*route53.ListHostedZonesByNameInput) (*route53.ListHostedZonesByNameOutput, error) {
			return &route53.ListHostedZonesByNameOutput{HostedZones: []r53types.HostedZone{
				{Id: aws.String("/hostedzone/ZONE1"), Name: aws.String("example.com.")},
			}}, nil
		},
	}
	s := &fakeSTS{
		getIdentity: func(*sts.GetCallerIdentityInput) (*sts.GetCallerIdentityOutput, error) {
			return &sts.GetCallerIdentityOutput{Account: aws.String("111122223333")}, nil
		},
	}
	get := func(string) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("203.0.113.7\n"))}, nil
	}
	return e, r, s, clients{ec2: e, r53: r, sts: s, get: get}
}

func testSpec() LaunchSpec {
	return LaunchSpec{
		ClusterID: "id-1", ClusterName: "mycluster", Region: "us-east-1",
		BaseDomain: "example.com", CreatedAt: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC),
	}
}

func TestLaunch_HappyPath(t *testing.T) {
	e, r, _, c := happyClients()
	sub, err := launch(context.Background(), c, testSpec())
	require.NoError(t, err)

	assert.Equal(t, "i-1", sub.InstanceID)
	assert.Equal(t, "198.51.100.9:22", sub.Addr)
	assert.Equal(t, "fedora", sub.User)
	assert.Equal(t, "198.51.100.9", sub.EIP)
	assert.Equal(t, "eipalloc-1", sub.AllocID)
	assert.Equal(t, "sg-1", sub.SGID)
	assert.Equal(t, "mycluster-key", sub.KeyName)
	assert.Equal(t, "ZONE1", sub.ZoneID)
	assert.Equal(t, "api.mycluster.example.com", sub.APIFQDN)
	assert.Equal(t, "*.apps.mycluster.example.com", sub.AppsFQDN)
	require.NotNil(t, sub.Signer)

	// Newest AMI chosen.
	assert.Equal(t, "ami-new", aws.ToString(e.runInput.ImageId))
	// gp3 root volume at HostVolumeGB default.
	require.Len(t, e.runInput.BlockDeviceMappings, 1)
	assert.Equal(t, "/dev/sda1", aws.ToString(e.runInput.BlockDeviceMappings[0].DeviceName))
	assert.Equal(t, ec2types.VolumeTypeGp3, e.runInput.BlockDeviceMappings[0].Ebs.VolumeType)
	assert.Equal(t, int32(1000), aws.ToInt32(e.runInput.BlockDeviceMappings[0].Ebs.VolumeSize))
	// Nested virtualization requested.
	require.NotNil(t, e.runInput.CpuOptions)
	assert.Equal(t, ec2types.NestedVirtualizationSpecificationEnabled, e.runInput.CpuOptions.NestedVirtualization)
	// Instance tagged with provenance + Name.
	assert.True(t, hasTag(e.runInput.TagSpecifications, "Name", "mycluster"))
	assert.True(t, hasTag(e.runInput.TagSpecifications, "ManagedBy", "ocpctl"))

	// Base ingress (22/6443/443/80 from caller) + hairpin (6443/443/80 from EIP).
	assert.True(t, authorized(e.authorizeCalls, 22, "203.0.113.7/32"))
	assert.True(t, authorized(e.authorizeCalls, 6443, "203.0.113.7/32"))
	assert.True(t, authorized(e.authorizeCalls, 6443, "198.51.100.9/32"))
	assert.True(t, authorized(e.authorizeCalls, 80, "198.51.100.9/32"))

	// DNS UPSERT of both records to the EIP.
	require.Len(t, r.changeCalls, 1)
	names := recordNamesAndValues(r.changeCalls[0])
	assert.Equal(t, "198.51.100.9", names["api.mycluster.example.com."])
	assert.Equal(t, "198.51.100.9", names["*.apps.mycluster.example.com."])
}

// hasTag, authorized, recordNamesAndValues are small helpers the implementer adds
// in launch_test.go to walk the recorded inputs. authorized scans every
// IpPermission across all authorizeCalls for a (port, cidr) pair.
```

The implementer writes the small helpers `hasTag(tagSpecs, k, v)`, `authorized(calls, port, cidr)`, and `recordNamesAndValues(changeInput)` in the test file, plus a failure test (`TestLaunch_InstanceTypeNotOffered`) asserting an error when `describeOfferings` returns an empty list.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/baremetal/substrate/ -run TestLaunch -v`
Expected: FAIL — `clients`, `launch`, `Launch` undefined.

- [ ] **Step 3: Write launch.go**

Implement `Launch`, `launch`, the `clients` struct, and helpers following the Design notes above and spec Section 2. Convert `buildTags` to sorted `[]ec2types.Tag`. Keep functions small (`preflight`, `ensureKeypair`, `ensureSecurityGroup`, `resolveAMI`, `runHost`, `allocateEIP`, `openHairpin`, `upsertDNS`). Wrap every error `fmt.Errorf("substrate <step>: %w", err)`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/baremetal/substrate/ -run TestLaunch -v` and `go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Run the full package test + vet**

Run: `go test ./internal/baremetal/substrate/ -count=1` and `go vet ./internal/baremetal/substrate/`
Expected: PASS; vet clean.

- [ ] **Step 6: Commit**

```bash
git add internal/baremetal/substrate/launch.go internal/baremetal/substrate/launch_test.go
git commit -m "substrate: Launch orchestration (keypair, SG, AMI, instance, EIP, DNS)"
```

---

### Task 5: Teardown

**Files:**
- Create: `internal/baremetal/substrate/teardown.go`
- Test: `internal/baremetal/substrate/teardown_test.go`

**Interfaces:**
- Consumes: `TeardownSpec`, the three interfaces, `resolveZoneID`, name/FQDN helpers, the shared fakes.
- Produces: `Teardown(ctx, TeardownSpec) error`; internal `teardown(ctx, clients, sleep func(time.Duration), TeardownSpec) error` (public `Teardown` builds real clients + `time.Sleep` and delegates).

**Design notes:**
- Discover the instance: `DescribeInstances` with filters `tag:ManagedBy=ocpctl`, `tag:ClusterName=<name>`, `instance-state-name` in `[pending running stopping stopped]`. For each instance, `DescribeAddresses` filter `instance-id` to collect `AllocationId` + `PublicIp`.
- DNS: `resolveZoneID(ctx, r53, spec.ZoneID, spec.BaseDomain)`; `ListResourceRecordSets` for the zone; find the A records named `api.<cluster>.<base>.` and `*.apps.<cluster>.<base>.`; `ChangeResourceRecordSets` DELETE with the exact fetched record sets. A missing zone or missing records is not an error.
- `TerminateInstances`; `ec2.NewInstanceTerminatedWaiter(c.ec2).Wait(...)`.
- `ReleaseAddress` per recorded allocation.
- SG: `DescribeSecurityGroups` by tag → `DeleteSecurityGroup`, retried up to 6 times with `sleep(...)` between attempts (deps linger).
- Keypair: `DescribeKeyPairs` by tag (or by name `<cluster>-key`) → `DeleteKeyPair`.
- Every step tolerates not-found (idempotent). Only a hard/unexpected error aborts.

- [ ] **Step 1: Write the failing test**

Create `internal/baremetal/substrate/teardown_test.go`:

```go
package substrate

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTeardown_FullPath(t *testing.T) {
	terminated := false
	released := false
	deletedKey := false
	sgDeleteAttempts := 0

	e := &fakeEC2{
		describeInstances: func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
			st := ec2types.InstanceStateNameTerminated
			if !terminated {
				st = ec2types.InstanceStateNameRunning
			}
			return &ec2.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{{
				InstanceId: aws.String("i-1"), State: &ec2types.InstanceState{Name: st},
			}}}}}, nil
		},
		describeAddresses: func(*ec2.DescribeAddressesInput) (*ec2.DescribeAddressesOutput, error) {
			return &ec2.DescribeAddressesOutput{Addresses: []ec2types.Address{{AllocationId: aws.String("eipalloc-1"), PublicIp: aws.String("198.51.100.9")}}}, nil
		},
		terminate: func(*ec2.TerminateInstancesInput) (*ec2.TerminateInstancesOutput, error) {
			terminated = true
			return &ec2.TerminateInstancesOutput{}, nil
		},
		releaseAddress: func(*ec2.ReleaseAddressInput) (*ec2.ReleaseAddressOutput, error) {
			released = true
			return &ec2.ReleaseAddressOutput{}, nil
		},
		describeSGs: func(*ec2.DescribeSecurityGroupsInput) (*ec2.DescribeSecurityGroupsOutput, error) {
			return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: []ec2types.SecurityGroup{{GroupId: aws.String("sg-1")}}}, nil
		},
		deleteSG: func(*ec2.DeleteSecurityGroupInput) (*ec2.DeleteSecurityGroupOutput, error) {
			sgDeleteAttempts++
			if sgDeleteAttempts < 2 { // first attempt fails (deps linger)
				return nil, errors.New("DependencyViolation")
			}
			return &ec2.DeleteSecurityGroupOutput{}, nil
		},
		describeKeyPairs: func(*ec2.DescribeKeyPairsInput) (*ec2.DescribeKeyPairsOutput, error) {
			return &ec2.DescribeKeyPairsOutput{KeyPairs: []ec2types.KeyPairInfo{{KeyName: aws.String("mycluster-key")}}}, nil
		},
		deleteKeyPair: func(*ec2.DeleteKeyPairInput) (*ec2.DeleteKeyPairOutput, error) {
			deletedKey = true
			return &ec2.DeleteKeyPairOutput{}, nil
		},
	}
	r := &fakeRoute53{
		listZones: func(*route53.ListHostedZonesByNameInput) (*route53.ListHostedZonesByNameOutput, error) {
			return &route53.ListHostedZonesByNameOutput{HostedZones: []r53types.HostedZone{{Id: aws.String("/hostedzone/ZONE1"), Name: aws.String("example.com.")}}}, nil
		},
		listRecords: func(*route53.ListResourceRecordSetsInput) (*route53.ListResourceRecordSetsOutput, error) {
			return &route53.ListResourceRecordSetsOutput{ResourceRecordSets: []r53types.ResourceRecordSet{
				{Name: aws.String("api.mycluster.example.com."), Type: r53types.RRTypeA, TTL: aws.Int64(60), ResourceRecords: []r53types.ResourceRecord{{Value: aws.String("198.51.100.9")}}},
				{Name: aws.String("\\052.apps.mycluster.example.com."), Type: r53types.RRTypeA, TTL: aws.Int64(60), ResourceRecords: []r53types.ResourceRecord{{Value: aws.String("198.51.100.9")}}},
			}}, nil
		},
	}
	c := clients{ec2: e, r53: r}
	err := teardown(context.Background(), c, func(time.Duration) {}, TeardownSpec{
		Region: "us-east-1", ClusterName: "mycluster", BaseDomain: "example.com",
	})
	require.NoError(t, err)
	assert.True(t, terminated)
	assert.True(t, released)
	assert.True(t, deletedKey)
	assert.GreaterOrEqual(t, sgDeleteAttempts, 2, "SG delete retried past first failure")
	require.Len(t, r.changeCalls, 1)
	assert.Equal(t, r53types.ChangeActionDelete, r.changeCalls[0].ChangeBatch.Changes[0].Action)
}

func TestTeardown_NothingFound(t *testing.T) {
	// Empty describes everywhere => no error (idempotent re-run).
	c := clients{ec2: &fakeEC2{}, r53: &fakeRoute53{}}
	err := teardown(context.Background(), c, func(time.Duration) {}, TeardownSpec{
		Region: "us-east-1", ClusterName: "gone", BaseDomain: "example.com",
	})
	require.NoError(t, err)
}
```

(The implementer adds the `time` import. Note the `\\052` escaping AWS uses for the `*` label in `ListResourceRecordSets` output — match records by comparing against both the raw `*.apps...` name and the escaped form, or normalize before comparing.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/baremetal/substrate/ -run TestTeardown -v`
Expected: FAIL — `teardown` undefined.

- [ ] **Step 3: Write teardown.go**

Implement `Teardown` and `teardown` per the Design notes. Add a small `retry(n int, sleep func(time.Duration), delay time.Duration, fn func() error) error` (or reuse a local one) for the SG delete. Handle the `*` record-name escaping. Tolerate not-found at every step.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/baremetal/substrate/ -run TestTeardown -v`
Expected: PASS.

- [ ] **Step 5: Full package gate**

Run: `go test ./internal/baremetal/substrate/ -count=1`, `go vet ./internal/baremetal/substrate/`, `go build ./...`
Expected: all PASS/clean.

- [ ] **Step 6: Commit**

```bash
git add internal/baremetal/substrate/teardown.go internal/baremetal/substrate/teardown_test.go
git commit -m "substrate: tag-based Teardown (DNS, instance, EIP, SG, keypair)"
```

---

## Self-Review

- **Spec coverage:** Section 1 API → Tasks 1,4,5 (types, Launch, Teardown). Section 2 launch steps → Task 4 (preflight/keypair/SG/AMI/instance/EIP/hairpin/DNS). Section 3 teardown → Task 5. Section 4 testing → mock-backed tests in every task. Section 6 profile field → Task 1 Step 1. Ephemeral keypair decision → Task 2. Tagging convention → Task 1 `buildTags` + asserted in Task 4. Tag-based teardown → Task 5.
- **Type consistency:** `clients`, `launch`, `teardown`, the three interfaces, `LaunchSpec`/`TeardownSpec`/`Substrate`, and helper names are used identically across tasks. Fakes defined once (Task 3 `mocks_test.go`) and reused by Tasks 4-5.
- **Placeholders:** none — all code is concrete. Nested-virt `CpuOptions` is pinned to the verified ec2 v1.296.0 field/enum. The one remaining SDK-behaviour spot (`*`-label escaping in Route53 `ListResourceRecordSets` output) is called out explicitly in Task 5 with handling instructions rather than left vague.
- **Out of scope confirmed:** no worker wiring, no shell-out removal (piece 4).
</content>
