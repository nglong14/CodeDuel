"""Unit tests for the GitHub Actions OIDC roles.

These assert over a trust policy document, which is the class of thing that is
easy to get subtly wrong and expensive to discover wrong: a `StringLike` instead
of a `StringEquals`, a missing `aud` condition, or a `sub` pattern loose enough
to admit a fork would hand AWS access to anyone who can open a pull request.
"""

import json
import sys
from pathlib import Path

import pulumi
import pytest

SHARED_DIR = str(Path(__file__).parent.parent / "shared")
TESTS_DIR = str(Path(__file__).parent)
for d in [SHARED_DIR, TESTS_DIR]:
    if d not in sys.path:
        sys.path.insert(0, d)

from github_oidc import (
    DEPLOY_ROLE_NAME,
    PLAN_ROLE_NAME,
    create_github_oidc,
    subject_prefix,
)

REPOSITORY = "nglong14/CodeDuel"
OWNER_ID = "109326300"
REPOSITORY_ID = "1327800635"
# What the repository's tokens actually carry, per
# `gh api /repos/nglong14/CodeDuel/actions/oidc/customization/sub`.
PREFIX = f"repo:nglong14@{OWNER_ID}/CodeDuel@{REPOSITORY_ID}"


@pytest.fixture(scope="module")
def github_oidc():
    return create_github_oidc(
        repository=REPOSITORY,
        cluster_name="codeduel",
        owner_id=OWNER_ID,
        repository_id=REPOSITORY_ID,
    )


def test_subject_prefix_uses_the_immutable_form_when_ids_are_configured():
    """The legacy and immutable forms are not interchangeable under StringEquals:

    a policy written for one rejects every token from the other, and STS reports
    it only as "Not authorized to perform sts:AssumeRoleWithWebIdentity".
    """
    assert subject_prefix(REPOSITORY, OWNER_ID, REPOSITORY_ID) == PREFIX
    assert subject_prefix(REPOSITORY) == f"repo:{REPOSITORY}"
    # Half-configured would otherwise emit a legacy prefix that looks deliberate.
    with pytest.raises(ValueError):
        subject_prefix(REPOSITORY, owner_id=OWNER_ID)


def _conditions(doc_json: str) -> dict:
    statement = json.loads(doc_json)["Statement"][0]
    assert statement["Action"] == "sts:AssumeRoleWithWebIdentity"
    assert set(statement["Condition"]) == {"StringEquals"}, (
        "Only StringEquals: a StringLike condition with a wildcard is how an "
        "unrelated repository or branch gets in"
    )
    return statement["Condition"]["StringEquals"]


@pulumi.runtime.test
def test_plan_role_trusts_only_pull_requests_on_this_repository(github_oidc):
    def check(doc_json):
        conditions = _conditions(doc_json)
        assert conditions["token.actions.githubusercontent.com:sub"] == [
            f"{PREFIX}:pull_request"
        ]
        assert conditions["token.actions.githubusercontent.com:aud"] == "sts.amazonaws.com"

    return github_oidc.plan_role.assume_role_policy.apply(check)


@pulumi.runtime.test
def test_deploy_role_trusts_only_main_and_the_two_environments(github_oidc):
    def check(doc_json):
        conditions = _conditions(doc_json)
        assert conditions["token.actions.githubusercontent.com:sub"] == [
            f"{PREFIX}:ref:refs/heads/main",
            f"{PREFIX}:environment:dev",
            f"{PREFIX}:environment:prod",
        ], "A branch other than main, or a fork, must not be able to deploy"
        assert conditions["token.actions.githubusercontent.com:aud"] == "sts.amazonaws.com"

    return github_oidc.deploy_role.assume_role_policy.apply(check)


@pulumi.runtime.test
def test_plan_role_has_read_only_access_and_nothing_else(github_oidc):
    """PR jobs run with this role, so anything beyond read is a real exposure."""
    attachments = github_oidc.plan_policy_attachments
    assert len(attachments) == 1, (
        f"Expected exactly one managed policy on {PLAN_ROLE_NAME}, found {len(attachments)}"
    )

    def check(policy_arn):
        assert policy_arn == "arn:aws:iam::aws:policy/ReadOnlyAccess"

    return attachments[0].policy_arn.apply(check)


@pulumi.runtime.test
def test_deploy_role_cannot_rewrite_its_own_trust_policy(github_oidc):
    """iam:* without this deny lets a compromised workflow make its access permanent."""

    def check(doc_json):
        statements = json.loads(doc_json)["Statement"]
        denies = [s for s in statements if s["Effect"] == "Deny"]
        assert len(denies) == 1
        assert "iam:UpdateAssumeRolePolicy" in denies[0]["Action"]
        assert set(denies[0]["Resource"]) == {
            f"arn:aws:iam::*:role/{PLAN_ROLE_NAME}",
            f"arn:aws:iam::*:role/{DEPLOY_ROLE_NAME}",
        }

    return github_oidc.deploy_inline_policy.policy.apply(check)


@pulumi.runtime.test
def test_both_roles_get_eks_access_entries(github_oidc):
    """`pulumi up` succeeding while `kubectl` is unauthorized is the failure this

    prevents: bootstrap_cluster_creator_admin_permissions only admitted the
    principal that created the cluster, so a CI role has no Kubernetes access
    until an access entry exists.
    """
    assert set(github_oidc.access_policies) == {"plan", "deploy"}

    def check(args):
        plan_policy, deploy_policy = args
        # AdminView is read-only but, unlike View, includes the RBAC objects
        # `kubectl diff` has to read from the overlay.
        assert plan_policy.endswith("/AmazonEKSAdminViewPolicy")
        assert deploy_policy.endswith("/AmazonEKSClusterAdminPolicy")

    return pulumi.Output.all(
        github_oidc.access_policies["plan"].policy_arn,
        github_oidc.access_policies["deploy"].policy_arn,
    ).apply(check)
