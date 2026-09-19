"""EKS Cluster and OIDC Provider for CodeDuel Shared Infrastructure.

Provisions:
- IAM role for EKS control plane.
- EKS cluster 'codeduel'.
- IAM OpenID Connect (OIDC) provider for IAM Roles for Service Accounts (IRSA).
"""

import json
from typing import NamedTuple
import pulumi
import pulumi_aws as aws


class EksResources(NamedTuple):
    cluster: aws.eks.Cluster
    cluster_role: aws.iam.Role
    oidc_provider: aws.iam.OpenIdConnectProvider


def create_eks_cluster(subnet_ids: list[pulumi.Input[str]]) -> EksResources:
    cluster_role = aws.iam.Role(
        "codeduel-eks-cluster-role",
        assume_role_policy=json.dumps(
            {
                "Version": "2012-10-17",
                "Statement": [
                    {
                        "Action": "sts:AssumeRole",
                        "Effect": "Allow",
                        "Principal": {
                            "Service": "eks.amazonaws.com",
                        },
                    }
                ],
            }
        ),
        tags={
            "Name": "codeduel-eks-cluster-role",
            "Project": "codeduel",
        },
    )

    aws.iam.RolePolicyAttachment(
        "codeduel-eks-cluster-policy-attachment",
        role=cluster_role.name,
        policy_arn="arn:aws:iam::aws:policy/AmazonEKSClusterPolicy",
    )

    aws.iam.RolePolicyAttachment(
        "codeduel-eks-service-policy-attachment",
        role=cluster_role.name,
        policy_arn="arn:aws:iam::aws:policy/AmazonEKSServicePolicy",
    )

    cluster = aws.eks.Cluster(
        "codeduel-cluster",
        name="codeduel",
        role_arn=cluster_role.arn,
        version="1.30",
        vpc_config=aws.eks.ClusterVpcConfigArgs(
            subnet_ids=subnet_ids,
            endpoint_public_access=True,
            endpoint_private_access=True,
        ),
        tags={
            "Name": "codeduel-cluster",
            "Project": "codeduel",
        },
    )

    oidc_provider = aws.iam.OpenIdConnectProvider(
        "codeduel-eks-oidc-provider",
        client_id_lists=["sts.amazonaws.com"],
        url=cluster.identities[0].oidcs[0].issuer,
        thumbprint_lists=["9e99a48a9960b14926cc7f3b02322d8e3b2f8e5b"],
        tags={
            "Name": "codeduel-eks-oidc-provider",
            "Project": "codeduel",
        },
    )

    return EksResources(
        cluster=cluster,
        cluster_role=cluster_role,
        oidc_provider=oidc_provider,
    )
