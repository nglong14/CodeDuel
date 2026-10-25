"""Bootstrap secrets and failure bounds, without a database or cluster."""

import json
import os
import subprocess
import time
import uuid

import pulumi
import pytest

from infra.env.database import (
    BOOTSTRAP_SCRIPT,
    configure_environment_database,
    create_database_bootstrap_job,
)
from infra.kubeconfig import build_kubeconfig


@pulumi.runtime.test
def test_bootstrap_passwords_use_secret_references_and_job_has_a_deadline():
    job = create_database_bootstrap_job(
        env_name="dev",
        db_name="codeduel_dev",
        db_user="codeduel_dev",
        db_password="target-secret",
        rds_address="database.internal",
        rds_port=5432,
        rds_master_username="admin",
        rds_master_password="master-secret",
        namespace="codeduel-dev",
        k8s_provider=None,
    )

    def check(spec):
        assert spec.active_deadline_seconds == 180
        assert spec.backoff_limit == 3
        pod = spec.template.spec
        assert pod.automount_service_account_token is False
        container = pod.containers[0]
        env = {entry.name: entry for entry in container.env}
        for key in ("PGPASSWORD", "TARGET_PASSWORD"):
            assert env[key].value is None
            assert env[key].value_from.secret_key_ref.key == key
        assert "master-secret" not in json.dumps(dict(spec), default=str)
        assert "target-secret" not in json.dumps(dict(spec), default=str)
        assert spec.template.metadata.annotations["codeduel.io/credentials-revision"]

    return job.spec.apply(check)


@pytest.mark.parametrize("name", ["Bad Name", "x;DROP DATABASE postgres", "a" * 64])
def test_bootstrap_rejects_invalid_identifiers(name):
    with pytest.raises(ValueError):
        configure_environment_database("dev", name, "codeduel_dev", "localhost", 5432)


def test_bootstrap_sql_fails_on_errors_and_quotes_identifiers_and_passwords():
    assert "ON_ERROR_STOP=1" in BOOTSTRAP_SCRIPT
    assert "PGCONNECT_TIMEOUT=5" in BOOTSTRAP_SCRIPT
    assert "PGSSLMODE=require" in BOOTSTRAP_SCRIPT
    assert "format('ALTER ROLE %I PASSWORD %L'" in BOOTSTRAP_SCRIPT
    assert "\\getenv target_password TARGET_PASSWORD" in BOOTSTRAP_SCRIPT
    assert "until " not in BOOTSTRAP_SCRIPT


@pulumi.runtime.test
def test_both_projects_share_the_same_exec_auth_kubeconfig():
    def check(rendered):
        config = json.loads(rendered)
        assert config["clusters"][0]["cluster"]["server"] == "https://cluster.example"
        assert config["users"][0]["user"]["exec"]["args"] == [
            "eks", "get-token", "--cluster-name", "codeduel",
        ]

    return build_kubeconfig("codeduel", "https://cluster.example", "ca").apply(check)


@pytest.mark.skipif(
    os.environ.get("CODEDUEL_INFRA_INTEGRATION") != "1",
    reason="set CODEDUEL_INFRA_INTEGRATION=1 for a disposable Docker PostgreSQL test",
)
def test_bootstrap_postgres_is_idempotent_and_fails_closed_over_tls(tmp_path):
    """Run the shipped SQL twice; no host ports or existing databases are used."""
    def run(*args, **kwargs):
        return subprocess.run(args, text=True, capture_output=True, timeout=60, check=True, **kwargs)

    certificate, key = tmp_path / "server.crt", tmp_path / "server.key"
    run("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
        "-subj", "/CN=localhost", "-keyout", str(key), "-out", str(certificate))
    name = "codeduel-infra-test-" + uuid.uuid4().hex
    run("docker", "create", "--name", name, "-e", "POSTGRES_PASSWORD=bootstrap-admin",
        "--entrypoint", "/bin/sh", "postgres:16-alpine", "-c",
        "chown postgres:postgres /tmp/server.crt /tmp/server.key; chmod 600 /tmp/server.key; "
        "exec docker-entrypoint.sh postgres -c ssl=on "
        "-c ssl_cert_file=/tmp/server.crt -c ssl_key_file=/tmp/server.key")
    try:
        run("docker", "cp", str(certificate), f"{name}:/tmp/server.crt")
        run("docker", "cp", str(key), f"{name}:/tmp/server.key")
        run("docker", "start", name)
        for _ in range(60):
            ready = subprocess.run(
                ["docker", "exec", name, "pg_isready", "-h", "127.0.0.1", "-U", "postgres"],
                capture_output=True, timeout=10, check=False,
            )
            if ready.returncode == 0:
                break
            time.sleep(0.5)
        else:
            pytest.fail("disposable PostgreSQL did not become ready")
        bootstrap = [
            "docker", "exec", "-i", "-e", "PGHOST=127.0.0.1", "-e", "PGUSER=postgres",
            "-e", "PGPASSWORD=bootstrap-admin", "-e", "TARGET_DB=codeduel_test",
            "-e", "TARGET_USER=codeduel_test", "-e", "TARGET_PASSWORD=target-password",
            name, "/bin/sh", "-s",
        ]
        for _ in range(2):
            run(*bootstrap, input=BOOTSTRAP_SCRIPT)
        authenticated = run(
            "docker", "exec", "-e", "PGPASSWORD=target-password", "-e", "PGSSLMODE=require",
            name, "psql", "-h", "127.0.0.1", "-U", "codeduel_test", "-d", "codeduel_test",
            "-Atc", "SELECT current_user || '/' || current_database()",
        )
        assert authenticated.stdout.strip() == "codeduel_test/codeduel_test"
        bootstrap[bootstrap.index("PGUSER=postgres")] = "PGUSER=nonexistent"
        failed = subprocess.run(
            bootstrap, input=BOOTSTRAP_SCRIPT, text=True, capture_output=True,
            timeout=30, check=False,
        )
        assert failed.returncode != 0
        assert "Database bootstrap complete" not in failed.stdout
    finally:
        run("docker", "rm", "--force", "--volumes", name)
