#!/usr/bin/env bash
# ==============================================================================
# CodeDuel S3 State Backend & Secrets Provider Bootstrapper
# ==============================================================================
# Sets up an encrypted, versioned AWS S3 bucket for Pulumi DIY state storage,
# logs into the backend, and initializes the shared, dev, and prod stacks
# using the passphrase secrets provider.
#
# Usage:
#   ./infra/scripts/bootstrap_backend.sh <bucket-name> [aws-region] [passphrase]
#
# Example:
#   ./infra/scripts/bootstrap_backend.sh codeduel-pulumi-state-123456 us-east-1
# ==============================================================================

set -euo pipefail

BUCKET_NAME="${1:-}"
AWS_REGION="${2:-us-east-1}"
PASSPHRASE="${3:-${PULUMI_CONFIG_PASSPHRASE:-}}"

if [ -z "$BUCKET_NAME" ]; then
  echo "Error: S3 bucket name is required."
  echo "Usage: $0 <bucket-name> [aws-region] [passphrase]"
  exit 1
fi

if [ -z "$PASSPHRASE" ]; then
  echo "Error: Passphrase is required either as arg 3 or via PULUMI_CONFIG_PASSPHRASE."
  exit 1
fi

export PULUMI_CONFIG_PASSPHRASE="$PASSPHRASE"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
INFRA_DIR="$ROOT_DIR/infra"

echo "==> 1. Verifying AWS caller identity..."
aws sts get-caller-identity > /dev/null

echo "==> 2. Checking / creating S3 state bucket: $BUCKET_NAME in $AWS_REGION..."
if ! aws s3api head-bucket --bucket "$BUCKET_NAME" 2>/dev/null; then
  if [ "$AWS_REGION" = "us-east-1" ]; then
    aws s3api create-bucket --bucket "$BUCKET_NAME" --region "$AWS_REGION"
  else
    aws s3api create-bucket --bucket "$BUCKET_NAME" --region "$AWS_REGION" \
      --create-bucket-configuration LocationConstraint="$AWS_REGION"
  fi
  echo "    Created bucket $BUCKET_NAME"
else
  echo "    Bucket $BUCKET_NAME already exists"
fi

echo "==> 3. Enabling versioning on $BUCKET_NAME..."
aws s3api put-bucket-versioning \
  --bucket "$BUCKET_NAME" \
  --versioning-configuration Status=Enabled

echo "==> 4. Enforcing server-side encryption (AES256) on $BUCKET_NAME..."
aws s3api put-bucket-encryption \
  --bucket "$BUCKET_NAME" \
  --server-side-encryption-configuration '{
    "Rules": [
      {
        "ApplyServerSideEncryptionByDefault": {
          "SSEAlgorithm": "AES256"
        }
      }
    ]
  }'

echo "==> 5. Blocking all public access on $BUCKET_NAME..."
aws s3api put-public-access-block \
  --bucket "$BUCKET_NAME" \
  --public-access-block-configuration '{
    "BlockPublicAcls": true,
    "IgnorePublicAcls": true,
    "BlockPublicPolicy": true,
    "RestrictPublicBuckets": true
  }'

echo "==> 6. Logging into Pulumi S3 backend..."
pulumi login "s3://${BUCKET_NAME}?region=${AWS_REGION}"

echo "==> 7. Initializing codeduel-shared stack..."
cd "$INFRA_DIR/shared"
if ! pulumi stack select shared 2>/dev/null; then
  pulumi stack init shared --secrets-provider=passphrase
fi
pulumi config set aws:region "$AWS_REGION"

echo "==> 8. Initializing codeduel-env stacks (dev and prod)..."
cd "$INFRA_DIR/env"
for env_stack in dev prod; do
  if ! pulumi stack select "$env_stack" 2>/dev/null; then
    pulumi stack init "$env_stack" --secrets-provider=passphrase
  fi
  pulumi config set aws:region "$AWS_REGION" --stack "$env_stack"
done

echo ""
echo "=============================================================================="
echo "Pulumi state backend bootstrap complete!"
echo "Backend: s3://${BUCKET_NAME}?region=${AWS_REGION}"
echo "Stacks initialized: codeduel-shared/shared, codeduel-env/dev, codeduel-env/prod"
echo ""
echo "Before running pulumi commands, ensure PULUMI_CONFIG_PASSPHRASE is set:"
echo "  export PULUMI_CONFIG_PASSPHRASE='${PASSPHRASE}'"
echo "  pulumi login s3://${BUCKET_NAME}?region=${AWS_REGION}"
echo "=============================================================================="
