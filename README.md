# apiserverproxy

Kubernetes API Server 透明代理，支持多集群、Watch 流式请求和本地缓存。

## 功能特性

- **多集群代理**：通过 config.json 配置多个集群，请求路径自动路由
- **SDK 兼容**：支持 Kubernetes Python SDK、Java SDK、client-go 等官方 SDK 直接接入
- **Watch 支持**：自动检测 `?watch=true`，使用 chunked 流式传输
- **本地缓存**：Pod 和 Service 列表请求走本地缓存，减少 API server 压力
- **labelSelector / fieldSelector**：完整支持 Kubernetes 原生过滤语法

## 快速开始

### 1. 构建

```bash
make build
```

### 2. 配置

创建 `config.json`：

```json
{
  "clusters": [
    {
      "name": "cluster1",
      "server": "https://10.0.0.1:6443",
      "token": "eyJhbGciOiJSUzI1NiIs..."
    },
    {
      "name": "cluster2",
      "server": "https://10.0.0.2:6443",
      "token": "eyJhbGciOiJSUzI1NiIs..."
    }
  ]
}
```

获取 token：

```bash
kubectl create token <service-account> -n default --duration=24h
```

### 3. 启动

```bash
./bin/apiserverproxy --config-file=config.json --listen=:8080
```

## 路由格式

代理支持两种路由格式：

| 格式 | 路径 | 用途 |
|------|------|------|
| 原有模式 | `/{cluster}/api/v1/pods` | curl / HTTP 客户端 |
| SDK 模式 | `/clusters/{cluster}/api/v1/pods` | K8s SDK (client-go / Python / Java) |

两种格式功能完全一致，SDK 模式是为了兼容 Kubernetes 官方 SDK 的 URL 拼接规则。

## 使用示例

### curl / HTTP 客户端

```bash
# 列出所有 Pod（走缓存）
curl http://localhost:8080/cluster1/api/v1/pods

# 指定命名空间
curl http://localhost:8080/cluster1/api/v1/namespaces/default/pods

# 使用 labelSelector
curl "http://localhost:8080/cluster1/api/v1/pods?labelSelector=app=nginx"

# 使用 fieldSelector
curl "http://localhost:8080/cluster1/api/v1/pods?fieldSelector=spec.nodeName=node1"

# 列出 Service（走缓存）
curl http://localhost:8080/cluster1/api/v1/services

# Watch 请求（不走缓存）
curl -N "http://localhost:8080/cluster1/api/v1/pods?watch=true"

# 其他资源（直接代理）
curl http://localhost:8080/cluster1/api/v1/namespaces
curl http://localhost:8080/cluster1/apis/apps/v1/deployments
```

### Kubernetes Python SDK

安装：

```bash
pip install kubernetes
```

使用：

```python
from kubernetes import client, config

# 配置代理地址
configuration = client.Configuration()
configuration.host = "http://localhost:8080/clusters/cluster1"
configuration.api_key = {"authorization": "Bearer <your-token>"}
configuration.verify_ssl = False  # 代理使用 HTTP

api = client.CoreV1Api(client.ApiClient(configuration))

# 列出所有 Pod
pods = api.list_pod_for_all_namespaces()
for pod in pods.items:
    print(f"{pod.metadata.namespace}/{pod.metadata.name} ({pod.status.phase})")

# 列出指定命名空间的 Service
services = api.list_namespaced_service("default")
for svc in services.items:
    print(f"{svc.metadata.name} ({svc.spec.type})")

# 使用 labelSelector 过滤
pods = api.list_pod_for_all_namespaces(label_selector="app=nginx")

# 使用 fieldSelector 过滤
pods = api.list_pod_for_all_namespaces(field_selector="status.phase=Running")

# Get 单个 Pod
pod = api.read_namespaced_pod("my-pod", "default")

# Watch
w = watch.W()
for event in w.stream(api.list_pod_for_all_namespaces, timeout_seconds=60):
    print(f"Event: {event['type']} {event['object'].metadata.name}")
```

### Kubernetes Java SDK

Maven 依赖：

```xml
<dependency>
    <groupId>io.kubernetes</groupId>
    <artifactId>client-java</artifactId>
    <version>22.0.0</version>
</dependency>
```

使用：

```java
import io.kubernetes.client.openapi.ApiClient;
import io.kubernetes.client.openapi.ApiException;
import io.kubernetes.client.openapi.Configuration;
import io.kubernetes.client.openapi.apis.CoreV1Api;
import io.kubernetes.client.openapi.models.V1PodList;
import io.kubernetes.client.openapi.models.V1ServiceList;
import io.kubernetes.client.util.Config;

ApiClient client = Config.defaultClient();

// 配置代理地址
client.setBasePath("http://localhost:8080/clusters/cluster1");
client.setApiKey("Bearer <your-token>");
client.setVerifyingSsl(false);  // 代理使用 HTTP

Configuration.setDefaultClient(client);
CoreV1Api api = new CoreV1Api();

// 列出所有 Pod
V1PodList pods = api.listPodForAllNamespaces()
    .execute();
pods.getItems().forEach(pod ->
    System.out.printf("%s/%s (%s)%n",
        pod.getMetadata().getNamespace(),
        pod.getMetadata().getName(),
        pod.getStatus().getPhase())
);

// 列出 Service
V1ServiceList services = api.listNamespacedService("default")
    .execute();

// 使用 labelSelector 过滤
pods = api.listPodForAllNamespaces()
    .labelSelector("app=nginx")
    .execute();

// 使用 fieldSelector 过滤
pods = api.listPodForAllNamespaces()
    .fieldSelector("status.phase=Running")
    .execute();
```

### client-go

```go
import (
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

cfg := &rest.Config{
    Host:        "http://localhost:8080/clusters/cluster1",
    BearerToken: "<your-token>",
    TLSClientConfig: rest.TLSClientConfig{
        Insecure: true,
    },
}

client, err := kubernetes.NewForConfig(cfg)

// 列出所有 Pod
pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})

// 列出指定命名空间的 Service
services, err := client.CoreV1().Services("default").List(ctx, metav1.ListOptions{})

// 使用 labelSelector 过滤
pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{
    LabelSelector: "app=nginx",
})

// Watch
watcher, err := client.CoreV1().Pods("").Watch(ctx, metav1.ListOptions{})
for event := range watcher.ResultChan() {
    pod := event.Object.(*corev1.Pod)
    fmt.Printf("%s %s/%s\n", event.Type, pod.Namespace, pod.Name)
}
```

## 缓存说明

缓存命中时响应头包含 `X-Cache: HIT`。

| 请求类型 | 是否走缓存 |
|---------|-----------|
| `GET /api/v1/pods` | 是 |
| `GET /api/v1/services` | 是 |
| `GET /api/v1/pods?watch=true` | 否 |
| `GET /api/v1/namespaces` | 否 |
| `POST/PUT/DELETE` | 否 |

支持的 fieldSelector：

| 资源 | 支持的字段 |
|------|-----------|
| Pod | metadata.name、spec.nodeName、status.phase |
| Service | metadata.name |

## 认证说明

代理使用 config.json 中配置的 token 向 Kubernetes API Server 认证。客户端通过 SDK 或 curl 发送的 token 会被代理覆盖，实际使用的是 config.json 中的 token。

## 命令行参数

```
--config-file    集群配置文件路径（必填）
--listen         监听地址（默认 :8080）
```
