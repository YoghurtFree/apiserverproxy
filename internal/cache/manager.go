package cache

import (
	"context"
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClusterCache manages cache for a single cluster.
type ClusterCache struct {
	name   string
	cache  ctrlcache.Cache
	cancel context.CancelFunc
}

// Manager manages caches for multiple clusters.
type Manager struct {
	mu     sync.RWMutex
	caches map[string]*ClusterCache
	scheme *runtime.Scheme
}

// NewManager creates a new cache manager.
func NewManager() *Manager {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)

	return &Manager{
		caches: make(map[string]*ClusterCache),
		scheme: scheme,
	}
}

// Start starts the cache for a cluster.
func (m *Manager) Start(ctx context.Context, name string, config *rest.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.caches[name]; exists {
		return fmt.Errorf("cache already started for cluster %q", name)
	}

	cacheCtx, cancel := context.WithCancel(ctx)
	trueVal := true
	cache, err := ctrlcache.New(config, ctrlcache.Options{
		Scheme: m.scheme,
		ByObject: map[client.Object]ctrlcache.ByObject{
			&corev1.Pod{}: {
				UnsafeDisableDeepCopy: &trueVal,
			},
			&corev1.Service{}: {
				UnsafeDisableDeepCopy: &trueVal,
			},
		},
	})
	if err != nil {
		cancel()
		return fmt.Errorf("create cache for %s: %w", name, err)
	}

	// Register field indexers for pod filtering
	if err := cache.IndexField(ctx, &corev1.Pod{}, "metadata.name", func(obj client.Object) []string {
		return []string{obj.GetName()}
	}); err != nil {
		cancel()
		return fmt.Errorf("index pod metadata.name for %s: %w", name, err)
	}

	if err := cache.IndexField(ctx, &corev1.Pod{}, "spec.nodeName", func(obj client.Object) []string {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			return nil
		}
		if pod.Spec.NodeName == "" {
			return nil
		}
		return []string{pod.Spec.NodeName}
	}); err != nil {
		cancel()
		return fmt.Errorf("index pod spec.nodeName for %s: %w", name, err)
	}

	if err := cache.IndexField(ctx, &corev1.Pod{}, "status.phase", func(obj client.Object) []string {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			return nil
		}
		if pod.Status.Phase == "" {
			return nil
		}
		return []string{string(pod.Status.Phase)}
	}); err != nil {
		cancel()
		return fmt.Errorf("index pod status.phase for %s: %w", name, err)
	}

	// Register field indexers for service filtering
	if err := cache.IndexField(ctx, &corev1.Service{}, "metadata.name", func(obj client.Object) []string {
		return []string{obj.GetName()}
	}); err != nil {
		cancel()
		return fmt.Errorf("index service metadata.name for %s: %w", name, err)
	}

	// Start the cache in background
	go func() {
		if err := cache.Start(cacheCtx); err != nil {
			logrus.Errorf("cache error for %s: %v", name, err)
		}
	}()

	// Wait for cache sync
	if !cache.WaitForCacheSync(cacheCtx) {
		cancel()
		return fmt.Errorf("cache sync failed for %s", name)
	}

	m.caches[name] = &ClusterCache{
		name:   name,
		cache:  cache,
		cancel: cancel,
	}

	return nil
}

// Stop stops the cache for a cluster.
func (m *Manager) Stop(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cc, exists := m.caches[name]; exists {
		cc.cancel()
		delete(m.caches, name)
	}
}

// StopAll stops all caches.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, cc := range m.caches {
		cc.cancel()
		delete(m.caches, name)
	}
}

// ListPods lists pods from cache with optional filters.
func (m *Manager) ListPods(ctx context.Context, cluster, namespace string, labelSelector labels.Selector, fieldSelector fields.Selector) (*corev1.PodList, error) {
	m.mu.RLock()
	cc, exists := m.caches[cluster]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no cache for cluster %q", cluster)
	}

	pods := &corev1.PodList{}
	opts := []client.ListOption{}
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}
	if labelSelector != nil {
		opts = append(opts, client.MatchingLabelsSelector{Selector: labelSelector})
	}
	if fieldSelector != nil {
		opts = append(opts, client.MatchingFieldsSelector{Selector: fieldSelector})
	}

	if err := cc.cache.List(ctx, pods, opts...); err != nil {
		return nil, fmt.Errorf("list pods from cache: %w", err)
	}

	return pods, nil
}

// ListServices lists services from cache with optional filters.
func (m *Manager) ListServices(ctx context.Context, cluster, namespace string, labelSelector labels.Selector, fieldSelector fields.Selector) (*corev1.ServiceList, error) {
	m.mu.RLock()
	cc, exists := m.caches[cluster]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no cache for cluster %q", cluster)
	}

	services := &corev1.ServiceList{}
	opts := []client.ListOption{}
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}
	if labelSelector != nil {
		opts = append(opts, client.MatchingLabelsSelector{Selector: labelSelector})
	}
	if fieldSelector != nil {
		opts = append(opts, client.MatchingFieldsSelector{Selector: fieldSelector})
	}

	if err := cc.cache.List(ctx, services, opts...); err != nil {
		return nil, fmt.Errorf("list services from cache: %w", err)
	}

	return services, nil
}

// HasCache returns true if cache exists for the cluster.
func (m *Manager) HasCache(cluster string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.caches[cluster]
	return exists
}
