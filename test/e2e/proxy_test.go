package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	proxyURL = "http://localhost:8080/clusters/minikube"
)

func getClient(t *testing.T) *kubernetes.Clientset {
	t.Helper()

	token := os.Getenv("K8S_TOKEN")
	if token == "" {
		t.Fatal("K8S_TOKEN environment variable is required")
	}

	cfg := &rest.Config{
		Host:        proxyURL,
		BearerToken: token,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
		QPS:   50,
		Burst: 100,
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	return client
}

func TestListPods(t *testing.T) {
	client := getClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list pods: %v", err)
	}

	fmt.Printf("Found %d pods\n", len(pods.Items))
	for _, pod := range pods.Items {
		fmt.Printf("  - %s/%s (phase=%s, node=%s)\n",
			pod.Namespace, pod.Name, pod.Status.Phase, pod.Spec.NodeName)
	}

	if len(pods.Items) == 0 {
		t.Log("Warning: no pods found, but API call succeeded")
	}
}

func TestListPodsInNamespace(t *testing.T) {
	client := getClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pods, err := client.CoreV1().Pods("default").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list pods in default namespace: %v", err)
	}

	fmt.Printf("Found %d pods in default namespace\n", len(pods.Items))
	for _, pod := range pods.Items {
		fmt.Printf("  - %s (phase=%s)\n", pod.Name, pod.Status.Phase)
	}
}

func TestListServices(t *testing.T) {
	client := getClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	services, err := client.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list services: %v", err)
	}

	fmt.Printf("Found %d services\n", len(services.Items))
	for _, svc := range services.Items {
		fmt.Printf("  - %s/%s (type=%s, clusterIP=%s)\n",
			svc.Namespace, svc.Name, svc.Spec.Type, svc.Spec.ClusterIP)
	}
}

func TestGetPod(t *testing.T) {
	client := getClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pods, err := client.CoreV1().Pods("default").List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		t.Fatalf("list pods: %v", err)
	}

	if len(pods.Items) == 0 {
		t.Skip("no pods in default namespace to test Get")
	}

	podName := pods.Items[0].Name
	pod, err := client.CoreV1().Pods("default").Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pod %s: %v", podName, err)
	}

	fmt.Printf("Got pod: %s/%s (phase=%s)\n", pod.Namespace, pod.Name, pod.Status.Phase)
}

func TestListNamespaces(t *testing.T) {
	client := getClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list namespaces: %v", err)
	}

	fmt.Printf("Found %d namespaces\n", len(namespaces.Items))
	for _, ns := range namespaces.Items {
		fmt.Printf("  - %s (phase=%s)\n", ns.Name, ns.Status.Phase)
	}
}

func TestListNodes(t *testing.T) {
	client := getClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}

	fmt.Printf("Found %d nodes\n", len(nodes.Items))
	for _, node := range nodes.Items {
		fmt.Printf("  - %s\n", node.Name)
		for _, addr := range node.Status.Addresses {
			fmt.Printf("    %s: %s\n", addr.Type, addr.Address)
		}
	}
}

func TestWatchPods(t *testing.T) {
	client := getClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	watcher, err := client.CoreV1().Pods("default").Watch(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("watch pods: %v", err)
	}
	defer watcher.Stop()

	fmt.Println("Watch started, waiting for events (10s timeout)...")

	timeout := time.After(5 * time.Second)
	select {
	case event, ok := <-watcher.ResultChan():
		if !ok {
			fmt.Println("Watch channel closed")
			return
		}
		pod, ok := event.Object.(*corev1.Pod)
		if !ok {
			fmt.Printf("Got event type=%s (non-pod object)\n", event.Type)
			return
		}
		fmt.Printf("Watch event: type=%s pod=%s/%s phase=%s\n",
			event.Type, pod.Namespace, pod.Name, pod.Status.Phase)
	case <-timeout:
		fmt.Println("No watch events in 5s (this is normal if cluster is idle)")
	}
}
