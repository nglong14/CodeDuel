"""Fluent Bit log shipping to CloudWatch Logs for CodeDuel.

Closes the gap the plan left open ("no Fluent Bit"): the per-environment
CloudWatch log groups existed but nothing wrote to them, so `kubectl logs` was
the only log path and logs died with the pod.

Provisions:
- IRSA role scoped to 'system:serviceaccount:kube-system:aws-for-fluent-bit',
  allowed to write only to the '/codeduel-*' log groups the env stacks own.
- The aws-for-fluent-bit DaemonSet (kube-system), tailing only container logs
  from the codeduel-* namespaces and routing each namespace to its own log
  group via the record-accessor template.

Two deliberate scoping decisions:
- One log group per environment ('/codeduel-dev', '/codeduel-prod'), with one
  stream per pod/container, instead of one group per role. Per-role queries are
  a stream-prefix filter; per-role groups would need a three-level record
  accessor into pod labels, which fails translation for any pod missing the
  label and floods Fluent Bit's own log.
- No toleration for the 'sandbox=true' taint, so the DaemonSet never lands on
  the gVisor node group. Submission stdout is untrusted user output that Judge
  already captures and returns; shipping it to CloudWatch would pay to store it
  twice.
"""

import json
from typing import NamedTuple

import pulumi
import pulumi_aws as aws
import pulumi_kubernetes as k8s

SERVICE_ACCOUNT_NAME = "aws-for-fluent-bit"
# Log groups are created by the env stacks (see infra/env/log_groups.py) so
# retention is managed; Fluent Bit is not allowed to create its own.
LOG_GROUP_PREFIX = "/codeduel-"
# Must resolve to exactly the log group names the env stacks create, or logs are
# dropped. infra/tests/test_logging.py asserts the two agree.
LOG_GROUP_TEMPLATE = "/$kubernetes['namespace_name']"
LOG_STREAM_TEMPLATE = "$kubernetes['pod_name'].$kubernetes['container_name']"


class FluentBitResources(NamedTuple):
    role: aws.iam.Role
    policy: aws.iam.Policy
    release: k8s.helm.v3.Release


def create_fluent_bit_irsa(
    oidc_provider_arn: pulumi.Input[str],
    oidc_provider_url: pulumi.Input[str],
) -> tuple[aws.iam.Role, aws.iam.Policy]:
    def build_assume_role_policy(args):
        arn, url = args
        clean_url = url.replace("https://", "")
        return json.dumps(
            {
                "Version": "2012-10-17",
                "Statement": [
                    {
                        "Effect": "Allow",
                        "Principal": {"Federated": arn},
                        "Action": "sts:AssumeRoleWithWebIdentity",
                        "Condition": {
                            "StringEquals": {
                                f"{clean_url}:sub": f"system:serviceaccount:kube-system:{SERVICE_ACCOUNT_NAME}",
                                f"{clean_url}:aud": "sts.amazonaws.com",
                            },
                        },
                    },
                ],
            }
        )

    role = aws.iam.Role(
        "codeduel-fluent-bit-role",
        name="codeduel-fluent-bit-role",
        assume_role_policy=pulumi.Output.all(
            oidc_provider_arn,
            oidc_provider_url,
        ).apply(build_assume_role_policy),
        tags={
            "Name": "codeduel-fluent-bit-role",
            "Project": "codeduel",
        },
    )

    # No logs:CreateLogGroup and no logs:PutRetentionPolicy: Pulumi owns the
    # groups, so an unexpected group name fails loudly instead of silently
    # creating a never-expiring group that accrues storage cost forever.
    policy = aws.iam.Policy(
        "codeduel-fluent-bit-policy",
        name="CodeDuelFluentBitCloudWatchLogs",
        description="Allows Fluent Bit to write pod logs into the CodeDuel environment log groups",
        policy=json.dumps(
            {
                "Version": "2012-10-17",
                "Statement": [
                    {
                        "Effect": "Allow",
                        "Action": [
                            "logs:CreateLogStream",
                            "logs:PutLogEvents",
                            "logs:DescribeLogStreams",
                        ],
                        "Resource": [
                            f"arn:aws:logs:*:*:log-group:{LOG_GROUP_PREFIX}*",
                            f"arn:aws:logs:*:*:log-group:{LOG_GROUP_PREFIX}*:log-stream:*",
                        ],
                    },
                ],
            }
        ),
        tags={
            "Name": "CodeDuelFluentBitCloudWatchLogs",
            "Project": "codeduel",
        },
    )

    aws.iam.RolePolicyAttachment(
        "codeduel-fluent-bit-policy-attachment",
        role=role.name,
        policy_arn=policy.arn,
    )

    return role, policy


def cloudwatch_logs_values(region: str) -> dict:
    """Values for the chart's cloudWatchLogs output.

    The plugin requires log_group_name, and one of log_stream_name or
    log_stream_prefix, EVEN WHEN the corresponding template is set: the templates
    are overrides, and the plain values are the fallback used when record-accessor
    translation fails. Omitting the stream fallback fails plugin initialization
    outright ("Either 'log_stream_name' or 'log_stream_prefix' is required"),
    which crash-loops the DaemonSet rather than degrading. Both fallbacks are
    named 'fallback' so anything landing there is visibly a translation failure.
    """
    return {
        "enabled": True,
        "region": region,
        "logGroupName": f"{LOG_GROUP_PREFIX}fallback",
        "logGroupTemplate": LOG_GROUP_TEMPLATE,
        "logStreamPrefix": "fallback-",
        "logStreamTemplate": LOG_STREAM_TEMPLATE,
        # String, not bool: the chart wraps this key in `{{- if }}`, so a Python
        # False would drop the line entirely and leave the plugin on its own
        # default rather than asserting "false".
        "autoCreateGroup": "false",
    }


def deploy_fluent_bit(
    oidc_provider_arn: pulumi.Input[str],
    oidc_provider_url: pulumi.Input[str],
    k8s_provider: pulumi.Input,
    region: str = "us-east-1",
) -> FluentBitResources:
    role, policy = create_fluent_bit_irsa(oidc_provider_arn, oidc_provider_url)

    release = k8s.helm.v3.Release(
        "aws-for-fluent-bit",
        name="aws-for-fluent-bit",
        chart="aws-for-fluent-bit",
        version="0.2.0",
        repository_opts=k8s.helm.v3.RepositoryOptsArgs(
            repo="https://aws.github.io/eks-charts",
        ),
        namespace="kube-system",
        values={
            "serviceAccount": {
                "create": True,
                "name": SERVICE_ACCOUNT_NAME,
                "annotations": {
                    "eks.amazonaws.com/role-arn": role.arn,
                },
            },
            "input": {
                # Container log filenames are
                # <pod>_<namespace>_<container>-<id>.log, so this glob restricts
                # collection to the codeduel-* namespaces. kube-system (including
                # Fluent Bit's own output, which would otherwise feed back into
                # itself) is never tailed.
                "path": "/var/log/containers/*_codeduel-*_*.log",
            },
            "cloudWatch": {"enabled": False},
            "cloudWatchLogs": cloudwatch_logs_values(region),
        },
        opts=pulumi.ResourceOptions(provider=k8s_provider),
    )

    return FluentBitResources(role=role, policy=policy, release=release)
