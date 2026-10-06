# Orbit Kubernetes Go: operations and security

This guide organizes runtime behavior documented by the package. It does not certify production readiness. Verify provider/client versions, permissions, transport security, limits, and failure behavior in the target environment before release.

## Configuration surface

Environment names found in the package README:

The package README does not name `ORBIT_*` variables. Use its typed constructors and application configuration, and confirm exact runtime inputs in the implementation before deployment.

Use the package README's constructor and deployment examples. Store credentials in a secret manager and avoid logging credentials, raw provider errors, request data, or opaque cursors.

## Lifecycle, failure behavior, and limits

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

## Production validation

Validate startup/shutdown cleanup, timeout and cancellation behavior, concurrency and payload bounds where applicable, secret rotation and least-privilege access, data durability, backup/restore, and failover against the selected provider. Do not infer distributed or durable guarantees from an in-process API or fake-client tests.
