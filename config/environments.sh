#!/bin/bash
# Central deployment-target configuration for ocpctl.
#
# Single source of truth for the *non-secret* targeting values that the deploy
# and ops scripts share (hosts, domain, SSH key paths/names, S3 buckets). This
# file is tracked in git on purpose — none of these are secrets.
#
# Secrets are NOT here and never should be. To actually deploy you additionally
# need, out-of-band (see docs/development/DEVOPS.md "What you need to deploy"):
#   1. The SSH private key at $SSH_KEY (or override OCPCTL_SSH_KEY).
#   2. The real config/{api,worker}.env.<env> files (gitignored; templates in git).
#   3. AWS credentials with access to the S3 buckets and the worker ASG.
#
# Usage (from a script):
#   SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
#   source "$SCRIPT_DIR/../config/environments.sh"
#   load_environment dev        # or: production
#   ssh -i "$SSH_KEY" "$SSH_USER@$API_HOST" ...
#
# Machine-specific bits can be overridden without editing this file:
#   OCPCTL_SSH_USER   SSH user (default: ubuntu)
#   OCPCTL_SSH_KEY    full path to the SSH private key (default: ~/.ssh/ocpctl-<env>-key)

# Base DNS zone shared by both environments (Route53 hosted zone).
OCPCTL_BASE_DOMAIN="mg.dog8code.com"

# load_environment <dev|production>
#
# Populates the following variables in the caller's shell:
#   ENV_NAME API_HOST WORKER_HOSTS[] SSH_USER SSH_KEY S3_BUCKET
#   S3_ARTIFACTS_BUCKET DOMAIN BASE_DOMAIN AUTOSCALE_TAG CONFIG_SUFFIX RDS_HOST
load_environment() {
  local env="${1:?load_environment: environment required (dev|production)}"

  SSH_USER="${OCPCTL_SSH_USER:-ubuntu}"
  BASE_DOMAIN="$OCPCTL_BASE_DOMAIN"

  case "$env" in
    dev)
      ENV_NAME="dev"
      API_HOST="44.214.230.178"
      WORKER_HOSTS=("44.214.230.178")
      SSH_KEY="${OCPCTL_SSH_KEY:-$HOME/.ssh/ocpctl-dev-key}"
      S3_BUCKET="s3://ocpctl-dev-binaries"
      S3_ARTIFACTS_BUCKET="s3://ocpctl-dev-artifacts"
      DOMAIN="dev.ocpctl.${OCPCTL_BASE_DOMAIN}"
      AUTOSCALE_TAG="ocpctl-dev-worker"
      CONFIG_SUFFIX="dev"
      RDS_HOST="ocpctl-dev-db.czu6z8r7it71.us-east-1.rds.amazonaws.com"
      ;;
    production)
      ENV_NAME="production"
      API_HOST="44.201.165.78"
      WORKER_HOSTS=("44.201.165.78")
      SSH_KEY="${OCPCTL_SSH_KEY:-$HOME/.ssh/ocpctl-production-key}"
      S3_BUCKET="s3://ocpctl-binaries"
      S3_ARTIFACTS_BUCKET="s3://ocpctl-artifacts"
      DOMAIN="ocpctl.${OCPCTL_BASE_DOMAIN}"
      AUTOSCALE_TAG="ocpctl-worker"
      CONFIG_SUFFIX="production"
      RDS_HOST="44.201.165.78"
      ;;
    *)
      echo "load_environment: unknown environment '$env' (expected dev|production)" >&2
      return 1
      ;;
  esac
}
