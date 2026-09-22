"""CloudWatch Log Group for a CodeDuel environment.

One group per environment, named after the Kubernetes namespace, because that
is exactly what the Fluent Bit DaemonSet's `log_group_template`
(`/$kubernetes['namespace_name']`, see infra/shared/fluent_bit.py) resolves to.
Fluent Bit is not permitted to create groups, so this resource is what makes
retention (and therefore cost) managed rather than unbounded.

Streams are `<pod>.<container>`, so per-role queries are a stream-prefix filter
(`gateway-`, `match-`, `judge-`, `reaper-`) instead of a separate group.
"""

import pulumi_aws as aws


def create_environment_log_group(
    env_name: str,
    namespace: str,
    retention_days: int = 7,
) -> aws.cloudwatch.LogGroup:
    group_name = f"/{namespace}"
    return aws.cloudwatch.LogGroup(
        f"codeduel-{env_name}-logs",
        name=group_name,
        retention_in_days=retention_days,
        tags={
            "Name": group_name,
            "Project": "codeduel",
            "Environment": env_name,
        },
    )
