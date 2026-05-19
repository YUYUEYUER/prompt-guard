# 前置提示词拦截网关实现设计

## 1. 文档目标

本文档将总体方案继续下钻到可编码层面，明确以下内容：

- 推荐技术栈
- 代码目录结构
- 模块职责与调用关系
- 核心数据流
- 开发阶段拆解
- 测试与上线建议

默认项目代号为 `prompt-guard`。

## 2. 实现目标

MVP 目标是实现一个独立部署的 Go 服务，具备以下能力：

- 对指定 AI 接口做透明代理
- 在请求转发前提取提示词文本
- 基于配置化规则执行匹配
- 命中后可阻断、仅记录或打标放行
- 输出结构化日志和基础运行指标

## 2.1 非功能实现约束

这一版实现必须同时满足下面的工程约束：

- 小：单进程、单二进制、少依赖
- 轻：无数据库、无后台、无异步任务前提
- 稳：默认 `fail_open`
- 快：只在必要时解析请求体
- 透：放行路径尽量透明，不改变上游和客户端体验

## 3. 推荐技术栈

### 3.1 语言与运行时

- `Go 1.22+`

理由：

- 单二进制部署简单
- 处理 HTTP 代理和 JSON 解析足够成熟
- 后续若需要内嵌到 `sub2api` 或 `new-api`，迁移成本较低

### 3.2 HTTP 实现

推荐优先使用标准库：

- `net/http`
- `httputil.ReverseProxy`

原因：

- 依赖少
- 行为稳定
- 便于对请求前后做中间件包装
- 对 SSE 和常见反向代理场景兼容更稳

### 3.3 配置与日志

- YAML 配置：`gopkg.in/yaml.v3`
- 结构化日志：`log/slog`
- 指标：`prometheus/client_golang`

不建议在第一版引入：

- ORM
- Redis
- MQ
- 热更新框架
- Web 管理后台

### 3.4 测试

- 单元测试：Go `testing`
- 接口测试：`httptest`
- 回归样本：`testdata/`

## 4. 目录结构建议

建议第一版直接采用清晰的单仓布局：

```text
prompt-guard/
├─ cmd/
│  └─ prompt-guard/
│     └─ main.go
├─ configs/
│  ├─ config.example.yaml
│  └─ rules.example.yaml
├─ internal/
│  ├─ app/
│  │  └─ bootstrap.go
│  ├─ audit/
│  │  ├─ logger.go
│  │  └─ sink.go
│  ├─ config/
│  │  ├─ loader.go
│  │  ├─ model.go
│  │  └─ validate.go
│  ├─ engine/
│  │  ├─ engine.go
│  │  ├─ matcher.go
│  │  └─ result.go
│  ├─ extractor/
│  │  ├─ openai_chat.go
│  │  ├─ openai_responses.go
│  │  ├─ anthropic.go
│  │  ├─ gemini.go
│  │  └─ registry.go
│  ├─ inspect/
│  │  ├─ service.go
│  │  └─ types.go
│  ├─ normalize/
│  │  └─ normalizer.go
│  ├─ proxy/
│  │  ├─ director.go
│  │  └─ reverse_proxy.go
│  ├─ server/
│  │  ├─ router.go
│  │  ├─ handlers.go
│  │  └─ middleware.go
│  └─ version/
│     └─ version.go
├─ testdata/
│  ├─ requests/
│  ├─ rules/
│  └─ golden/
├─ go.mod
├─ go.sum
└─ README.md
```

## 5. 模块职责

### 5.1 `cmd/prompt-guard`

职责：

- 读取启动参数
- 加载配置
- 初始化日志、规则引擎、代理和 HTTP 服务
- 优雅启动与关闭

### 5.2 `internal/config`

职责：

- 读取 YAML 配置
- 完成默认值注入
- 完成配置校验
- 向其他模块暴露强类型配置对象

### 5.3 `internal/server`

职责：

- 注册 HTTP 路由
- 组装中间件
- 挂接业务处理器

推荐内置端点：

- `GET /healthz`
- `GET /readyz`
- `GET /metrics`
- `POST /admin/reload`

### 5.4 `internal/extractor`

职责：

- 按接口和协议解析请求体
- 提取可被规则引擎检查的文本片段
- 输出统一数据结构

### 5.5 `internal/normalize`

职责：

- 对文本片段做归一化
- 输出归一化文本并保留原文引用

### 5.6 `internal/engine`

职责：

- 对文本片段应用规则集
- 计算最终动作
- 返回命中详情

### 5.7 `internal/inspect`

职责：

- 串联 `extractor -> normalize -> engine`
- 处理单个请求的检查流程
- 产出代理阶段需要的决策结果

### 5.8 `internal/proxy`

职责：

- 基于 `ReverseProxy` 透传请求
- 在需要时附加调试 Header 或上下文
- 处理后端错误映射

### 5.9 `internal/audit`

职责：

- 输出结构化审计日志
- 支持后续扩展为文件、数据库、消息队列等 sink

## 6. 运行时调用链

### 6.1 正常放行路径

```text
HTTP Request
  -> middleware
  -> inspect service
  -> no rule hit
  -> reverse proxy
  -> upstream response
  -> client
```

### 6.2 命中拦截路径

```text
HTTP Request
  -> middleware
  -> inspect service
  -> rule hit with block action
  -> audit log
  -> blocked JSON response
  -> client

## 6.3 极简快速路径

为了把延迟压到最低，运行时应严格区分“快路径”和“慢路径”。

快路径：

- 路径不在 `inspect_paths` 中
- 请求方法不是 `POST`
- `Content-Type` 不是 JSON
- 命中白名单绕过

以上情况必须直接透传，不读取请求体，不进入 JSON 解析。

慢路径：

- 仅在命中受管控接口后才读取请求体
- 仅在请求体小于限制时才尝试解析
- 仅提取必要字段，不做完整协议重建
```

## 7. 核心数据结构建议

### 7.1 待检查片段

```go
type TextFragment struct {
    Scope      string
    Path       string
    Role       string
    Original   string
    Normalized string
}
```

说明：

- `Scope` 表示规则作用域，如 `user`、`system`
- `Path` 表示原字段位置，如 `messages[1].content`
- `Role` 用于补充消息角色
- `Original` 保留原始文本
- `Normalized` 为归一化后文本

### 7.2 匹配结果

```go
type MatchResult struct {
    Matched      bool
    RuleID       string
    Action       string
    Scope        string
    Path         string
    Evidence     string
    StatusCode   int
    ResponseBody string
}
```

### 7.3 检查结果

```go
type InspectionResult struct {
    Decision       string
    MatchedRules    []MatchResult
    FragmentsCount  int
    DurationMs      int64
}
```

## 8. 请求处理设计

### 8.1 请求体读取策略

第一版建议采用“完整读取后再转发”的模式：

1. 读取请求体到内存
2. 限制最大大小
3. 进行检查
4. 若放行，则重新构造 `io.ReadCloser` 供代理转发

优点：

- 实现简单
- 便于审计和测试

代价：

- 增加一次内存拷贝

对于 MVP 这是可接受的。

补充约束：

- 只对受管控路径执行此策略
- 对超大请求不尝试深解析
- 若存在 `Content-Length` 且已超阈值，可提前拒绝或按策略放行

### 8.2 检查前置条件

只有满足以下条件时才触发检查：

- 请求路径命中受管控接口
- 请求方法为 `POST`
- `Content-Type` 为 JSON 或兼容 JSON
- 请求体未超过限制

否则直接透传。

补充建议：

- 若 `Content-Encoding` 为当前实现不支持的压缩格式，默认直接透传
- 若协议结构未知，默认直接透传而非强行拦截

### 8.3 代理 Header 策略

默认保留所有原始 Header，另外可新增：

- `X-Request-Id`
- `X-Prompt-Guard-Decision`
- `X-Prompt-Guard-Hits`

默认不建议把命中关键词明文通过 Header 传递给后端。

### 8.4 响应透传策略

为了不影响用户体验，响应侧遵循以下原则：

- 非阻断请求必须原样返回上游状态码
- 保留流式输出，不缓存完整响应体
- 不对 SSE 分片做二次包装
- 不修改正常业务响应 JSON 结构

## 9. 规则执行策略

### 9.1 规则优先级

建议规则按 `priority` 从高到低执行。

### 9.2 多规则命中处理

MVP 建议策略：

- 收集全部命中
- 决策时取最强动作

动作强度建议：

```text
block > tag_and_pass > log_only > allow
```

### 9.3 决策合并

如果多条规则同时命中：

- 若存在 `block`，最终结果为 `block`
- 否则若存在 `tag_and_pass`，最终结果为 `tag_and_pass`
- 否则为 `log_only`

## 9.4 轻量化规则边界

为了保持低延迟，规则能力有意收敛：

- 第一版支持 `contains_any`、`exact`、`regex`、`fuzzy_contains_any`
- 不支持嵌套布尔表达式
- 不支持跨字段上下文推理
- 不支持语义模型调用
- 不支持远程规则查询

这不是能力不足，而是主动控制复杂度和尾延迟。

## 10. 观察与诊断

### 10.1 日志分类

- 访问日志
- 审计日志
- 代理错误日志
- 配置加载日志

### 10.2 建议日志格式

统一输出 JSON，便于接入 Loki、ELK、Datadog 等系统。

### 10.3 调试模式

建议提供可选 `debug` 开关，用于：

- 输出提取到的片段数量
- 输出匹配到的规则 ID
- 输出配置版本

不要在 debug 日志中无保护打印完整提示词原文。

## 10.4 延迟预算建议

MVP 建议把额外延迟目标控制在：

- 非检查请求：接近 0 额外业务逻辑开销
- 检查请求：常规小请求尽量控制在单毫秒到低毫秒级附加开销

为了实现这一点，应遵循：

- 不做外部网络调用
- 不做磁盘同步写入阻塞主链路
- 不做多次 JSON 编解码
- 不做复杂字符串分词

## 11. 开发阶段拆解

### 11.1 阶段一：基础骨架

交付物：

- 可启动的 Go HTTP 服务
- 健康检查接口
- 基础反向代理能力
- 配置文件加载

### 11.2 阶段二：检查链路

交付物：

- OpenAI Chat 提取器
- OpenAI Responses 提取器
- 基础归一化
- `contains_any` 和 `regex` 规则匹配

### 11.3 阶段三：审计与控制

交付物：

- 结构化审计日志
- `dry-run` / `enforce` 模式
- API key 白名单绕过
- 基础指标

### 11.4 阶段四：增强兼容

交付物：

- Anthropic / Gemini 提取器
- 热重载
- 管理接口保护
- 压测与性能调优

## 12. 测试设计

### 12.1 单元测试

重点覆盖：

- 文本归一化
- 规则匹配
- 配置校验
- 各提取器对请求体的解析

### 12.2 集成测试

重点覆盖：

- 未命中规则时正常转发
- 命中规则时返回阻断结果
- 规则为 `log_only` 时仍可访问后端
- 后端不可用时代理错误处理

### 12.3 回归样本

建议维护一组固定 JSON 样本：

- 正常请求
- 直接命中关键词
- 空格拆分绕过样本
- 零宽字符绕过样本
- 非法 JSON

## 13. 安全与运维要求

### 13.1 请求体大小限制

必须限制最大请求体，例如：

- 默认 `2MB`
- 可配置

### 13.2 管理接口保护

`/admin/reload` 不应直接暴露公网，建议：

- 仅监听内网
- 或要求管理 token

### 13.3 直连绕过防护

必须从网络层限制客户端只能访问 `prompt-guard`，不能直接访问 `sub2api` 或 `new-api`。

## 13.4 极简部署要求

生产部署建议维持极简：

- 一个主监听端口
- 一个配置文件
- 一个可执行文件
- 一个 systemd 服务或一个 Docker 容器

避免第一版引入：

- 多组件编排依赖
- 分布式配置中心
- 专用审计数据库
- 复杂 sidecar

## 14. 第一版实现顺序建议

推荐先按下面顺序编码：

1. `config`
2. `server`
3. `proxy`
4. `extractor/openai_chat`
5. `normalize`
6. `engine`
7. `inspect`
8. `audit`
9. `metrics`

这样可以尽快形成“可启动、可转发、可拦截”的最短闭环。

## 15. 实现完成后的验收标准

满足以下条件即可认为 MVP 合格：

- 配置加载成功后服务可启动
- `/v1/chat/completions` 可被透明代理
- 命中指定规则时能正确返回拦截 JSON
- `dry-run` 模式下只记录不阻断
- 结构化日志可看到规则命中详情
- 对非法 JSON 和超限请求有稳定行为

另外还应满足以下体验约束：

- 非目标接口不受影响
- 流式响应不被破坏
- 普通请求体感延迟变化尽量不可察觉
- 整体部署和回滚足够简单
