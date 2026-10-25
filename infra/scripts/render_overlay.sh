#!/usr/bin/env bash
# ==============================================================================
# Render a Kustomize overlay with real ECR image references
# ==============================================================================
# deploy/k8s ships placeholders (ACCOUNT_ID.dkr.ecr.REGION.amazonaws.com and
# RELEASE_TAG) so manifests render locally without an AWS account. Deployment
# and PR artifact rendering share the same substitutions on a throwaway copy.
#
# Pure text substitution: no aws or pulumi calls, so callers pass values they
# have already resolved.
#
# Usage:
#   ./infra/scripts/render_overlay.sh <dev|prod> <ecr-registry> <image-tag> <dest-dir>
#
# <image-tag> is a full lowercase Git commit SHA. <dest-dir> must not exist;
# it is created and left in place for the caller to
# use and clean up. Prints the path to the rendered overlay directory.
# ==============================================================================

set -euo pipefail

ENV_NAME="${1:-}"
ECR_REGISTRY="${2:-}"
IMAGE_TAG="${3:-}"
DEST_DIR="${4:-}"

case "$ENV_NAME" in
  dev | prod) ;;
  *)
    echo "Usage: $0 <dev|prod> <ecr-registry> <image-tag> <dest-dir>" >&2
    exit 1
    ;;
esac

if [ -z "$ECR_REGISTRY" ] || [ -z "$IMAGE_TAG" ] || [ -z "$DEST_DIR" ]; then
  echo "Usage: $0 <dev|prod> <ecr-registry> <image-tag> <dest-dir>" >&2
  exit 1
fi

if [[ ! "$IMAGE_TAG" =~ ^[a-f0-9]{40}$ ]] || \
  [[ ! "$ECR_REGISTRY" =~ ^[a-zA-Z0-9.-]+$ ]]; then
  echo "Expected a registry hostname and full lowercase Git commit SHA." >&2
  exit 1
fi
if [ -e "$DEST_DIR" ]; then
  echo "Render destination must not exist: $DEST_DIR" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

mkdir -p "$DEST_DIR"
cp -r "$ROOT_DIR/deploy/k8s" "$DEST_DIR/k8s"

sed -i \
  -e "s|ACCOUNT_ID.dkr.ecr.REGION.amazonaws.com|$ECR_REGISTRY|g" \
  -e "s|newTag: RELEASE_TAG|newTag: $IMAGE_TAG|" \
  "$DEST_DIR/k8s/overlays/$ENV_NAME/kustomization.yaml"

# Carries the sandbox image references (JUDGE_PYTHON_IMAGE and friends).
sed -i \
  -e "s|ACCOUNT_ID.dkr.ecr.REGION.amazonaws.com|$ECR_REGISTRY|g" \
  -e "s|:RELEASE_TAG|:$IMAGE_TAG|g" \
  "$DEST_DIR/k8s/base/config.env"

sed -i "s|^  name: migrate$|  name: migrate-$IMAGE_TAG|" \
  "$DEST_DIR/k8s/base/migrate-job.yaml"

echo "$DEST_DIR/k8s/overlays/$ENV_NAME"
