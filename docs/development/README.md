# Orbit Kubernetes Go: development

Use the checks for each language manifest present in this repository:

### Go package

```bash
make test
make vet
make build
make integration
gofmt -d $(git ls-files '*.go')
```

The Make targets run through `scripts/test-local-workspace.sh`, which resolves the declared
`orbit-discovery-go` module version to the sibling checkout in a temporary Go workspace. Keep that
checkout at `../orbit-discovery-go` until the declared prerelease is available from the Go module
proxy. Direct `go` commands resolve published module versions and are appropriate once the SDK
release exists.

`make integration` additionally needs Python 3.11 through 3.14 with Core's `process-plugins` extra,
`orbit-discovery[process]`, and `pytest` installed. It launches the Go plugin through Python Core
against a local fake Kubernetes API; it does not contact a Kubernetes cluster.

Optional SDKs and live-service tests are described by the package README. Do not run an opt-in live test against a shared or production service unless its own documentation explicitly supports that use.

## Contribution expectations

Add focused regression coverage for behavior changes. Update the README and relevant guide when a contract, configuration option, security property, lifecycle rule, or operational assumption changes. Report the exact versions and services exercised; local checks do not imply hosted CI or release validation.
