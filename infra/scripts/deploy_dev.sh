#!/usr/bin/env bash
# ==============================================================================
# CodeDuel Dev Environment Deploy Pipeline
# ==============================================================================
# Run this after infra/scripts/bootstrap_backend.sh has initialized the Pulumi
# stacks. Drives the full deploy sequence for the 'dev' environment:
#   1. Applies the codeduel-shared stack (VPC, EKS, RDS, ElastiCache, ECR, ALB
#      controller). Safe to re-run; Pulumi is idempotent.
#   2. Applies the codeduel-env 'dev' stack (namespace, secrets, DB bootstrap
#      Job, log groups).
#   3. Builds and pushes the codeduel + sandbox images to ECR.
#   4. Applies the 'dev' Kustomize overlay and runs the DB migration Job.
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
#   ./infra/scripts/deploy_dev.sh [image-tag]
#
# image-tag defaults to the current git short SHA.
# ==============================================================================

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
INFRA_DIR="$ROOT_DIR/infra"
IMAGE_TAG="${1:-$(git -C "$ROOT_DIR" rev-parse --short HEAD)}"

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

echo "==> 2. Applying codeduel-env 'dev' stack (namespace, secrets, DB bootstrap job)..."
cd "$INFRA_DIR/env"
pulumi stack select dev
pulumi up --yes

echo "==> 3. Updating local kubeconfig for cluster $EKS_CLUSTER_NAME..."
aws eks update-kubeconfig --name "$EKS_CLUSTER_NAME" --region "$AWS_REGION"

echo "==> 4. Logging into ECR ($ECR_REGISTRY)..."
aws ecr get-login-password --region "$AWS_REGION" | docker login --username AWS --password-stdin "$ECR_REGISTRY"

echo "==> 5. Building and pushing codeduel image for linux/arm64 ($IMAGE_TAG)..."
docker buildx build --platform linux/arm64 \
  -t "$CODEDUEL_ECR:$IMAGE_TAG" -t "$CODEDUEL_ECR:latest" \
  --push "$ROOT_DIR"

echo "==> 6. Building and pushing sandbox images for linux/amd64 (latest)..."
docker buildx build --platform linux/amd64 --pull -t "$SANDBOX_PY_ECR:latest" --push "$ROOT_DIR/deploy/sandbox/python"
docker buildx build --platform linux/amd64 --pull -t "$SANDBOX_CPP_ECR:latest" --push "$ROOT_DIR/deploy/sandbox/cpp"
docker buildx build --platform linux/amd64 --pull -t "$SANDBOX_JAVA_ECR:latest" --push "$ROOT_DIR/deploy/sandbox/java"

echo "==> 7. Rendering 'dev' K8s manifests with real ECR image references..."
RENDER_DIR="$(mktemp -d)"
trap 'rm -rf "$RENDER_DIR"' EXIT
cp -r "$ROOT_DIR/deploy/k8s" "$RENDER_DIR/k8s"
sed -i \
  -e "s|ACCOUNT_ID.dkr.ecr.REGION.amazonaws.com|$ECR_REGISTRY|g" \
  -e "s|newTag: latest|newTag: $IMAGE_TAG|" \
  "$RENDER_DIR/k8s/overlays/dev/kustomization.yaml"
sed -i \
  "s|ACCOUNT_ID.dkr.ecr.REGION.amazonaws.com|$ECR_REGISTRY|g" \
  "$RENDER_DIR/k8s/base/config.env"

echo "==> 8. Applying 'dev' overlay to namespace codeduel-dev..."
kubectl apply -k "$RENDER_DIR/k8s/overlays/dev"

echo "==> 9. Running database migration Job..."
# migrate-job.yaml is applied standalone (not part of the overlay's continuous
# resource list), so it never gets Kustomize's ConfigMap name-hash rewrite.
# Look up the real hashed name so configMapRef: codeduel-config resolves.
CONFIG_MAP_NAME="$(kubectl get configmap -n codeduel-dev -l app.kubernetes.io/part-of=codeduel -o jsonpath='{.items[0].metadata.name}')"
sed \
  -e "s|image: codeduel$|image: $CODEDUEL_ECR:$IMAGE_TAG|" \
  -e "s|name: codeduel-config$|name: $CONFIG_MAP_NAME|" \
  "$RENDER_DIR/k8s/base/migrate-job.yaml" \
  | kubectl apply -n codeduel-dev -f -
kubectl wait --for=condition=complete job/migrate -n codeduel-dev --timeout=180s

echo "==> 10. Waiting for deployments to become ready..."
for deploy in gateway match reaper judge; do
  kubectl rollout status "deployment/$deploy" -n codeduel-dev --timeout=180s
done

echo ""
echo "=============================================================================="
echo "Dev deploy complete. Image tag: $IMAGE_TAG"
echo "=============================================================================="
