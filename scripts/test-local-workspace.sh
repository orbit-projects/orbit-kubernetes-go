#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
sdk_root=$(CDPATH= cd -- "$repo_root/../orbit-discovery-go" && pwd)
workspace_root=$(mktemp -d)
trap 'rm -rf "$workspace_root"' EXIT HUP INT TERM

cat > "$workspace_root/go.work" <<EOF
go 1.24.0

use (
	$sdk_root
	$repo_root
)

replace github.com/orbit-projects/orbit-discovery-go v0.1.0-alpha.1 => $sdk_root
EOF

GOWORK="$workspace_root/go.work" go test -race ./...
GOWORK="$workspace_root/go.work" go vet ./...
GOWORK="$workspace_root/go.work" go build -trimpath ./...
