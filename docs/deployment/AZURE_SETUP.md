# Azure Platform Setup Guide

This guide covers the configuration required to provision **Azure OpenShift IPI**
clusters (`azure-standard`, `azure-sno-ga`) with ocpctl.

> **Scope.** This is for self-managed OpenShift IPI on Azure VMs
> (`clusterType: openshift`, `platform: azure`). It is **not** ARO
> (`clusterType: aro`, `aro-standard`), which is a managed service that uses
> `az aro create` and Microsoft's published version catalog — see the ARO notes
> in the profile and CLAUDE.md instead.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Azure Credentials (Service Principal)](#azure-credentials-service-principal)
- [DNS Configuration](#dns-configuration)
- [Environment Variables](#environment-variables)
- [Creating Your First Azure Cluster](#creating-your-first-azure-cluster)
- [Troubleshooting](#troubleshooting)

## Prerequisites

1. **Azure subscription** the worker service principal can operate in. In this
   deployment that is `migration-eng` (`72701f06-9d25-42f9-85d0-2adc2979020e`,
   tenant `520cf09d-78ff-44ed-a731-abd623e73b09`).
2. **Service principal** with Contributor on the subscription (see below).
3. **A public Azure DNS zone** for the base domain, delegated from the Route53
   parent (see [DNS Configuration](#dns-configuration)). **This is the step most
   likely to be missing** — Azure IPI cannot create records in Route53.
4. **OpenShift pull secret** from Red Hat.
5. Worker binaries installed by `ensure-installers.sh` (`openshift-install`, `az`).

## Azure Credentials (Service Principal)

Azure IPI authenticates via a service principal. `scripts/azure-login.sh` runs as
an `ExecStartPre` hook on every worker: it runs `az login --service-principal`
and then writes `~/.azure/osServicePrincipal.json`, which `openshift-install`
reads. The following environment variables (from `worker.env` /
`s3://ocpctl-binaries/config/worker.env`) drive it:

```bash
AZURE_SUBSCRIPTION_ID=72701f06-9d25-42f9-85d0-2adc2979020e
AZURE_TENANT_ID=520cf09d-78ff-44ed-a731-abd623e73b09
AZURE_CLIENT_ID=<service-principal-app-id>
AZURE_CLIENT_SECRET=<service-principal-password>
```

> If `AZURE_SUBSCRIPTION_ID` is unset, `azure-login.sh` skips
> `osServicePrincipal.json` and `openshift-install` fails with
> `failed to retrieve credentials from user: EOF`. See CLAUDE.md, "Azure
> Credential Fix on Autoscale Workers".

## DNS Configuration

ocpctl uses **subdomain delegation** from the Route53 parent domain to each
cloud's own DNS service (same pattern as [IBM Cloud](IBMCLOUD_SETUP.md)). For
Azure the subdomain must be a **public Azure DNS zone** — the installer creates
the `api.<cluster>` / `*.apps.<cluster>` records there.

### DNS Architecture

```
mg.dog8code.com (Route53 - parent)
├── <cluster>.mg.dog8code.com        (AWS clusters)
├── ibm.mg.dog8code.com              (IBM CIS - delegated)
└── azure.mg.dog8code.com            (Azure DNS - delegated)   <-- this guide
    ├── api.<cluster>.azure.mg.dog8code.com
    └── *.apps.<cluster>.azure.mg.dog8code.com
```

The Azure profiles are already configured for this base domain and resource
group:

```yaml
# internal/profile/definitions/azure-standard.yaml (and azure-sno-ga.yaml)
baseDomains:
  allowlist: [azure.mg.dog8code.com]
  default: azure.mg.dog8code.com
platformConfig:
  azure:
    baseDomainResourceGroup: azure-mg-dog8code-com-dns
```

### Setting Up DNS Delegation

#### Step 1: Create the Azure DNS zone

Create it in the **same subscription the worker SP uses** — the RG name must match
`baseDomainResourceGroup` in the profiles.

```bash
az group create -n azure-mg-dog8code-com-dns -l eastus
az network dns zone create -g azure-mg-dog8code-com-dns -n azure.mg.dog8code.com

# Read the assigned Azure name servers:
az network dns zone show -g azure-mg-dog8code-com-dns -n azure.mg.dog8code.com \
  --query nameServers -o json
# e.g. ns1-02.azure-dns.com, ns2-02.azure-dns.net, ns3-02.azure-dns.org, ns4-02.azure-dns.info
```

#### Step 2: Delegate the subdomain in Route53

Create (or update) an `NS` record for `azure.mg.dog8code.com` in the Route53
parent (`mg.dog8code.com`, zone id `Z2GE8CSGW2ZA8W`) pointing at the name servers
from Step 1:

```bash
aws route53 change-resource-record-sets \
  --hosted-zone-id Z2GE8CSGW2ZA8W \
  --change-batch '{
    "Changes": [{
      "Action": "UPSERT",
      "ResourceRecordSet": {
        "Name": "azure.mg.dog8code.com",
        "Type": "NS",
        "TTL": 300,
        "ResourceRecords": [
          {"Value": "ns1-02.azure-dns.com"},
          {"Value": "ns2-02.azure-dns.net"},
          {"Value": "ns3-02.azure-dns.org"},
          {"Value": "ns4-02.azure-dns.info"}
        ]
      }
    }]
  }'
```

> **Do not create a Route53 hosted zone for `azure.mg.dog8code.com`.** The
> subdomain must resolve to Azure DNS. A parallel Route53 zone for the same name
> is non-authoritative cruft (the parent's NS record wins) and should be deleted:
> `aws route53 delete-hosted-zone --id <zone-id>`.

#### Step 3: Verify

```bash
dig @8.8.8.8 azure.mg.dog8code.com NS +short
# should list the ns*-NN.azure-dns.* servers, NOT awsdns / Route53

# And confirm the worker's SP can actually see the zone:
az network dns zone show -g azure-mg-dog8code-com-dns -n azure.mg.dog8code.com \
  --subscription 72701f06-9d25-42f9-85d0-2adc2979020e --query name -o tsv
```

## Environment Variables

Add to `/etc/ocpctl/worker.env` (single source of truth:
`s3://ocpctl-binaries/config/worker.env`):

```bash
AZURE_SUBSCRIPTION_ID=<subscription-id>
AZURE_TENANT_ID=<tenant-id>
AZURE_CLIENT_ID=<sp-app-id>
AZURE_CLIENT_SECRET=<sp-password>

OPENSHIFT_PULL_SECRET='<your-pull-secret-json>'
```

Restart the worker (and re-run `azure-login.sh` implicitly via the unit) after
changes: `sudo systemctl restart ocpctl-worker`.

## Creating Your First Azure Cluster

1. Confirm the profile is enabled and visible:
   ```bash
   curl -H "Authorization: Bearer <token>" \
     https://dev.ocpctl.mg.dog8code.com/api/v1/profiles \
     | jq '.data[] | select(.name=="azure-standard") | {name, enabled}'
   ```
2. Create `azure-standard` @ 4.22 in `eastus2` (base domain
   `azure.mg.dog8code.com`) via the UI or API.
3. Monitor: cluster status should advance CREATING → READY. The DNS record
   creation step (`api.<cluster>` CNAME) is where a missing/misdelegated zone
   fails — see below.

> **Hibernate/resume is not implemented for Azure OpenShift IPI** (only AWS, GCP,
> IBM). Off-hours scaling is disabled on both Azure profiles; control cost via
> TTL or manual destroy. Tracked in #147.

## Troubleshooting

### `ResourceGroupNotFound` creating DNS records

**Symptom:** infra provisions, then create fails at:
```
error creating DNS records: failed to create public record set:
PUT .../resourceGroups/azure-mg-dog8code-com-dns/.../dnsZones/azure.mg.dog8code.com/CNAME/api.<cluster>
RESPONSE 404: ResourceGroupNotFound
```

**Cause:** the Azure DNS zone / resource group named by
`platformConfig.azure.baseDomainResourceGroup` does not exist **in the
subscription the worker SP operates in**. Delegation in Route53 alone is not
enough — the zone must be an Azure DNS zone the SP can write.

**Fix:** follow [DNS Configuration](#dns-configuration). Verify with:
```bash
az network dns zone show -g azure-mg-dog8code-com-dns -n azure.mg.dog8code.com \
  --subscription <sub-id>
```

### `failed to retrieve credentials from user: EOF`

The SP file was not written. Ensure `AZURE_SUBSCRIPTION_ID` (and the other
`AZURE_*` vars) are set in `worker.env`; `azure-login.sh` writes
`~/.azure/osServicePrincipal.json` only when they are present.

### Create retries 3× then fails "Cluster API server temporarily unreachable"

A permanent error (e.g. the `ResourceGroupNotFound` above) can be misclassified
as transient because the CAPI logs also contain `cluster is not reachable`
(DNS lookups failing). Each retry re-provisions and tears down infra. Look for
the **first** `level=error` line in the worker journal for the real cause rather
than the final transient message. (Classifier fix tracked separately.)

## Reference Links

- [Installing OpenShift on Azure](https://docs.redhat.com/en/documentation/openshift_container_platform/4.22/html/installing_on_azure/index)
- [Azure DNS zone delegation](https://learn.microsoft.com/en-us/azure/dns/dns-delegate-domain-azure-dns)
- IBM Cloud equivalent: [IBMCLOUD_SETUP.md](IBMCLOUD_SETUP.md)
