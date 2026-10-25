#!/usr/bin/env bash
# Deploy application releases only. Provision infrastructure separately.
# Requires AWS_REGION, ECR_REGISTRY, EKS_CLUSTER_NAME, aws, kubectl, and (for
# missing dev images only) Docker/buildx. Production never builds images.
# Usage: deploy_env.sh <dev|prod> [full-commit-sha]
set -euo pipefail

ENV_NAME="${1:-}"
case "$ENV_NAME" in
  dev | prod) ;;
  *) echo "Usage: $0 <dev|prod> [full-commit-sha]" >&2; exit 1 ;;
esac

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
IMAGE_TAG="${2:-$(git -C "$ROOT_DIR" rev-parse HEAD)}"
if [[ ! "$IMAGE_TAG" =~ ^[a-f0-9]{40}$ ]]; then
  echo "Release must be a full, lowercase Git commit SHA." >&2
  exit 1
fi
: "${AWS_REGION:?Set AWS_REGION}"
: "${ECR_REGISTRY:?Set ECR_REGISTRY to the ECR registry hostname}"
: "${EKS_CLUSTER_NAME:?Set EKS_CLUSTER_NAME}"
export AWS_REGION AWS_DEFAULT_REGION="$AWS_REGION"

umask 077
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT
export KUBECONFIG="$WORK_DIR/kubeconfig"
NAMESPACE="codeduel-$ENV_NAME"
REPOSITORIES=(sandbox-python sandbox-cpp sandbox-java codeduel)
MISSING=()

# Only ImageNotFoundException means a missing image. Authentication/network/API
# errors must not be mistaken for permission to rebuild an immutable release.
image_exists() {
  local repository="$1"
  if aws ecr describe-images --repository-name "$repository" \
    --image-ids "imageTag=$IMAGE_TAG" --query 'imageDetails[0].imageDigest' \
    --output text > "$WORK_DIR/digest" 2> "$WORK_DIR/ecr-error"; then
    if ! grep -Eq '^sha256:[a-f0-9]{64}$' "$WORK_DIR/digest"; then
      echo "Invalid image digest returned for $repository." >&2
      exit 1
    fi
  elif grep -q 'ImageNotFoundException' "$WORK_DIR/ecr-error"; then
    return 1
  else
    cat "$WORK_DIR/ecr-error" >&2
    exit 1
  fi
}

for repository in "${REPOSITORIES[@]}"; do
  repository_info="$(aws ecr describe-repositories --repository-names "$repository" \
    --query 'repositories[0].[imageTagMutability,repositoryUri]' --output text)"
  read -r mutability repository_uri <<< "$repository_info"
  if [ "$repository_uri" != "$ECR_REGISTRY/$repository" ]; then
    echo "ECR_REGISTRY does not match $repository in the active AWS account/region." >&2
    exit 1
  fi
  if [ "$mutability" != "IMMUTABLE" ]; then
    echo "$repository must enforce immutable tags; update the shared stack first." >&2
    exit 1
  fi
  if ! image_exists "$repository"; then
    MISSING+=("$repository")
  fi
done

if [ "${#MISSING[@]}" -gt 0 ]; then
  if [ "$ENV_NAME" = prod ]; then
    echo "Incomplete release $IMAGE_TAG: missing ${MISSING[*]}; production cannot build." >&2
    exit 1
  fi
  if [ "$IMAGE_TAG" != "$(git -C "$ROOT_DIR" rev-parse HEAD)" ] || \
    [ -n "$(git -C "$ROOT_DIR" status --porcelain)" ]; then
    echo "Build missing images only from a clean checkout of release $IMAGE_TAG." >&2
    exit 1
  fi
  aws ecr get-login-password | docker login --username AWS --password-stdin "$ECR_REGISTRY"
  for repository in "${MISSING[@]}"; do
    if [ "$repository" = codeduel ]; then
      platform=linux/arm64
      context="$ROOT_DIR"
    else
      platform=linux/amd64
      context="$ROOT_DIR/deploy/sandbox/${repository#sandbox-}"
    fi
    docker buildx build --platform "$platform" --pull \
      -t "$ECR_REGISTRY/$repository:$IMAGE_TAG" --push "$context"
  done
fi

# Verify the entire release, including images built during a partial retry.
for repository in "${REPOSITORIES[@]}"; do
  if ! image_exists "$repository"; then
    echo "Release $IMAGE_TAG is missing $repository after publication." >&2
    exit 1
  fi
done

OVERLAY_DIR="$("$ROOT_DIR/infra/scripts/render_overlay.sh" \
  "$ENV_NAME" "$ECR_REGISTRY" "$IMAGE_TAG" "$WORK_DIR/render")"
kubectl kustomize "$OVERLAY_DIR" > "$WORK_DIR/release.yaml"
aws eks update-kubeconfig --name "$EKS_CLUSTER_NAME" --region "$AWS_REGION"

# Fail before migration/rollout if platform provisioning has not been completed.
kubectl get namespace "$NAMESPACE" > /dev/null
kubectl get secret codeduel-secrets -n "$NAMESPACE" > /dev/null
kubectl get runtimeclass gvisor > /dev/null

# Kustomize renders the Job with the exact same hashed ConfigMap and release
# image as the workloads. A release-specific Job is never deleted by this path.
kubectl apply -n "$NAMESPACE" -f "$WORK_DIR/release.yaml" \
  -l app.kubernetes.io/name=configuration
kubectl apply -n "$NAMESPACE" -f "$WORK_DIR/release.yaml" \
  -l app.kubernetes.io/name=migrate
kubectl wait -n "$NAMESPACE" --for=condition=complete \
  "job/migrate-$IMAGE_TAG" --timeout=240s

# Old application pods must remain compatible with the forward migration.
kubectl apply -n "$NAMESPACE" -f "$WORK_DIR/release.yaml" \
  -l 'app.kubernetes.io/name!=migrate'
for deployment in gateway match reaper judge; do
  kubectl rollout status "deployment/$deployment" -n "$NAMESPACE" --timeout=180s
done
echo "$ENV_NAME application release deployed: $IMAGE_TAG"
