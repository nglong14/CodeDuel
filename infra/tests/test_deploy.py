"""Exercise the actual release scripts without AWS, Docker, or a cluster."""

import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

import pytest
import yaml

ROOT = Path(__file__).resolve().parents[2]
SHA = "a" * 40
REGISTRY = "123456789012.dkr.ecr.us-east-1.amazonaws.com"
REPOSITORIES = {"codeduel", "sandbox-python", "sandbox-cpp", "sandbox-java"}

# A single recording stub ensures tests can never fall through to a real cloud
# command. Missing images are persisted so a Docker push can simulate recovery.
STUB = r'''
import json, os, pathlib, sys
command = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
with open(os.environ["TEST_LOG"], "a") as log:
    log.write(json.dumps([command, *args]) + "\n")
missing_file = pathlib.Path(os.environ["TEST_MISSING"])
missing = json.loads(missing_file.read_text())
if command == "aws":
    if args[:2] == ["ecr", "describe-repositories"]:
        repo = args[args.index("--repository-names") + 1]
        print(os.environ.get("MUTABILITY", "IMMUTABLE") + "\t" + os.environ["ECR_REGISTRY"] + "/" + repo)
    elif args[:2] == ["ecr", "describe-images"]:
        if os.environ.get("AWS_IMAGE_ERROR"):
            print("AccessDeniedException", file=sys.stderr)
            sys.exit(1)
        repo = args[args.index("--repository-name") + 1]
        if repo in missing:
            print("ImageNotFoundException", file=sys.stderr)
            sys.exit(1)
        print("sha256:" + "b" * 64)
    elif args[:2] == ["ecr", "get-login-password"]:
        print("fake-password")
    elif args[:2] != ["eks", "update-kubeconfig"]:
        sys.exit("Unexpected AWS call: " + str(args))
elif command == "docker":
    if args[0] == "login":
        sys.stdin.read()
    elif args[:2] == ["buildx", "build"]:
        repo = args[args.index("-t") + 1].split("/")[-1].split(":")[0]
        missing.remove(repo)
        missing_file.write_text(json.dumps(missing))
    else:
        sys.exit("Unexpected Docker call")
elif command == "kubectl":
    if args[0] == "kustomize":
        print("apiVersion: v1\nkind: List\nitems: []")
    if args[0] == "wait" and os.environ.get("MIGRATION_FAIL"):
        sys.exit(1)
elif command == "git":
    if "rev-parse" in args:
        print(os.environ["TEST_SHA"])
    elif "status" in args:
        print(" M README.md" if os.environ.get("DIRTY") else "", end="")
    else:
        sys.exit("Unexpected Git call")
else:
    sys.exit("Forbidden command: " + command)
'''


@pytest.fixture
def deployment(tmp_path):
    tools = tmp_path / "tools"
    tools.mkdir()
    stub = tools / "stub"
    stub.write_text(f"#!{sys.executable}\n" + STUB)
    stub.chmod(0o755)
    for command in ("aws", "kubectl", "docker", "git", "pulumi"):
        (tools / command).symlink_to(stub)
    log = tmp_path / "commands.jsonl"
    missing = tmp_path / "missing.json"

    def run(environment="dev", absent=(), tag=SHA, **overrides):
        missing.write_text(json.dumps(list(absent)))
        log.write_text("")
        env = {
            **os.environ,
            "PATH": str(tools) + os.pathsep + os.environ["PATH"],
            "AWS_REGION": "us-east-1",
            "ECR_REGISTRY": REGISTRY,
            "EKS_CLUSTER_NAME": "codeduel",
            "TEST_SHA": SHA,
            "TEST_LOG": str(log),
            "TEST_MISSING": str(missing),
            "TMPDIR": str(tmp_path),
            **overrides,
        }
        result = subprocess.run(
            ["bash", str(ROOT / "infra/scripts/deploy_env.sh"), environment, tag],
            env=env, capture_output=True, text=True, check=False,
        )
        commands = [json.loads(line) for line in log.read_text().splitlines()]
        return result, commands

    return run


@pytest.mark.parametrize("missing", sorted(REPOSITORIES))
def test_production_rejects_any_incomplete_release_without_build_or_rollout(deployment, missing):
    result, commands = deployment("prod", absent=[missing])
    assert result.returncode != 0
    assert "Incomplete release" in result.stderr
    assert not any(command[0] in ("docker", "kubectl", "pulumi") for command in commands)


def test_development_retries_only_missing_images_and_versions_them_together(deployment):
    result, commands = deployment(absent=["sandbox-cpp", "codeduel"])
    assert result.returncode == 0, result.stderr
    builds = [command for command in commands if command[:3] == ["docker", "buildx", "build"]]
    assert len(builds) == 2
    assert {command[command.index("-t") + 1] for command in builds} == {
        f"{REGISTRY}/sandbox-cpp:{SHA}", f"{REGISTRY}/codeduel:{SHA}",
    }
    assert builds[0][-1].endswith("deploy/sandbox/cpp")
    assert not any(command[0] == "pulumi" for command in commands)


def test_complete_production_release_migrates_before_workload_rollout(deployment):
    result, commands = deployment("prod")
    assert result.returncode == 0, result.stderr
    assert not any(command[0] in ("docker", "pulumi") for command in commands)
    applies = [command for command in commands if command[:2] == ["kubectl", "apply"]]
    assert [command[-1] for command in applies] == [
        "app.kubernetes.io/name=configuration", "app.kubernetes.io/name=migrate",
        "app.kubernetes.io/name!=migrate",
    ]
    wait = next(command for command in commands if command[:2] == ["kubectl", "wait"])
    assert f"job/migrate-{SHA}" in wait
    assert commands.index(applies[1]) < commands.index(wait) < commands.index(applies[2])
    assert not any(command[:2] == ["kubectl", "delete"] for command in commands)


def test_migration_failure_prevents_workload_apply_and_rollout(deployment):
    result, commands = deployment(MIGRATION_FAIL="1")
    assert result.returncode != 0
    assert not any("app.kubernetes.io/name!=migrate" in command for command in commands)
    assert not any(command[:2] == ["kubectl", "rollout"] for command in commands)


@pytest.mark.parametrize("overrides", [{"MUTABILITY": "MUTABLE"}, {"AWS_IMAGE_ERROR": "1"}])
def test_registry_errors_and_mutable_repositories_fail_closed(deployment, overrides):
    result, commands = deployment(**overrides)
    assert result.returncode != 0
    assert not any(command[0] in ("docker", "kubectl") for command in commands)


def test_dirty_checkout_cannot_publish_a_release(deployment):
    result, commands = deployment(absent=["codeduel"], DIRTY="1")
    assert result.returncode != 0
    assert "clean checkout" in result.stderr
    assert not any(command[0] == "docker" for command in commands)


def test_non_sha_release_is_rejected_before_cloud_access(deployment):
    result, commands = deployment(tag="latest")
    assert result.returncode != 0
    assert commands == []


@pytest.mark.parametrize("environment", ["dev", "prod"])
def test_rendered_release_has_exact_migration_configuration_and_single_ownership(tmp_path, environment):
    kubectl = shutil.which("kubectl")
    if not kubectl:
        pytest.skip("install kubectl to validate real Kustomize rendering")
    result = subprocess.run(
        ["bash", str(ROOT / "infra/scripts/render_overlay.sh"), environment, REGISTRY, SHA, str(tmp_path / "render")],
        text=True, capture_output=True, check=True,
    )
    rendered = subprocess.check_output([kubectl, "kustomize", result.stdout.strip()], text=True)
    resources = list(yaml.safe_load_all(rendered))
    assert not {"Namespace", "RuntimeClass", "Secret"} & {item["kind"] for item in resources}
    config = next(item for item in resources if item["kind"] == "ConfigMap")
    assert config["metadata"]["labels"]["app.kubernetes.io/name"] == "configuration"
    job = next(item for item in resources if item["kind"] == "Job")
    assert job["metadata"]["name"] == f"migrate-{SHA}"
    assert job["metadata"]["namespace"] == f"codeduel-{environment}"
    container = job["spec"]["template"]["spec"]["containers"][0]
    assert container["image"] == f"{REGISTRY}/codeduel:{SHA}"
    assert container["envFrom"][0]["configMapRef"]["name"] == config["metadata"]["name"]
    for language in ("PYTHON", "CPP", "JAVA"):
        assert config["data"][f"JUDGE_{language}_IMAGE"] == f"{REGISTRY}/sandbox-{language.lower()}:{SHA}"
    for item in resources:
        if item["kind"] == "Deployment":
            assert item["spec"]["template"]["spec"]["containers"][0]["image"] == container["image"]
    assert config["data"]["JUDGE_CONCURRENCY"] == ("1" if environment == "dev" else "2")
    policy = next(item for item in resources if item["kind"] == "NetworkPolicy")
    assert set(policy["spec"]["policyTypes"]) == {"Ingress", "Egress"}
    assert not policy["spec"].get("ingress") and not policy["spec"].get("egress")
    assert policy["spec"]["podSelector"]["matchLabels"] == {"com.codeduel.sandbox": "true"}


def test_renderer_does_not_overwrite_an_existing_destination(tmp_path):
    result = subprocess.run(
        ["bash", str(ROOT / "infra/scripts/render_overlay.sh"), "dev", REGISTRY, SHA, str(tmp_path)],
        capture_output=True, text=True, check=False,
    )
    assert result.returncode != 0
    assert "must not exist" in result.stderr


def test_workflows_separate_application_and_platform_updates():
    deploy = yaml.safe_load((ROOT / ".github/workflows/deploy.yml").read_text())
    for job in deploy["jobs"].values():
        assert "PULUMI_CONFIG_PASSPHRASE" not in job.get("env", {})
        assert not any("pulumi" in step.get("run", "").lower() for step in job["steps"])
    platform = yaml.safe_load((ROOT / ".github/workflows/infrastructure.yml").read_text())
    assert platform["concurrency"]["group"] == "codeduel-platform"
    assert platform["jobs"]["apply"]["environment"] == "infrastructure"


def test_all_infrastructure_scripts_have_valid_shell_syntax():
    for script in (ROOT / "infra/scripts").glob("*.sh"):
        subprocess.run(["bash", "-n", str(script)], check=True)
