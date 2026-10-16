#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
sdk_root=$(CDPATH= cd -- "$repo_root/../orbit-discovery-go" && pwd)
workspace_root=$(mktemp -d)
trap 'rm -rf "$workspace_root"' EXIT HUP INT TERM
action=${1:-test}

cat > "$workspace_root/go.work" <<EOF
go 1.24.0

use (
	$sdk_root
	$repo_root
)

replace github.com/orbit-projects/orbit-discovery-go v0.1.0-alpha.1 => $sdk_root
EOF

case "$action" in
    test)
        GOWORK="$workspace_root/go.work" go test -race ./...
        ;;
    vet)
        GOWORK="$workspace_root/go.work" go vet ./...
        ;;
    build)
        mkdir -p "$repo_root/dist"
        GOWORK="$workspace_root/go.work" go build -trimpath \
            -o "$repo_root/dist/orbit-kubernetes-plugin" ./cmd/orbit-kubernetes-plugin
        ;;
    integration)
        PYTHONPATH="../orbit-core/src:../orbit-discovery/src${PYTHONPATH:+:$PYTHONPATH}" \
            GOWORK="$workspace_root/go.work" \
            python -m pytest -q tests/e2e
        ;;
    *)
        printf 'usage: %s [test|vet|build|integration]\n' "$0" >&2
        exit 2
        ;;
esac
