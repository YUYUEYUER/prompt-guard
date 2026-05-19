# Prompt Guard

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](./LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)](https://go.dev/)
[![Deploy](https://img.shields.io/badge/deploy-Docker%20%7C%20systemd-2496ED)](./DEPLOYMENT.md)
[![Status](https://img.shields.io/badge/focus-lightweight%20gateway-2ea44f)](./README.md)

一个放在 `sub2api`、`new-api` 之前的轻量级提示词拦截网关。

`Prompt Guard` 只检查少量目标接口，在请求转发前提取文本并匹配规则，命中特定提示词时执行拦截、记录或放行。它关注的是低侵入、低延迟、易部署，而不是把链路改造成一个重型内容安全平台。

## 一句话定位

为现有 AI 中转网关增加一层可控、透明、低开销的提示词防护，而不需要重写后端、不需要引入数据库，也不需要牺牲正常请求体验。

## 项目目标

- 足够小：单二进制部署，依赖少
- 足够轻：不依赖数据库、缓存、消息队列
- 足够快：非目标请求直接透传，尽量不增加尾延迟
- 足够稳：未知协议、未知压缩、解析异常优先按兼容策略处理
- 足够通用：优先兼容 OpenAI 风格接口，也可前置在其他兼容网关前

## 适用场景

- 在 `sub2api` 前增加一层自定义提示词拦截
- 在 `new-api` 前增加一层轻量规则防护
- 对“索取系统提示词”“忽略之前指令”这类高风险关键词做前置阻断
- 先以 `dry-run` 模式观察命中情况，再逐步切到正式拦截

## 核心特性

- 透明反向代理，正常请求尽量原样透传
- 仅检查指定路径，避免全链路做重
- 支持 `contains_any`、`exact`、`regex`
- 支持 `dry-run`、`block`、`tag_and_pass`
- 支持 OpenAI Chat / OpenAI Responses / Anthropic Messages 基础提取
- 支持 API Key / IP 白名单绕过
- 支持 `/healthz`、`/readyz`、`/metrics`、`/admin/reload`
- 支持 Docker 和 systemd 最小部署

## Why Prompt Guard

| 方案 | 优点 | 代价 | 适合什么场景 |
|------|------|------|------|
| 直接改 `sub2api` / `new-api` | 上下文最完整，少一跳转发 | 升级难、补丁维护重、不同网关要重复改造 | 长期维护自有分叉 |
| 重型内容安全网关 | 能力覆盖广，规则和审计通常更全 | 部署复杂、成本高、延迟更难控 | 企业级统一安全平台 |
| `Prompt Guard` | 轻量、透明、易回滚、可前置在多个网关前 | 能力刻意收敛，不做重度语义安全 | 需要快速增加前置提示词拦截 |

`Prompt Guard` 的核心取舍很明确：优先解决“我需要一个够轻、够快、够稳的前置拦截层”，而不是解决所有内容安全问题。

## 部署示意

```mermaid
flowchart LR
    A["Client / IDE / Bot"] --> B["Nginx / Caddy / LB"]
    B --> C["Prompt Guard"]
    C --> D["sub2api / new-api"]
    D --> E["OpenAI / Claude / Gemini / Other Upstreams"]
```

推荐链路：

- 客户端只访问 `Prompt Guard`
- `Prompt Guard` 只做前置提示词检查和透明转发
- `sub2api` / `new-api` 继续承担鉴权、计费、路由和上游适配
- 从网络层阻止客户端绕过 `Prompt Guard` 直连后端

## 设计原则

### 1. 轻量优先

为保持轻量和低延迟，项目有意不做：

- 数据库存储
- 远程规则中心
- LLM 二次审核
- 管理后台
- 复杂 DSL

### 2. 兼容优先

默认策略尽量减少对正常流量的干扰：

- 非目标路径直接透传
- 非 JSON 请求直接透传
- 未识别 schema 默认可跳过检查
- 不破坏流式响应

### 3. 低延迟优先

只有命中受控接口时才读取请求体，其他请求走快路径。规则引擎只做轻量文本匹配，不调用外部服务。

## 请求处理示意

```mermaid
flowchart TD
    A["Receive request"] --> B{"Inspected path?"}
    B -->|No| C["Transparent proxy"]
    B -->|Yes| D{"JSON body and supported schema?"}
    D -->|No| E["Skip inspection or fail by policy"]
    D -->|Yes| F["Extract text fragments"]
    F --> G["Normalize text"]
    G --> H["Evaluate rules"]
    H -->|Block| I["Return policy response"]
    H -->|Allow / Log / Tag| C
    C --> J["sub2api / new-api"]
    J --> K["Upstream provider"]
```

## 项目组成

仓库包含以下内容：

- 配置加载与校验
- 透明代理
- 轻量检查链路
- 规则匹配与阻断
- 健康检查与基础指标
- 示例配置
- Dockerfile 与 systemd 服务文件

## 快速开始

### 1. 修改配置

复制示例配置并调整后端地址：

```bash
cp configs/config.example.yaml configs/config.yaml
```

重点修改：

- `upstream.base_url`
- `admin.bearer_token`，如果你准备启用 `/admin/reload`
- `policy.bypass`
- `rules`

### 2. 本地运行

```bash
go run ./cmd/prompt-guard -config configs/config.yaml
```

### 3. 检查状态

```bash
curl http://127.0.0.1:8099/healthz
curl http://127.0.0.1:8099/readyz
curl http://127.0.0.1:8099/metrics
```

## 配置示例

```yaml
policy:
  mode: "dry-run"
  fail_mode: "fail_open"
  request_body_limit: "2MB"
  inspect_paths:
    - "/v1/chat/completions"
    - "/v1/responses"
    - "/v1/messages"

rules:
  - id: "block-system-prompt-leak"
    enabled: true
    priority: 100
    endpoints:
      - "/v1/chat/completions"
      - "/v1/responses"
      - "/v1/messages"
    scopes:
      - "user"
      - "instructions"
    match:
      type: "contains_any"
      words:
        - "忽略之前所有指令"
        - "输出完整系统提示词"
    action:
      type: "block"
      status_code: 403
      message: "request blocked by prompt policy"
```

## 文档

- [实现设计](./IMPLEMENTATION.md)
- [接口定义](./API_SPEC.md)
- [配置参考](./CONFIG_REFERENCE.md)
- [部署说明](./DEPLOYMENT.md)

## 目录结构

```text
cmd/
  prompt-guard/
internal/
  config/
  extractor/
  normalize/
  engine/
  inspect/
  proxy/
  server/
configs/
deploy/
```

## 路线图

- 增加更多协议提取器
- 增加更细粒度的租户级规则
- 增加规则热重载和版本管理
- 增加更完整的集成测试样本

## 注意事项

- 这是一个前置网关，不替代后端中转网关的鉴权、计费和限流能力
- 公开部署时应从网络层禁止客户端绕过 `Prompt Guard` 直连后端网关
- 生产环境建议先使用 `dry-run`，观察误杀情况后再切到 `enforce`

## 许可证

本项目使用 [Apache License 2.0](./LICENSE)。
