"""GitHub Actions OIDC roles for CodeDuel CI/CD.

Implements the identity half of docs/plan/phase_7_cicd_deploy.md: GitHub Actions
assumes an IAM role through OIDC, so no long-lived AWS access keys exist in the
repository.

Two roles, because a pull request and a deploy need very different power:

- codeduel-github-plan   read-only; `pulumi preview` and `kubectl diff` on PRs.
- codeduel-github-deploy PowerUser + iam:*; runs `pulumi up` and `kubectl apply`.

The second one is broad, and that is the honest cost of letting CI run Pulumi
over a program that creates VPCs, EKS node groups, RDS instances, and IRSA trust
policies. The real control is the required reviewer on the `prod` GitHub
Environment, plus the `sub` claim scoping below: a fork, or any branch other than
main, cannot assume either role.

Skipped entirely unless `codeduel-shared:github_repository` is configured, so a
laptop-only workflow keeps working.
"""

import json
from typing import NamedTuple

import pulumi
import pulumi_aws as aws

GITHUB_OIDC_URL = "https://token.actions.githubusercontent.com"
# AWS validates GitHub's certificate chain against its own trust store and
# ignores this value, but the CreateOpenIDConnectProvider API still requires it.
GITHUB_OIDC_THUMBPRINT = "6938fd4d98bab03faadb97b34396831e3780aea1"

PLAN_ROLE_NAME = "codeduel-github-plan"
DEPLOY_ROLE_NAME = "codeduel-github-deploy"

# GitHub sets sub to `repo:<owner>/<repo>:environment:<name>` when a job declares
# an environment, and `repo:<owner>/<repo>:ref:<git-ref>` when it does not, so the
# deploy role needs both forms. Pull requests get `...:pull_request`.
PLAN_SUB_TEMPLATES = ["repo:{repo}:pull_request"]
DEPLOY_SUB_TEMPLATES = [
    "repo:{repo}:ref:refs/heads/main",
    "repo:{repo}:environment:dev",
    "repo:{repo}:environment:prod",
]


class GithubOidcResources(NamedTuple):
    provider_arn: pulumi.Output[str]
    plan_role: aws.iam.Role
    deploy_role: aws.iam.Role
    # Returned so the tests can assert on them directly; the plan role's total
    # set of attachments is the invariant that matters, not just one member of it.
    plan_policy_attachments: list[aws.iam.RolePolicyAttachment]
    deploy_inline_policy: aws.iam.RolePolicy
    access_policies: dict[str, aws.eks.AccessPolicyAssociation]


def _assume_role_policy(
    provider_arn: pulumi.Input[str],
    repository: str,
    sub_templates: list[str],
) -> pulumi.Output[str]:
    subs = [template.format(repo=repository) for template in sub_templates]

    return pulumi.Output.from_input(provider_arn).apply(
        lambda arn: json.dumps(
            {
                "Version": "2012-10-17",
                "Statement": [
                    {
                        "Effect": "Allow",
                        "Principal": {"Federated": arn},
                        "Action": "sts:AssumeRoleWithWebIdentity",
                        "Condition": {
                            # StringEquals on a list is an OR over exact values.
                            # StringLike with a wildcard is what lets an unrelated
                            # repository in; it is deliberately not used here.
                            "StringEquals": {
                                "token.actions.githubusercontent.com:sub": subs,
                                "token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
                            },
                        },
                    },
                ],
            }
        )
    )


def create_github_oidc(
    repository: str,
    cluster_name: pulumi.Input[str],
    existing_provider_arn: str | None = None,
) -> GithubOidcResources:
    """Provision the GitHub OIDC provider (unless reusing one) and both CI roles.

    An AWS account can hold only one OIDC provider per issuer URL, so pass
    `existing_provider_arn` (config `codeduel-shared:github_oidc_provider_arn`)
    if the account already has one from another project.
    """
    if existing_provider_arn:
        provider_arn: pulumi.Output[str] = pulumi.Output.from_input(existing_provider_arn)
    else:
        provider = aws.iam.OpenIdConnectProvider(
            "codeduel-github-oidc-provider",
            url=GITHUB_OIDC_URL,
            client_id_lists=["sts.amazonaws.com"],
            thumbprint_lists=[GITHUB_OIDC_THUMBPRINT],
            tags={
                "Name": "codeduel-github-oidc-provider",
                "Project": "codeduel",
            },
        )
        provider_arn = provider.arn

    # 1. Plan role: PR previews and diffs. ReadOnlyAccess already covers reading
    # the Pulumi state bucket, so no inline policy is needed.
    plan_role = aws.iam.Role(
        "codeduel-github-plan-role",
        name=PLAN_ROLE_NAME,
        assume_role_policy=_assume_role_policy(provider_arn, repository, PLAN_SUB_TEMPLATES),
        tags={
            "Name": PLAN_ROLE_NAME,
            "Project": "codeduel",
        },
    )
    plan_policy_attachments = [
        aws.iam.RolePolicyAttachment(
            "codeduel-github-plan-readonly",
            role=plan_role.name,
            policy_arn="arn:aws:iam::aws:policy/ReadOnlyAccess",
        )
    ]

    # 2. Deploy role: PowerUserAccess covers everything except IAM, which Pulumi
    # needs for the EKS/IRSA roles, so iam:* is added on top.
    deploy_role = aws.iam.Role(
        "codeduel-github-deploy-role",
        name=DEPLOY_ROLE_NAME,
        assume_role_policy=_assume_role_policy(provider_arn, repository, DEPLOY_SUB_TEMPLATES),
        tags={
            "Name": DEPLOY_ROLE_NAME,
            "Project": "codeduel",
        },
    )
    aws.iam.RolePolicyAttachment(
        "codeduel-github-deploy-poweruser",
        role=deploy_role.name,
        policy_arn="arn:aws:iam::aws:policy/PowerUserAccess",
    )
    deploy_inline_policy = aws.iam.RolePolicy(
        "codeduel-github-deploy-iam",
        role=deploy_role.name,
        policy=json.dumps(
            {
                "Version": "2012-10-17",
                "Statement": [
                    {
                        "Sid": "PulumiManagesIam",
                        "Effect": "Allow",
                        "Action": ["iam:*"],
                        "Resource": "*",
                    },
                    {
                        # Without this, iam:* lets a compromised workflow widen
                        # its own trust policy to any repository and keep the
                        # access permanently. Closing that is worth the six lines;
                        # everything else about this role stays broad.
                        "Sid": "DenySelfEscalation",
                        "Effect": "Deny",
                        "Action": [
                            "iam:UpdateAssumeRolePolicy",
                            "iam:AttachRolePolicy",
                            "iam:PutRolePolicy",
                            "iam:DeleteRole",
                        ],
                        "Resource": [
                            f"arn:aws:iam::*:role/{PLAN_ROLE_NAME}",
                            f"arn:aws:iam::*:role/{DEPLOY_ROLE_NAME}",
                        ],
                    },
                ],
            }
        ),
    )

    # 3. Kubernetes access. Without these, `pulumi up` succeeds and `kubectl`
    # fails with an authorization error, because
    # bootstrap_cluster_creator_admin_permissions only admitted whoever created
    # the cluster. Easiest step to forget; its error message does not point here.
    access_policies: dict[str, aws.eks.AccessPolicyAssociation] = {}
    for name, role, policy in [
        # AdminView rather than View for the plan role: `kubectl diff` reads the
        # Judge Role/RoleBinding in the overlay, and the plain View policy
        # excludes RBAC objects, which fails the whole diff.
        ("plan", plan_role, "AmazonEKSAdminViewPolicy"),
        ("deploy", deploy_role, "AmazonEKSClusterAdminPolicy"),
    ]:
        access_entry = aws.eks.AccessEntry(
            f"codeduel-github-{name}-access-entry",
            cluster_name=cluster_name,
            principal_arn=role.arn,
            type="STANDARD",
        )
        access_policies[name] = aws.eks.AccessPolicyAssociation(
            f"codeduel-github-{name}-access-policy",
            cluster_name=cluster_name,
            principal_arn=role.arn,
            policy_arn=f"arn:aws:eks::aws:cluster-access-policy/{policy}",
            access_scope=aws.eks.AccessPolicyAssociationAccessScopeArgs(type="cluster"),
            opts=pulumi.ResourceOptions(depends_on=[access_entry]),
        )

    return GithubOidcResources(
        provider_arn=provider_arn,
        plan_role=plan_role,
        deploy_role=deploy_role,
        plan_policy_attachments=plan_policy_attachments,
        deploy_inline_policy=deploy_inline_policy,
        access_policies=access_policies,
    )
