#!/usr/bin/env bash
# ==============================================================================
# Render a Kustomize overlay with real ECR image references
# ==============================================================================
# deploy/k8s ships placeholders (ACCOUNT_ID.dkr.ecr.REGION.amazonaws.com and
# newTag: latest) so the manifests render locally without an AWS account. Both
# the deploy path and the PR diff path need the same substitutions applied to a
# throwaway copy of the tree - if they disagree, `kubectl diff` reports changes
# that the deploy would not actually make.
#
# Pure text substitution: no aws or pulumi calls, so callers pass values they
# have already resolved.
#
# Usage:
#   ./infra/scripts/render_overlay.sh <dev|prod> <ecr-registry> <image-tag> <dest-dir>
#
# <dest-dir> must not exist; it is created and left in place for the caller to
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

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

mkdir -p "$DEST_DIR"
cp -r "$ROOT_DIR/deploy/k8s" "$DEST_DIR/k8s"

sed -i \
  -e "s|ACCOUNT_ID.dkr.ecr.REGION.amazonaws.com|$ECR_REGISTRY|g" \
  -e "s|newTag: latest|newTag: $IMAGE_TAG|" \
  "$DEST_DIR/k8s/overlays/$ENV_NAME/kustomization.yaml"

# Carries the sandbox image references (JUDGE_PYTHON_IMAGE and friends).
sed -i \
  "s|ACCOUNT_ID.dkr.ecr.REGION.amazonaws.com|$ECR_REGISTRY|g" \
  "$DEST_DIR/k8s/base/config.env"

echo "$DEST_DIR/k8s/overlays/$ENV_NAME"
