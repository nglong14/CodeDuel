"""Security Groups for CodeDuel Shared Infrastructure.

Key architectural constraints:
- ALB SG: Accepts 80 and 443 from 0.0.0.0/0.
- App Node SG: Accepts 8080 only from ALB SG.
- gVisor Node SG: STRICTLY ZERO INBOUND RULES from any source (outbound only to ECR / K8s API).
- RDS SG: Inbound 5432 only from App Node SG.
- ElastiCache SG: Inbound 6379 only from App Node SG.
"""

from typing import NamedTuple

import pulumi
import pulumi_aws as aws


class SecurityGroupResources(NamedTuple):
    alb_sg: aws.ec2.SecurityGroup
    app_node_sg: aws.ec2.SecurityGroup
    gvisor_node_sg: aws.ec2.SecurityGroup
    rds_sg: aws.ec2.SecurityGroup
    elasticache_sg: aws.ec2.SecurityGroup


def create_security_groups(vpc_id: pulumi.Input[str]) -> SecurityGroupResources:
    # 1. ALB Security Group
    alb_sg = aws.ec2.SecurityGroup(
        "codeduel-alb-sg",
        vpc_id=vpc_id,
        description="Security group for shared Application Load Balancer",
        ingress=[
            aws.ec2.SecurityGroupIngressArgs(
                description="HTTP from anywhere (redirected to HTTPS)",
                protocol="tcp",
                from_port=80,
                to_port=80,
                cidr_blocks=["0.0.0.0/0"],
            ),
            aws.ec2.SecurityGroupIngressArgs(
                description="HTTPS from anywhere",
                protocol="tcp",
                from_port=443,
                to_port=443,
                cidr_blocks=["0.0.0.0/0"],
            ),
        ],
        egress=[
            aws.ec2.SecurityGroupEgressArgs(
                description="Outbound to internet / targets",
                protocol="-1",
                from_port=0,
                to_port=0,
                cidr_blocks=["0.0.0.0/0"],
            ),
        ],
        tags={
            "Name": "codeduel-alb-sg",
            "Project": "codeduel",
        },
    )

    # 2. App Node Security Group (Gateway, Match, Reaper, Judge)
    app_node_sg = aws.ec2.SecurityGroup(
        "codeduel-app-node-sg",
        vpc_id=vpc_id,
        description="Security group for application worker nodes",
        ingress=[
            aws.ec2.SecurityGroupIngressArgs(
                description="Gateway port from ALB only",
                protocol="tcp",
                from_port=8080,
                to_port=8080,
                security_groups=[alb_sg.id],
            ),
        ],
        egress=[
            aws.ec2.SecurityGroupEgressArgs(
                description="Outbound to internet, ECR, CloudWatch, EKS API",
                protocol="-1",
                from_port=0,
                to_port=0,
                cidr_blocks=["0.0.0.0/0"],
            ),
        ],
        tags={
            "Name": "codeduel-app-node-sg",
            "Project": "codeduel",
        },
    )

    # Allow inter-node communication within the app node group
    aws.ec2.SecurityGroupRule(
        "app-node-self-ingress",
        type="ingress",
        security_group_id=app_node_sg.id,
        source_security_group_id=app_node_sg.id,
        protocol="-1",
        from_port=0,
        to_port=0,
        description="Allow node-to-node communication within app nodes",
    )

    # 3. gVisor Runner Node Security Group
    # STRICTLY ZERO INBOUND RULES: No ingress rules are created here.
    gvisor_node_sg = aws.ec2.SecurityGroup(
        "codeduel-gvisor-node-sg",
        vpc_id=vpc_id,
        description="Security group for isolated gVisor runner nodes (strictly zero inbound)",
        ingress=[],
        egress=[
            aws.ec2.SecurityGroupEgressArgs(
                description="Outbound only: pull sandbox images from ECR, status to EKS API",
                protocol="-1",
                from_port=0,
                to_port=0,
                cidr_blocks=["0.0.0.0/0"],
            ),
        ],
        tags={
            "Name": "codeduel-gvisor-node-sg",
            "Project": "codeduel",
        },
    )

    # 4. RDS PostgreSQL Security Group
    rds_sg = aws.ec2.SecurityGroup(
        "codeduel-rds-sg",
        vpc_id=vpc_id,
        description="Security group for RDS PostgreSQL, accessible only by app nodes",
        ingress=[
            aws.ec2.SecurityGroupIngressArgs(
                description="PostgreSQL port 5432 from app nodes only",
                protocol="tcp",
                from_port=5432,
                to_port=5432,
                security_groups=[app_node_sg.id],
            ),
        ],
        egress=[
            aws.ec2.SecurityGroupEgressArgs(
                description="Outbound for updates / responses",
                protocol="-1",
                from_port=0,
                to_port=0,
                cidr_blocks=["0.0.0.0/0"],
            ),
        ],
        tags={
            "Name": "codeduel-rds-sg",
            "Project": "codeduel",
        },
    )

    # 5. ElastiCache Redis Security Group
    elasticache_sg = aws.ec2.SecurityGroup(
        "codeduel-elasticache-sg",
        vpc_id=vpc_id,
        description="Security group for ElastiCache Redis, accessible only by app nodes",
        ingress=[
            aws.ec2.SecurityGroupIngressArgs(
                description="Redis port 6379 from app nodes only",
                protocol="tcp",
                from_port=6379,
                to_port=6379,
                security_groups=[app_node_sg.id],
            ),
        ],
        egress=[
            aws.ec2.SecurityGroupEgressArgs(
                description="Outbound for responses",
                protocol="-1",
                from_port=0,
                to_port=0,
                cidr_blocks=["0.0.0.0/0"],
            ),
        ],
        tags={
            "Name": "codeduel-elasticache-sg",
            "Project": "codeduel",
        },
    )

    return SecurityGroupResources(
        alb_sg=alb_sg,
        app_node_sg=app_node_sg,
        gvisor_node_sg=gvisor_node_sg,
        rds_sg=rds_sg,
        elasticache_sg=elasticache_sg,
    )


def attach_cluster_to_app_node_rules(
    app_node_sg_id: pulumi.Input[str],
    cluster_sg_id: pulumi.Input[str],
) -> aws.ec2.SecurityGroupRule:
    """Allow full inbound communication from EKS control plane security group to app nodes.

    Required for kubelet communication (port 10250), kubectl logs, kubectl exec,
    and controller admission webhooks.
    """
    return aws.ec2.SecurityGroupRule(
        "app-node-cluster-control-plane-ingress",
        type="ingress",
        security_group_id=app_node_sg_id,
        source_security_group_id=cluster_sg_id,
        protocol="-1",
        from_port=0,
        to_port=0,
        description="Allow full traffic from EKS control plane security group to app nodes",
    )
