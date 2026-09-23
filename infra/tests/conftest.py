"""Shared Pytest Configuration and Pulumi Mocks for CodeDuel Infrastructure."""

import sys
from pathlib import Path

import pulumi

# Ensure infra/shared is in sys.path for test imports
SHARED_DIR = str(Path(__file__).parent.parent / "shared")
if SHARED_DIR not in sys.path:
    sys.path.insert(0, SHARED_DIR)


from typing import ClassVar


class CodeDuelMocks(pulumi.runtime.Mocks):
    created_resources: ClassVar[list[str]] = []

    def new_resource(self, args: pulumi.runtime.MockResourceArgs):
        self.created_resources.append(args.typ)
        outputs = dict(args.inputs)


        # Populate computed properties based on resource type
        if args.typ == "aws:ec2/vpc:Vpc":
            outputs["id"] = "vpc-12345678"
        elif args.typ == "aws:ec2/subnet:Subnet":
            outputs["id"] = f"subnet-{args.name}"
        elif args.typ == "aws:ec2/internetGateway:InternetGateway":
            outputs["id"] = f"igw-{args.name}"
        elif args.typ == "aws:ec2/routeTable:RouteTable":
            outputs["id"] = f"rt-{args.name}"
        elif args.typ == "aws:ec2/securityGroup:SecurityGroup":
            outputs["id"] = f"sg-{args.name}"
        elif args.typ == "aws:eks/cluster:Cluster":
            outputs["id"] = "codeduel"
            outputs["name"] = "codeduel"
            outputs["endpoint"] = "https://k8s.example.com"
            outputs["certificateAuthorities"] = [
                {"data": "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCg=="}
            ]
            outputs["kubernetesNetworkConfig"] = {"serviceIpv4Cidr": "172.20.0.0/16"}
            outputs["vpcConfig"] = {"clusterSecurityGroupId": "sg-eks-cluster-mock"}
            outputs["identities"] = [
                {
                    "oidcs": [
                        {
                            "issuer": "https://oidc.eks.us-east-1.amazonaws.com/id/EXAMPLED5E428766C6979208CD55FB3D"
                        }
                    ]
                }
            ]
        elif args.typ == "aws:iam/role:Role":
            outputs["arn"] = f"arn:aws:iam::123456789012:role/{args.name}"
            outputs["name"] = args.name
        elif args.typ == "aws:iam/policy:Policy":
            outputs["arn"] = f"arn:aws:iam::123456789012:policy/{args.name}"
        elif args.typ == "aws:iam/openIdConnectProvider:OpenIdConnectProvider":
            outputs["arn"] = f"arn:aws:iam::123456789012:oidc-provider/{args.name}"
            outputs["url"] = "https://oidc.eks.us-east-1.amazonaws.com/id/EXAMPLED5E428766C6979208CD55FB3D"
        elif args.typ == "aws:ec2/launchTemplate:LaunchTemplate":
            outputs["id"] = f"lt-{args.name}"
            outputs["latestVersion"] = 1
        elif args.typ == "aws:eks/nodeGroup:NodeGroup":
            outputs["id"] = f"ng-{args.name}"
            outputs["arn"] = f"arn:aws:eks:us-east-1:123456789012:nodegroup/codeduel/{args.name}"
        elif args.typ == "aws:rds/subnetGroup:SubnetGroup" or args.typ == "aws:rds/parameterGroup:ParameterGroup":
            outputs["id"] = args.name
            outputs["name"] = args.name
        elif args.typ == "aws:rds/instance:Instance":
            outputs["id"] = "codeduel-postgres"
            outputs["identifier"] = "codeduel-postgres"
            outputs["address"] = "codeduel-postgres.c123456.us-east-1.rds.amazonaws.com"
            outputs["port"] = 5432
            outputs["username"] = "codeduel_admin"
        elif args.typ == "aws:elasticache/subnetGroup:SubnetGroup" or args.typ == "aws:elasticache/parameterGroup:ParameterGroup":
            outputs["id"] = args.name
            outputs["name"] = args.name
        elif args.typ == "aws:elasticache/cluster:Cluster":
            outputs["id"] = "codeduel-cache"
            outputs["clusterId"] = "codeduel-cache"
            outputs["cacheNodes"] = [{"address": "codeduel-cache.12345.cache.amazonaws.com"}]
            outputs["port"] = 6379
        elif args.typ == "aws:ecr/repository:Repository":
            outputs["id"] = args.name
            outputs["repositoryUrl"] = f"123456789012.dkr.ecr.us-east-1.amazonaws.com/{args.name}"
        elif args.typ == "random:index/randomPassword:RandomPassword":
            outputs["result"] = "mock_random_password_123456789"
        elif args.typ == "aws:budgets/budget:Budget":
            outputs["id"] = "codeduel-monthly-budget"
        elif args.typ == "aws:cloudwatch/metricAlarm:MetricAlarm":
            outputs["id"] = args.name

        return [args.name + "_id", outputs]

    def call(self, args: pulumi.runtime.MockCallArgs):
        if args.token == "aws:index/getAvailabilityZones:getAvailabilityZones":
            return {
                "names": ["us-east-1a", "us-east-1b"],
                "zoneIds": ["use1-az1", "use1-az2"],
                "id": "us-east-1",
            }
        return {}


def init_mocks():
    pulumi.runtime.set_mocks(
        CodeDuelMocks(),
        project="codeduel-shared",
        stack="shared",
        preview=False,
    )


# Run before any test module is imported or executed
init_mocks()

