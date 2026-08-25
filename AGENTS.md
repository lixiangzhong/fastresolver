# Repository Guidelines

## 项目结构与模块组织

本仓库是 Go 模块：`github.com/lixiangzhong/fastresolver/v3`。核心代码位于仓库根目录，包名统一为 `fastresolver`。

- 根目录 `*.go` 文件实现解析器与通用能力，例如 `lookup.go`、`recursive.go`、`doh.go`、`cache.go`、`balance.go`。
- 测试文件与实现文件同级，命名为 `*_test.go`，例如 `lookup_test.go`、`doh_test.go`。
- `example/` 存放示例 DNS 输入/输出文件，`example/testdns/testdns.go` 是示例程序。

新增功能应放在职责最接近的文件中。若逻辑跨越多个职责，优先拆分为聚焦的新文件，避免扩张单个长函数。

## 构建、测试与开发命令

- `go test ./...`：运行完整测试套件。
- `go test -run TestName ./...`：迭代时运行指定测试。
- `go vet ./...`：检查常见 Go 代码问题。
- `gofmt -w file.go`：格式化修改过的 Go 文件。
- `go run ./example/testdns`：运行示例 DNS 工具。

部分测试会访问公共 DNS 解析器，失败可能来自网络不可用、解析器限流或上游 DNS 行为变化。

## 编码风格与命名约定

使用 Go 标准格式，提交前运行 `gofmt`。导出类型和函数使用 PascalCase，例如 `Resolver`、`NewResolver`；未导出辅助函数使用 camelCase，例如 `validateResponse`。接口应保持小而聚焦，参考 `ILookup`。

涉及网络 I/O 的操作优先传递 `context.Context`。避免魔法字符串和重复字面量；重复使用的 DNS 常量、网络名称、超时时间应抽取为常量。

## 测试指南

使用 Go 标准库 `testing`。测试命名优先采用 `TestType_Method` 或 `TestFunction_Behavior`，参考现有 `TestResolver_Lookup`。新增测试应放在被测实现同目录。网络敏感逻辑尽量使用确定性 fixture 或可控测试解析器。

## Commit 与 Pull Request 规范

提交信息使用 Conventional Commits，并与现有历史保持一致：

- `feat(resolver): add built-in resolver`
- `fix(lookup): correct NXDomain detection`

Pull Request 应说明行为变化、列出已运行命令，并标注依赖网络的测试。有关联 issue 时请链接。只有修改生成报告或其他可视化产物时才需要截图。

## Agent 专用说明

编辑模块前，先阅读相邻文件以及本目录或子目录中的 `agents.md` / `AGENTS.md`。保留用户已有改动，控制修改范围；除非用户明确要求，不要主动创建 git commit。
