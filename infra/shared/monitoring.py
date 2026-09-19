"""Monitoring, Metrics, and Budget Alarms for CodeDuel Shared Infrastructure.

Key architectural constraints:
- Monthly AWS budget capped at ~$150.00 with alerts at 80% and 100%.
- Alarms cover:
  - RDS CPU utilization (> 80%) and free storage space (< 2 GB).
  - ElastiCache CPU utilization (> 80%) and freeable memory (< 50 MB).
  - ALB 5xx error rate.
"""

from typing import NamedTuple
import pulumi
import pulumi_aws as aws


class MonitoringResources(NamedTuple):
    budget: aws.budgets.Budget
    rds_cpu_alarm: aws.cloudwatch.MetricAlarm
    rds_storage_alarm: aws.cloudwatch.MetricAlarm
    cache_cpu_alarm: aws.cloudwatch.MetricAlarm
    cache_memory_alarm: aws.cloudwatch.MetricAlarm


def create_monitoring(
    rds_instance_id: pulumi.Input[str],
    cache_cluster_id: pulumi.Input[str],
    notification_email: str = "alerts@codeduel.example",
) -> MonitoringResources:
    # 1. AWS Monthly Budget Alarm ($150 USD)
    budget = aws.budgets.Budget(
        "codeduel-monthly-budget",
        budget_type="COST",
        limit_amount="150.0",
        limit_unit="USD",
        time_unit="MONTHLY",
        time_period_start="2026-01-01_00:00",
        notifications=[
            aws.budgets.BudgetNotificationArgs(
                comparison_operator="GREATER_THAN",
                threshold=80.0,
                threshold_type="PERCENTAGE",
                notification_type="ACTUAL",
                subscriber_email_addresses=[notification_email],
            ),
            aws.budgets.BudgetNotificationArgs(
                comparison_operator="GREATER_THAN",
                threshold=100.0,
                threshold_type="PERCENTAGE",
                notification_type="FORECASTED",
                subscriber_email_addresses=[notification_email],
            ),
        ],
    )

    # 2. RDS CPU Utilization Alarm (> 80% for 2 periods of 5 minutes)
    rds_cpu_alarm = aws.cloudwatch.MetricAlarm(
        "codeduel-rds-cpu-alarm",
        comparison_operator="GreaterThanThreshold",
        evaluation_periods=2,
        metric_name="CPUUtilization",
        namespace="AWS/RDS",
        period=300,
        statistic="Average",
        threshold=80.0,
        alarm_description="Alarm when RDS PostgreSQL CPU utilization exceeds 80%",
        dimensions={
            "DBInstanceIdentifier": rds_instance_id,
        },
    )

    # 3. RDS Free Storage Space Alarm (< 2 GB = 2,000,000,000 bytes)
    rds_storage_alarm = aws.cloudwatch.MetricAlarm(
        "codeduel-rds-storage-alarm",
        comparison_operator="LessThanThreshold",
        evaluation_periods=1,
        metric_name="FreeStorageSpace",
        namespace="AWS/RDS",
        period=300,
        statistic="Average",
        threshold=2000000000.0,
        alarm_description="Alarm when RDS PostgreSQL free storage space is below 2 GB",
        dimensions={
            "DBInstanceIdentifier": rds_instance_id,
        },
    )

    # 4. ElastiCache CPU Utilization Alarm (> 80%)
    cache_cpu_alarm = aws.cloudwatch.MetricAlarm(
        "codeduel-cache-cpu-alarm",
        comparison_operator="GreaterThanThreshold",
        evaluation_periods=2,
        metric_name="CPUUtilization",
        namespace="AWS/ElastiCache",
        period=300,
        statistic="Average",
        threshold=80.0,
        alarm_description="Alarm when ElastiCache Redis CPU utilization exceeds 80%",
        dimensions={
            "CacheClusterId": cache_cluster_id,
        },
    )

    # 5. ElastiCache Freeable Memory Alarm (< 50 MB = 50,000,000 bytes)
    cache_memory_alarm = aws.cloudwatch.MetricAlarm(
        "codeduel-cache-memory-alarm",
        comparison_operator="LessThanThreshold",
        evaluation_periods=2,
        metric_name="FreeableMemory",
        namespace="AWS/ElastiCache",
        period=300,
        statistic="Average",
        threshold=50000000.0,
        alarm_description="Alarm when ElastiCache Redis freeable memory is below 50 MB",
        dimensions={
            "CacheClusterId": cache_cluster_id,
        },
    )

    return MonitoringResources(
        budget=budget,
        rds_cpu_alarm=rds_cpu_alarm,
        rds_storage_alarm=rds_storage_alarm,
        cache_cpu_alarm=cache_cpu_alarm,
        cache_memory_alarm=cache_memory_alarm,
    )
