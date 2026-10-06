# Orbit Kubernetes Go

`orbit-kubernetes-go` is a complete Go implementation of Orbit's Kubernetes service-discovery
adapter. It performs namespace-scoped Kubernetes EndpointSlice reads with Kubernetes `client-go`,
implements the same provider-neutral `orbit.discovery.v1.ServiceDiscovery/Resolve` capability as
Python `orbit-kubernetes`, and connects to Python Core through the versioned local gRPC process
protocol. The Python implementation remains independently installable; applications select one
runtime explicitly. The Go discovery contract library supplies shared generated bindings and
validation helpers, while this repository owns all Kubernetes API access and provider behavior.

This package reads namespace-scoped Kubernetes EndpointSlices. It filters endpoints explicitly
marked unready, selects a named TCP port, follows bounded pagination, limits result size, and caches
immutable endpoint snapshots for a bounded TTL. It supports in-cluster credentials or an explicit
kubeconfig path and context. It does not create Kubernetes resources or require cluster-wide reads.

## Build

Requires Go 1.24 or later and Protocol Buffers compiler. `make tools` installs the pinned Go
Protobuf generators used by `make generate`. The discovery contract bindings are consumed from
`orbit-discovery-go`; this adapter generates only its Core process-host protocol.

```sh
go test -race ./...
go vet ./...
go build -trimpath -o dist/orbit-kubernetes-plugin ./cmd/orbit-kubernetes-plugin
```

Before `orbit-discovery-go` is published, source-workspace maintainers can run
`sh scripts/test-local-workspace.sh` from a checkout with the sibling SDK directory. It creates a
temporary Go workspace and does not change either module's release manifest.

The cross-language conformance test additionally requires Python Core's process-plugin extra and
`orbit-discovery[process]` in the environment:

```sh
PYTHONPATH="../orbit-core/src:../orbit-discovery/src" \
  python -m pytest -q tests/e2e
```

Core starts the executable with an absolute command path. Configuration is delivered after the
authenticated process handshake, not in command-line arguments. Example process configuration:

```json
{
  "namespace": "apps",
  "port_name": "http",
  "scheme": "http",
  "cache_ttl_ms": 5000,
  "max_cache_services": 128,
  "page_size": 100,
  "max_pages": 64,
  "request_timeout_ms": 5000
}
```

Omit `kubeconfig` to use the standard in-cluster service-account token and CA files, with
`https://kubernetes.default.svc` as the API origin. Set `api_server` to another HTTPS origin for
clusters with a nonstandard API service name. For local administration, provide an explicit
`kubeconfig` path and optional `context`; `api_server` is then rejected as ambiguous. Apply
least-privilege RBAC for read-only EndpointSlice access in the configured namespace. Protect the
kubeconfig as a credential. Health is process readiness; it does not make a live API request.

In app wiring, create the Core `ProcessPlugin` with name `orbit-kubernetes-go`, capability
`orbit.discovery`, the absolute executable path, and the JSON configuration above. Register the
same process object with `ProcessServiceDiscovery` from `orbit-discovery[process]` and bind that
adapter under `DISCOVERY_DEPENDENCY_KEY`. If using the Python adapter, install and bind
`orbit-kubernetes` instead. The selection is explicit; both implementations are not started for one
discovery dependency.

## Boundaries and release status

The local gRPC stream is plaintext loopback for trusted same-user plugin code. Core's one-use token
prevents accidental connections; it is not a sandbox or protection from another process running as
the same user. This adapter does not support mutually untrusted plugins.

The process host runs at most 16 operations concurrently, bounds plugin work, serializes activation
and deactivation commands, and drains in-flight work during shutdown. Fake-client tests validate
selection, caching, configuration, and bounds. A live Kubernetes
cluster, RBAC policy, version skew, hosted CI, signed binary provenance, and release publication
have not been verified. This package is pre-alpha and is not certified production-ready.

Licensed under Apache-2.0.

## Documentation

The package-specific guides cover [architecture](docs/architecture/overview.md), [operations and security](docs/operations/README.md), and [development](docs/development/README.md), with [security guidance](docs/security/overview.md). The [documentation index](docs/README.md) links to the full package overview and project policies.

