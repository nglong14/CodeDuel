#!/usr/bin/env bash
# ==============================================================================
# CodeDuel Environment Deploy Pipeline
# ==============================================================================
# The single deploy path for both environments, used by GitHub Actions
# (.github/workflows/deploy.yml) and by hand as the break-glass path. There is
# deliberately no second implementation of this sequence in workflow YAML: two
# copies drift, and the one that drifts is the one you are not debugging.
#
# Run this after infra/scripts/bootstrap_backend.sh has initialized the Pulumi
# stacks. Drives the full deploy sequence for one environment:
#   1. Applies the codeduel-shared stack (VPC, EKS, RDS, ElastiCache, ECR, ALB
#      controller, Fluent Bit). Safe to re-run; Pulumi is idempotent.
#   2. Applies the codeduel-env stack (namespace, secrets, DB bootstrap Job,
#      log group).
#   3. Builds and pushes the codeduel + sandbox images to ECR, UNLESS the image
#      tag is already there (see "Promotion" below).
#   4. Applies the Kustomize overlay and runs the DB migration Job.
#
# Promotion: if codeduel:<image-tag> already exists in ECR, all four builds are
# skipped and the existing images are deployed as-is. That is what makes prod
# run the exact bytes dev ran - invoke it with the same tag - and it makes
# re-running a failed deploy cheap. No digest plumbing required.
#
# Requires: aws cli (logged in), pulumi cli (logged into the S3 backend),
# docker with buildx, kubectl, jq, and PULUMI_CONFIG_PASSPHRASE set.
#
# Note on architectures: the 'codeduel' image runs on the app node group
# (arm64 Graviton), so it is cross-built for linux/arm64 regardless of the
# host running this script. Sandbox images run on the gVisor node group
# (x86_64), so they are built for linux/amd64.
#
# Usage:
#   ./infra/scripts/deploy_env.sh <dev|prod> [image-tag]
#
# image-tag defaults to the current git short SHA.
# ==============================================================================

set -euo pipefail

ENV_NAME="${1:-}"
case "$ENV_NAME" in
  dev | prod) ;;
  *)
    echo "Usage: $0 <dev|prod> [image-tag]"
    exit 1
    ;;
esac

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
INFRA_DIR="$ROOT_DIR/infra"
IMAGE_TAG="${2:-$(git -C "$ROOT_DIR" rev-parse --short HEAD)}"
NAMESPACE="codeduel-$ENV_NAME"

if [ -z "${PULUMI_CONFIG_PASSPHRASE:-}" ]; then
  echo "Error: PULUMI_CONFIG_PASSPHRASE must be set (see bootstrap_backend.sh)."
  exit 1
fi

echo "==> 1. Applying codeduel-shared stack (VPC, EKS, RDS, ElastiCache, ECR, ALB controller)..."
cd "$INFRA_DIR/shared"
pulumi stack select shared
pulumi up --yes

AWS_REGION="$(pulumi config get aws:region)"
EKS_CLUSTER_NAME="$(pulumi stack output eks_cluster_name)"
ECR_URLS_JSON="$(pulumi stack output ecr_repository_urls --json)"
CODEDUEL_ECR="$(echo "$ECR_URLS_JSON" | jq -r '.codeduel')"
SANDBOX_PY_ECR="$(echo "$ECR_URLS_JSON" | jq -r '."sandbox-python"')"
SANDBOX_CPP_ECR="$(echo "$ECR_URLS_JSON" | jq -r '."sandbox-cpp"')"
SANDBOX_JAVA_ECR="$(echo "$ECR_URLS_JSON" | jq -r '."sandbox-java"')"
ECR_REGISTRY="${CODEDUEL_ECR%%/*}"

echo "==> 2. Applying codeduel-env '$ENV_NAME' stack (namespace, secrets, DB bootstrap job)..."
cd "$INFRA_DIR/env"
pulumi stack select "$ENV_NAME"
pulumi up --yes

echo "==> 3. Updating local kubeconfig for cluster $EKS_CLUSTER_NAME..."
aws eks update-kubeconfig --name "$EKS_CLUSTER_NAME" --region "$AWS_REGION"

echo "==> 4. Checking whether codeduel:$IMAGE_TAG is already in ECR..."
if aws ecr describe-images \
  --region "$AWS_REGION" \
  --repository-name codeduel \
  --image-ids imageTag="$IMAGE_TAG" >/dev/null 2>&1; then
  echo "    Found it. Skipping all four builds and promoting the existing images."
  echo "    (This is how prod runs the identical bytes dev ran.)"
else
  echo "    Not found. Building and pushing."

  echo "==> 4a. Logging into ECR ($ECR_REGISTRY)..."
  aws ecr get-login-password --region "$AWS_REGION" | docker login --username AWS --password-stdin "$ECR_REGISTRY"

  echo "==> 4b. Building and pushing codeduel image for linux/arm64 ($IMAGE_TAG)..."
  docker buildx build --platform linux/arm64 \
    -t "$CODEDUEL_ECR:$IMAGE_TAG" -t "$CODEDUEL_ECR:latest" \
    --push "$ROOT_DIR"

  echo "==> 4c. Building and pushing sandbox images for linux/amd64 (latest)..."
  docker buildx build --platform linux/amd64 --pull -t "$SANDBOX_PY_ECR:latest" --push "$ROOT_DIR/deploy/sandbox/python"
  docker buildx build --platform linux/amd64 --pull -t "$SANDBOX_CPP_ECR:latest" --push "$ROOT_DIR/deploy/sandbox/cpp"
  docker buildx build --platform linux/amd64 --pull -t "$SANDBOX_JAVA_ECR:latest" --push "$ROOT_DIR/deploy/sandbox/java"
fi

echo "==> 5. Rendering '$ENV_NAME' K8s manifests with real ECR image references..."
RENDER_DIR="$(mktemp -d)"
trap 'rm -rf "$RENDER_DIR"' EXIT
OVERLAY_DIR="$("$ROOT_DIR/infra/scripts/render_overlay.sh" \
  "$ENV_NAME" "$ECR_REGISTRY" "$IMAGE_TAG" "$RENDER_DIR/render")"

echo "==> 6. Applying '$ENV_NAME' overlay to namespace $NAMESPACE..."
kubectl apply -k "$OVERLAY_DIR"

echo "==> 7. Running database migration Job..."
# migrate-job.yaml is applied standalone (not part of the overlay's continuous
# resource list), so it never gets Kustomize's ConfigMap name-hash rewrite.
# Look up the real hashed name so configMapRef: codeduel-config resolves.
CONFIG_MAP_NAME="$(kubectl get configmap -n "$NAMESPACE" -l app.kubernetes.io/part-of=codeduel -o jsonpath='{.items[0].metadata.name}')"
# A previous run leaves a completed Job behind; its pod template is immutable,
# so re-applying with a new image fails. Delete before re-creating.
kubectl delete job migrate -n "$NAMESPACE" --ignore-not-found
sed \
  -e "s|image: codeduel$|image: $CODEDUEL_ECR:$IMAGE_TAG|" \
  -e "s|name: codeduel-config$|name: $CONFIG_MAP_NAME|" \
  "${OVERLAY_DIR%/overlays/*}/base/migrate-job.yaml" \
  | kubectl apply -n "$NAMESPACE" -f -
kubectl wait --for=condition=complete job/migrate -n "$NAMESPACE" --timeout=180s

echo "==> 8. Waiting for deployments to become ready..."
for deploy in gateway match reaper judge; do
  kubectl rollout status "deployment/$deploy" -n "$NAMESPACE" --timeout=180s
done

echo ""
echo "=============================================================================="
echo "$ENV_NAME deploy complete. Image tag: $IMAGE_TAG"
echo "=============================================================================="
