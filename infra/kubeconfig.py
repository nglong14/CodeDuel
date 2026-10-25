"""One AWS exec-auth kubeconfig format for both Pulumi projects."""

import json

import pulumi


def build_kubeconfig(
    cluster_name: pulumi.Input[str],
    endpoint: pulumi.Input[str],
    ca_data: pulumi.Input[str],
) -> pulumi.Output[str]:
    return pulumi.Output.all(cluster_name, endpoint, ca_data).apply(
        lambda args: json.dumps({
            "apiVersion": "v1",
            "kind": "Config",
            "clusters": [{
                "name": "kubernetes",
                "cluster": {"server": args[1], "certificate-authority-data": args[2]},
            }],
            "contexts": [{
                "name": "aws",
                "context": {"cluster": "kubernetes", "user": "aws"},
            }],
            "current-context": "aws",
            "users": [{
                "name": "aws",
                "user": {"exec": {
                    "apiVersion": "client.authentication.k8s.io/v1beta1",
                    "command": "aws",
                    "args": ["eks", "get-token", "--cluster-name", args[0]],
                }},
            }],
        })
    )
