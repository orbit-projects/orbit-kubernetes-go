# Operations

This adapter reads Kubernetes EndpointSlices in one configured namespace. It filters endpoints
that Kubernetes explicitly marks unready, selects the configured named TCP port, follows bounded
pagination, and returns immutable discovery snapshots from a bounded TTL cache. It does not
create, change, or delete Kubernetes resources.

## Identity and RBAC

For in-cluster mode, the plugin uses the Pod's service-account identity and Kubernetes API service
origin. Grant namespace-scoped permission to read EndpointSlices only. For local administration,
provide an explicit kubeconfig path and optional context; protect that file as a credential and
grant the same narrow read access. The `api_server` override is intended for nonstandard in-cluster
API origins and is rejected when kubeconfig mode is active to avoid ambiguous identity/endpoint
configuration.

Do not enable cluster-wide reads for convenience. Review the effective Role/RoleBinding in the
target namespace and test with the exact workload identity.

## Process plugin wiring

Build the `orbit-kubernetes-plugin` executable and use its absolute path in Core's
`ProcessPlugin` configuration. Set the plugin name to `orbit-kubernetes-go`, declare capability
`orbit.discovery`, and deliver the documented JSON configuration after the process handshake.
Register the same Core process object with `ProcessServiceDiscovery` and bind that adapter under
`DISCOVERY_DEPENDENCY_KEY`. Select either this Go implementation or Python `orbit-kubernetes` in
application wiring; do not register both for the same dependency key.

Core owns process launch, one-use authentication, deadlines, health checks, and process shutdown.
The host bounds concurrent operations, serializes activation and deactivation, and drains in-flight
requests during shutdown. Health indicates process readiness only, not live Kubernetes API
availability. The local gRPC stream is plaintext loopback for trusted same-user code; it is not a
sandbox.

## Configuration and capacity

Review namespace, named port, HTTP(S) scheme, cache TTL, maximum cached services, page size, maximum
pages, and API timeout for the workload. Cache TTL trades freshness for API load. Pagination,
fan-out, metadata size, request deadlines, and process concurrency are bounded; size them against
the number of services and EndpointSlices expected in the namespace. The resolver returns only
ready instances with a matching TCP port.

## Validation before rollout

Run Go unit tests and the local workspace script when using the sibling `orbit-discovery-go` source
checkout. The Python interoperability suite additionally needs Core's process-plugin extra and
`orbit-discovery[process]`. In a dedicated non-production cluster, verify service-account RBAC,
EndpointSlice updates, unready endpoints, port selection, pagination, cache freshness, API outages,
credential rotation, process restart, deadlines, and shutdown. Fake clients do not certify a live
cluster or production readiness.
