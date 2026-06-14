# CLAUDE.md

本文件为 Claude Code 在此仓库中工作提供指导。

## 基本原则

- 所有回答尽量使用中文

## 常用命令

```bash
make build                           # 构建二进制文件
make test                            # 运行测试
go test -race ./...                  # 直接运行测试
make clean                           # 清理构建产物
```

## 项目架构

基于 Gin 框架的 Kubernetes API Server 透明代理，支持 watch 流式请求和多集群代理。

### 运行方式

```bash
./bin/apiserverproxy --config-file=config.json --listen=:8080
```

### 请求流程

```
# 原有模式
GET /{cluster}/api/v1/namespaces → 转发到对应集群

# SDK 兼容模式 (K8s Python/Java SDK 可直接使用)
GET /clusters/{cluster}/api/v1/namespaces → 转发到对应集群
```

### K8s SDK 配置示例

```python
# Python SDK
from kubernetes import client, config
configuration = client.Configuration()
configuration.host = "http://proxy:8080/clusters/minikube"
configuration.api_key = {"authorization": "Bearer <token>"}
configuration.verify_ssl = False
api = client.CoreV1Api(client.ApiClient(configuration))
pods = api.list_pod_for_all_namespaces()
```

```java
// Java SDK
ApiClient client = Config.defaultClient();
client.setBasePath("http://proxy:8080/clusters/minikube");
client.setApiKey("Bearer <token>");
client.setVerifyingSsl(false);
CoreV1Api api = new CoreV1Api(client);
V1PodList pods = api.listPodForAllNamespaces(null, null, null, null, null, null, null, null, null, null);
```

### 核心组件

- `cmd/apiserverproxy/main.go` - 入口
- `internal/config/clusters.go` - 多集群配置加载（config.json）
- `internal/proxy/multi_cluster.go` - 多集群代理处理器
- `internal/proxy/watch.go` - watch 流式处理
- `internal/cache/manager.go` - 缓存管理器（使用 controller-runtime）

### config.json 格式

```json
{
  "clusters": [
    {
      "name": "cluster-name",
      "server": "https://k8s-api:6443",
      "token": "bearer-token"
    }
  ]
}
```

### 认证方式

Token 配置在 config.json 中，自动跳过 TLS 验证。

### Watch 处理

- 检测 `?watch=true` 查询参数
- 使用 `bufio.Reader` 逐行读取
- 每个 chunk 立即 flush（无写超时）

### 缓存机制

使用 controller-runtime 实现本地缓存，减少 API server 压力：

- **缓存范围**：Pod, Service
- **缓存策略**：Informer 模式（List + Watch 自动同步）
- **使用场景**：`GET /api/v1/pods` 和 `GET /api/v1/services` 请求使用缓存
- **降级策略**：缓存失败时自动降级到 API server
- **Pod fieldSelector**：metadata.name、spec.nodeName、status.phase
- **Service fieldSelector**：metadata.name

```
启动 → 为每个集群创建 cache → 注册 field indexer → 自动 List + Watch
     ↓
用户请求 → 判断是否为 list pods/services
     ↓
是 → 解析 labelSelector + fieldSelector → 从缓存返回（Header: X-Cache: HIT）
否 → 直接代理到 API server
```
