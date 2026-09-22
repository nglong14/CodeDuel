"""Shared Stack Reference Loader for CodeDuel Environment Stacks.

Reads outputs exported by the 'codeduel-shared' project.
"""

from typing import NamedTuple

import pulumi


class SharedStackOutputs(NamedTuple):
    vpc_id: pulumi.Output[str]
    eks_cluster_name: pulumi.Output[str]
    eks_cluster_endpoint: pulumi.Output[str]
    eks_cluster_ca_data: pulumi.Output[str]
    rds_address: pulumi.Output[str]
    rds_port: pulumi.Output[int]
    rds_master_username: pulumi.Output[str]
    rds_master_password: pulumi.Output[str]
    elasticache_address: pulumi.Output[str]
    elasticache_port: pulumi.Output[int]
    ecr_repository_urls: pulumi.Output[dict]


def load_shared_stack_outputs(shared_stack_ref_name: str) -> SharedStackOutputs:
    shared_stack = pulumi.StackReference(shared_stack_ref_name)

    # StackReference outputs round-trip through JSON, so ints (e.g. RDS/
    # ElastiCache ports) come back as floats. Cast once here so no downstream
    # consumer has to remember to, or renders "6379.0" into a URL/DSN.
    def as_int(output: pulumi.Output) -> pulumi.Output[int]:
        return output.apply(lambda v: int(v))

    return SharedStackOutputs(
        vpc_id=shared_stack.get_output("vpc_id"),
        eks_cluster_name=shared_stack.get_output("eks_cluster_name"),
        eks_cluster_endpoint=shared_stack.get_output("eks_cluster_endpoint"),
        eks_cluster_ca_data=shared_stack.get_output(
            "eks_cluster_certificate_authority_data"
        ),
        rds_address=shared_stack.get_output("rds_address"),
        rds_port=as_int(shared_stack.get_output("rds_port")),
        rds_master_username=shared_stack.get_output("rds_master_username"),
        rds_master_password=shared_stack.get_output("rds_master_password"),
        elasticache_address=shared_stack.get_output("elasticache_address"),
        elasticache_port=as_int(shared_stack.get_output("elasticache_port")),
        ecr_repository_urls=shared_stack.get_output("ecr_repository_urls"),
    )
