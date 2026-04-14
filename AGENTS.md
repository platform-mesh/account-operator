## Repository Description
- `account-operator` manages `Account` and `AccountInfo` resources for Platform Mesh.
- `Account` is the primary tenant/account resource. `AccountInfo` stores derived and related account metadata used across workspaces.
- This is a Go operator repo built around [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime), [multicluster-runtime](https://github.com/kubernetes-sigs/multicluster-runtime), and generated Kubernetes APIs.
- Read the org-wide [AGENTS.md](https://github.com/platform-mesh/.github/blob/main/AGENTS.md) for general conventions.

## Core Principles
- Keep changes small and local. Prefer the narrowest fix that solves the real problem.
- Verify behavior before finishing. Run the smallest relevant tests first, then broader checks if needed.
- Prefer existing repo workflows over ad-hoc commands.
- Keep human-facing process details in `CONTRIBUTING.md`; keep this file focused on agent execution.

## Project Structure
- `api/v1alpha1`: API types, webhooks, generated deepcopy code.
- `internal/controller`: reconcilers and controller tests.
- `internal/config`: runtime configuration parsing and tests.
- `pkg/subroutines`: reusable reconciliation subroutines and mocks.
- `config/crd`: generated CRDs.
- `config/resources` and `test/setup`: generated API resources used by runtime and tests.
- `cmd` and `main.go`: CLI and process entrypoints.
- `hack`: tooling helpers and boilerplate for generation.

## Commands
- `task fmt` — format Go code.
- `task lint` — run formatting plus golangci-lint.
- `task envtest` — run Go tests without bootstrapping extra tools.
- `task test` — run the standard local test path with required tooling.
- `go test ./...` — fast fallback for targeted verification.
- `task manifests` — regenerate CRDs.
- `task generate` — regenerate deepcopy code and API resource output after API changes.
- `docker build .` — build the container image.
- `task docker:kind` — build, load, and restart the deployment in kind.

## Code Conventions
- Follow existing Go patterns in the touched package before introducing new abstractions.
- Keep controller logic in `internal/controller`; put reusable reconciliation helpers in `pkg/subroutines`.
- Add or update `_test.go` files next to changed production code.
- When editing API types under `api/v1alpha1`, regenerate derived files instead of hand-editing generated output.
- Never edit `api/v1alpha1/zz_generated.deepcopy.go` manually.
- Keep logging structured and avoid logging secrets or full credentials.

## Generated Artifacts
- If CRD schemas or API types change, run `task generate`.
- Review generated changes in `config/crd`, `config/resources`, and `test/setup`.
- Do not mix unrelated manual edits into generated files.

## Do Not
- Edit `api/v1alpha1/zz_generated.deepcopy.go` directly.
- Edit generated files in `config/crd` or `config/resources` without running `task generate`.
- Skip regeneration after changing API types or CRD schema.

## Hard Boundaries
- Do not invent new build or test workflows when a `task` target already exists.
- Do not move code across packages unless the change actually requires it.
- Ask before making changes that affect release flow, CI wiring, container publishing, or Helm chart integration outside this repository.

## Human-Facing Guidance
- Use `CONTRIBUTING.md` for contribution process, DCO, and broader developer workflow expectations.
