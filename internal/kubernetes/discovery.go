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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	orbitdiscovery "github.com/orbit-projects/orbit-discovery-go/discovery"
	discoverywire "github.com/orbit-projects/orbit-discovery-go/v1"
	pluginhost "github.com/orbit-projects/orbit-kubernetes-go/internal/plugin"
	processwire "github.com/orbit-projects/orbit-kubernetes-go/internal/wire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	maxEndpoints       = 4096
	maxCachedEndpoints = 16384
	maxConfig          = 1024 * 1024
)

var (
	dnsLabel  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	serviceID = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,126}[A-Za-z0-9])?$`)
	portID    = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,13}[a-z0-9])?$`)
)

type settings struct {
	Kubeconfig       string `json:"kubeconfig"`
	Context          string `json:"context"`
	APIServer        string `json:"api_server"`
	Namespace        string `json:"namespace"`
	PortName         string `json:"port_name"`
	Scheme           string `json:"scheme"`
	CacheTTLMS       int    `json:"cache_ttl_ms"`
	MaxCacheServices int    `json:"max_cache_services"`
	PageSize         int64  `json:"page_size"`
	MaxPages         int    `json:"max_pages"`
	RequestTimeoutMS int    `json:"request_timeout_ms"`
}

type cacheEntry struct {
	key       string
	instances []*discoverywire.ServiceInstance
	expires   time.Time
}

type resolver struct {
	client          kubernetes.Interface
	transport       interface{ CloseIdleConnections() }
	settings        settings
	stateMu         sync.Mutex
	cond            *sync.Cond
	cache           map[string]*list.Element
	lru             *list.List
	cachedEndpoints int
	stripes         [64]sync.Mutex
	active          int
	closed          bool
}

// Handler implements the Core discovery capability using namespace-scoped EndpointSlices.
type Handler struct {
	mu       sync.RWMutex
	resolver *resolver
}

// Activate validates bounded configuration and loads in-cluster or explicit kubeconfig access.
func (h *Handler) Activate(_ context.Context, raw []byte) error {
	configuration, err := parseSettings(raw)
	if err != nil {
		return err
	}
	clientConfig, err := loadConfig(configuration)
	if err != nil {
		return err
	}
	transport, err := rest.TransportFor(clientConfig)
	if err != nil {
		return errors.New("could not initialize Kubernetes API transport")
	}
	client, err := kubernetes.NewForConfigAndClient(clientConfig, &http.Client{
		Transport: transport,
		Timeout:   clientConfig.Timeout,
	})
	if err != nil {
		return errors.New("could not initialize Kubernetes API client")
	}
	r := &resolver{
		client: client, transport: transportCloser(transport), settings: configuration,
		cache: make(map[string]*list.Element), lru: list.New(),
	}
	r.cond = sync.NewCond(&r.stateMu)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.resolver != nil {
		return errors.New("Kubernetes provider is already active")
	}
	h.resolver = r
	return nil
}

// Deactivate waits for bounded in-flight Kubernetes reads and drops cached endpoints.
func (h *Handler) Deactivate(ctx context.Context) error {
	h.mu.Lock()
	r := h.resolver
	h.resolver = nil
	h.mu.Unlock()
	if r == nil {
		return nil
	}
	return r.close(ctx)
}

// Health reports process health without exposing cluster, credential, or transport details.
func (h *Handler) Health(context.Context) (pluginhost.Health, error) {
	h.mu.RLock()
	active := h.resolver != nil
	h.mu.RUnlock()
	if !active {
		return pluginhost.Health{Status: processwire.PluginHealth_STATUS_UNHEALTHY, Message: "Kubernetes provider is not active."}, nil
	}
	return pluginhost.Health{Status: processwire.PluginHealth_STATUS_HEALTHY, Message: "Kubernetes provider is ready."}, nil
}

// Resolve performs a validated lookup against the capability contract.
func (h *Handler) Resolve(ctx context.Context, service string) (*discoverywire.ResolveResponse, error) {
	h.mu.RLock()
	r := h.resolver
	h.mu.RUnlock()
	if r == nil {
		return nil, errors.New("Kubernetes provider is not active")
	}
	if !serviceID.MatchString(service) {
		return nil, errors.New("invalid service identifier")
	}
	if err := r.enter(); err != nil {
		return nil, err
	}
	defer r.leave()
	instances, err := r.resolve(ctx, service)
	if err != nil {
		return nil, err
	}
	return &discoverywire.ResolveResponse{Instances: cloneInstances(instances)}, nil
}

func (h *Handler) Invoke(ctx context.Context, method string, request *anypb.Any) (*anypb.Any, string) {
	if method != orbitdiscovery.ResolveMethod {
		return nil, "unimplemented"
	}
	input := &discoverywire.ResolveRequest{}
	if err := anypb.UnmarshalTo(request, input, proto.UnmarshalOptions{}); err != nil {
		return nil, "invalid-request"
	}
	output, err := h.Resolve(ctx, input.Service)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, "timeout"
		}
		if errors.Is(err, context.Canceled) {
			return nil, "cancelled"
		}
		if strings.Contains(err.Error(), "invalid service") {
			return nil, "invalid-request"
		}
		return nil, "unavailable"
	}
	packed, err := anypb.New(output)
	if err != nil {
		return nil, "serialization"
	}
	return packed, ""
}

func (r *resolver) enter() error {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.closed {
		return errors.New("Kubernetes resolver is closed")
	}
	r.active++
	return nil
}

func (r *resolver) leave() {
	r.stateMu.Lock()
	r.active--
	if r.active == 0 {
		r.cond.Broadcast()
	}
	r.stateMu.Unlock()
}

func (r *resolver) close(ctx context.Context) error {
	r.stateMu.Lock()
	r.closed = true
	for r.active > 0 {
		if ctx.Err() != nil {
			r.stateMu.Unlock()
			return ctx.Err()
		}
		// EndpointSlice requests have their own bounded timeout, so this wait is finite.
		r.cond.Wait()
	}
	r.cache = make(map[string]*list.Element)
	r.lru.Init()
	r.cachedEndpoints = 0
	r.stateMu.Unlock()
	if r.transport != nil {
		r.transport.CloseIdleConnections()
	}
	return nil
}

func transportCloser(transport http.RoundTripper) interface{ CloseIdleConnections() } {
	base, ok := transport.(*http.Transport)
	if !ok || base == http.DefaultTransport {
		return nil
	}
	return base
}

func (r *resolver) resolve(ctx context.Context, service string) ([]*discoverywire.ServiceInstance, error) {
	stripe := &r.stripes[hashIndex(service)]
	stripe.Lock()
	defer stripe.Unlock()
	now := time.Now()
	r.stateMu.Lock()
	if cached, ok := r.cache[service]; ok {
		entry := cached.Value.(*cacheEntry)
		if now.Before(entry.expires) {
			r.lru.MoveToFront(cached)
			result := cloneInstances(entry.instances)
			r.stateMu.Unlock()
			return result, nil
		}
		r.lru.Remove(cached)
		r.cachedEndpoints -= len(entry.instances)
		delete(r.cache, service)
	}
	r.stateMu.Unlock()
	result, err := r.listEndpointSlices(ctx, service)
	if err != nil {
		return nil, err
	}
	if r.settings.CacheTTLMS > 0 {
		r.stateMu.Lock()
		for r.lru.Len() > 0 && (r.lru.Len() >= r.settings.MaxCacheServices ||
			r.cachedEndpoints+len(result) > maxCachedEndpoints) {
			oldest := r.lru.Back()
			oldEntry := oldest.Value.(*cacheEntry)
			delete(r.cache, oldEntry.key)
			r.cachedEndpoints -= len(oldEntry.instances)
			r.lru.Remove(oldest)
		}
		if len(result) <= maxCachedEndpoints {
			entry := &cacheEntry{key: service, instances: cloneInstances(result), expires: time.Now().Add(time.Duration(r.settings.CacheTTLMS) * time.Millisecond)}
			r.cache[service] = r.lru.PushFront(entry)
			r.cachedEndpoints += len(entry.instances)
		}
		r.stateMu.Unlock()
	}
	return result, nil
}

func (r *resolver) listEndpointSlices(ctx context.Context, service string) ([]*discoverywire.ServiceInstance, error) {
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(r.settings.RequestTimeoutMS)*time.Millisecond)
	defer cancel()
	selector := labels.Set{discoveryv1.LabelServiceName: service}.AsSelector().String()
	continuation := ""
	seen := make(map[string]struct{})
	result := make([]*discoverywire.ServiceInstance, 0)
	endpointCount := 0
	for page := 0; page < r.settings.MaxPages; page++ {
		items, err := r.client.DiscoveryV1().EndpointSlices(r.settings.Namespace).List(requestCtx, metav1.ListOptions{
			LabelSelector: selector, Limit: r.settings.PageSize, Continue: continuation,
		})
		if err != nil {
			return nil, errors.New("Kubernetes EndpointSlice lookup failed")
		}
		for i := range items.Items {
			slice := &items.Items[i]
			port := namedPort(slice.Ports, r.settings.PortName)
			if port == 0 {
				continue
			}
			for _, endpoint := range slice.Endpoints {
				endpointCount++
				if endpointCount > maxEndpoints {
					return nil, errors.New("Kubernetes endpoint limit exceeded")
				}
				if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
					continue
				}
				for _, address := range endpoint.Addresses {
					if len(result) >= maxEndpoints {
						return nil, errors.New("Kubernetes endpoint limit exceeded")
					}
					host, ok := normalizeHost(address)
					if !ok {
						continue
					}
					key := net.JoinHostPort(host, fmt.Sprint(port))
					if _, exists := seen[key]; exists {
						continue
					}
					seen[key] = struct{}{}
					result = append(result, &discoverywire.ServiceInstance{
						Service: service, InstanceId: instanceID(slice.Name, host, port), Host: host,
						Port: uint32(port), Scheme: r.settings.Scheme, Weight: 1,
						Metadata: map[string]string{"namespace": r.settings.Namespace},
					})
				}
			}
		}
		continuation = items.Continue
		if continuation == "" {
			return result, nil
		}
	}
	return nil, errors.New("Kubernetes EndpointSlice page limit exceeded")
}

func parseSettings(raw []byte) (settings, error) {
	var value settings
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if len(raw) == 0 || len(raw) > maxConfig || decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		return value, errors.New("invalid Kubernetes plugin configuration")
	}
	if !dnsLabel.MatchString(value.Namespace) || !portID.MatchString(value.PortName) {
		return value, errors.New("namespace or Kubernetes port_name is invalid")
	}
	if value.Kubeconfig != "" && value.APIServer != "" {
		return value, errors.New("api_server is only valid when kubeconfig is omitted")
	}
	if value.Kubeconfig != "" && (!filepath.IsAbs(value.Kubeconfig) || strings.ContainsRune(value.Kubeconfig, '\x00')) {
		return value, errors.New("kubeconfig must be an absolute path without NUL characters")
	}
	if value.Context != "" && value.Kubeconfig == "" {
		return value, errors.New("context requires an explicit kubeconfig")
	}
	if value.Scheme == "" {
		value.Scheme = "http"
	}
	if value.Scheme != "http" && value.Scheme != "https" {
		return value, errors.New("scheme must be http or https")
	}
	if value.CacheTTLMS == 0 {
		value.CacheTTLMS = 5000
	}
	if value.CacheTTLMS < 0 || value.CacheTTLMS > 300_000 {
		return value, errors.New("cache_ttl_ms is outside the supported range")
	}
	if value.MaxCacheServices == 0 {
		value.MaxCacheServices = 128
	}
	if value.MaxCacheServices < 1 || value.MaxCacheServices > 1024 {
		return value, errors.New("max_cache_services is outside the supported range")
	}
	if value.PageSize == 0 {
		value.PageSize = 100
	}
	if value.PageSize < 1 || value.PageSize > 500 {
		return value, errors.New("page_size is outside the supported range")
	}
	if value.MaxPages == 0 {
		value.MaxPages = 64
	}
	if value.MaxPages < 1 || value.MaxPages > 64 {
		return value, errors.New("max_pages is outside the supported range")
	}
	if value.RequestTimeoutMS == 0 {
		value.RequestTimeoutMS = 5000
	}
	if value.RequestTimeoutMS < 50 || value.RequestTimeoutMS > 60_000 {
		return value, errors.New("request_timeout_ms is outside the supported range")
	}
	return value, nil
}

func loadConfig(value settings) (*rest.Config, error) {
	var configuration *rest.Config
	var err error
	if value.Kubeconfig == "" {
		apiServer := value.APIServer
		if apiServer == "" {
			apiServer = "https://kubernetes.default.svc"
		}
		parsed, parseErr := url.Parse(apiServer)
		if parseErr != nil || parsed.Scheme != "https" || parsed.Hostname() == "" ||
			parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
			return nil, errors.New("api_server must be an HTTPS origin without credentials")
		}
		serviceAccount := "/var/run/secrets/kubernetes.io/serviceaccount"
		configuration = &rest.Config{
			Host:            apiServer,
			BearerTokenFile: filepath.Join(serviceAccount, "token"),
			TLSClientConfig: rest.TLSClientConfig{CAFile: filepath.Join(serviceAccount, "ca.crt")},
		}
	} else {
		rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: value.Kubeconfig}
		overrides := &clientcmd.ConfigOverrides{CurrentContext: value.Context}
		configuration, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	}
	if err != nil {
		return nil, errors.New("could not load Kubernetes credentials")
	}
	configuration.Timeout = time.Duration(value.RequestTimeoutMS) * time.Millisecond
	return configuration, nil
}

func namedPort(ports []discoveryv1.EndpointPort, name string) int32 {
	for _, port := range ports {
		if port.Name == nil || *port.Name != name || port.Port == nil {
			continue
		}
		if port.Protocol != nil && *port.Protocol != corev1.ProtocolTCP {
			continue
		}
		if *port.Port > 0 && *port.Port <= 65535 {
			return *port.Port
		}
	}
	return 0
}

func normalizeHost(value string) (string, bool) {
	if ip := net.ParseIP(value); ip != nil {
		return ip.String(), true
	}
	if len(value) == 0 || len(value) > 253 || strings.HasSuffix(value, ".") {
		return "", false
	}
	for _, label := range strings.Split(strings.ToLower(value), ".") {
		if !dnsLabel.MatchString(label) {
			return "", false
		}
	}
	return strings.ToLower(value), true
}

func instanceID(slice, host string, port int32) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", slice, host, port)))
	return "k8s-" + hex.EncodeToString(hash[:16])
}

func hashIndex(value string) int {
	hash := sha256.Sum256([]byte(value))
	return int(hash[0]) % 64
}

func cloneInstances(value []*discoverywire.ServiceInstance) []*discoverywire.ServiceInstance {
	result := make([]*discoverywire.ServiceInstance, 0, len(value))
	for _, item := range value {
		result = append(result, proto.Clone(item).(*discoverywire.ServiceInstance))
	}
	return result
}
