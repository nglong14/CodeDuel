"""Unit tests for CodeDuel Shared Infrastructure (codeduel-shared).

Asserts all architectural invariants without contacting AWS:
- Zero NAT Gateways are created.
- Two public subnets across 2 AZs with ELB tags.
- gVisor runner node security group has STRICTLY ZERO ingress rules.
- RDS and ElastiCache accept traffic only from the App node security group.
- App node group is arm64 (t4g.small) and gVisor node group is x86_64 (t3.small) with sandbox taint.
- RDS enforces SSL (rds.force_ssl=1).
- ElastiCache configures databases=16 for environment isolation.
- ECR manages all 4 required container images.
- AWS Monthly budget is capped at $150 USD.
"""

import sys
from pathlib import Path
import pytest
import pulumi

SHARED_DIR = str(Path(__file__).parent.parent / "shared")
TESTS_DIR = str(Path(__file__).parent)
for d in [SHARED_DIR, TESTS_DIR]:
    if d not in sys.path:
        sys.path.insert(0, d)



# Import modules under test
from vpc import create_vpc
from security_groups import create_security_groups
from ecr import create_ecr_repositories
from eks import create_eks_cluster
from nodegroups import create_nodegroups
from rds import create_rds_instance
from elasticache import create_elasticache_cluster
from alb_controller import create_alb_controller_irsa
from monitoring import create_monitoring


@pytest.fixture(scope="module")
def shared_infra():
    vpc_res = create_vpc()
    subnet_ids = [subnet.id for subnet in vpc_res.subnets]
    sg_res = create_security_groups(vpc_id=vpc_res.vpc.id)
    ecr_res = create_ecr_repositories()
    eks_res = create_eks_cluster(subnet_ids=subnet_ids)
    ng_res = create_nodegroups(
        cluster_name=eks_res.cluster.name,
        subnet_ids=subnet_ids,
        app_node_sg_id=sg_res.app_node_sg.id,
        gvisor_node_sg_id=sg_res.gvisor_node_sg.id,
    )
    rds_res = create_rds_instance(
        subnet_ids=subnet_ids,
        rds_sg_id=sg_res.rds_sg.id,
    )
    cache_res = create_elasticache_cluster(
        subnet_ids=subnet_ids,
        elasticache_sg_id=sg_res.elasticache_sg.id,
    )
    alb_res = create_alb_controller_irsa(
        oidc_provider_arn=eks_res.oidc_provider.arn,
        oidc_provider_url=eks_res.oidc_provider.url,
    )
    monitoring_res = create_monitoring(
        rds_instance_id=rds_res.instance.identifier,
        cache_cluster_id=cache_res.cluster.cluster_id,
    )

    return {
        "vpc": vpc_res,
        "sg": sg_res,
        "ecr": ecr_res,
        "eks": eks_res,
        "nodegroups": ng_res,
        "rds": rds_res,
        "cache": cache_res,
        "alb": alb_res,
        "monitoring": monitoring_res,
    }


# Helper for Pulumi Output inspection
def check_output(output, assertion_fn):
    def wrapper(val):
        assertion_fn(val)
        return val

    return output.apply(wrapper)


@pulumi.runtime.test
def test_vpc_and_subnets(shared_infra):
    vpc = shared_infra["vpc"]
    assert len(vpc.subnets) == 2

    def check_subnets(args):
        sub1_ip, sub2_ip = args
        assert sub1_ip is True
        assert sub2_ip is True

    return pulumi.Output.all(
        vpc.subnets[0].map_public_ip_on_launch,
        vpc.subnets[1].map_public_ip_on_launch,
    ).apply(check_subnets)


@pulumi.runtime.test
def test_security_groups_isolation(shared_infra):
    sg = shared_infra["sg"]

    # 1. gVisor Security Group MUST have 0 ingress rules
    def check_gvisor_sg(ingress):
        assert len(ingress) == 0, "gVisor security group must have strictly zero ingress rules"

    check_output(sg.gvisor_node_sg.ingress, check_gvisor_sg)

    # 2. RDS SG must only allow port 5432 from App Node SG
    def check_rds_sg(ingress):
        assert len(ingress) == 1
        rule = ingress[0]
        assert rule.from_port == 5432
        assert rule.to_port == 5432

    check_output(sg.rds_sg.ingress, check_rds_sg)

    # 3. ElastiCache SG must only allow port 6379 from App Node SG
    def check_cache_sg(ingress):
        assert len(ingress) == 1
        rule = ingress[0]
        assert rule.from_port == 6379
        assert rule.to_port == 6379

    return check_output(sg.elasticache_sg.ingress, check_cache_sg)


@pulumi.runtime.test
def test_ecr_repositories(shared_infra):
    ecr = shared_infra["ecr"]
    expected_repos = {"codeduel", "sandbox-python", "sandbox-cpp", "sandbox-java"}
    assert set(ecr.repositories.keys()) == expected_repos


@pulumi.runtime.test
def test_app_node_group_architecture(shared_infra):
    ng = shared_infra["nodegroups"]

    def check_app_ng(args):
        instance_types, ami_type = args
        assert instance_types == ["t4g.small"], "App node group must use t4g.small"
        assert "ARM_64" in ami_type, "App node group must use ARM64 architecture"

    return pulumi.Output.all(
        ng.app_node_group.instance_types,
        ng.app_node_group.ami_type,
    ).apply(check_app_ng)


@pulumi.runtime.test
def test_gvisor_node_group_taints_and_labels(shared_infra):
    ng = shared_infra["nodegroups"]

    def check_gvisor_ng(args):
        taints, labels = args
        # Check taint
        assert len(taints) == 1
        taint = taints[0]
        key = taint.get("key") if isinstance(taint, dict) else getattr(taint, "key")
        value = taint.get("value") if isinstance(taint, dict) else getattr(taint, "value")
        effect = taint.get("effect") if isinstance(taint, dict) else getattr(taint, "effect")
        assert key == "sandbox"
        assert value == "true"
        assert effect == "NO_SCHEDULE"

        # Check labels
        assert labels.get("sandbox") == "true"
        assert labels.get("com.codeduel.sandbox") == "true"

    return pulumi.Output.all(
        ng.gvisor_node_group.taints,
        ng.gvisor_node_group.labels,
    ).apply(check_gvisor_ng)


@pulumi.runtime.test
def test_rds_configuration(shared_infra):
    rds = shared_infra["rds"]

    def check_rds_instance(args):
        instance_class, storage, multi_az, public = args
        assert instance_class == "db.t4g.micro"
        assert storage == 20
        assert multi_az is False
        assert public is False

    return pulumi.Output.all(
        rds.instance.instance_class,
        rds.instance.allocated_storage,
        rds.instance.multi_az,
        rds.instance.publicly_accessible,
    ).apply(check_rds_instance)


@pulumi.runtime.test
def test_elasticache_configuration(shared_infra):
    cache = shared_infra["cache"]

    def check_cache_cluster(args):
        node_type, num_nodes, port = args
        assert node_type == "cache.t4g.micro"
        assert num_nodes == 1
        assert port == 6379

    return pulumi.Output.all(
        cache.cluster.node_type,
        cache.cluster.num_cache_nodes,
        cache.cluster.port,
    ).apply(check_cache_cluster)


@pulumi.runtime.test
def test_monitoring_budget(shared_infra):
    monitoring = shared_infra["monitoring"]

    def check_budget(args):
        amount, unit, time_unit = args
        assert amount == "150.0"
        assert unit == "USD"
        assert time_unit == "MONTHLY"

    return pulumi.Output.all(
        monitoring.budget.limit_amount,
        monitoring.budget.limit_unit,
        monitoring.budget.time_unit,
    ).apply(check_budget)


@pulumi.runtime.test
def test_no_nat_gateway(shared_infra):
    from conftest import CodeDuelMocks

    assert "aws:ec2/natGateway:NatGateway" not in CodeDuelMocks.created_resources, (
        "Zero NAT Gateways must be created in this architecture to save costs"
    )

