---

description: "Implementation tasks for native DNS response migration"
---

# Tasks: Native DNS Response

**Input**: Design documents from `specs/001-native-dns-response/`

**Prerequisites**: `plan.md`, `spec.md`, `research.md`, `data-model.md`, `contracts/resolver-api.md`, `quickstart.md`

**Tests**: 规格 FR-012 明确要求确定性测试；各用户故事均按先写失败测试、再实现的顺序执行。

**Organization**: 任务按用户故事分组。由于 `ILookup` 返回类型是包级破坏性变更，US1 建立契约后必须完成 US2 才能恢复整个包的编译；US3 在可构建实现上完成仓库调用方和迁移交付。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 与同阶段其他标记任务修改不同文件，且不依赖尚未完成的任务
- **[Story]**: 对应 `spec.md` 中的 US1、US2、US3
- 所有任务均包含具体文件路径

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: 建立破坏性主版本的模块身份。

- [X] T001 将模块路径从 `/v2` 更新为 `/v3`，保持 Go 1.21 和现有依赖版本不变，修改 `go.mod`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: 建立所有 resolver、缓存和测试共同依赖的原生响应契约与确定性 fixture。

**CRITICAL**: 完成本阶段后才能开始任何用户故事实现。

- [X] T002 [P] 添加构造含 Question/Answer/Ns/Extra/flags 的 `*dns.Msg` 以及查找具体 RR 的共享测试辅助函数，修改 `test_helper_test.go`
- [X] T003 [P] 将 `ILookup.Lookup` 公共签名改为 `(*dns.Msg, error)`，定义 `ErrNoResponse` 并实现 Question、REFUSED、`nil,nil` 结果的公共校验，修改 `lookup.go` 和 `error.go`

**Checkpoint**: 原生响应和错误组合已定义；后续实现不得引入包装结果或兼容返回分支。

---

## Phase 3: User Story 1 - 获取完整原生 DNS 响应 (Priority: P1)

**Goal**: UDP/TCP、DoH 和 JSON DNS 查询均返回完整 `*dns.Msg`，保留可表达区段、RCODE 和协议标志。

**Independent Test**: 使用本地 UDP、TCP 和 HTTP fixture 返回包含 Answer、Authority、Additional、flags 与多种 RCODE 的消息；验证原生内容完整，NXDOMAIN/SERVFAIL 为响应加 nil error，REFUSED、缺 Question 和截断失败保留响应并返回可判定错误。

### Tests for User Story 1

> 先编写并确认这些测试因旧返回模型或缺失行为而失败，再开始实现。

- [X] T004 [P] [US1] 将基础 resolver 测试改为原生消息断言，并新增全区段保真、NXDOMAIN/SERVFAIL/REFUSED、缺 Question、UDP 截断到 TCP 成功及 TCP 失败场景，修改 `lookup_test.go` 和 `test_helper_test.go`
- [X] T005 [P] [US1] 将 DoH 测试改为原生消息断言，并覆盖 flags、Ns、Extra、EDNS、REFUSED 响应、非 200 与畸形 wire body，修改 `doh_test.go`
- [X] T006 [P] [US1] 将 JSON DNS 测试改为原生消息断言，并覆盖 Question/flags/Rcode/Answer/Ns、多 RR 类型、未知类型、非法 RR、CR/LF、缺 Question、REFUSED 与 NODATA/NXDOMAIN，修改 `json_api_test.go`

### Implementation for User Story 1

- [X] T007 [P] [US1] 让 UDP/TCP resolver 直接返回 `*dns.Msg`，实现截断 TCP 回退与 response/error 组合，并删除 `DNSRR`、`AuthNS`、`MX`、`SOA`、`toDNSRR`、`newSOA`，修改 `lookup.go`
- [X] T008 [P] [US1] 让 DoH 解包后直接返回原生消息，删除 Rtt/Network/ServerAddr 聚合并将 User-Agent 更新为 `/v3`，修改 `doh.go`
- [X] T009 [P] [US1] 将 JSON DTO 安全映射到 `dns.Msg`，使用 `dns.NewRR` 解析并校验 RR，删除 `parseMX`/`parseSOA` 聚合路径，修改 `json_api.go`
- [X] T010 [US1] 运行并修复 `quickstart.md` 第 2、3 节的 US1 聚焦测试，确保行为符合 `contracts/resolver-api.md`

**Checkpoint**: 三种基础传输的原生响应行为完整；整个包将在 US2 完成全部接口实现迁移后恢复编译验证。

---

## Phase 4: User Story 2 - 保持解析器组合方式 (Priority: P2)

**Goal**: 所有装饰器、缓存、CNAME 和递归 resolver 继续通过同一接口组合，并正确处理可变原生响应及 response/error 对。

**Independent Test**: 构建覆盖缓存、超时、重试、限流、均衡、并发、指标、熔断、回退和 CNAME 的组合链，验证构造参数和顺序不变、响应不被透明层修改、缓存副本隔离、递归读取标准区段。

### Tests for User Story 2

> 先迁移测试替身签名并增加目标行为断言，确认测试在实现前失败。

- [X] T011 [P] [US2] 将缓存测试迁移为 `*dns.Msg`，新增 Set/Get 深拷贝、singleflight 每调用方隔离、Answer/Ns 最小 TTL、默认 TTL、一分钟下限及 Extra 不参与 TTL 测试，修改 `cache_test.go`
- [X] T012 [P] [US2] 将并发 resolver 测试迁移为原生消息，并覆盖零 resolver、响应错误配对、全失败 joined error、失败响应保留、`nil,nil` 子实现和上下文取消，修改 `concurrency_test.go`
- [X] T013 [P] [US2] 将 CNAME 测试迁移为 Answer RR 扫描，并覆盖 A/AAAA/PTR 跟随、已有目标类型、NS/其他类型、首个 CNAME、只读 Answer、深度错误保留最后响应，修改 `cname_test.go`
- [X] T014 [P] [US2] 将递归测试迁移为 Answer A、Authority NS 和 MsgHdr 断言，并覆盖权威/NXDOMAIN/SOA 启发式、委派、NS 原区段返回、zonedb miss、深度错误和递归缓存副本，修改 `recursive_test.go`
- [X] T015 [P] [US2] 将透明装饰器和构建器测试替身迁移为 `*dns.Msg`，验证成功指针透传、response+error 失败计数、上下文错误及构造顺序，修改 `balance_test.go`、`circuit_breaker_test.go`、`timeout_test.go` 和 `builder_test.go`

### Implementation for User Story 2

- [X] T016 [P] [US2] 将 `Cache`、LRU 条目和 `CacheResolver` 迁移为 `*dns.Msg`，在 Set/Get/singleflight 三个所有权边界深拷贝并保留既有 TTL 策略，修改 `cache.go`
- [X] T017 [P] [US2] 用绑定 `{response,error}` 的单结果通道重写竞速收集，消除关闭通道空成功并保留全失败关联响应，修改 `concurrency.go`
- [X] T018 [P] [US2] 直接扫描 Answer 中 CNAME 和目标 RR，保留既有跟随类型、深度与最终响应语义，修改 `cname.go`
- [X] T019 [P] [US2] 直接从 Answer/Ns/MsgHdr 驱动递归与回退，保留内部 SOA 终止启发式、构造 zonedb NXDOMAIN 并保留错误响应，修改 `recursive.go`
- [X] T020 [P] [US2] 将 Retry、Timeout、RateLimit、Metrics、CircuitBreaker 和 LoadBalance 迁移为透明 `*dns.Msg` 传递且保持 error 驱动策略，修改 `retry.go`、`timeout.go`、`ratelimit.go`、`circuit_breaker.go` 和 `balance.go`
- [X] T021 [US2] 删除无调用方的 `cacheNetLookupIP` 及其 DNSRR 专用依赖，确认 Default/internal/builder/unwrap 组合继续满足新 `ILookup`，修改 `default.go`、`internal.go`、`builder.go` 和 `unwrap.go`
- [X] T022 [US2] 运行并修复 `quickstart.md` 第 4、5 节的 US2 聚焦测试与 race 测试，确保所有 resolver 实现重新编译并满足 `contracts/resolver-api.md`

**Checkpoint**: 根包恢复完整编译，所有 resolver 和装饰器可通过原有构造方式组合，缓存与并发所有权安全。

---

## Phase 5: User Story 3 - 迁移现有调用方 (Priority: P3)

**Goal**: 受版本控制的测试、示例和文档只使用原生 DNS 响应，不保留第二结果模型，并向下游提供完整 v2 到 v3 迁移说明。

**Independent Test**: 静态搜索确认生产代码、测试和受版本控制示例中不存在 `DNSRR` 类型/转换函数；完整测试和示例构建通过；迁移文档覆盖签名、字段、错误与模块路径。

### Tests and Callers for User Story 3

- [X] T023 [P] [US3] 将 Default 相关测试改为从 `dns.Msg.Answer` 读取 A/AAAA 并移除已删除 helper 的测试，修改 `default_test.go`
- [X] T024 [P] [US3] 将受版本控制的构建器示例 import 更新为 `/v3` 并通过具体 `dns.A` 断言 Answer，修改 `example_builder_test.go`

### Documentation and Cleanup for User Story 3

- [X] T025 [P] [US3] 编写 v2 到 v3 迁移指南，覆盖 import、ILookup/Cache、自定义实现、区段读取、RCODE/error、删除类型、元数据移除和 NODATA/NXDOMAIN，创建 `MIGRATION.md`
- [X] T026 [P] [US3] 将项目模块说明更新为 `/v3`，移除以已删除 `DNSRR`/`toDNSRR` 作为命名示例的内容，修改 `AGENTS.md`
- [X] T027 [US3] 按 `quickstart.md` 第 1、7 节核对受版本控制的 `*.go`、`go.mod`、`MIGRATION.md`，确保无 `DNSRR`、`AuthNS`、fastresolver `MX`/`SOA` 或转换函数残留

**Checkpoint**: 仓库内公共契约、调用方、示例与迁移文档一致，用户可按文档完成 v3 迁移。

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: 清理接口迁移产生的死代码，并执行全量质量门禁。

- [X] T028 对所有修改过的 Go 文件执行 gofmt，并对 `go.mod`/`go.sum` 执行 `go mod tidy`
- [X] T029 审查 `lookup.go`、`doh.go`、`json_api.go`、`cache.go`、`concurrency.go`、`cname.go` 和 `recursive.go`，移除本次迁移产生的废弃 imports、无用变量、隐藏 fallback、吞错和 `nil,nil` 路径
- [X] T030 按 `quickstart.md` 运行全部聚焦测试、`go test ./...` 和 `go test -race ./...`，修复所有与本次变更相关的失败
- [X] T031 从只包含受版本控制文件的干净检出运行 `go vet ./...` 与 `go build ./...`，并在交付说明中区分被忽略的 `example/main.go` 既有不可达代码风险，依据 `quickstart.md`
- [X] T032 复核 `specs/001-native-dns-response/contracts/resolver-api.md` 与实际 `go doc` 输出一致，并更新 `specs/001-native-dns-response/quickstart.md` 中不匹配的测试命令或预期

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: 无依赖，可立即开始
- **Foundational (Phase 2)**: 依赖 T001；阻塞所有用户故事
- **User Story 1 (Phase 3)**: 依赖 T002-T003
- **User Story 2 (Phase 4)**: 依赖 US1 的原生接口与传输实现；完成后整个 Go 包恢复可构建
- **User Story 3 (Phase 5)**: 依赖 US1 和 US2 的最终接口与行为
- **Polish (Phase 6)**: 依赖目标用户故事全部完成

### User Story Dependency Graph

```text
Setup -> Foundational -> US1 (native transports) -> US2 (composable package) -> US3 (repository migration) -> Polish
```

接口返回类型跨越整个根包，因此三个故事顺序交付；各故事内部的测试和不同实现文件仍可并行。

### Within Each User Story

- 测试任务必须先完成，并确认旧实现下失败
- 每个生产文件的实现任务依赖对应测试任务
- US1 的 T010 依赖 T004-T009
- US2 的 T016-T020 分别依赖 T011-T015 中对应测试；T021 依赖 T016-T020；T022 依赖 T011-T021
- US3 的 T027 依赖 T023-T026

### Parallel Opportunities

- T002 与 T003 可并行
- US1 测试 T004-T006 可并行；实现 T007-T009 在对应测试完成后可并行
- US2 测试 T011-T015 可并行；实现 T016-T020 在对应测试完成后可并行
- US3 的 T023-T026 修改不同文件，可并行
- Polish 顺序执行，避免格式化、静态检查和文档复核读取中间状态

---

## Parallel Examples

### User Story 1

```text
Task T004: lookup_test.go 与 test_helper_test.go 的原生 UDP/TCP 契约测试
Task T005: doh_test.go 的 wire 响应保真测试
Task T006: json_api_test.go 的 JSON 映射与输入边界测试
```

### User Story 2

```text
Task T011: cache_test.go 的 Copy/TTL/singleflight 测试
Task T012: concurrency_test.go 的 response/error 配对测试
Task T013: cname_test.go 的 Answer 扫描测试
Task T014: recursive_test.go 的递归区段测试
Task T015: 透明装饰器与 builder 测试
```

### User Story 3

```text
Task T023: default_test.go 调用方迁移
Task T024: example_builder_test.go 示例迁移
Task T025: MIGRATION.md 迁移指南
Task T026: AGENTS.md 模块与命名说明
```

---

## Implementation Strategy

### Buildable MVP

1. 完成 Phase 1 Setup
2. 完成 Phase 2 Foundational
3. 完成 Phase 3 User Story 1，建立完整原生响应契约
4. 完成 Phase 4 User Story 2，使整个包及组合链重新可编译和独立验证
5. 停止并运行 T022；这是该破坏性接口迁移的最小可构建 MVP

### Incremental Delivery

1. Setup + Foundational：固定 `/v3` 与 outcome 契约
2. US1：完成 UDP/TCP、DoH、JSON 原生响应
3. US2：恢复所有装饰器、缓存和递归组合，形成可构建 MVP
4. US3：完成仓库调用方和用户迁移说明
5. Polish：执行 race、vet、build 与契约复核

### Parallel Team Strategy

1. 团队共同完成 Setup 与 Foundational
2. US1 中分别并行处理 UDP/TCP、DoH、JSON
3. US2 中分别并行处理 Cache、Concurrency、CNAME、Recursive、透明装饰器
4. US3 中并行处理测试调用方、示例、迁移指南和项目说明
5. 每阶段 checkpoint 由单一集成者运行，避免共享接口处出现临时双模型

---

## Notes

- `[P]` 仅标记不同文件且无未完成依赖的任务
- 所有用户故事任务均带 `[US1]`、`[US2]` 或 `[US3]`
- 不修改 `.gitignore` 排除的本地 `example/main.go` 与 `example/testdns/testdns.go`
- 不创建兼容 alias、旧接口适配器或第二响应模型
- 不执行 git commit、tag 或发布操作
