# Orbit Kubernetes Go: architecture and boundaries

## Responsibility

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

## Declared dependencies

The following dependency declarations come from the checked-in manifests. Optional groups and development dependencies are called out separately.

### `go.mod`
- `github.com/orbit-projects/orbit-discovery-go v0.1.0-alpha.1`
- `google.golang.org/grpc v1.75.1`
- `google.golang.org/protobuf v1.36.6`
- `k8s.io/api v0.34.1`
- `k8s.io/apimachinery v0.34.1`
- `k8s.io/client-go v0.34.1`
- `github.com/davecgh/go-spew v1.1.1 // indirect`
- `github.com/emicklei/go-restful/v3 v3.12.2 // indirect`
- `github.com/fxamacker/cbor/v2 v2.9.0 // indirect`
- `github.com/go-logr/logr v1.4.3 // indirect`
- `github.com/go-openapi/jsonpointer v0.21.0 // indirect`
- `github.com/go-openapi/jsonreference v0.20.2 // indirect`
- `github.com/go-openapi/swag v0.23.0 // indirect`
- `github.com/gogo/protobuf v1.3.2 // indirect`
- `github.com/google/gnostic-models v0.7.0 // indirect`
- `github.com/google/uuid v1.6.0 // indirect`
- `github.com/josharian/intern v1.0.0 // indirect`
- `github.com/json-iterator/go v1.1.12 // indirect`
- `github.com/mailru/easyjson v0.7.7 // indirect`
- `github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect`
- `github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect`
- `github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect`
- `github.com/pkg/errors v0.9.1 // indirect`
- `github.com/spf13/pflag v1.0.6 // indirect`
- `github.com/x448/float16 v0.8.4 // indirect`
- `go.yaml.in/yaml/v2 v2.4.2 // indirect`
- `go.yaml.in/yaml/v3 v3.0.4 // indirect`
- `golang.org/x/net v0.41.0 // indirect`
- `golang.org/x/oauth2 v0.30.0 // indirect`
- `golang.org/x/sys v0.33.0 // indirect`
- `golang.org/x/term v0.32.0 // indirect`
- `golang.org/x/text v0.26.0 // indirect`
- `golang.org/x/time v0.9.0 // indirect`
- `google.golang.org/genproto/googleapis/rpc v0.0.0-20250707201910-8d1bb00bc6a7 // indirect`
- `gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect`
- `gopkg.in/inf.v0 v0.9.1 // indirect`
- `gopkg.in/yaml.v3 v3.0.1 // indirect`
- `k8s.io/klog/v2 v2.130.1 // indirect`
- `k8s.io/kube-openapi v0.0.0-20250710124328-f3f2b991d03b // indirect`
- `k8s.io/utils v0.0.0-20250604170112-4c0f3b243397 // indirect`
- `sigs.k8s.io/json v0.0.0-20241014173422-cfa47c3a1cc8 // indirect`
- `sigs.k8s.io/randfill v1.0.0 // indirect`
- `sigs.k8s.io/structured-merge-diff/v6 v6.3.0 // indirect`
- `sigs.k8s.io/yaml v1.6.0 // indirect`

Declared dependencies do not mean that optional providers or services are bundled with this package.

## Implementation layout

Representative implementation files in this checkout:

- `cmd/orbit-kubernetes-plugin/main.go`
- `internal/kubernetes/discovery.go`
- `internal/kubernetes/discovery_test.go`
- `internal/plugin/host.go`
- `internal/plugin/host_test.go`
- `internal/wire/process_plugin.pb.go`
- `internal/wire/process_plugin_grpc.pb.go`

## Public contract and scope

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

## Boundary rules

Keep provider SDKs, credentials, transports, and provider-specific error translation in provider adapters. Keep reusable capability contracts in the matching capability package and lifecycle orchestration in Core. Apply the relevant layer for this repository and preserve the dependency direction shown above.
