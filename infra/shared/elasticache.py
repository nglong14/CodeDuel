"""ElastiCache Redis / Valkey for CodeDuel Shared Infrastructure.

Key architectural constraints:
- Node type: cache.t4g.micro.
- Cluster mode: DISABLED (single node; ElastiCache Serverless is rejected because cluster mode rejects SELECT and multi-key Lua scripts).
- Parameter group: databases explicitly set to "16" to support logical DB 0 (prod) and DB 1 (dev).
- Network: Subnet group within VPC, accessible on port 6379 ONLY by App Node SG.
"""

from typing import NamedTuple
import pulumi
import pulumi_aws as aws


class ElastiCacheResources(NamedTuple):
    cluster: aws.elasticache.Cluster
    subnet_group: aws.elasticache.SubnetGroup
    parameter_group: aws.elasticache.ParameterGroup


def create_elasticache_cluster(
    subnet_ids: list[pulumi.Input[str]],
    elasticache_sg_id: pulumi.Input[str],
) -> ElastiCacheResources:
    # 1. Subnet Group
    subnet_group = aws.elasticache.SubnetGroup(
        "codeduel-cache-subnet-group",
        subnet_ids=subnet_ids,
        description="Subnet group for CodeDuel Redis cache",
        tags={
            "Name": "codeduel-cache-subnet-group",
            "Project": "codeduel",
        },
    )

    # 2. Parameter Group with explicit databases = 16
    parameter_group = aws.elasticache.ParameterGroup(
        "codeduel-redis7-params",
        family="redis7",
        description="Parameter group for CodeDuel Redis with databases=16 for env separation",
        parameters=[
            aws.elasticache.ParameterGroupParameterArgs(
                name="databases",
                value="16",
            ),
        ],
        tags={
            "Name": "codeduel-redis7-params",
            "Project": "codeduel",
        },
    )

    # 3. ElastiCache Cluster (Cluster-Mode Disabled)
    cluster = aws.elasticache.Cluster(
        "codeduel-cache",
        cluster_id="codeduel-cache",
        engine="redis",
        engine_version="7.1",
        node_type="cache.t4g.micro",
        num_cache_nodes=1,
        parameter_group_name=parameter_group.name,
        subnet_group_name=subnet_group.name,
        security_group_ids=[elasticache_sg_id],
        port=6379,
        tags={
            "Name": "codeduel-cache",
            "Project": "codeduel",
        },
    )

    return ElastiCacheResources(
        cluster=cluster,
        subnet_group=subnet_group,
        parameter_group=parameter_group,
    )
