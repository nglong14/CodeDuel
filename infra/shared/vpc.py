"""VPC and Networking for CodeDuel Shared Infrastructure.

Key architectural constraints:
- Exactly 2 public subnets in two distinct AZs.
- NO NAT Gateway (cost constraint: ~$32/month saved; nodes have public IPs, locked down via security groups).
- Tagged for AWS Load Balancer Controller discovery (kubernetes.io/role/elb=1).
"""

from typing import NamedTuple

import pulumi_aws as aws


class VpcResources(NamedTuple):
    vpc: aws.ec2.Vpc
    subnets: list[aws.ec2.Subnet]
    igw: aws.ec2.InternetGateway
    route_table: aws.ec2.RouteTable


def create_vpc() -> VpcResources:
    vpc = aws.ec2.Vpc(
        "codeduel-vpc",
        cidr_block="10.0.0.0/16",
        enable_dns_hostnames=True,
        enable_dns_support=True,
        tags={
            "Name": "codeduel-vpc",
            "Project": "codeduel",
        },
    )

    azs = aws.get_availability_zones(state="available")

    subnets: list[aws.ec2.Subnet] = []
    subnet_cidrs = ["10.0.1.0/24", "10.0.2.0/24"]

    for i, cidr in enumerate(subnet_cidrs):
        subnet = aws.ec2.Subnet(
            f"codeduel-public-{i + 1}",
            vpc_id=vpc.id,
            cidr_block=cidr,
            availability_zone=azs.names[i],
            map_public_ip_on_launch=True,
            tags={
                "Name": f"codeduel-public-{i + 1}",
                "Project": "codeduel",
                "kubernetes.io/role/elb": "1",
                "kubernetes.io/cluster/codeduel": "shared",
            },
        )
        subnets.append(subnet)

    igw = aws.ec2.InternetGateway(
        "codeduel-igw",
        vpc_id=vpc.id,
        tags={
            "Name": "codeduel-igw",
            "Project": "codeduel",
        },
    )

    route_table = aws.ec2.RouteTable(
        "codeduel-public-rt",
        vpc_id=vpc.id,
        routes=[
            aws.ec2.RouteTableRouteArgs(
                cidr_block="0.0.0.0/0",
                gateway_id=igw.id,
            )
        ],
        tags={
            "Name": "codeduel-public-rt",
            "Project": "codeduel",
        },
    )

    for i, subnet in enumerate(subnets):
        aws.ec2.RouteTableAssociation(
            f"codeduel-rta-{i + 1}",
            subnet_id=subnet.id,
            route_table_id=route_table.id,
        )

    return VpcResources(
        vpc=vpc,
        subnets=subnets,
        igw=igw,
        route_table=route_table,
    )
