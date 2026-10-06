// Copyright 2026-present Orbit Contributors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kubernetes

import (
	"container/list"
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func testResolver(t *testing.T, cacheTTL time.Duration) *resolver {
	t.Helper()
	portName, number, protocol := "http", int32(8080), corev1.ProtocolTCP
	notReady, ready := false, true
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: "catalog-abc", Namespace: "apps",
			Labels: map[string]string{discoveryv1.LabelServiceName: "catalog"},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports:       []discoveryv1.EndpointPort{{Name: &portName, Port: &number, Protocol: &protocol}},
		Endpoints: []discoveryv1.Endpoint{
			{Addresses: []string{"10.0.0.2"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}},
			{Addresses: []string{"10.0.0.3"}, Conditions: discoveryv1.EndpointConditions{Ready: &notReady}},
		},
	}
	client := fake.NewSimpleClientset(slice)
	r := &resolver{
		client: client,
		settings: settings{
			Namespace: "apps", PortName: "http", Scheme: "https",
			CacheTTLMS: int(cacheTTL / time.Millisecond), MaxCacheServices: 4,
			PageSize: 10, MaxPages: 2, RequestTimeoutMS: 1000,
		},
		cache: make(map[string]*list.Element), lru: list.New(),
	}
	r.cond = sync.NewCond(&r.stateMu)
	return r
}

func TestResolveReturnsOnlyReadyNamedTCPAddresses(t *testing.T) {
	r := testResolver(t, 0)
	if err := r.enter(); err != nil {
		t.Fatal(err)
	}
	got, err := r.resolve(context.Background(), "catalog")
	r.leave()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Host != "10.0.0.2" || got[0].Port != 8080 || got[0].Scheme != "https" {
		t.Fatalf("unexpected endpoint snapshot: %#v", got)
	}
	if got[0].InstanceId == "" || got[0].Metadata["namespace"] != "apps" {
		t.Fatalf("endpoint identity or namespace missing: %#v", got[0])
	}
}

func TestResolveCachesByServiceAndReturnsCopies(t *testing.T) {
	r := testResolver(t, 30*time.Second)
	if err := r.enter(); err != nil {
		t.Fatal(err)
	}
	first, err := r.resolve(context.Background(), "catalog")
	if err != nil {
		t.Fatal(err)
	}
	first[0].Host = "changed.invalid"
	second, err := r.resolve(context.Background(), "catalog")
	r.leave()
	if err != nil {
		t.Fatal(err)
	}
	if second[0].Host != "10.0.0.2" {
		t.Fatalf("cache leaked a mutable snapshot: %#v", second[0])
	}
}

func TestSettingsRejectUnknownFieldsAndClampBounds(t *testing.T) {
	_, err := parseSettings([]byte(`{"namespace":"apps","port_name":"http","password":"secret"}`))
	if err == nil {
		t.Fatal("unknown configuration field accepted")
	}
	input := map[string]any{"namespace": "apps", "port_name": "http", "page_size": 501}
	raw, _ := json.Marshal(input)
	if _, err := parseSettings(raw); err == nil {
		t.Fatal("oversized Kubernetes page size accepted")
	}
}

func TestInClusterConfigurationUsesHTTPSAndMountedServiceAccountFiles(t *testing.T) {
	configuration, err := loadConfig(settings{RequestTimeoutMS: 1200})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Host != "https://kubernetes.default.svc" || configuration.BearerTokenFile == "" || configuration.TLSClientConfig.CAFile == "" {
		t.Fatalf("in-cluster authentication was not configured explicitly: %#v", configuration)
	}
	if _, err := loadConfig(settings{APIServer: "http://cluster.local"}); err == nil {
		t.Fatal("insecure API origin accepted")
	}
	if _, err := loadConfig(settings{APIServer: "https://user:password@cluster.local"}); err == nil {
		t.Fatal("credential-bearing API origin accepted")
	}
}

func TestNormalizeHostRejectsURLSyntaxAndCanonicalizesIP(t *testing.T) {
	if host, ok := normalizeHost("2001:0db8::1"); !ok || host != "2001:db8::1" {
		t.Fatalf("IPv6 was not canonicalized: %q %v", host, ok)
	}
	for _, input := range []string{"https://example.com", "user@example.com", "bad/name"} {
		if _, ok := normalizeHost(input); ok {
			t.Fatalf("unsafe endpoint host accepted: %q", input)
		}
	}
}
