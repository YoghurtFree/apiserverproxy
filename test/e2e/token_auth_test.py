#!/usr/bin/env python3
"""
Token 鉴权 E2E 测试 - Python k8s client 版本
测试场景：GET 缓存命中、GET 缓存未命中、非 GET 操作（带 token）
"""

import json
import sys
from kubernetes import client, config

PROXY_HOST = "http://localhost:8080/clusters/minikube"
TEST_NS = "test-auth-py-e2e"

# 从 config.json 读取 token
with open("config.json") as f:
    cfg = json.load(f)
TOKEN = cfg["clusters"][0]["token"]

# 权限不足的 token (limited-sa，无 ClusterRoleBinding)
LIMITED_TOKEN = "eyJhbGciOiJSUzI1NiIsImtpZCI6IkIyYjNJZmxOdEZLeG1zRVhQTEtDQzJpZTJjLWg5QU9JcDBUMkZWQ24tNmsifQ.eyJhdWQiOlsiaHR0cHM6Ly9rdWJlcm5ldGVzLmRlZmF1bHQuc3ZjLmNsdXN0ZXIubG9jYWwiXSwiZXhwIjoxODEzMzA0MzE3LCJpYXQiOjE3ODE3NjgzMTcsImlzcyI6Imh0dHBzOi8va3ViZXJuZXRlcy5kZWZhdWx0LnN2Yy5jbHVzdGVyLmxvY2FsIiwianRpIjoiMzU3YjMyM2UtZDJkZi00ZmEwLWI5ZGItOGVkOWNjYWNhNzc3Iiwia3ViZXJuZXRlcy5pbyI6eyJuYW1lc3BhY2UiOiJkZWZhdWx0Iiwic2VydmljZWFjY291bnQiOnsibmFtZSI6ImxpbWl0ZWQtc2EiLCJ1aWQiOiJhYTg4ZTYwZi0wYjliLTQ3NGMtOWI3Zi1iZmJjNmRhZTQzYjAifX0sIm5iZiI6MTc4MTc2ODMxNywic3ViIjoic3lzdGVtOnNlcnZpY2VhY2NvdW50OmRlZmF1bHQ6bGltaXRlZC1zYSJ9.VVtG-yBRXOfyz07wedcpYnxdcRPGyPFflDbuuCLUI1B6jhhEbjTb-xF_U_hJ8yCa6xJ60eRY7PBqGPqqeoWbYNI22-fMS4LPv_yuOb1L6aLoLD4yUyseMEAcHaIMneXlDdyS4Fr5JIZAYAlq1B2QP4PW873WklWVJ7vCXi4I2wZSUpF4I5-b0qYAqa92jG5Vs8enIgnzrY7JkP0yby0YcpzdruSvH_kasN59PLYLPMsqzF_viNI4oGNQD9I4XsKZR1znCCW6Af0gsrP8uJmOYRjFmFErXJoBMmRQ4chla78qqQUQ6BIl4br327xlBJC6U9il9iGWc3F5hIWlHv1YfQ"

PASS = 0
FAIL = 0
GREEN = "\033[92m"
RED = "\033[91m"
RESET = "\033[0m"


def make_client(token=None):
    """创建指向代理的 k8s client"""
    c = client.Configuration()
    c.host = PROXY_HOST
    c.verify_ssl = False
    if token:
        c.api_key = {"authorization": f"Bearer {token}"}
    return client.ApiClient(c)


def test_case(name, func):
    global PASS, FAIL
    try:
        func()
        print(f"{GREEN}PASS{RESET}: {name}")
        PASS += 1
    except AssertionError as e:
        print(f"{RED}FAIL{RESET}: {name} - {e}")
        FAIL += 1
    except Exception as e:
        print(f"{RED}FAIL{RESET}: {name} - {type(e).__name__}: {e}")
        FAIL += 1


def main():
    global PASS, FAIL

    print("=" * 50)
    print(" Token 鉴权 E2E 测试 (Python k8s client)")
    print("=" * 50)
    print()

    # 使用 config token 的客户端（用于 GET 请求和缓存测试）
    api = client.CoreV1Api(make_client(TOKEN))

    # --- 1. list pods（命中缓存） ---
    print("--- 1. list pods (缓存命中) ---")
    # 预热缓存
    try:
        api.list_pod_for_all_namespaces()
    except Exception:
        pass

    def test_list_pods():
        pods = api.list_pod_for_all_namespaces()
        assert pods.items is not None, "items should not be None"
        assert len(pods.items) > 0, "should have at least 1 pod"

    test_case("list pods 返回 pod 列表", test_list_pods)
    print()

    # --- 2. list services（命中缓存） ---
    print("--- 2. list services (缓存命中) ---")
    try:
        api.list_service_for_all_namespaces()
    except Exception:
        pass

    def test_list_services():
        svcs = api.list_service_for_all_namespaces()
        assert svcs.items is not None, "items should not be None"

    test_case("list services 返回 service 列表", test_list_services)
    print()

    # --- 3. list namespaces（未命中缓存） ---
    print("--- 3. list namespaces (缓存未命中) ---")

    def test_list_namespaces():
        nss = api.list_namespace()
        assert nss.kind == "NamespaceList", f"expected NamespaceList, got {nss.kind}"
        names = [ns.metadata.name for ns in nss.items]
        assert "default" in names, "should contain 'default' namespace"

    test_case("list namespaces 返回 NamespaceList", test_list_namespaces)
    print()

    # --- 4. list nodes（未命中缓存） ---
    print("--- 4. list nodes (缓存未命中) ---")

    def test_list_nodes():
        nodes = api.list_node()
        assert nodes.kind == "NodeList", f"expected NodeList, got {nodes.kind}"
        assert len(nodes.items) > 0, "should have at least 1 node"

    test_case("list nodes 返回 NodeList", test_list_nodes)
    print()

    # --- 5. create namespace（非 GET，带 token） ---
    print("--- 5. create namespace (非 GET，带 token) ---")

    def test_create_ns():
        ns = client.V1Namespace(
            api_version="v1",
            kind="Namespace",
            metadata=client.V1ObjectMeta(
                name=TEST_NS,
                annotations={"can-this-namespace-delete": "true"},
            ),
        )
        try:
            result = api.create_namespace(ns)
            assert result.metadata.name == TEST_NS
        except client.exceptions.ApiException as e:
            if e.status == 409:
                pass  # already exists from previous run, acceptable
            else:
                raise

    test_case("create namespace 成功 (201 或 409)", test_create_ns)
    print()

    # --- 6. delete namespace（非 GET，带 token） ---
    print("--- 6. delete namespace (非 GET，带 token) ---")

    def test_delete_ns():
        # Ensure annotation exists (Kyverno policy requirement)
        try:
            api.patch_namespace(
                TEST_NS,
                {"metadata": {"annotations": {"can-this-namespace-delete": "true"}}},
            )
        except Exception:
            pass
        api.delete_namespace(TEST_NS)

    test_case("delete namespace 成功", test_delete_ns)
    print()

    # --- 7. 非 GET 不带 token（应返回 401） ---
    print("--- 7. create namespace 不带 token (应返回 401) ---")

    def test_create_ns_no_token():
        no_auth_api = client.CoreV1Api(make_client(token=None))
        ns = client.V1Namespace(
            api_version="v1",
            kind="Namespace",
            metadata=client.V1ObjectMeta(name="test-no-auth"),
        )
        try:
            no_auth_api.create_namespace(ns)
            raise AssertionError("expected 401 but request succeeded")
        except client.exceptions.ApiException as e:
            assert e.status == 401, f"expected 401, got {e.status}"
            body = json.loads(e.body)
            assert "Authorization" in body.get("error", ""), f"error message should mention Authorization"

    test_case("create namespace 不带 token 返回 401", test_create_ns_no_token)
    print()

    # --- 8. GET 带伪造 token（应被忽略，使用 config token） ---
    print("--- 8. GET 带伪造 token (应被忽略) ---")

    def test_get_fake_token():
        fake_api = client.CoreV1Api(make_client(token="fake-invalid-token"))
        nss = fake_api.list_namespace()
        assert nss.kind == "NamespaceList"
        names = [ns.metadata.name for ns in nss.items]
        assert "default" in names

    test_case("GET 带伪造 token 仍返回 200", test_get_fake_token)
    print()

    # --- 9. POST 带权限不足的 token（应返回 403） ---
    print("--- 9. POST 带权限不足的 token (应返回 403) ---")

    def test_create_ns_limited_token():
        limited_api = client.CoreV1Api(make_client(token=LIMITED_TOKEN))
        ns = client.V1Namespace(
            api_version="v1",
            kind="Namespace",
            metadata=client.V1ObjectMeta(name="test-limited-sa"),
        )
        try:
            limited_api.create_namespace(ns)
            raise AssertionError("expected 403 but request succeeded")
        except client.exceptions.ApiException as e:
            assert e.status == 403, f"expected 403, got {e.status}"

    test_case("POST 带权限不足的 token 返回 403", test_create_ns_limited_token)
    print()

    # --- 汇总 ---
    print("=" * 50)
    print(" 测试结果汇总")
    print("=" * 50)
    total = PASS + FAIL
    print(f"通过: {PASS} / {total}")
    print(f"失败: {FAIL} / {total}")
    if FAIL == 0:
        print(f"{GREEN}所有测试通过！{RESET}")
    else:
        print(f"{RED}存在失败的测试！{RESET}")
        sys.exit(1)


if __name__ == "__main__":
    main()
