"""CodeDuel Shared Infrastructure Entry Point.

Orchestrates provisioning of:
- VPC & public subnets (strictly no NAT gateway)
- Least-privilege Security Groups
- ECR Repositories (codeduel + sandboxes)
- EKS Cluster & OIDC Provider
- Node Groups (App Graviton arm64 + gVisor tainted x86_64)
- RDS PostgreSQL & ElastiCache Redis
- AWS Load Balancer Controller IRSA
- Fluent Bit log shipping to CloudWatch Logs (IRSA + DaemonSet)
- GitHub Actions OIDC plan/deploy roles (optional; needs github_repository)
- CloudWatch & AWS Budget Alarms
"""

import pulumi
import pulumi_kubernetes as k8s
from alb_controller import (
    build_cluster_kubeconfig,
    create_alb_controller_irsa,
    deploy_alb_controller,
)
from certificate import create_certificate
from ecr import create_ecr_repositories
from eks import create_eks_cluster
from elasticache import create_elasticache_cluster
from fluent_bit import deploy_fluent_bit
from github_oidc import create_github_oidc
from monitoring import create_monitoring
from nodegroups import create_nodegroups
from rds import create_rds_instance
from security_groups import attach_cluster_to_app_node_rules, create_security_groups
from vpc import create_vpc

vpc_res = create_vpc()
subnet_ids = [subnet.id for subnet in vpc_res.subnets]

sg_res = create_security_groups(vpc_id=vpc_res.vpc.id)

ecr_res = create_ecr_repositories()

eks_res = create_eks_cluster(subnet_ids=subnet_ids)

# Allow EKS cluster control plane full access to app nodes (kubelet, logs, webhooks)
attach_cluster_to_app_node_rules(
    app_node_sg_id=sg_res.app_node_sg.id,
    cluster_sg_id=eks_res.cluster.vpc_config.cluster_security_group_id,
)

nodegroups_res = create_nodegroups(
    cluster_name=eks_res.cluster.name,
    cluster_endpoint=eks_res.cluster.endpoint,
    cluster_ca_data=eks_res.cluster.certificate_authorities[0].data,
    cluster_service_cidr=eks_res.cluster.kubernetes_network_config.apply(
        lambda cfg: cfg.service_ipv4_cidr
    ),
    subnet_ids=subnet_ids,
    app_node_sg_id=sg_res.app_node_sg.id,
    gvisor_node_sg_id=sg_res.gvisor_node_sg.id,
    cluster_security_group_id=eks_res.cluster.vpc_config.cluster_security_group_id,
)

rds_res = create_rds_instance(
    subnet_ids=subnet_ids,
    rds_sg_id=sg_res.rds_sg.id,
)

cache_res = create_elasticache_cluster(
    subnet_ids=subnet_ids,
    elasticache_sg_id=sg_res.elasticache_sg.id,
)

alb_irsa_res = create_alb_controller_irsa(
    oidc_provider_arn=eks_res.oidc_provider.arn,
    oidc_provider_url=eks_res.oidc_provider.url,
)

# Deploy the AWS Load Balancer Controller into the cluster so Ingress
# resources (see deploy/k8s/base/ingress.yaml) can actually provision an ALB.
cluster_kubeconfig = build_cluster_kubeconfig(
    cluster_name=eks_res.cluster.name,
    cluster_endpoint=eks_res.cluster.endpoint,
    cluster_ca_data=eks_res.cluster.certificate_authorities[0].data,
)
cluster_k8s_provider = k8s.Provider(
    "codeduel-cluster-k8s-provider",
    kubeconfig=cluster_kubeconfig,
)
aws_region = pulumi.Config("aws").require("region")
alb_controller_release = deploy_alb_controller(
    cluster_name=eks_res.cluster.name,
    vpc_id=vpc_res.vpc.id,
    role_arn=alb_irsa_res.role.arn,
    k8s_provider=cluster_k8s_provider,
    region=aws_region,
)

# Ships pod stdout/stderr from the codeduel-* namespaces into the per-environment
# CloudWatch log groups created by the env stacks. Without it those groups stay
# empty and `kubectl logs` is the only log path, so logs die with the pod.
fluent_bit_res = deploy_fluent_bit(
    oidc_provider_arn=eks_res.oidc_provider.arn,
    oidc_provider_url=eks_res.oidc_provider.url,
    k8s_provider=cluster_k8s_provider,
    region=aws_region,
)

monitoring_res = create_monitoring(
    rds_instance_id=rds_res.instance.identifier,
    cache_cluster_id=cache_res.cluster.cluster_id,
)

pulumi.export("vpc_id", vpc_res.vpc.id)
pulumi.export("public_subnet_ids", subnet_ids)

pulumi.export("alb_sg_id", sg_res.alb_sg.id)
pulumi.export("app_node_sg_id", sg_res.app_node_sg.id)
pulumi.export("gvisor_node_sg_id", sg_res.gvisor_node_sg.id)
pulumi.export("rds_sg_id", sg_res.rds_sg.id)
pulumi.export("elasticache_sg_id", sg_res.elasticache_sg.id)

pulumi.export("eks_cluster_name", eks_res.cluster.name)
pulumi.export("eks_cluster_endpoint", eks_res.cluster.endpoint)
pulumi.export("eks_cluster_security_group_id", eks_res.cluster.vpc_config.cluster_security_group_id)
pulumi.export(
    "eks_cluster_certificate_authority_data",
    eks_res.cluster.certificate_authorities[0].data,
)
pulumi.export("eks_oidc_provider_arn", eks_res.oidc_provider.arn)
pulumi.export("eks_oidc_provider_url", eks_res.oidc_provider.url)

pulumi.export("rds_instance_id", rds_res.instance.identifier)
pulumi.export("rds_address", rds_res.instance.address)
pulumi.export("rds_port", rds_res.instance.port)
pulumi.export("rds_master_username", rds_res.instance.username)
pulumi.export("rds_master_password", pulumi.Output.secret(rds_res.master_password.result))

pulumi.export("elasticache_cluster_id", cache_res.cluster.cluster_id)
pulumi.export("elasticache_address", cache_res.cluster.cache_nodes[0].address)
pulumi.export("elasticache_port", cache_res.cluster.port)

pulumi.export(
    "ecr_repository_urls",
    {name: repo.repository_url for name, repo in ecr_res.repositories.items()},
)
shared_config = pulumi.Config("codeduel-shared")
domain_name = shared_config.get("domain_name")
zone_id = shared_config.get("route53_zone_id")
cert_res = create_certificate(domain_name=domain_name, zone_id=zone_id)

# GitHub Actions CI/CD roles. Skipped unless the repository is configured, so a
# laptop-only workflow needs no extra config:
#   pulumi config set codeduel-shared:github_repository <owner>/CodeDuel
github_repository = shared_config.get("github_repository")
github_res = (
    create_github_oidc(
        repository=github_repository,
        cluster_name=eks_res.cluster.name,
        existing_provider_arn=shared_config.get("github_oidc_provider_arn"),
    )
    if github_repository
    else None
)

pulumi.export("alb_controller_role_arn", alb_irsa_res.role.arn)
pulumi.export("fluent_bit_role_arn", fluent_bit_res.role.arn)
pulumi.export("budget_id", monitoring_res.budget.id)
pulumi.export("acm_certificate_arn", cert_res.certificate_arn)
if github_res:
    pulumi.export("github_plan_role_arn", github_res.plan_role.arn)
    pulumi.export("github_deploy_role_arn", github_res.deploy_role.arn)
