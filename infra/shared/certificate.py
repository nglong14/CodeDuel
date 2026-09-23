"""ACM Certificate and Route 53 DNS Validation for CodeDuel Shared Infrastructure.

Optional TLS certificate provisioning for shared ALB HTTPS / WSS termination.
If 'domain_name' is not configured, certificate provisioning is skipped and can
be supplied via stack configuration or external ACM ARN.
"""

from typing import NamedTuple

import pulumi
import pulumi_aws as aws


class CertificateResources(NamedTuple):
    certificate: aws.acm.Certificate | None
    validation: aws.acm.CertificateValidation | None
    certificate_arn: pulumi.Output[str]


def create_certificate(
    domain_name: str | None = None,
    zone_id: str | None = None,
) -> CertificateResources:
    if not domain_name:
        return CertificateResources(
            certificate=None,
            validation=None,
            certificate_arn=pulumi.Output.from_input(""),
        )

    # 1. ACM Certificate for domain and wildcard subdomain
    cert = aws.acm.Certificate(
        "codeduel-acm-cert",
        domain_name=domain_name,
        subject_alternative_names=[f"*.{domain_name}"],
        validation_method="DNS",
        tags={
            "Name": f"codeduel-{domain_name}-cert",
            "Project": "codeduel",
        },
    )

    # 2. DNS Validation via Route53 if zone_id is provided
    validation = None
    if zone_id:
        validation_records = []
        for i in range(2):
            dvo = cert.domain_validation_options[i]
            record = aws.route53.Record(
                f"codeduel-cert-validation-{i}",
                name=dvo.resource_record_name,
                zone_id=zone_id,
                type=dvo.resource_record_type,
                records=[dvo.resource_record_value],
                ttl=60,
            )
            validation_records.append(record.fqdn)

        validation = aws.acm.CertificateValidation(
            "codeduel-cert-validation",
            certificate_arn=cert.arn,
            validation_record_fqdns=validation_records,
        )

    return CertificateResources(
        certificate=cert,
        validation=validation,
        certificate_arn=cert.arn,
    )
