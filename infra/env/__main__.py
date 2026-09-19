"""CodeDuel Environment Stack Entry Point (dev and prod).

Orchestrates provisioning of:
- Scoped PostgreSQL role, credentials, and connection DSN.
- Logical Redis URL for the environment.
- Kubernetes Namespace (enforcing Pod Security Standards 'restricted').
- Kubernetes Secret 'codeduel-secrets' (JWT_SECRET, POSTGRES_DSN, REDIS_URL).
- CloudWatch Log Groups for Gateway, Match, Judge, and Reaper.
"""

import pulumi
from stack_refs import load_shared_stack_outputs
from database import configure_environment_database
from k8s_secrets import configure_k8s_secrets
from log_groups import create_environment_log_groups


# 1. Read Stack Configuration
config = pulumi.Config()
env_name = config.require("env")
redis_db_index = config.require_int("redis_db_index")
db_name = config.require("db_name")
db_user = config.require("db_user")
app_env = config.get("app_env") or ("production" if env_name == "prod" else "development")
judge_concurrency = config.get_int("judge_concurrency") or (1 if env_name == "dev" else 4)
log_retention_days = config.get_int("log_retention_days") or (30 if env_name == "prod" else 7)
shared_stack_ref_name = config.get("shared_stack_ref") or "codeduel-shared/shared"

# 2. Read Outputs from Shared Stack (VPC, EKS, RDS, ElastiCache, ECR)
shared_outputs = load_shared_stack_outputs(shared_stack_ref_name)

# 3. Configure Database Credentials & Scoped DSN
db_res = configure_environment_database(
    env_name=env_name,
    db_name=db_name,
    db_user=db_user,
    rds_address=shared_outputs.rds_address,
    rds_port=shared_outputs.rds_port,
)

# 4. Construct Redis URL for the Environment Logical DB Index
redis_url = pulumi.Output.all(
    shared_outputs.elasticache_address,
    shared_outputs.elasticache_port,
).apply(
    lambda args: f"redis://{args[0]}:{args[1]}/{redis_db_index}"
)

# 5. Provision Kubernetes Namespace and 'codeduel-secrets' Secret
k8s_res = configure_k8s_secrets(
    env_name=env_name,
    cluster_name=shared_outputs.eks_cluster_name,
    cluster_endpoint=shared_outputs.eks_cluster_endpoint,
    cluster_ca_data=shared_outputs.eks_cluster_ca_data,
    postgres_dsn=db_res.postgres_dsn,
    redis_url=redis_url,
)

# 6. CloudWatch Log Groups
log_res = create_environment_log_groups(
    env_name=env_name,
    retention_days=log_retention_days,
)

# 7. Export Stack Outputs
pulumi.export("env", env_name)
pulumi.export("app_env", app_env)
pulumi.export("judge_concurrency", judge_concurrency)
pulumi.export("namespace", k8s_res.namespace.metadata.name)
pulumi.export("db_name", db_name)
pulumi.export("db_user", db_user)
pulumi.export("postgres_dsn", db_res.postgres_dsn)
pulumi.export("redis_url", redis_url)
pulumi.export("jwt_secret", pulumi.Output.secret(k8s_res.jwt_secret.result))
pulumi.export(
    "log_group_names",
    {role: lg.name for role, lg in log_res.log_groups.items()},
)
