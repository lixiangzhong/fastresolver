# Implementation Plan: Native DNS Response

**Branch**: `refactor/native-dns-response` | **Date**: 2026-08-25 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/001-native-dns-response/spec.md`

## Summary

将 `ILookup.Lookup` 及所有实现从自定义 `DNSRR` 迁移为 `(*dns.Msg, error)`，保留方法入参、resolver 装饰器模型和既有查询策略。基础 UDP/TCP 与 DoH 直接返回已解码消息；JSON DNS 适配器构造等价的原生消息；缓存、CNAME 和递归逻辑直接读取标准 DNS 区段。删除不再承担职责的自定义结果模型与转换函数，以 `/v3` 模块路径发布该破坏性变更，并提供迁移说明。

## Technical Context

**Language/Version**: Go 1.21.0

**Primary Dependencies**: `github.com/miekg/dns` v1.1.63；沿用现有 `golang-lru/v2`、`singleflight`、`zonedb`、`publicsuffix`、`ratelimit` 与 `conc`

**Storage**: 进程内 LRU DNS 响应缓存，无持久化存储

**Testing**: Go 标准库 `testing`，本地 UDP DNS 与 `httptest` HTTP/DoH fixture；`go test`、`go test -race`、`go vet`、`go build`

**Target Platform**: 支持 Go 1.21 的跨平台网络库；UDP、TCP、HTTP/HTTPS

**Project Type**: 单模块 Go library，根目录为 `fastresolver` 包

**Performance Goals**: 不增加网络往返；保留 singleflight 合并同键并发查询；透明装饰器不复制响应，只有缓存所有权边界执行深拷贝

**Constraints**: 不引入响应包装类型或兼容返回分支；不更改重试、限流、熔断、均衡和递归轮数；必须保留可用 DNS 响应与错误的关联；缓存返回对象必须相互隔离

**Scale/Scope**: 20 个生产 Go 文件、14 个测试文件、12 个生产 `Lookup` 实现；公共 `ILookup`、`Cache`、`RecursiveLookup` 及所有内部/测试实现受影响

## Constitution Check

*GATE: Phase 0 前检查，并在 Phase 1 后复核。*

`.specify/memory/constitution.md` 仍是未填写模板，没有已批准的项目宪章条款。以下门禁来自仓库 `AGENTS.md`、功能规格和 Go 1.21 约束：

- **最小职责变更**: 只迁移响应模型及其直接依赖；不顺带重写 DNS 策略。PASS。
- **单一事实来源**: 删除 `DNSRR`、`AuthNS`、`MX`、`SOA` 和 `toDNSRR`，不保留隐藏兼容层。PASS。
- **公共一致性**: `ILookup`、所有实现、装饰器、缓存、递归入口和测试替身一次性迁移。PASS。
- **边界安全**: JSON DNS 外部文本经库解析器处理，拒绝 CR/LF，并验证生成 RR 的类型、类和 TTL。PASS。
- **错误可判定**: 保留 sentinel/typed error 与 `errors.Is` 语义，禁止 `nil, nil` 成功。PASS。
- **确定性验证**: 网络协议行为由本地 fixture 覆盖；不依赖公共 DNS。PASS。
- **版本约束**: 实现仅使用 Go 1.21 与现有依赖提供的能力。PASS。

**Post-design re-check**: Phase 1 的数据模型、接口契约和 quickstart 未引入第二响应模型、额外服务或新策略；所有门禁继续 PASS，无需复杂度豁免。

## Project Structure

### Documentation (this feature)

```text
specs/001-native-dns-response/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   └── resolver-api.md
└── tasks.md                 # 由 $speckit-tasks 生成，本阶段不创建
```

### Source Code (repository root)

```text
go.mod                       # 主版本模块路径迁移至 /v3
lookup.go                    # ILookup、基础 UDP/TCP resolver、公共响应校验
doh.go                       # DoH wire 响应直接返回
json_api.go                  # JSON transport DTO -> dns.Msg/RR
cache.go                     # *dns.Msg 缓存、TTL、Copy 所有权
cname.go                     # 从 Answer 扫描 CNAME/目标记录
recursive.go                 # 从 Answer/Ns/MsgHdr 驱动递归
concurrency.go               # 绑定 response/error 的竞速结果
retry.go
timeout.go
ratelimit.go
balance.go
circuit_breaker.go
default.go
builder.go
unwrap.go                    # 透明装饰器与默认组合的签名迁移
*_test.go                    # 本地 fixture、契约和回归测试
example_builder_test.go      # 可发布示例迁移
MIGRATION.md                 # v2 -> v3 破坏性变更说明
AGENTS.md                    # 模块路径说明同步为 /v3
```

**Structure Decision**: 保持现有单包根目录结构。辅助逻辑放回最接近职责的文件：TTL 在 `cache.go`，CNAME 扫描在 `cname.go`，递归 RR 提取在 `recursive.go`，JSON 转换在 `json_api.go`；不新增通用 response service 或包装层。

## Implementation Strategy

### 1. Establish the native response contract

- 将模块路径升级为 `github.com/lixiangzhong/fastresolver/v3`，同步仓库内受版本控制的自引用和 User-Agent。
- 将 `ILookup.Lookup` 改为 `(*dns.Msg, error)`，增加 `ErrNoResponse` 表示自定义 resolver 返回非法 `nil, nil`。
- 基础 resolver 在 UDP 成功时返回原消息；TC 响应自动回退 TCP。TCP 失败时返回截断 UDP 消息和错误，TCP 仍截断时返回 TCP 消息和 `TruncatedError`。
- 对已解码但缺少 Question 的消息返回该消息和 `ErrNoQuestion`；REFUSED 返回该消息和 `ServerRefusedError`；其他 RCODE 保持响应加 nil error。
- 删除 `DNSRR` 及其子类型、`toDNSRR`、`newSOA`，不提供别名或兼容入口。

### 2. Migrate transports and transparent decorators

- DoH 对 wire body `Unpack` 后直接返回消息，保留全部 flags、区段、OPT 和库支持的 RR。
- JSON DNS 映射 MsgHdr、Question、Answer 和 Authority；以安全固定 owner 调用 `dns.NewRR` 解析 RDATA，再写回经验证的源 owner。缺失 Question或非法 RR 返回部分消息和错误。
- Retry、Timeout、RateLimit、Metrics、CircuitBreaker、LoadBalance 和 Fallback 原样传递响应指针，以 error 继续驱动策略。
- Concurrency 改为单个 `{response,error}` 结果通道，避免响应与错误失配；零 resolver 返回 `ErrNoResolver`，所有失败返回 joined errors，并保留一个关联失败响应用于诊断。

### 3. Migrate stateful and inspecting resolvers

- `Cache` 接口和条目类型改为 `*dns.Msg`。Set、Get 以及 singleflight 向每个等待者交付时都执行 `Msg.Copy()`。
- 缓存 TTL 继续取 Answer 与 Authority 全部 RR 的最小 Header TTL；0/无 RR 使用默认 TTL，最终维持至少一分钟；Extra 不参与过期计算。
- CNAME 仅扫描 Answer，继续只为 A/AAAA/PTR 且缺少目标类型时跟随第一个 CNAME；NS 和其他类型不跟随，超深返回最后消息和包装错误。
- 递归解析从 Answer 读取 A，从 Ns 读取委派 NS，从 MsgHdr 读取 Authoritative/Rcode；保留现有 SOA 启发式作为内部终止判断，但不篡改原生 Rcode。zonedb 未命中时构造本地 NXDOMAIN 消息。
- 删除无调用方的 `cacheNetLookupIP`，避免为已废弃的 `DNSRR` 路径建立第二套原生消息合成逻辑。

### 4. Migrate tests, examples, and documentation

- 测试替身统一返回 `*dns.Msg`；断言直接检查 Answer/Ns/Extra 与具体 `dns.RR` 类型。
- 增加响应完整性、RCODE/error 组合、UDP-TCP 回退、JSON 映射安全、缓存深拷贝、singleflight 隔离、并发全失败和递归区段语义测试。
- 迁移受版本控制的 `example_builder_test.go`；本地 `.gitignore` 排除的原型示例不纳入版本化交付，也不覆盖用户文件。
- 新增 `MIGRATION.md`，记录模块路径、签名、字段读取、删除类型、传输元数据移除和错误组合。
- 当前工作区 `go vet ./...` 会被忽略的 `example/main.go` 既有不可达代码阻塞；最终交付验证以受版本控制文件的干净树为准，同时明确报告本地忽略文件风险。
