package cache

import (
	"context"
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"

	appsv1 "k8s.io/api/apps/v1"
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
	appsv1.AddToScheme(scheme)

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
			&corev1.ConfigMap{}: {
				UnsafeDisableDeepCopy: &trueVal,
			},
			&corev1.Secret{}: {
				UnsafeDisableDeepCopy: &trueVal,
			},
			&corev1.Node{}: {
				UnsafeDisableDeepCopy: &trueVal,
			},
			&corev1.Namespace{}: {
				UnsafeDisableDeepCopy: &trueVal,
			},
			&appsv1.Deployment{}: {
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

	// Register field indexers for new resource types
	for _, res := range []struct {
		obj  client.Object
		name string
	}{
		{&corev1.ConfigMap{}, "configmap"},
		{&corev1.Secret{}, "secret"},
		{&corev1.Node{}, "node"},
		{&corev1.Namespace{}, "namespace"},
		{&appsv1.Deployment{}, "deployment"},
	} {
		if err := cache.IndexField(ctx, res.obj, "metadata.name", func(obj client.Object) []string {
			return []string{obj.GetName()}
		}); err != nil {
			cancel()
			return fmt.Errorf("index %s metadata.name for %s: %w", res.name, name, err)
		}
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

// ListConfigMaps lists configmaps from cache with optional filters.
func (m *Manager) ListConfigMaps(ctx context.Context, cluster, namespace string, labelSelector labels.Selector, fieldSelector fields.Selector) (*corev1.ConfigMapList, error) {
	m.mu.RLock()
	cc, exists := m.caches[cluster]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no cache for cluster %q", cluster)
	}

	list := &corev1.ConfigMapList{}
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

	if err := cc.cache.List(ctx, list, opts...); err != nil {
		return nil, fmt.Errorf("list configmaps from cache: %w", err)
	}

	return list, nil
}

// ListSecrets lists secrets from cache with optional filters.
func (m *Manager) ListSecrets(ctx context.Context, cluster, namespace string, labelSelector labels.Selector, fieldSelector fields.Selector) (*corev1.SecretList, error) {
	m.mu.RLock()
	cc, exists := m.caches[cluster]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no cache for cluster %q", cluster)
	}

	list := &corev1.SecretList{}
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

	if err := cc.cache.List(ctx, list, opts...); err != nil {
		return nil, fmt.Errorf("list secrets from cache: %w", err)
	}

	return list, nil
}

// ListNodes lists nodes from cache with optional filters.
func (m *Manager) ListNodes(ctx context.Context, cluster string, labelSelector labels.Selector, fieldSelector fields.Selector) (*corev1.NodeList, error) {
	m.mu.RLock()
	cc, exists := m.caches[cluster]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no cache for cluster %q", cluster)
	}

	list := &corev1.NodeList{}
	opts := []client.ListOption{}
	if labelSelector != nil {
		opts = append(opts, client.MatchingLabelsSelector{Selector: labelSelector})
	}
	if fieldSelector != nil {
		opts = append(opts, client.MatchingFieldsSelector{Selector: fieldSelector})
	}

	if err := cc.cache.List(ctx, list, opts...); err != nil {
		return nil, fmt.Errorf("list nodes from cache: %w", err)
	}

	return list, nil
}

// ListNamespaces lists namespaces from cache with optional filters.
func (m *Manager) ListNamespaces(ctx context.Context, cluster string, labelSelector labels.Selector, fieldSelector fields.Selector) (*corev1.NamespaceList, error) {
	m.mu.RLock()
	cc, exists := m.caches[cluster]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no cache for cluster %q", cluster)
	}

	list := &corev1.NamespaceList{}
	opts := []client.ListOption{}
	if labelSelector != nil {
		opts = append(opts, client.MatchingLabelsSelector{Selector: labelSelector})
	}
	if fieldSelector != nil {
		opts = append(opts, client.MatchingFieldsSelector{Selector: fieldSelector})
	}

	if err := cc.cache.List(ctx, list, opts...); err != nil {
		return nil, fmt.Errorf("list namespaces from cache: %w", err)
	}

	return list, nil
}

// ListDeployments lists deployments from cache with optional filters.
func (m *Manager) ListDeployments(ctx context.Context, cluster, namespace string, labelSelector labels.Selector, fieldSelector fields.Selector) (*appsv1.DeploymentList, error) {
	m.mu.RLock()
	cc, exists := m.caches[cluster]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no cache for cluster %q", cluster)
	}

	list := &appsv1.DeploymentList{}
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

	if err := cc.cache.List(ctx, list, opts...); err != nil {
		return nil, fmt.Errorf("list deployments from cache: %w", err)
	}

	return list, nil
}

// HasCache returns true if cache exists for the cluster.
func (m *Manager) HasCache(cluster string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.caches[cluster]
	return exists
}
