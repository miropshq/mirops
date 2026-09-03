# Contributing to mirops

Thanks for your interest in mirops — an open-source Kubernetes operator that builds a logical mirror
of a cluster and evaluates upgrade readiness. Contributions of all kinds are welcome: bug reports,
features, docs, and add-on compatibility data.

By participating you agree to abide by our [Code of Conduct](CODE_OF_CONDUCT.md).

## The ecosystem

mirops is a few repositories that work together:

| Repo | What it is |
| --- | --- |
| **mirops** (this repo) | The operator (Go, controller-runtime) — collector, mirror engine, scoring, decision. |
| **mirops-headlamp-plugin** | The Headlamp plugin (React/TS) that visualizes the report. |
| **mirops-cli** | CLI to run/enforce an analysis in CI. |
| **helm-charts** | The `mirops-operator` Helm chart. |
| **mirops-compat** | The community add-on ↔ Kubernetes compatibility matrix. |

Add-on compatibility fixes usually belong in **mirops-compat**, not here.

## Getting started

Prerequisites: Go (see `go.mod`), `make`, Docker, and a cluster (kind/minikube/AKS/EKS) for e2e.

```bash
make generate    # deepcopy / CRD code
make manifests   # regenerate CRDs from types
make lint        # golangci-lint
make test        # unit tests
make build       # compile the binary
```

> **Note on `internal/compat/matrix.yaml`**: the operator embeds the compatibility matrix at build
> time via `go:embed`. The file is **not** committed — CI fetches it from the `mirops-compat` OCI
> artifact. To build locally, copy a matrix in first:
> `cp ../mirops-compat/dist/matrix.yaml internal/compat/matrix.yaml` (it is git-ignored).

## Making a change

1. **Fork** and branch from `main` (`feat/…`, `fix/…`).
2. Keep the change focused. Add/adjust tests for behavior changes.
3. Run `make generate manifests lint test build` before pushing.
4. Open a PR against `main`.

### Commit & PR conventions

This project uses **[Conventional Commits](https://www.conventionalcommits.org/)** — releases are cut
automatically by semantic-release from commit history, and PR titles are checked. Use:

```
feat: …      # a new feature (minor)
fix: …       # a bug fix (patch)
docs: …      # documentation only
refactor: …  # no behavior change
test: …      # tests only
chore: …     # tooling/CI
```

A breaking change adds a `!` (`feat!: …`) or a `BREAKING CHANGE:` footer (major).

## Reporting bugs & requesting features

Open an issue with: what you expected, what happened, the target vs current Kubernetes version, and
(if relevant) a redacted `report.json`. For security issues, **do not** open a public issue — see
[SECURITY.md](SECURITY.md).

## License

By contributing, you agree that your contributions are licensed under the project's
[Apache License 2.0](LICENSE).
