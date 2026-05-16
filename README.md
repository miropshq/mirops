# Mirops Operator

Mirops is a Kubernetes operator that analyzes whether a cluster is ready for a Kubernetes version upgrade. It watches `UpgradeAnalysis` custom resources, collects cluster health and workload signals, calculates an upgrade-readiness score, and writes a JSON report to a configured destination.

The report can be consumed by `mirops-cli` in CI/CD pipelines to warn or block an upgrade when the cluster is not healthy enough.

## What It Checks

- Pod readiness and restart counts
- Node readiness and cluster capacity pressure
- StatefulSet, DaemonSet, Job, and PodDisruptionBudget risks
- Deprecated API usage for the target Kubernetes version
- Overall readiness score from 0 to 100
- Final decision: `SAFE`, `WARNING`, or `BLOCK`

## Repository Layout

```text
api/                 UpgradeAnalysis API types and generated code
cmd/                 Operator entrypoint
config/              CRDs, RBAC, manager, samples, and kustomize overlays
internal/analysis/   Scoring and report generation
internal/collector/  Kubernetes cluster data collection
internal/controller/ UpgradeAnalysis reconciliation logic
internal/exporter/   File, S3, and Azure Blob report exporters
test/                Envtest and e2e tests
```

## Prerequisites

- Go 1.24.6 or newer
- Docker or another compatible container tool
- `kubectl`
- Access to a Kubernetes cluster
- Optional: `kind` for e2e tests

Project tools such as `controller-gen`, `kustomize`, `setup-envtest`, and `golangci-lint` are installed into `bin/` by the Makefile when needed.

## Local Development

Format, generate, vet, and run tests:

```sh
make test
```

Run the controller locally against the current kubeconfig:

```sh
make run
```

Build the manager binary:

```sh
make build
```

Run e2e tests with Kind:

```sh
make test-e2e
```

## Deploy With Kustomize

Build and push the controller image:

```sh
make docker-build docker-push IMG=<registry>/mirops:<tag>
```

Install the CRDs:

```sh
make install
```

Deploy the controller:

```sh
make deploy IMG=<registry>/mirops:<tag>
```

Create an `UpgradeAnalysis`:

```yaml
apiVersion: mirops.mirops.io/v1
kind: UpgradeAnalysis
metadata:
  name: upgrade-check
  namespace: mirops
spec:
  targetVersion: "1.29"
  scope:
    mode: application
    excludeNamespaces:
      - monitoring
  source:
    type: file
    path: /tmp/mirops-report.json
```

Apply it:

```sh
kubectl apply -f upgrade-analysis.yaml
kubectl get upgradeanalysis -n mirops
kubectl describe upgradeanalysis upgrade-check -n mirops
```

## Report Destinations

By default, the operator writes a local JSON report through the file exporter. The `spec.source.type` field supports:

| Type | Purpose | Key fields |
| ---- | ------- | ---------- |
| `file` | Write the report to the controller filesystem | `path` |
| `s3` | Upload the report to Amazon S3 | `bucket`, `region`, `key`, `credentialsSecret` |
| `blob` | Upload the report to Azure Blob Storage | `accountName`, `containerName`, `blobName`, `credentialsSecret` |

When `credentialsSecret` is set, the operator reads credentials from a Kubernetes Secret in the same namespace as the `UpgradeAnalysis`.

S3 secret keys:

```text
AWS_ACCESS_KEY_ID
AWS_SECRET_ACCESS_KEY
```

Azure secret keys:

```text
AZURE_CLIENT_ID
AZURE_CLIENT_SECRET
AZURE_TENANT_ID
```

## Uninstall

Delete sample resources:

```sh
kubectl delete -k config/samples/
```

Remove the controller:

```sh
make undeploy
```

Remove the CRDs:

```sh
make uninstall
```

## Build A Single Installer

Generate a bundled manifest in `dist/install.yaml`:

```sh
make build-installer IMG=<registry>/mirops:<tag>
```

Users can install the generated bundle with:

```sh
kubectl apply -f dist/install.yaml
```

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0. See the license text in this repository for details.
