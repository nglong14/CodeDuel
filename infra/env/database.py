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
