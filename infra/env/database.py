"""Database Credentials and DSN Generation for CodeDuel Environment.

Manages:
- Per-environment scoped PostgreSQL role and database naming.
- Cryptographically strong password generation.
- Production-hardened DSN with sslmode=require.
- Bootstrap SQL for creating database and scoped role.
"""

from typing import NamedTuple

import pulumi
import pulumi_random as random


class DatabaseConfig(NamedTuple):
    db_name: str
    db_user: str
    db_password: random.RandomPassword
    postgres_dsn: pulumi.Output[str]
    bootstrap_sql: pulumi.Output[str]


def configure_environment_database(
    env_name: str,
    db_name: str,
    db_user: str,
    rds_address: pulumi.Input[str],
    rds_port: pulumi.Input[int],
) -> DatabaseConfig:
    # 1. Generate strong per-environment user password
    db_password = random.RandomPassword(
        f"codeduel-{env_name}-db-pw",
        length=32,
        special=False,
    )

    # 2. Build scoped POSTGRES_DSN with sslmode=require
    postgres_dsn = pulumi.Output.all(
        rds_address,
        rds_port,
        db_password.result,
    ).apply(
        lambda args: f"postgres://{db_user}:{args[2]}@{args[0]}:{args[1]}/{db_name}?sslmode=require"
    )

    # 3. Bootstrap SQL statements for initial DB & scoped user creation
    bootstrap_sql = db_password.result.apply(
        lambda pw: (
            f"CREATE DATABASE {db_name};\n"
            f"CREATE USER {db_user} WITH ENCRYPTED PASSWORD '{pw}';\n"
            f"GRANT ALL PRIVILEGES ON DATABASE {db_name} TO {db_user};\n"
            f"\\c {db_name}\n"
            f"GRANT ALL ON SCHEMA public TO {db_user};\n"
        )
    )

    return DatabaseConfig(
        db_name=db_name,
        db_user=db_user,
        db_password=db_password,
        postgres_dsn=pulumi.Output.secret(postgres_dsn),
        bootstrap_sql=pulumi.Output.secret(bootstrap_sql),
    )


BOOTSTRAP_SCRIPT = """set -e
echo "Waiting for PostgreSQL at $PGHOST:$PGPORT..."
until pg_isready -h "$PGHOST" -p "$PGPORT" -U "$PGUSER"; do
  sleep 2
done

echo "Creating role $TARGET_USER if not exists..."
psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d postgres -tc "SELECT 1 FROM pg_roles WHERE rolname = '$TARGET_USER'" | grep -q 1 || \
psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d postgres -c "CREATE USER \"$TARGET_USER\" WITH ENCRYPTED PASSWORD '$TARGET_PASSWORD';"

echo "Updating role $TARGET_USER password..."
psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d postgres -c "ALTER USER \"$TARGET_USER\" WITH ENCRYPTED PASSWORD '$TARGET_PASSWORD';"

echo "Creating database $TARGET_DB if not exists..."
psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d postgres -tc "SELECT 1 FROM pg_database WHERE datname = '$TARGET_DB'" | grep -q 1 || \
psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d postgres -c "CREATE DATABASE \"$TARGET_DB\" OWNER \"$TARGET_USER\";"

echo "Granting privileges to $TARGET_USER..."
psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d postgres -c "GRANT ALL PRIVILEGES ON DATABASE \"$TARGET_DB\" TO \"$TARGET_USER\";"
psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$TARGET_DB" -c "GRANT ALL ON SCHEMA public TO \"$TARGET_USER\";"

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
    import pulumi_kubernetes as k8s

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
            ttl_seconds_after_finished=300,
            template=k8s.core.v1.PodTemplateSpecArgs(
                metadata=k8s.meta.v1.ObjectMetaArgs(
                    labels={
                        "app.kubernetes.io/part-of": "codeduel",
                        "app.kubernetes.io/environment": env_name,
                        "app.kubernetes.io/name": "db-bootstrap",
                    },
                ),
                spec=k8s.core.v1.PodSpecArgs(
                    restart_policy="OnFailure",
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
                                    value=pulumi.Output.from_input(rds_port).apply(str),
                                ),
                                k8s.core.v1.EnvVarArgs(
                                    name="PGUSER", value=rds_master_username
                                ),
                                k8s.core.v1.EnvVarArgs(
                                    name="PGPASSWORD", value=rds_master_password
                                ),
                                k8s.core.v1.EnvVarArgs(name="TARGET_DB", value=db_name),
                                k8s.core.v1.EnvVarArgs(name="TARGET_USER", value=db_user),
                                k8s.core.v1.EnvVarArgs(
                                    name="TARGET_PASSWORD", value=db_password
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
        opts=pulumi.ResourceOptions(provider=k8s_provider),
    )
