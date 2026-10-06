# Orbit Kubernetes Go: development

Use the checks for each language manifest present in this repository:

### Go package

```bash
go test ./...
go test -race ./...
go vet ./...
gofmt -d $(git ls-files '*.go')
go build ./...
```

Optional SDKs and live-service tests are described by the package README. Do not run an opt-in live test against a shared or production service unless its own documentation explicitly supports that use.

## Contribution expectations

Add focused regression coverage for behavior changes. Update the README and relevant guide when a contract, configuration option, security property, lifecycle rule, or operational assumption changes. Report the exact versions and services exercised; local checks do not imply hosted CI or release validation.
