# Copyright 2026-present Orbit Contributors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from __future__ import annotations

import json
import shutil
import subprocess
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import pytest

pytest.importorskip("grpc")
pytest.importorskip("orbit")
pytest.importorskip("orbit_discovery")

from orbit.plugins.metadata import PluginMetadata  # noqa: E402
from orbit.plugins.process import ProcessPlugin  # noqa: E402
from orbit.plugins.registry import PluginRegistry  # noqa: E402
from orbit_discovery import ProcessServiceDiscovery  # noqa: E402


_ROOT = Path(__file__).resolve().parents[2]


class _KubernetesAPI(BaseHTTPRequestHandler):
    requests = 0

    def do_GET(self) -> None:
        type(self).requests += 1
        payload = {
            "apiVersion": "discovery.k8s.io/v1",
            "kind": "EndpointSliceList",
            "metadata": {"resourceVersion": "1"},
            "items": [
                {
                    "metadata": {
                        "name": "catalog-abc",
                        "namespace": "apps",
                        "labels": {"kubernetes.io/service-name": "catalog"},
                    },
                    "addressType": "IPv4",
                    "ports": [{"name": "http", "protocol": "TCP", "port": 8080}],
                    "endpoints": [
                        {
                            "addresses": ["10.0.0.2"],
                            "conditions": {"ready": True},
                        },
                        {
                            "addresses": ["10.0.0.3"],
                            "conditions": {"ready": False},
                        },
                    ],
                }
            ],
        }
        data = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, _format: str, *_args: object) -> None:
        return


@pytest.mark.asyncio
async def test_go_process_plugin_runs_through_python_core_host_and_discovery_contract(
    tmp_path: Path,
) -> None:
    if shutil.which("go") is None:
        pytest.skip("Go is required for the cross-language process test")
    binary = tmp_path / "orbit-kubernetes-plugin"
    subprocess.run(
        ["go", "build", "-trimpath", "-o", str(binary), "./cmd/orbit-kubernetes-plugin"],
        cwd=_ROOT,
        check=True,
    )
    _KubernetesAPI.requests = 0
    server = ThreadingHTTPServer(("127.0.0.1", 0), _KubernetesAPI)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    kubeconfig = tmp_path / "kubeconfig.yaml"
    kubeconfig.write_text(
        """apiVersion: v1
kind: Config
clusters:
- name: local
  cluster:
    server: http://127.0.0.1:%d
contexts:
- name: local
  context:
    cluster: local
    user: local
current-context: local
users:
- name: local
  user: {}
"""
        % server.server_address[1],
        encoding="utf-8",
    )
    plugin = ProcessPlugin(
        PluginMetadata(
            name="orbit-kubernetes-go",
            version="0.1.0",
            capabilities=frozenset({"orbit.discovery"}),
        ),
        (str(binary),),
        configuration_json=json.dumps(
            {
                "kubeconfig": str(kubeconfig),
                "namespace": "apps",
                "port_name": "http",
                "cache_ttl_ms": 30_000,
            }
        ).encode(),
        startup_timeout=10,
        command_timeout=5,
    )
    registry = PluginRegistry()
    registry.register(plugin)
    discovery = ProcessServiceDiscovery(plugin)
    try:
        await registry.activate()
        first = await discovery.resolve("catalog")
        second = await discovery.resolve("catalog")
        assert first == second
        assert [(item.host, item.port) for item in first] == [("10.0.0.2", 8080)]
        assert first[0].metadata["namespace"] == "apps"
        assert _KubernetesAPI.requests == 1
    finally:
        await discovery.aclose()
        await registry.deactivate()
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)
