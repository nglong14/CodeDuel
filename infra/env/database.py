"""Database Credentials and DSN Generation for CodeDuel Environment.

Manages:
- Per-environment scoped PostgreSQL role and database naming.
- Cryptographically strong password generation.
- Production-hardened DSN with sslmode=require.
- Bootstrap SQL for creating database and scoped role.
"""

import hashlib
import re
from typing import NamedTuple

import pulumi
import pulumi_kubernetes as k8s
import pulumi_random as random


class DatabaseConfig(NamedTuple):
    db_name: str
    db_user: str
    db_password: random.RandomPassword
    postgres_dsn: pulumi.Output[str]


def configure_environment_database(
    env_name: str,
    db_name: str,
    db_user: str,
    rds_address: pulumi.Input[str],
    rds_port: pulumi.Input[int],
) -> DatabaseConfig:
    for name in (db_name, db_user):
        if not re.fullmatch(r"[a-z_][a-z0-9_]{0,62}", name):
            raise ValueError("database and role names must be lowercase PostgreSQL identifiers")
    # 1. Generate strong per-environment user password
    db_password = random.RandomPassword(
        f"codeduel-{env_name}-db-pw",
        length=32,
        special=False,
    )

    # 2. Build scoped POSTGRES_DSN with sslmode=require
    # rds_port arrives as a float from the shared stack's Output (RDS provider
    # quirk); cast to int here so the DSN never renders "5432.0".
    postgres_dsn = pulumi.Output.all(
        rds_address,
        rds_port,
        db_password.result,
    ).apply(
        lambda args: f"postgres://{db_user}:{args[2]}@{args[0]}:{int(args[1])}/{db_name}?sslmode=require"
    )

    return DatabaseConfig(
        db_name=db_name,
        db_user=db_user,
        db_password=db_password,
        postgres_dsn=pulumi.Output.secret(postgres_dsn),
    )


BOOTSTRAP_SCRIPT = r"""set -eu
export PGCONNECT_TIMEOUT=5 PGSSLMODE=require
psql --no-psqlrc --set=ON_ERROR_STOP=1 --dbname=postgres <<'SQL'
\getenv target_db TARGET_DB
\getenv target_user TARGET_USER
\getenv target_password TARGET_PASSWORD
SELECT format('CREATE ROLE %I LOGIN', :'target_user')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'target_user') \gexec
SELECT format('ALTER ROLE %I PASSWORD %L', :'target_user', :'target_password') \gexec
SELECT format('CREATE DATABASE %I OWNER %I', :'target_db', :'target_user')
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = :'target_db') \gexec
SELECT format('GRANT ALL PRIVILEGES ON DATABASE %I TO %I', :'target_db', :'target_user') \gexec
\connect :target_db
SELECT format('GRANT ALL ON SCHEMA public TO %I', :'target_user') \gexec
SQL
echo "Database bootstrap complete."
"""


def create_database_bootstrap_job(
    env_name: str,
    db_name: str,
    db_user: str,
    db_password: pulumi.Input[str],
    rds_address: pulumi.Input[str],
    rds_port: pulumi.Input[int],
    rds_master_username: pulumi.Input[str],
    rds_master_password: pulumi.Input[str],
    namespace: pulumi.Input[str],
    k8s_provider: pulumi.Input,
):
    """Create a Kubernetes Job running inside the EKS cluster on app nodes that connects

    to RDS and creates the scoped database, user, and grants.
    """
    credentials = k8s.core.v1.Secret(
        f"codeduel-{env_name}-db-bootstrap-credentials",
        metadata=k8s.meta.v1.ObjectMetaArgs(
            name=f"codeduel-{env_name}-db-bootstrap-credentials",
            namespace=namespace,
        ),
        string_data={"PGPASSWORD": rds_master_password, "TARGET_PASSWORD": db_password},
        opts=pulumi.ResourceOptions(provider=k8s_provider),
    )
    # Secret references alone don't rerun a completed Job when a password changes.
    revision = pulumi.Output.all(rds_master_password, db_password).apply(
        lambda values: hashlib.sha256("\0".join(values).encode()).hexdigest()
    )

    return k8s.batch.v1.Job(
        f"codeduel-{env_name}-db-bootstrap",
        metadata=k8s.meta.v1.ObjectMetaArgs(
            name=f"codeduel-{env_name}-db-bootstrap",
            namespace=namespace,
            labels={
                "app.kubernetes.io/part-of": "codeduel",
                "app.kubernetes.io/environment": env_name,
                "app.kubernetes.io/name": "db-bootstrap",
            },
        ),
        spec=k8s.batch.v1.JobSpecArgs(
            backoff_limit=3,
            active_deadline_seconds=180,
            ttl_seconds_after_finished=300,
            template=k8s.core.v1.PodTemplateSpecArgs(
                metadata=k8s.meta.v1.ObjectMetaArgs(
                    annotations={"codeduel.io/credentials-revision": revision},
                    labels={
                        "app.kubernetes.io/part-of": "codeduel",
                        "app.kubernetes.io/environment": env_name,
                        "app.kubernetes.io/name": "db-bootstrap",
                    },
                ),
                spec=k8s.core.v1.PodSpecArgs(
                    restart_policy="Never",
                    automount_service_account_token=False,
                    node_selector={"role": "app"},
                    containers=[
                        k8s.core.v1.ContainerArgs(
                            name="db-bootstrap",
                            image="postgres:16-alpine",
                            command=["/bin/sh", "-c"],
                            args=[BOOTSTRAP_SCRIPT],
                            env=[
                                k8s.core.v1.EnvVarArgs(name="PGHOST", value=rds_address),
                                k8s.core.v1.EnvVarArgs(
                                    name="PGPORT",
                                    value=pulumi.Output.from_input(rds_port).apply(
                                        lambda p: str(int(p))
                                    ),
                                ),
                                k8s.core.v1.EnvVarArgs(
                                    name="PGUSER", value=rds_master_username
                                ),
                                k8s.core.v1.EnvVarArgs(
                                    name="PGPASSWORD",
                                    value_from=k8s.core.v1.EnvVarSourceArgs(
                                        secret_key_ref=k8s.core.v1.SecretKeySelectorArgs(
                                            name=credentials.metadata.name, key="PGPASSWORD"
                                        )
                                    ),
                                ),
                                k8s.core.v1.EnvVarArgs(name="TARGET_DB", value=db_name),
                                k8s.core.v1.EnvVarArgs(name="TARGET_USER", value=db_user),
                                k8s.core.v1.EnvVarArgs(
                                    name="TARGET_PASSWORD",
                                    value_from=k8s.core.v1.EnvVarSourceArgs(
                                        secret_key_ref=k8s.core.v1.SecretKeySelectorArgs(
                                            name=credentials.metadata.name, key="TARGET_PASSWORD"
                                        )
                                    ),
                                ),
                            ],
                            resources=k8s.core.v1.ResourceRequirementsArgs(
                                requests={"cpu": "50m", "memory": "64Mi"},
                                limits={"cpu": "200m", "memory": "128Mi"},
                            ),
                            security_context=k8s.core.v1.SecurityContextArgs(
                                allow_privilege_escalation=False,
                                run_as_non_root=True,
                                run_as_user=70,  # postgres user in alpine
                                run_as_group=70,
                                capabilities=k8s.core.v1.CapabilitiesArgs(drop=["ALL"]),
                                seccomp_profile=k8s.core.v1.SeccompProfileArgs(
                                    type="RuntimeDefault"
                                ),
                            ),
                        )
                    ],
                ),
            ),
        ),
        opts=pulumi.ResourceOptions(provider=k8s_provider, delete_before_replace=True),
    )
