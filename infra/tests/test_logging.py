"""Unit tests for CloudWatch log shipping.

The failure mode these guard is silent: if Fluent Bit's log_group_template
resolves to a name the env stack did not create, nothing errors loudly — the
group simply cannot be written to (auto_create_group is off) and logs are lost.
"""

import json
import sys
from pathlib import Path

import pulumi
import pytest

ENV_DIR = str(Path(__file__).parent.parent / "env")
SHARED_DIR = str(Path(__file__).parent.parent / "shared")
for d in [ENV_DIR, SHARED_DIR]:
    if d not in sys.path:
        sys.path.insert(0, d)

from fluent_bit import (
    LOG_GROUP_PREFIX,
    LOG_GROUP_TEMPLATE,
    SERVICE_ACCOUNT_NAME,
    cloudwatch_logs_values,
    create_fluent_bit_irsa,
)
from log_groups import create_environment_log_group


@pytest.fixture(scope="module")
def fluent_bit_irsa():
    role, policy = create_fluent_bit_irsa(
        oidc_provider_arn="arn:aws:iam::123456789012:oidc-provider/oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE",
        oidc_provider_url="https://oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE",
    )
    return role, policy


def test_cloudwatch_output_sets_the_fallbacks_the_plugin_requires():
    """Regression: the templates are overrides, not replacements.

    cloudwatch_logs requires log_group_name plus one of log_stream_name or
    log_stream_prefix regardless of whether the templates are set. Shipping only
    the templates made the plugin fail initialization and crash-loop the
    DaemonSet; Helm rendered the config perfectly, so only Fluent Bit itself
    rejected it.
    """
    values = cloudwatch_logs_values("us-east-1")

    assert values["logGroupTemplate"], "the template is what routes per namespace"
    assert values["logStreamTemplate"]
    assert values["logGroupName"], "log_group_name is required even with a template"
    assert values["logStreamPrefix"] or values.get("logStreamName"), (
        "cloudwatch_logs refuses to initialize without log_stream_prefix or "
        "log_stream_name, even when log_stream_template is set"
    )


def test_record_accessor_templates_respect_the_separator_limitation():
    """Only '.' and ',' may follow a record-accessor variable in a template."""
    for template in (LOG_GROUP_TEMPLATE, cloudwatch_logs_values("us-east-1")["logStreamTemplate"]):
        for part in template.split("$")[1:]:
            after = part[part.index("]") + 1 :] if "]" in part else ""
            assert after == "" or after[0] in ".,", (
                f"{template!r}: a variable is followed by {after[0]!r}; the "
                "record_accessor parser only accepts '.' or ',' there"
            )


@pulumi.runtime.test
def test_log_group_template_matches_created_group_names(fluent_bit_irsa):
    """The DaemonSet's template and the env stack's group name must agree."""
    log_group = create_environment_log_group(
        env_name="dev",
        namespace="codeduel-dev",
        retention_days=7,
    )
    rendered = LOG_GROUP_TEMPLATE.replace("$kubernetes['namespace_name']", "codeduel-dev")

    def check(name):
        assert name == rendered, (
            f"Fluent Bit would write to {rendered!r} but the env stack creates {name!r}; "
            "logs would be dropped because auto_create_group is off"
        )
        assert name.startswith(LOG_GROUP_PREFIX), (
            f"{name!r} falls outside the IAM policy's {LOG_GROUP_PREFIX}* resource scope"
        )

    return log_group.name.apply(check)


@pulumi.runtime.test
def test_fluent_bit_policy_is_write_only_and_scoped(fluent_bit_irsa):
    _, policy = fluent_bit_irsa

    def check(doc_json):
        statements = json.loads(doc_json)["Statement"]
        actions = {a for s in statements for a in s["Action"]}
        assert "logs:CreateLogGroup" not in actions, (
            "Fluent Bit must not create log groups; Pulumi owns them so retention is managed"
        )
        assert actions == {
            "logs:CreateLogStream",
            "logs:PutLogEvents",
            "logs:DescribeLogStreams",
        }
        for statement in statements:
            for resource in statement["Resource"]:
                assert LOG_GROUP_PREFIX in resource, (
                    f"{resource!r} is broader than the CodeDuel environment log groups"
                )

    return policy.policy.apply(check)


@pulumi.runtime.test
def test_fluent_bit_role_trusts_only_its_service_account(fluent_bit_irsa):
    role, _ = fluent_bit_irsa

    def check(doc_json):
        conditions = json.loads(doc_json)["Statement"][0]["Condition"]["StringEquals"]
        subs = [v for k, v in conditions.items() if k.endswith(":sub")]
        assert subs == [f"system:serviceaccount:kube-system:{SERVICE_ACCOUNT_NAME}"]

    return role.assume_role_policy.apply(check)
