#!/bin/bash
# Token 鉴权 E2E 测试 - curl 版本
# 测试场景：GET 缓存命中、GET 缓存未命中、非 GET 无 token、非 GET 有 token

set -e

PROXY_URL="http://localhost:8080/clusters/minikube"
PASS=0
FAIL=0
NO_PROXY="--noproxy localhost"

# 从 config.json 提取 token
TOKEN=$(python3 -c "import json; print(json.load(open('config.json'))['clusters'][0]['token'])")

# 权限不足的 token (limited-sa，无 ClusterRoleBinding)
LIMITED_TOKEN="eyJhbGciOiJSUzI1NiIsImtpZCI6IkIyYjNJZmxOdEZLeG1zRVhQTEtDQzJpZTJjLWg5QU9JcDBUMkZWQ24tNmsifQ.eyJhdWQiOlsiaHR0cHM6Ly9rdWJlcm5ldGVzLmRlZmF1bHQuc3ZjLmNsdXN0ZXIubG9jYWwiXSwiZXhwIjoxODEzMzA0MzE3LCJpYXQiOjE3ODE3NjgzMTcsImlzcyI6Imh0dHBzOi8va3ViZXJuZXRlcy5kZWZhdWx0LnN2Yy5jbHVzdGVyLmxvY2FsIiwianRpIjoiMzU3YjMyM2UtZDJkZi00ZmEwLWI5ZGItOGVkOWNjYWNhNzc3Iiwia3ViZXJuZXRlcy5pbyI6eyJuYW1lc3BhY2UiOiJkZWZhdWx0Iiwic2VydmljZWFjY291bnQiOnsibmFtZSI6ImxpbWl0ZWQtc2EiLCJ1aWQiOiJhYTg4ZTYwZi0wYjliLTQ3NGMtOWI3Zi1iZmJjNmRhZTQzYjAifX0sIm5iZiI6MTc4MTc2ODMxNywic3ViIjoic3lzdGVtOnNlcnZpY2VhY2NvdW50OmRlZmF1bHQ6bGltaXRlZC1zYSJ9.VVtG-yBRXOfyz07wedcpYnxdcRPGyPFflDbuuCLUI1B6jhhEbjTb-xF_U_hJ8yCa6xJ60eRY7PBqGPqqeoWbYNI22-fMS4LPv_yuOb1L6aLoLD4yUyseMEAcHaIMneXlDdyS4Fr5JIZAYAlq1B2QP4PW873WklWVJ7vCXi4I2wZSUpF4I5-b0qYAqa92jG5Vs8enIgnzrY7JkP0yby0YcpzdruSvH_kasN59PLYLPMsqzF_viNI4oGNQD9I4XsKZR1znCCW6Af0gsrP8uJmOYRjFmFErXJoBMmRQ4chla78qqQUQ6BIl4br327xlBJC6U9il9iGWc3F5hIWlHv1YfQ"

GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

assert_status() {
    local name="$1" expected="$2" actual="$3"
    if [ "$actual" = "$expected" ]; then
        echo -e "${GREEN}PASS${NC}: $name (HTTP $actual)"
        PASS=$((PASS + 1))
    else
        echo -e "${RED}FAIL${NC}: $name (expected $expected, got $actual)"
        FAIL=$((FAIL + 1))
    fi
}

assert_contains() {
    local name="$1" expected="$2" actual="$3"
    if echo "$actual" | grep -q "$expected"; then
        echo -e "${GREEN}PASS${NC}: $name"
        PASS=$((PASS + 1))
    else
        echo -e "${RED}FAIL${NC}: $name (expected to contain '$expected')"
        FAIL=$((FAIL + 1))
    fi
}

assert_not_contains() {
    local name="$1" unexpected="$2" actual="$3"
    if echo "$actual" | grep -q "$unexpected"; then
        echo -e "${RED}FAIL${NC}: $name (should NOT contain '$unexpected')"
        FAIL=$((FAIL + 1))
    else
        echo -e "${GREEN}PASS${NC}: $name"
        PASS=$((PASS + 1))
    fi
}

echo "=========================================="
echo " Token 鉴权 E2E 测试 (curl)"
echo "=========================================="
echo ""

# ------ 1. GET 命中缓存 (list pods) ------
echo "--- 1. GET list pods (缓存命中) ---"
# 第一次请求预热缓存
curl -s $NO_PROXY "$PROXY_URL/api/v1/pods" > /dev/null 2>&1
# 第二次请求应该命中缓存
RESP=$(curl -s $NO_PROXY -D /tmp/headers_pods "$PROXY_URL/api/v1/pods")
HEADERS=$(cat /tmp/headers_pods)
STATUS=$(head -1 /tmp/headers_pods | awk '{print $2}')
assert_status "GET /api/v1/pods 返回 200" "200" "$STATUS"
assert_contains "GET /api/v1/pods 响应头含 X-Cache: HIT" "X-Cache: HIT" "$HEADERS"
assert_contains "GET /api/v1/pods 返回 pod 数据" '"items"' "$RESP"
echo ""

# ------ 2. GET 命中缓存 (list services) ------
echo "--- 2. GET list services (缓存命中) ---"
curl -s $NO_PROXY "$PROXY_URL/api/v1/services" > /dev/null 2>&1
RESP=$(curl -s $NO_PROXY -D /tmp/headers_svc "$PROXY_URL/api/v1/services")
HEADERS=$(cat /tmp/headers_svc)
STATUS=$(head -1 /tmp/headers_svc | awk '{print $2}')
assert_status "GET /api/v1/services 返回 200" "200" "$STATUS"
assert_contains "GET /api/v1/services 响应头含 X-Cache: HIT" "X-Cache: HIT" "$HEADERS"
echo ""

# ------ 3. GET 未命中缓存 (list namespaces) ------
echo "--- 3. GET list namespaces (缓存未命中) ---"
RESP=$(curl -s $NO_PROXY -D /tmp/headers_ns "$PROXY_URL/api/v1/namespaces")
HEADERS=$(cat /tmp/headers_ns)
STATUS=$(head -1 /tmp/headers_ns | awk '{print $2}')
assert_status "GET /api/v1/namespaces 返回 200" "200" "$STATUS"
assert_not_contains "GET /api/v1/namespaces 无 X-Cache 头" "X-Cache" "$HEADERS"
echo ""

# ------ 4. GET 未命中缓存 (list nodes) ------
echo "--- 4. GET list nodes (缓存未命中) ---"
RESP=$(curl -s $NO_PROXY -D /tmp/headers_nodes "$PROXY_URL/api/v1/nodes")
HEADERS=$(cat /tmp/headers_nodes)
STATUS=$(head -1 /tmp/headers_nodes | awk '{print $2}')
assert_status "GET /api/v1/nodes 返回 200" "200" "$STATUS"
assert_not_contains "GET /api/v1/nodes 无 X-Cache 头" "X-Cache" "$HEADERS"
echo ""

# ------ 5. GET 带自定义 token（应被忽略，使用 config token） ------
echo "--- 5. GET 带伪造 token (应被忽略) ---"
RESP=$(curl -s $NO_PROXY -H "Authorization: Bearer fake-invalid-token" -D /tmp/headers_fake "$PROXY_URL/api/v1/namespaces")
STATUS=$(head -1 /tmp/headers_fake | awk '{print $2}')
assert_status "GET 带伪造 token 仍返回 200 (使用 config token)" "200" "$STATUS"
echo ""

# ------ 6. POST 不带 token（应返回 401） ------
echo "--- 6. POST 不带 token (应返回 401) ---"
RESP=$(curl -s $NO_PROXY -X POST -H "Content-Type: application/json" \
    -d '{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"test-auth-e2e"}}' \
    -D /tmp/headers_post_noauth \
    "$PROXY_URL/api/v1/namespaces")
STATUS=$(head -1 /tmp/headers_post_noauth | awk '{print $2}')
assert_status "POST 不带 token 返回 401" "401" "$STATUS"
assert_contains "响应体包含 error 字段" '"error"' "$RESP"
echo ""

# ------ 7. DELETE 不带 token（应返回 401） ------
echo "--- 7. DELETE 不带 token (应返回 401) ---"
RESP=$(curl -s $NO_PROXY -X DELETE -D /tmp/headers_del_noauth "$PROXY_URL/api/v1/namespaces/test-auth-e2e")
STATUS=$(head -1 /tmp/headers_del_noauth | awk '{print $2}')
assert_status "DELETE 不带 token 返回 401" "401" "$STATUS"
echo ""

# ------ 8. POST 带 token（应通过代理鉴权） ------
echo "--- 8. POST 带正确 token ---"
RESP=$(curl -s $NO_PROXY -X POST -H "Content-Type: application/json" \
    -H "Authorization: Bearer $TOKEN" \
    -d '{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"test-auth-e2e"}}' \
    -D /tmp/headers_post_auth \
    "$PROXY_URL/api/v1/namespaces")
STATUS=$(head -1 /tmp/headers_post_auth | awk '{print $2}')
# 201 创建成功 或 409 已存在 都算通过
if [ "$STATUS" = "201" ] || [ "$STATUS" = "409" ]; then
    echo -e "${GREEN}PASS${NC}: POST 带 token 非 401 (HTTP $STATUS)"
    PASS=$((PASS + 1))
else
    echo -e "${RED}FAIL${NC}: POST 带 token 应非 401 (HTTP $STATUS)"
    FAIL=$((FAIL + 1))
fi
echo ""

# ------ 9. POST 带权限不足的 token（应返回 403） ------
echo "--- 9. POST 带权限不足的 token (应返回 403) ---"
RESP=$(curl -s $NO_PROXY -X POST -H "Content-Type: application/json" \
    -H "Authorization: Bearer $LIMITED_TOKEN" \
    -d '{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"test-limited-sa"}}' \
    -D /tmp/headers_post_limited \
    "$PROXY_URL/api/v1/namespaces")
STATUS=$(head -1 /tmp/headers_post_limited | awk '{print $2}')
assert_status "POST 带权限不足的 token 返回 403" "403" "$STATUS"
echo ""

# ------ 10. PUT 不带 token（应返回 401） ------
echo "--- 10. PUT 不带 token (应返回 401) ---"
RESP=$(curl -s $NO_PROXY -X PUT -H "Content-Type: application/json" \
    -d '{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"test-auth-e2e"}}' \
    -D /tmp/headers_put_noauth \
    "$PROXY_URL/api/v1/namespaces/test-auth-e2e")
STATUS=$(head -1 /tmp/headers_put_noauth | awk '{print $2}')
assert_status "PUT 不带 token 返回 401" "401" "$STATUS"
echo ""

# ------ 清理 ------
echo "--- 清理测试资源 ---"
curl -s $NO_PROXY -X DELETE -H "Authorization: Bearer $TOKEN" "$PROXY_URL/api/v1/namespaces/test-auth-e2e" > /dev/null 2>&1 || true
echo ""

# ------ 汇总 ------
echo "=========================================="
echo " 测试结果汇总"
echo "=========================================="
TOTAL=$((PASS + FAIL))
echo "通过: $PASS / $TOTAL"
echo "失败: $FAIL / $TOTAL"
if [ $FAIL -eq 0 ]; then
    echo -e "${GREEN}所有测试通过！${NC}"
else
    echo -e "${RED}存在失败的测试！${NC}"
    exit 1
fi
