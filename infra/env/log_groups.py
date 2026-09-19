"""CloudWatch Log Groups for CodeDuel Environment Workloads.

Manages application log streams for:
- Gateway
- Match
- Judge
- Reaper
"""

from typing import NamedTuple
import pulumi_aws as aws


ROLES = ["gateway", "match", "judge", "reaper"]


class LogGroupResources(NamedTuple):
    log_groups: dict[str, aws.cloudwatch.LogGroup]


def create_environment_log_groups(
    env_name: str,
    retention_days: int = 7,
) -> LogGroupResources:
    log_groups: dict[str, aws.cloudwatch.LogGroup] = {}

    for role in ROLES:
        group_name = f"/codeduel/{env_name}/{role}"
        log_group = aws.cloudwatch.LogGroup(
            f"codeduel-{env_name}-log-{role}",
            name=group_name,
            retention_in_days=retention_days,
            tags={
                "Name": group_name,
                "Project": "codeduel",
                "Environment": env_name,
                "Role": role,
            },
        )
        log_groups[role] = log_group

    return LogGroupResources(log_groups=log_groups)
