"""Kubernetes Namespace and Secrets Provisioning for CodeDuel Environment.

Manages:
- EKS Kubeconfig generation for the Kubernetes provider.
- Namespaces with Pod Security Standards (PSS) 'restricted' mode enforced.
- Generated strong JWT secret (≥ 32 bytes) per environment.
- Kubernetes Secret 'codeduel-secrets' containing JWT_SECRET, POSTGRES_DSN, and REDIS_URL.
"""

import json
from typing import NamedTuple
import pulumi
import pulumi_kubernetes as k8s
import pulumi_random as random


class K8sResources(NamedTuple):
    provider: k8s.Provider
    namespace: k8s.core.v1.Namespace
    jwt_secret: random.RandomPassword
    secret: k8s.core.v1.Secret


def create_kubeconfig(
    cluster_name: pulumi.Input[str],
    endpoint: pulumi.Input[str],
    ca_data: pulumi.Input[str],
) -> pulumi.Output[str]:
    return pulumi.Output.all(cluster_name, endpoint, ca_data).apply(
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
                        "context": {
                            "cluster": "kubernetes",
                            "user": "aws",
                        },
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
                                "args": [
                                    "eks",
                                    "get-token",
                                    "--cluster-name",
                                    args[0],
                                ],
                            }
                        },
                    }
                ],
            }
        )
    )


def configure_k8s_secrets(
    env_name: str,
    cluster_name: pulumi.Input[str],
    cluster_endpoint: pulumi.Input[str],
    cluster_ca_data: pulumi.Input[str],
    postgres_dsn: pulumi.Input[str],
    redis_url: pulumi.Input[str],
) -> K8sResources:
    # 1. Kubernetes Provider bound to the shared EKS cluster
    kubeconfig = create_kubeconfig(cluster_name, cluster_endpoint, cluster_ca_data)
    provider = k8s.Provider(
        f"codeduel-{env_name}-k8s-provider",
        kubeconfig=kubeconfig,
    )

    namespace_name = f"codeduel-{env_name}"

    # 2. Namespace with Pod Security Standards 'restricted' mode
    namespace = k8s.core.v1.Namespace(
        f"codeduel-{env_name}-ns",
        metadata=k8s.meta.v1.ObjectMetaArgs(
            name=namespace_name,
            labels={
                "app.kubernetes.io/part-of": "codeduel",
                "app.kubernetes.io/environment": env_name,
                "pod-security.kubernetes.io/enforce": "restricted",
                "pod-security.kubernetes.io/enforce-version": "latest",
                "pod-security.kubernetes.io/audit": "restricted",
                "pod-security.kubernetes.io/audit-version": "latest",
                "pod-security.kubernetes.io/warn": "restricted",
                "pod-security.kubernetes.io/warn-version": "latest",
            },
        ),
        opts=pulumi.ResourceOptions(provider=provider),
    )

    # 3. Generate cryptographically strong JWT secret (48 characters)
    jwt_secret = random.RandomPassword(
        f"codeduel-{env_name}-jwt-secret",
        length=48,
        special=False,
    )

    # 4. Kubernetes Secret 'codeduel-secrets'
    secret = k8s.core.v1.Secret(
        f"codeduel-{env_name}-secrets",
        metadata=k8s.meta.v1.ObjectMetaArgs(
            name="codeduel-secrets",
            namespace=namespace.metadata.name,
            labels={
                "app.kubernetes.io/part-of": "codeduel",
                "app.kubernetes.io/environment": env_name,
            },
        ),
        type="Opaque",
        string_data={
            "JWT_SECRET": jwt_secret.result,
            "POSTGRES_DSN": postgres_dsn,
            "REDIS_URL": redis_url,
        },
        opts=pulumi.ResourceOptions(
            provider=provider,
            depends_on=[namespace],
        ),
    )

    return K8sResources(
        provider=provider,
        namespace=namespace,
        jwt_secret=jwt_secret,
        secret=secret,
    )
