"""ECR Repositories for CodeDuel Shared Infrastructure.

Manages container image registries for:
- codeduel: The unified application binary image (Gateway, Match, Reaper, Judge, Migrate).
- sandbox-python: The Python 3.13 untrusted runner image.
- sandbox-cpp: The C++ GCC 14 untrusted runner image.
- sandbox-java: The Java Temurin 21 untrusted runner image.
"""

import json
from typing import NamedTuple
import pulumi_aws as aws


class EcrResources(NamedTuple):
    repositories: dict[str, aws.ecr.Repository]


REPOSITORY_NAMES = [
    "codeduel",
    "sandbox-python",
    "sandbox-cpp",
    "sandbox-java",
]

LIFECYCLE_POLICY = json.dumps(
    {
        "rules": [
            {
                "rulePriority": 1,
                "description": "Expire untagged images older than 14 days",
                "selection": {
                    "tagStatus": "untagged",
                    "countType": "sinceImagePushed",
                    "countUnit": "days",
                    "countNumber": 14,
                },
                "action": {"type": "expire"},
            },
            {
                "rulePriority": 2,
                "description": "Keep last 30 tagged images",
                "selection": {
                    "tagStatus": "any",
                    "countType": "imageCountMoreThan",
                    "countNumber": 30,
                },
                "action": {"type": "expire"},
            },
        ]
    }
)


def create_ecr_repositories() -> EcrResources:
    repos: dict[str, aws.ecr.Repository] = {}

    for name in REPOSITORY_NAMES:
        repo = aws.ecr.Repository(
            f"codeduel-ecr-{name}",
            name=name,
            image_tag_mutability="MUTABLE",
            image_scanning_configuration=aws.ecr.RepositoryImageScanningConfigurationArgs(
                scan_on_push=True,
            ),
            tags={
                "Name": name,
                "Project": "codeduel",
            },
        )

        aws.ecr.LifecyclePolicy(
            f"codeduel-ecr-lifecycle-{name}",
            repository=repo.name,
            policy=LIFECYCLE_POLICY,
        )

        repos[name] = repo

    return EcrResources(repositories=repos)
