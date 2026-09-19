import base64
import json
from typing import NamedTuple

import pulumi
import pulumi_aws as aws


class NodeGroupResources(NamedTuple):
    node_role: aws.iam.Role
    app_node_group: aws.eks.NodeGroup
    gvisor_node_group: aws.eks.NodeGroup
    gvisor_launch_template: aws.ec2.LaunchTemplate


GVISOR_USERDATA_SCRIPT = """MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="==BOUNDARY=="

--==BOUNDARY==
Content-Type: text/x-shellscript; charset="us-ascii"

#!/bin/bash
set -euo pipefail
ARCH=$(uname -m)
URL="https://storage.googleapis.com/gvisor/releases/release/latest/${ARCH}"
curl -fsSL "${URL}/runsc" -o /usr/local/bin/runsc
curl -fsSL "${URL}/containerd-shim-runsc-v1" -o /usr/local/bin/containerd-shim-runsc-v1
chmod a+rx /usr/local/bin/runsc /usr/local/bin/containerd-shim-runsc-v1

mkdir -p /etc/containerd/conf.d
cat << 'EOF' > /etc/containerd/conf.d/gvisor.toml
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
  runtime_type = "io.containerd.runsc.v1"
EOF
systemctl restart containerd

--==BOUNDARY==--
"""


def create_nodegroups(
    cluster_name: pulumi.Input[str],
    subnet_ids: list[pulumi.Input[str]],
    app_node_sg_id: pulumi.Input[str],
    gvisor_node_sg_id: pulumi.Input[str],
) -> NodeGroupResources:
    # 1. IAM Role for Worker Nodes
    node_role = aws.iam.Role(
        "codeduel-eks-node-role",
        assume_role_policy=json.dumps(
            {
                "Version": "2012-10-17",
                "Statement": [
                    {
                        "Action": "sts:AssumeRole",
                        "Effect": "Allow",
                        "Principal": {
                            "Service": "ec2.amazonaws.com",
                        },
                    }
                ],
            }
        ),
        tags={
            "Name": "codeduel-eks-node-role",
            "Project": "codeduel",
        },
    )

    for policy in [
        "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy",
        "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly",
        "arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy",
    ]:
        policy_name = policy.split("/")[-1]
        aws.iam.RolePolicyAttachment(
            f"codeduel-node-{policy_name}-attachment",
            role=node_role.name,
            policy_arn=policy,
        )

    # 2. App Node Group (arm64, t4g.small)
    app_node_group = aws.eks.NodeGroup(
        "codeduel-app-nodegroup",
        cluster_name=cluster_name,
        node_group_name="codeduel-app",
        node_role_arn=node_role.arn,
        subnet_ids=subnet_ids,
        instance_types=["t4g.small"],
        ami_type="AL2023_ARM_64_STANDARD",
        scaling_config=aws.eks.NodeGroupScalingConfigArgs(
            desired_size=2,
            min_size=1,
            max_size=3,
        ),
        labels={
            "role": "app",
        },
        tags={
            "Name": "codeduel-app-node",
            "Project": "codeduel",
        },
    )

    # 3. gVisor Runner Launch Template (x86_64, installs runsc, attached to zero-inbound SG)
    encoded_userdata = base64.b64encode(GVISOR_USERDATA_SCRIPT.encode("utf-8")).decode("utf-8")

    gvisor_launch_template = aws.ec2.LaunchTemplate(
        "codeduel-gvisor-launch-template",
        name_prefix="codeduel-gvisor-lt-",
        instance_type="t3.small",
        user_data=encoded_userdata,
        vpc_security_group_ids=[gvisor_node_sg_id],
        tag_specifications=[
            aws.ec2.LaunchTemplateTagSpecificationArgs(
                resource_type="instance",
                tags={
                    "Name": "codeduel-gvisor-runner-node",
                    "Project": "codeduel",
                },
            ),
        ],
        tags={
            "Name": "codeduel-gvisor-launch-template",
            "Project": "codeduel",
        },
    )

    # 4. gVisor Node Group (x86_64, t3.small, tainted sandbox=true:NoSchedule)
    gvisor_node_group = aws.eks.NodeGroup(
        "codeduel-gvisor-nodegroup",
        cluster_name=cluster_name,
        node_group_name="codeduel-gvisor",
        node_role_arn=node_role.arn,
        subnet_ids=subnet_ids,
        scaling_config=aws.eks.NodeGroupScalingConfigArgs(
            desired_size=1,
            min_size=1,
            max_size=2,
        ),
        taints=[
            aws.eks.NodeGroupTaintArgs(
                key="sandbox",
                value="true",
                effect="NO_SCHEDULE",
            ),
        ],
        labels={
            "sandbox": "true",
            "com.codeduel.sandbox": "true",
        },
        launch_template=aws.eks.NodeGroupLaunchTemplateArgs(
            id=gvisor_launch_template.id,
            version=gvisor_launch_template.latest_version.apply(str),
        ),
        tags={
            "Name": "codeduel-gvisor-node",
            "Project": "codeduel",
        },
    )

    return NodeGroupResources(
        node_role=node_role,
        app_node_group=app_node_group,
        gvisor_node_group=gvisor_node_group,
        gvisor_launch_template=gvisor_launch_template,
    )
