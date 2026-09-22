"""AWS Load Balancer Controller IRSA for CodeDuel Shared Infrastructure.

Provisions:
- IAM Role with OIDC Federated trust bound strictly to the
  'system:serviceaccount:kube-system:aws-load-balancer-controller' ServiceAccount.
- IAM Policy with permissions required for ALB lifecycle management.
"""

import json
from typing import NamedTuple

import pulumi
import pulumi_aws as aws


class AlbControllerResources(NamedTuple):
    role: aws.iam.Role
    policy: aws.iam.Policy


# Standard AWS Load Balancer Controller v2.7+ policy specification
ALB_CONTROLLER_POLICY_DOCUMENT = {
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "iam:CreateServiceLinkedRole",
            ],
            "Resource": "*",
            "Condition": {
                "StringEquals": {
                    "iam:AWSServiceName": "elasticloadbalancing.amazonaws.com",
                },
            },
        },
        {
            "Effect": "Allow",
            "Action": [
                "ec2:DescribeAccountAttributes",
                "ec2:DescribeAddresses",
                "ec2:DescribeAvailabilityZones",
                "ec2:DescribeInternetGateways",
                "ec2:DescribeVpcs",
                "ec2:DescribeVpcPeeringConnections",
                "ec2:DescribeSubnets",
                "ec2:DescribeSecurityGroups",
                "ec2:DescribeInstances",
                "ec2:DescribeNetworkInterfaces",
                "ec2:DescribeTags",
                "ec2:GetCoipPoolUsage",
                "ec2:DescribeCoipPools",
                "elasticloadbalancing:DescribeLoadBalancers",
                "elasticloadbalancing:DescribeLoadBalancerAttributes",
                "elasticloadbalancing:DescribeListeners",
                "elasticloadbalancing:DescribeListenerCertificates",
                "elasticloadbalancing:DescribeSSLPolicies",
                "elasticloadbalancing:DescribeRules",
                "elasticloadbalancing:DescribeTargetGroups",
                "elasticloadbalancing:DescribeTargetGroupAttributes",
                "elasticloadbalancing:DescribeTargetHealth",
                "elasticloadbalancing:DescribeTags",
                "elasticloadbalancing:DescribeTrustStores",
            ],
            "Resource": "*",
        },
        {
            "Effect": "Allow",
            "Action": [
                "cognito-idp:DescribeUserPoolClient",
                "acm:ListCertificates",
                "acm:DescribeCertificate",
                "iam:ListServerCertificates",
                "iam:GetServerCertificate",
                "waf-regional:GetWebACL",
                "waf-regional:GetWebACLForResource",
                "waf-regional:AssociateWebACL",
                "waf-regional:DisassociateWebACL",
                "wafv2:GetWebACL",
                "wafv2:GetWebACLForResource",
                "wafv2:AssociateWebACL",
                "wafv2:DisassociateWebACL",
                "shield:GetSubscriptionState",
                "shield:DescribeProtection",
                "shield:CreateProtection",
                "shield:DeleteProtection",
            ],
            "Resource": "*",
        },
        {
            "Effect": "Allow",
            "Action": [
                "ec2:AuthorizeSecurityGroupIngress",
                "ec2:RevokeSecurityGroupIngress",
            ],
            "Resource": "arn:aws:ec2:*:*:security-group/*",
        },
        {
            "Effect": "Allow",
            "Action": [
                "ec2:CreateSecurityGroup",
            ],
            "Resource": "*",
        },
        {
            "Effect": "Allow",
            "Action": [
                "ec2:CreateTags",
                "ec2:DeleteTags",
            ],
            "Resource": "arn:aws:ec2:*:*:security-group/*",
        },
        {
            "Effect": "Allow",
            "Action": [
                "elasticloadbalancing:CreateLoadBalancer",
                "elasticloadbalancing:CreateTargetGroup",
            ],
            "Resource": "*",
        },
        {
            "Effect": "Allow",
            "Action": [
                "elasticloadbalancing:CreateListener",
                "elasticloadbalancing:DeleteListener",
                "elasticloadbalancing:CreateRule",
                "elasticloadbalancing:DeleteRule",
            ],
            "Resource": "*",
        },
        {
            "Effect": "Allow",
            "Action": [
                "elasticloadbalancing:AddTags",
                "elasticloadbalancing:RemoveTags",
            ],
            "Resource": [
                "arn:aws:elasticloadbalancing:*:*:targetgroup/*/*",
                "arn:aws:elasticloadbalancing:*:*:loadbalancer/*/*",
            ],
        },
        {
            "Effect": "Allow",
            "Action": [
                "elasticloadbalancing:ModifyLoadBalancerAttributes",
                "elasticloadbalancing:SetIpAddressType",
                "elasticloadbalancing:SetSecurityGroups",
                "elasticloadbalancing:SetSubnets",
                "elasticloadbalancing:DeleteLoadBalancer",
                "elasticloadbalancing:ModifyTargetGroup",
                "elasticloadbalancing:ModifyTargetGroupAttributes",
                "elasticloadbalancing:DeleteTargetGroup",
                "elasticloadbalancing:RegisterTargets",
                "elasticloadbalancing:DeregisterTargets",
                "elasticloadbalancing:SetWebAcl",
                "elasticloadbalancing:ModifyListener",
                "elasticloadbalancing:AddListenerCertificates",
                "elasticloadbalancing:RemoveListenerCertificates",
                "elasticloadbalancing:ModifyRule",
            ],
            "Resource": "*",
        },
    ],
}


def create_alb_controller_irsa(
    oidc_provider_arn: pulumi.Input[str],
    oidc_provider_url: pulumi.Input[str],
) -> AlbControllerResources:
    # 1. IAM Role with OIDC Federated Trust
    def build_assume_role_policy(args):
        arn, url = args
        clean_url = url.replace("https://", "")
        return json.dumps(
            {
                "Version": "2012-10-17",
                "Statement": [
                    {
                        "Effect": "Allow",
                        "Principal": {
                            "Federated": arn,
                        },
                        "Action": "sts:AssumeRoleWithWebIdentity",
                        "Condition": {
                            "StringEquals": {
                                f"{clean_url}:sub": "system:serviceaccount:kube-system:aws-load-balancer-controller",
                                f"{clean_url}:aud": "sts.amazonaws.com",
                            },
                        },
                    },
                ],
            }
        )

    assume_role_policy = pulumi.Output.all(
        oidc_provider_arn,
        oidc_provider_url,
    ).apply(build_assume_role_policy)

    role = aws.iam.Role(
        "codeduel-alb-controller-role",
        name="codeduel-alb-controller-role",
        assume_role_policy=assume_role_policy,
        tags={
            "Name": "codeduel-alb-controller-role",
            "Project": "codeduel",
        },
    )

    # 2. Controller Policy
    policy = aws.iam.Policy(
        "codeduel-alb-controller-policy",
        name="AWSLoadBalancerControllerIAMPolicy",
        description="Policy granting required ELB and EC2 permissions to AWS Load Balancer Controller",
        policy=json.dumps(ALB_CONTROLLER_POLICY_DOCUMENT),
        tags={
            "Name": "AWSLoadBalancerControllerIAMPolicy",
            "Project": "codeduel",
        },
    )

    aws.iam.RolePolicyAttachment(
        "codeduel-alb-controller-policy-attachment",
        role=role.name,
        policy_arn=policy.arn,
    )

    return AlbControllerResources(role=role, policy=policy)


def build_cluster_kubeconfig(
    cluster_name: pulumi.Input[str],
    cluster_endpoint: pulumi.Input[str],
    cluster_ca_data: pulumi.Input[str],
) -> pulumi.Output[str]:
    """Build a kubeconfig (using 'aws eks get-token') for the shared EKS cluster."""
    return pulumi.Output.all(cluster_name, cluster_endpoint, cluster_ca_data).apply(
        lambda args: json.dumps(
            {
                "apiVersion": "v1",
                "clusters": [
                    {
                        "cluster": {
                            "server": args[1],
                            "certificate-authority-data": args[2],
                        },
                        "name": "kubernetes",
                    }
                ],
                "contexts": [
                    {
                        "context": {"cluster": "kubernetes", "user": "aws"},
                        "name": "aws",
                    }
                ],
                "current-context": "aws",
                "kind": "Config",
                "users": [
                    {
                        "name": "aws",
                        "user": {
                            "exec": {
                                "apiVersion": "client.authentication.k8s.io/v1beta1",
                                "command": "aws",
                                "args": ["eks", "get-token", "--cluster-name", args[0]],
                            }
                        },
                    }
                ],
            }
        )
    )


def deploy_alb_controller(
    cluster_name: pulumi.Input[str],
    vpc_id: pulumi.Input[str],
    role_arn: pulumi.Input[str],
    k8s_provider: pulumi.Input,
    region: str = "us-east-1",
):
    """Deploy AWS Load Balancer Controller Helm chart into kube-system namespace.

    Binds the controller to the IAM role via IRSA so it can provision ALBs for Ingress.
    """
    import pulumi_kubernetes as k8s

    return k8s.helm.v3.Release(
        "aws-load-balancer-controller",
        name="aws-load-balancer-controller",
        chart="aws-load-balancer-controller",
        version="1.8.1",
        repository_opts=k8s.helm.v3.RepositoryOptsArgs(
            repo="https://aws.github.io/eks-charts",
        ),
        namespace="kube-system",
        values={
            "clusterName": cluster_name,
            "region": region,
            "vpcId": vpc_id,
            "serviceAccount": {
                "create": True,
                "name": "aws-load-balancer-controller",
                "annotations": {
                    "eks.amazonaws.com/role-arn": role_arn,
                },
            },
        },
        opts=pulumi.ResourceOptions(provider=k8s_provider),
    )
