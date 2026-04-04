#!/usr/bin/env python3
"""
cleanup-iam-roles.py - Identify and delete orphaned OpenShift IAM roles
Created by ocpctl to clean up IAM roles from failed cluster installations
"""

import argparse
import boto3
import json
import re
import sys
from collections import defaultdict
from typing import Set, List, Dict, Tuple

# ANSI color codes
class Colors:
    RED = '\033[0;31m'
    GREEN = '\033[0;32m'
    YELLOW = '\033[1;33m'
    BLUE = '\033[0;34m'
    NC = '\033[0m'  # No Color


def extract_infra_id(role_name: str) -> str:
    """
    Extract infraID from role name.
    Example: "tsanders-test-445-kqbf7-openshift-cloud-credential-operator-clou" -> "tsanders-test-445-kqbf7"
    """
    # Pattern: {name}-{5chars}-openshift-*
    match = re.match(r'^(.+)-([a-z0-9]{5})-openshift-', role_name)
    if match:
        return f"{match.group(1)}-{match.group(2)}"

    # Pattern: {name}-{5chars}-(master|worker)-role
    match = re.match(r'^(.+)-([a-z0-9]{5})-(master|worker)-role', role_name)
    if match:
        return f"{match.group(1)}-{match.group(2)}"

    return ""


def extract_cluster_name(infra_id: str) -> str:
    """
    Extract cluster name from infraID.
    Example: "tsanders-test-445-kqbf7" -> "tsanders-test-445"
    """
    # Remove the 5-char suffix
    match = re.match(r'^(.+)-[a-z0-9]{5}$', infra_id)
    if match:
        return match.group(1)
    return ""


def get_active_clusters() -> Set[str]:
    """
    Get list of active cluster names from ocpctl API.
    Returns empty set if unable to fetch (safer to not delete anything).
    """
    import subprocess

    try:
        # Query ocpctl API via SSH to get active clusters
        cmd = [
            'ssh', '-i', '/Users/tsanders/.ssh/ocpctl-test-key.pem',
            'ec2-user@52.90.135.148',
            'curl -s http://localhost:8080/api/v1/clusters 2>/dev/null'
        ]

        result = subprocess.run(cmd, capture_output=True, text=True, timeout=10)

        if result.returncode != 0:
            print(f"{Colors.RED}ERROR: Failed to fetch clusters from API{Colors.NC}")
            print(f"{Colors.YELLOW}Aborting - cannot safely determine orphaned roles{Colors.NC}")
            sys.exit(1)

        # Parse JSON response
        data = json.loads(result.stdout)
        clusters = data.get('clusters', [])

        # Get cluster names that are not destroyed
        active_cluster_names = set()
        for cluster in clusters:
            if cluster.get('status') != 'destroyed':
                active_cluster_names.add(cluster['name'])

        print(f"{Colors.GREEN}Found {len(active_cluster_names)} active clusters in database{Colors.NC}")

        # Print active clusters for verification
        if active_cluster_names:
            print(f"{Colors.BLUE}Active clusters:{Colors.NC}")
            for name in sorted(list(active_cluster_names)[:10]):
                print(f"  - {name}")
            if len(active_cluster_names) > 10:
                print(f"  ... and {len(active_cluster_names) - 10} more")

        return active_cluster_names

    except subprocess.TimeoutExpired:
        print(f"{Colors.RED}ERROR: Timeout fetching clusters from API{Colors.NC}")
        print(f"{Colors.YELLOW}Aborting - cannot safely determine orphaned roles{Colors.NC}")
        sys.exit(1)
    except json.JSONDecodeError as e:
        print(f"{Colors.RED}ERROR: Failed to parse API response: {e}{Colors.NC}")
        print(f"{Colors.YELLOW}Aborting - cannot safely determine orphaned roles{Colors.NC}")
        sys.exit(1)
    except Exception as e:
        print(f"{Colors.RED}ERROR: Failed to get active clusters: {e}{Colors.NC}")
        print(f"{Colors.YELLOW}Aborting - cannot safely determine orphaned roles{Colors.NC}")
        sys.exit(1)


def scan_iam_roles(region: str) -> Tuple[List[str], Dict[str, List[str]]]:
    """
    Scan all IAM roles and identify OpenShift-related ones.
    Returns: (all_openshift_roles, roles_by_infra_id)
    """
    iam_client = boto3.client('iam', region_name=region)

    all_openshift_roles = []
    roles_by_infra_id = defaultdict(list)

    print(f"{Colors.BLUE}Scanning IAM roles...{Colors.NC}")

    # Paginate through all roles
    paginator = iam_client.get_paginator('list_roles')
    total_roles = 0

    for page in paginator.paginate():
        for role in page['Roles']:
            role_name = role['RoleName']
            total_roles += 1

            # Check if this is an OpenShift role
            if '-openshift-' in role_name or role_name.endswith('-master-role') or role_name.endswith('-worker-role'):
                all_openshift_roles.append(role_name)

                # Extract infraID
                infra_id = extract_infra_id(role_name)
                if infra_id:
                    roles_by_infra_id[infra_id].append(role_name)

    print(f"{Colors.GREEN}Scanned {total_roles} total IAM roles{Colors.NC}")
    print(f"{Colors.YELLOW}Found {len(all_openshift_roles)} OpenShift-related roles{Colors.NC}")
    print(f"{Colors.YELLOW}Grouped into {len(roles_by_infra_id)} infrastructure IDs{Colors.NC}")
    print()

    return all_openshift_roles, roles_by_infra_id


def get_all_known_clusters() -> Set[str]:
    """
    Get ALL cluster names that have ever been in the ocpctl database.
    This includes both active and destroyed clusters - we only want to delete
    roles for clusters that we created via ocpctl.
    """
    import subprocess

    try:
        cmd = [
            'ssh', '-i', '/Users/tsanders/.ssh/ocpctl-test-key.pem',
            'ec2-user@52.90.135.148',
            'sudo -u postgres psql -d ocpctl -t -c "SELECT name FROM clusters;"'
        ]

        result = subprocess.run(cmd, capture_output=True, text=True, timeout=10)

        if result.returncode != 0:
            print(f"{Colors.RED}ERROR: Failed to fetch cluster list from database{Colors.NC}")
            print(f"{Colors.YELLOW}Aborting - cannot safely determine which roles belong to ocpctl{Colors.NC}")
            sys.exit(1)

        # Parse cluster names (strip whitespace)
        cluster_names = set()
        for line in result.stdout.split('\n'):
            line = line.strip()
            if line and not line.startswith('Permission denied'):
                cluster_names.add(line)

        print(f"{Colors.GREEN}Found {len(cluster_names)} total clusters in ocpctl database{Colors.NC}")
        print(f"{Colors.BLUE}Will ONLY delete roles for clusters managed by ocpctl{Colors.NC}")

        return cluster_names

    except Exception as e:
        print(f"{Colors.RED}ERROR: Failed to get cluster list: {e}{Colors.NC}")
        print(f"{Colors.YELLOW}Aborting - cannot safely determine which roles belong to ocpctl{Colors.NC}")
        sys.exit(1)


def filter_orphaned_roles(roles_by_infra_id: Dict[str, List[str]], active_clusters: Set[str], all_known_clusters: Set[str]) -> Dict[str, List[str]]:
    """
    Filter roles to find orphaned ones (not belonging to active clusters).
    IMPORTANT: Only consider roles orphaned if:
    1. The cluster name exists in ocpctl database (ever)
    2. AND the cluster is not currently active

    This prevents deleting roles from clusters created outside of ocpctl.
    """
    orphaned_by_infra_id = {}
    skipped_not_ocpctl = []

    for infra_id, roles in roles_by_infra_id.items():
        cluster_name = extract_cluster_name(infra_id)

        if not cluster_name:
            print(f"{Colors.YELLOW}WARNING: Could not extract cluster name from infraID: {infra_id}{Colors.NC}")
            continue

        # Check if this cluster was EVER managed by ocpctl
        if cluster_name not in all_known_clusters:
            skipped_not_ocpctl.append(cluster_name)
            continue

        # Check if cluster is active
        if cluster_name not in active_clusters:
            orphaned_by_infra_id[infra_id] = roles

    if skipped_not_ocpctl:
        unique_skipped = list(set(skipped_not_ocpctl))
        print(f"\n{Colors.YELLOW}Skipped {len(unique_skipped)} clusters NOT managed by ocpctl:{Colors.NC}")
        for name in sorted(unique_skipped[:10]):
            print(f"  - {name}")
        if len(unique_skipped) > 10:
            print(f"  ... and {len(unique_skipped) - 10} more")
        print()

    return orphaned_by_infra_id


def display_summary(orphaned_by_infra_id: Dict[str, List[str]]):
    """
    Display summary of orphaned roles.
    """
    total_orphaned = sum(len(roles) for roles in orphaned_by_infra_id.values())

    print(f"{Colors.BLUE}=== Orphaned Roles Summary ==={Colors.NC}")
    print()
    print(f"  Total orphaned clusters: {Colors.RED}{len(orphaned_by_infra_id)}{Colors.NC}")
    print(f"  Total orphaned roles: {Colors.RED}{total_orphaned}{Colors.NC}")
    print()

    # Group by cluster and show counts
    for infra_id in sorted(orphaned_by_infra_id.keys()):
        roles = orphaned_by_infra_id[infra_id]
        cluster_name = extract_cluster_name(infra_id)
        print(f"  {Colors.YELLOW}{cluster_name:<40}{Colors.NC} (infraID: {infra_id}) - {len(roles)} roles")

    print()

    # Show sample roles
    print(f"{Colors.BLUE}Sample orphaned roles (first 20):{Colors.NC}")
    count = 0
    for roles in orphaned_by_infra_id.values():
        for role in roles[:5]:  # Show up to 5 roles per cluster
            if count >= 20:
                break
            print(f"  - {role}")
            count += 1
        if count >= 20:
            break

    if total_orphaned > 20:
        print(f"  ... and {total_orphaned - 20} more")

    print()


def delete_role(iam_client, role_name: str) -> bool:
    """
    Delete a single IAM role (with policies).
    Returns True if successful, False otherwise.
    """
    try:
        # Detach managed policies
        attached_policies = iam_client.list_attached_role_policies(RoleName=role_name)
        for policy in attached_policies.get('AttachedPolicies', []):
            iam_client.detach_role_policy(
                RoleName=role_name,
                PolicyArn=policy['PolicyArn']
            )

        # Delete inline policies
        inline_policies = iam_client.list_role_policies(RoleName=role_name)
        for policy_name in inline_policies.get('PolicyNames', []):
            iam_client.delete_role_policy(
                RoleName=role_name,
                PolicyName=policy_name
            )

        # Delete the role
        iam_client.delete_role(RoleName=role_name)
        return True

    except Exception as e:
        print(f"{Colors.RED}ERROR deleting {role_name}: {e}{Colors.NC}")
        return False


def delete_orphaned_roles(orphaned_by_infra_id: Dict[str, List[str]], region: str, force: bool = False):
    """
    Delete orphaned IAM roles.
    """
    total_to_delete = sum(len(roles) for roles in orphaned_by_infra_id.values())

    if not force:
        print(f"{Colors.RED}WARNING: This will permanently delete {total_to_delete} IAM roles!{Colors.NC}")
        print()
        confirm = input("Type 'yes' to confirm deletion: ")
        if confirm != 'yes':
            print(f"{Colors.YELLOW}Cancelled.{Colors.NC}")
            return

    iam_client = boto3.client('iam', region_name=region)

    print()
    print(f"{Colors.BLUE}Deleting orphaned IAM roles...{Colors.NC}")

    deleted = 0
    failed = 0

    for infra_id, roles in orphaned_by_infra_id.items():
        print(f"\n{Colors.YELLOW}Cluster: {extract_cluster_name(infra_id)} ({infra_id}){Colors.NC}")

        for role in roles:
            sys.stdout.write(f"  Deleting {role}... ")
            sys.stdout.flush()

            if delete_role(iam_client, role):
                print(f"{Colors.GREEN}OK{Colors.NC}")
                deleted += 1
            else:
                print(f"{Colors.RED}FAILED{Colors.NC}")
                failed += 1

    print()
    print(f"{Colors.BLUE}Deletion complete:{Colors.NC}")
    print(f"  Successfully deleted: {Colors.GREEN}{deleted}{Colors.NC}")
    if failed > 0:
        print(f"  Failed: {Colors.RED}{failed}{Colors.NC}")
    print()


def main():
    parser = argparse.ArgumentParser(
        description='Identify and delete orphaned OpenShift IAM roles',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # List all orphaned roles (dry-run)
  %(prog)s

  # Delete orphaned roles with confirmation
  %(prog)s --execute

  # Delete without confirmation (USE WITH CAUTION)
  %(prog)s --execute --force
        """
    )

    parser.add_argument('--execute', action='store_true',
                        help='Actually delete roles (default: dry-run)')
    parser.add_argument('--force', action='store_true',
                        help='Skip confirmation prompts')
    parser.add_argument('--region', default='us-east-1',
                        help='AWS region (default: us-east-1)')

    args = parser.parse_args()

    print(f"{Colors.BLUE}=== OpenShift IAM Role Cleanup ==={Colors.NC}")
    print()
    print(f"Region: {Colors.YELLOW}{args.region}{Colors.NC}")
    print(f"Mode: {Colors.YELLOW}{'EXECUTE' if args.execute else 'DRY RUN'}{Colors.NC}")
    print()

    # Get active clusters
    active_clusters = get_active_clusters()
    print()

    # Get ALL clusters ever managed by ocpctl (to filter out non-ocpctl roles)
    all_known_clusters = get_all_known_clusters()
    print()

    # Scan IAM roles
    all_roles, roles_by_infra_id = scan_iam_roles(args.region)

    # Filter orphaned roles (only from ocpctl-managed clusters)
    orphaned_by_infra_id = filter_orphaned_roles(roles_by_infra_id, active_clusters, all_known_clusters)

    if not orphaned_by_infra_id:
        print(f"{Colors.GREEN}No orphaned IAM roles found!{Colors.NC}")
        return 0

    # Display summary
    display_summary(orphaned_by_infra_id)

    if not args.execute:
        print(f"{Colors.YELLOW}DRY RUN MODE - No roles will be deleted{Colors.NC}")
        print()
        print("To actually delete these roles, run:")
        print(f"  {sys.argv[0]} --execute")
        return 0

    # Delete roles
    delete_orphaned_roles(orphaned_by_infra_id, args.region, args.force)

    print(f"{Colors.GREEN}=== Cleanup Complete ==={Colors.NC}")
    print()
    print("Next steps:")
    print("  1. Verify current IAM role count: aws iam list-roles --query 'length(Roles)'")
    print("  2. Run orphan detection: Check admin console")
    print("  3. Retry failed cluster creation")

    return 0


if __name__ == '__main__':
    sys.exit(main())
