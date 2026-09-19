"""RDS PostgreSQL for CodeDuel Shared Infrastructure.

Key architectural constraints:
- Instance class: db.t4g.micro (cost optimized).
- Allocated storage: 20 GB gp3.
- Single-AZ (cost choice).
- Parameter group forces SSL: rds.force_ssl = 1.
- Network: Resides in public subnets with NO public accessibility, security group permits port 5432 ONLY from App Node SG.
"""

from typing import NamedTuple
import pulumi
import pulumi_aws as aws
import pulumi_random as random


class RdsResources(NamedTuple):
    instance: aws.rds.Instance
    subnet_group: aws.rds.SubnetGroup
    parameter_group: aws.rds.ParameterGroup
    master_password: random.RandomPassword


def create_rds_instance(
    subnet_ids: list[pulumi.Input[str]],
    rds_sg_id: pulumi.Input[str],
) -> RdsResources:
    # 1. DB Subnet Group
    subnet_group = aws.rds.SubnetGroup(
        "codeduel-db-subnet-group",
        subnet_ids=subnet_ids,
        description="Subnet group for CodeDuel PostgreSQL instance",
        tags={
            "Name": "codeduel-db-subnet-group",
            "Project": "codeduel",
        },
    )

    # 2. Parameter Group (Force SSL)
    param_group = aws.rds.ParameterGroup(
        "codeduel-pg16-params",
        family="postgres16",
        description="Parameter group forcing SSL for CodeDuel PostgreSQL",
        parameters=[
            aws.rds.ParameterGroupParameterArgs(
                name="rds.force_ssl",
                value="1",
            ),
        ],
        tags={
            "Name": "codeduel-pg16-params",
            "Project": "codeduel",
        },
    )

    # 3. Master Password
    master_password = random.RandomPassword(
        "codeduel-rds-master-pw",
        length=24,
        special=False,
    )

    # 4. PostgreSQL RDS Instance
    instance = aws.rds.Instance(
        "codeduel-postgres",
        identifier="codeduel-postgres",
        engine="postgres",
        engine_version="16.3",
        instance_class="db.t4g.micro",
        allocated_storage=20,
        storage_type="gp3",
        multi_az=False,
        publicly_accessible=False,
        db_subnet_group_name=subnet_group.name,
        vpc_security_group_ids=[rds_sg_id],
        parameter_group_name=param_group.name,
        username="codeduel_admin",
        password=master_password.result,
        skip_final_snapshot=True,
        deletion_protection=False,
        tags={
            "Name": "codeduel-postgres",
            "Project": "codeduel",
        },
    )

    return RdsResources(
        instance=instance,
        subnet_group=subnet_group,
        parameter_group=param_group,
        master_password=master_password,
    )
