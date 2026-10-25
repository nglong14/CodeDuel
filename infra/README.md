# Infrastructure operations

Pulumi provisions the platform; Kustomize deploys application releases. Ordinary
application deployment never runs Pulumi or changes shared AWS resources.

## Ownership

| Owner | Resources |
| --- | --- |
| `codeduel-shared/shared` | AWS infrastructure, EKS VPC CNI/network-policy configuration, cluster controllers, and the shared `gvisor` RuntimeClass |
| `codeduel-env/dev` and `codeduel-env/prod` | Protected namespaces, application/bootstrap Secrets, database bootstrap Jobs, and log groups |
| Kustomize overlays | Application configuration, workloads, Services, ingress, application RBAC, sandbox NetworkPolicy, and release migration Jobs |

Application settings live in Kustomize, not Pulumi. Production Judge concurrency
stays at its existing effective value of **2**; the removed Pulumi setting of 4
was never consumed by application deployment.

## Existing installation: one-time adoption

These steps require an operator with platform permissions. **Do not run a destroy,
delete namespaces, rotate credentials, or recreate the database to adopt resources.**

1. Pause automatic application deployments during the transition. Back up Pulumi
   state securely and take a database snapshot/export. Record namespace UIDs and
   the current database identifier. Keep secrets/state exports out of Git.
2. Confirm both namespaces are already in the corresponding Pulumi environment
   state (`pulumi -C infra/env stack --stack dev --show-urns`, likewise prod).
   If a namespace exists only in Kubernetes, import it into the environment stack
   using the matching logical name (`codeduel-dev-ns` / `codeduel-prod-ns`) and
   that stack's Kubernetes provider before applying. Preserve the existing UID.
   Removing namespace YAML from Kustomize does not delete the namespace; do not prune it.
3. If `gvisor` already exists, set
   `pulumi -C infra/shared config set adopt_gvisor_runtimeclass true --stack shared`.
   The next shared-stack update imports it in place. After a successful update,
   remove that config key; do not delete/recreate the RuntimeClass.
4. Check `aws eks describe-addon --cluster-name codeduel --addon-name vpc-cni`.
   If the addon is already managed by EKS, import it before the shared update:
   `pulumi -C infra/shared import --stack shared aws:eks/addon:Addon codeduel-vpc-cni codeduel:vpc-cni`.
   An unmanaged `aws-node` installation is adopted by addon creation with
   `OVERWRITE`; review its existing custom settings first. The shared stack enables
   NetworkPolicy support; it does not prove that policies work on real pods.
5. Review `make infra-preview-shared`. Expected changes include immutable ECR tags,
   CNI policy enforcement, RuntimeClass adoption, and database safeguards. **Stop
   if the preview would replace/delete RDS, namespaces, credentials, or node groups
   unexpectedly.** Apply the shared update with an operator identity first: the
   existing CI role intentionally cannot rewrite its own trust policy to admit
   the new `infrastructure` environment.
6. Preview and apply each environment stack separately. Bootstrap passwords now
   come from a namespace-scoped Secret instead of literal Job environment fields.
   The completed bootstrap Job may be replaced in place; application namespaces
   and database data must remain unchanged. Verify bootstrap success and unchanged
   namespace UIDs/database identity before resuming application deployment.
7. Configure GitHub variables and approvals below. Build a new full-SHA release
   in dev and verify it before approving production. Legacy short-SHA/`latest`
   releases do not satisfy the new release contract.

For a fresh installation, initialize the backend with
`infra/scripts/bootstrap_backend.sh`, configure the shared/environment stacks,
then provision shared, dev, and prod explicitly. No import is needed for resources
that do not exist. Review AWS costs and cluster/addon version compatibility first.

## Platform changes

Use `make infra-preview-shared`, `make infra-preview-dev`, or
`make infra-preview-prod` before the corresponding `make infra-up-*` target.
The manual **Infrastructure** workflow can apply one selected stack after approval.
Its global concurrency group serializes shared/dev/prod platform updates. Manual
operators must also avoid concurrent platform updates and application releases;
Pulumi state locking is not a substitute for rollout coordination.

Configure these GitHub repository/environment settings:

- Variables: `AWS_REGION`, `ECR_REGISTRY` (hostname only), `EKS_CLUSTER_NAME`,
  `AWS_PLAN_ROLE_ARN`, `AWS_DEPLOY_ROLE_ARN`, and `PULUMI_BACKEND_URL`.
- Secret: `PULUMI_CONFIG_PASSPHRASE` for preview/platform jobs only.
- Required CI checks through branch protection.
- Main-only deployment rules for `dev`, `prod`, and `infrastructure` environments;
  required reviewers for `prod` and `infrastructure`.

The deploy IAM role remains broad and is shared by the approved workflows.
Separating workflows prevents accidental platform updates during app deployment;
it is **not** an IAM least-privilege boundary. Environment OIDC subjects do not
encode a Git branch, so GitHub environment branch restrictions matter.

## Application releases

Export `AWS_REGION`, `ECR_REGISTRY`, and `EKS_CLUSTER_NAME`. For example, obtain
the cluster name and registry from shared-stack outputs during platform setup,
then save them as deployment variables. Application deployment needs no Pulumi
passphrase or state access.

```sh
make deploy-dev RELEASE="$(git rev-parse HEAD)"
# After development verification:
make deploy-prod RELEASE=<the-same-full-40-character-commit-sha>
```

The release script:

1. Checks the active AWS account/registry and immutable-tag policy for each ECR repository.
2. Checks all four images individually. Dev builds only missing images from a
   clean checkout matching the release SHA. Prod never builds, including on rollback.
3. Renders all four image references with the same release SHA and renders the
   migration Job with the workloads' exact hashed ConfigMap reference.
4. Applies the current ConfigMap, applies `migrate-<release-sha>`, and waits for success.
5. Only then applies application resources and waits for role rollouts.

Application images remain arm64; sandbox images remain amd64. No image is pushed
as `latest`. The script never deletes a migration Job; failed Jobs must be
investigated and explicitly retried by an operator. Completed Jobs expire by TTL.
Migration failure prevents the new workload rollout, but a partially applied SQL
migration may still require operator recovery. Do not deploy overlays directly
with `kubectl apply -k`: that bypasses migration sequencing and release rendering.

Keep migrations backward-compatible with the previous running application release
(expand/contract changes). Rollback means redeploying an older **complete** release,
not automatically running down migrations. Immutable tags prevent overwrites, not
deletion: retain all four images for every supported rollback release, and review
ECR lifecycle retention before relying on old tags.

## Database protection and recovery

The shared RDS instance has Pulumi protection, AWS deletion protection, seven-day
automated backups, and a required final snapshot. This protects both environments
sharing that instance; it does not provide high availability or an exercised restore.

Before destructive maintenance, take and verify an additional snapshot/export.
Deleting the instance intentionally requires separately disabling the two protection
layers and choosing an unused final snapshot identifier (the default is
`codeduel-postgres-final`). Do not relax these defaults in a routine deploy.

Recovery drill, first in a disposable environment:

1. Record the source snapshot or point-in-time recovery timestamp and current release.
2. Restore to a **new** RDS instance; never overwrite the source as the first step.
   Reapply the intended network/TLS restrictions to the restored instance.
3. Use an isolated application environment with credentials pointing at the restored
   database; confirm schema, representative row counts, and account/match access.
4. Run readiness/auth checks and an actual two-player duel with a working Judge.
5. Record recovery duration and missing data, then plan an explicit endpoint/secret
   cutover and application restart. Preserve the source until recovery is accepted.

Database endpoint cutover is not automated by this cleanup. Do not change the
managed instance identifier or allow Pulumi to replace it as a shortcut to restore.

## Verification and remaining limits

```sh
make infra-lint
make infra-test
make compose-check
make k8s-check
```

For the actual database bootstrap SQL, Docker and OpenSSL can exercise TLS,
idempotency, credentials, and failure handling against a disposable PostgreSQL
container (no published ports or existing database data):

```sh
CODEDUEL_INFRA_INTEGRATION=1 infra/.venv/bin/pytest -v infra/tests/test_environment.py -k bootstrap_postgres
```

Tests exercise real release scripts with recording AWS/Docker/kubectl stubs, and
real Kustomize rendering when kubectl is installed (CI installs it). They verify
incomplete-release rejection, partial publication recovery, production no-build,
migration failure gating, exact configuration references, and ownership boundaries.
Pulumi tests inspect resource relationships, CNI configuration, and database safeguards.

On a disposable cluster, separately verify CNI NetworkPolicy enforcement with real
sandbox-labelled pods, including denied access to services, database/cache, and
metadata endpoints. A supplemental security group with no ingress is not a deny
override: sandbox nodes also attach the EKS cluster SG. Do not infer isolation from
a mock test or a rendered NetworkPolicy alone.

The existing HTTP smoke script checks readiness and optionally registration/auth.
It is not a gameplay, network-isolation, backup-restore, or migration-compatibility
test. PR previews publish locally rendered manifests instead of attempting
server-side `kubectl diff` writes with a read-only role.

The Kubernetes Judge still requires the separately missing sandbox harness. This
infrastructure cleanup does not make Kubernetes grading ready for public use.
