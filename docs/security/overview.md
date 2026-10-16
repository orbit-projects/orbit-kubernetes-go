# Security

## Kubernetes credentials and access

Use the in-cluster service-account identity with namespace-scoped read access to EndpointSlices.
For local use, supply a protected kubeconfig with an identity granted the same least privilege.
Do not embed kubeconfig contents, bearer tokens, client keys, or cluster credentials in plugin JSON,
command-line arguments, repository files, logs, or error messages.

Review the effective service account, Role, and RoleBinding in the target namespace. This adapter
does not create or modify Kubernetes permissions and does not need cluster-wide inventory access.
Credential paths and API origins should be controlled by deployment configuration, not untrusted
input.

## Process isolation

The process transport is plaintext loopback for trusted same-user plugin code. Core's one-use
authentication token helps bind the expected plugin session but is not a sandbox or protection
from another process running as the same OS user. Do not run mutually untrusted plugins in this
trust boundary. Use OS/container identity, filesystem restrictions, network policy, and resource
limits when the deployment threat model requires isolation.

## Endpoint data and egress

Resolved hosts, ports, service names, and endpoint metadata describe internal topology. Treat them
as operationally sensitive; do not log full discovery responses or cache contents by default.
Endpoint validation is syntactic and does not authorize connections. Apply egress restrictions and
destination validation before callers connect to returned endpoints. A valid discovery result is
not an SSRF defense.

## Supply chain and reporting

Pin and review Kubernetes client and Go module updates. Build the plugin through a controlled
release workflow and verify provenance before deployment; local builds are not signed-release
evidence. Report suspected vulnerabilities through the repository's
[private security policy](../../SECURITY.md).
