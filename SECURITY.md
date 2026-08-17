# Security Policy

mirops runs inside a Kubernetes cluster and reads cluster state to evaluate upgrade readiness. We take
its security seriously and appreciate responsible disclosure.

## Supported versions

Security fixes land on the latest released minor version. We recommend always running the most recent
release.

| Version | Supported |
| --- | --- |
| latest release | ✅ |
| older releases | ❌ (please upgrade) |

## Reporting a vulnerability

**Please do not open a public issue for security vulnerabilities.**

Report privately through either channel:

- **GitHub private vulnerability reporting** — *Security → Report a vulnerability* on the repository.
  This opens a private advisory visible only to the maintainers.
- **Email** — [security@mirops.com](mailto:security@mirops.com).

Please include:

- A description of the issue and its impact.
- Steps to reproduce (a minimal manifest or `report.json` helps).
- Affected version(s) and environment (managed provider, Kubernetes version).

### What to expect

- **Acknowledgement** within a few business days.
- An assessment and, if confirmed, a fix timeline shared with you.
- Credit in the advisory once a fix is released, unless you prefer to remain anonymous.

## Scope & hardening notes

mirops requires read access to cluster resources (and, for remediation, scoped write access). When
deploying:

- Grant the **least-privilege RBAC** the chart ships; review before widening it.
- Treat generated **reports** as sensitive — they enumerate workloads, namespaces, and versions. Secure
  any remote report destination (S3 / Azure Blob / PVC) accordingly.
- Prefer cloud-native identity (IRSA on EKS, Workload Identity on AKS) over long-lived credentials.

Thank you for helping keep mirops and its users safe.
