# Architecture

`orbit-kubernetes-go` is the Go implementation of Orbit's Kubernetes EndpointSlice discovery
adapter and a Core-supervised process plugin. It uses `client-go` for namespace-scoped reads and
the shared `orbit.discovery.v1.ServiceDiscovery/Resolve` capability for language-neutral endpoint
snapshots. The Python adapter remains a separate implementation; application wiring selects one
runtime explicitly.

```text
Orbit Core process host (Python)
  ├── starts executable with absolute path
  ├── authenticates local process session and sends configuration
  ├── supervises health, deadlines, and shutdown
  └── ProcessServiceDiscovery bridge → discovery.Resolve
                                      ↓ gRPC stream
Go plugin host
  ├── bounded process protocol and lifecycle serialization
  └── Kubernetes EndpointSlice resolver
      ├── namespace-scoped list with continuation
      ├── filters unready endpoints and selects named TCP port
      └── bounded immutable cache → orbit-discovery-go SDK validation
```

The host supports in-cluster service-account credentials or an explicit kubeconfig path/context.
It performs no Kubernetes writes and does not require cluster-wide read permissions. Readiness
reports process/plugin health; it does not prove that the Kubernetes API is currently reachable.

## Ownership

Core owns process startup, one-use authentication, configuration delivery, process health,
invocation deadlines, and parent-process shutdown. The Go process owns Kubernetes client
configuration, RBAC usage, EndpointSlice pagination, endpoint filtering, local cache, and its gRPC
server/client resources. The application owns container/network isolation and namespace/RBAC
policy.

The discovery SDK validates the service identifier and endpoint values at the contract boundary.
The Kubernetes adapter additionally validates configuration and provider responses. Neither layer
checks whether the returned destination is safe for arbitrary outbound access; callers still apply
egress policy.

## Trusted process boundary

The local stream is plaintext loopback and assumes trusted same-user plugin code. A one-use token
prevents accidental connections but is not a sandbox or defense against another process running as
the same user. This adapter does not support mutually untrusted plugins. Use operating-system or
container isolation and restrict filesystem and network access where the threat model requires it.

## Generated protocol files

`internal/wire` contains generated bindings for the Core process-plugin protocol. The source schema
is `proto/orbit/plugin/v1/process_plugin.proto`; regenerate with the pinned `protoc-gen-go` and
`protoc-gen-go-grpc` tools using `make generate`. Discovery endpoint types come from the separate
`orbit-discovery-go` module and its mirrored `orbit-discovery` Protobuf schema.
