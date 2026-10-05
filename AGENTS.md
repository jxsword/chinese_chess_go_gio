除非显式指定读取当前工作区 tmp/ 目录下的文件，否则默认不读取该目录的任何文件。

# ChineseChessUltra Gio — 项目常驻指令（AGENTS.md）

> 本文件位于项目根目录，AI 编码代理每次会话自动读取。它是所有会话共享的"系统提示词"。
> 各里程碑/子任务提示词（见 design_docs/11-开发执行手册.md）在此基础上叠加，**不重复本文件内容**。

## 项目定位

将中国象棋应用从上游 Wails v2 版（`/home/ssy/proj/chinese_chess_go`，tag v1.0.0-rc1，M0~M7 全部用户验收）重构为 **Gio 全自绘单二进制桌面应用**。重构策略与事实源：

- **领域层**：上游映照整体复制（DR-G001，internal/ 六包 + 金标准 + cmd/eval，零修改）；
- **状态层**：上游 frontend/src/stores 为行为锚点，逐用例翻译为 Go（DR-G002，internal/state）；
- **UI 层**：Gio 从零自绘（DR-G003，internal/ui + internal/app），交互规格事实源=上游 08 文档 + Electron 版组件；
- 本仓库设计文档（`design_docs/`）是 Gio 版架构与工程事实源；领域协议/算法以上游同名文档与复制物为逐字事实源，冲突时按 design_docs/README.md 的归属裁决。

## 技术栈（已定稿，见 design_docs/decision_log.md DR-G001~G003，不得擅自更换）

- 语言：**Go 1.26+**；GUI：**gioui.org**（立即模式全自绘）+ **go-text/typesetting**（文本整形/中文）
- 数据库：**modernc.org/sqlite**（复制物不变）；凭据：OS keyring + 明文 0600 回退（复制物不变）
- LLM：net/http + SSE（复制物 transport 收口）；**思维链强制关闭**（复制物恒发关闭参数，UI 无开关）
- 测试：`go test -race` + 跨语言金标准对拍 + 状态层翻译测试 + 手测清单制（无浏览器 E2E，09 §5）
- **依赖白名单（新依赖必须先说明理由等确认，走 DR 流程）**：gioui.org、go-text/typesetting、modernc.org/sqlite、zalando/go-keyring、golang.org/x/text 及其传递依赖。

## 架构铁律（违反即返工）

1. **#G1 纯 Go 包边界**：`internal/{rules,engine,solver,llm,parsers,storage,state}` 禁止 import gioui.org / net/http（`internal/llm/transport` 除外——net/http 仅许该包）/ 任何 GUI 符号；仅 `internal/app` 与 `internal/ui` 允许 import gioui.org。它们必须可被 `go test`、internal/app+ui、cmd/eval 三端直调。
2. **#G2 上游映照不得就地改**：internal/ 六包复制物与上游 v1.0.0-rc1 逐文件对应；缺陷修正回上游或登记 docs/KNOWN_ISSUES.md，禁止就地静默改；涉及复制物的 commit 必须可陈述 diff 范围。
3. **#G3 立即模式单事件循环**：UI 状态由主 goroutine（`window.Event()` 循环）独占读写；CPU 密集与 I/O 只在独立 goroutine（复制物 Runner 语义，goroutine+ctx），结果经 internal/app 事件通道回主循环并以 `Invalidate()` 排帧——**后台 goroutine 禁止直写 UI/state**。
4. **#G4 每局一实例**：对局状态用工厂创建（createGameStore 语义），禁止全局单例；仅 globalSettings 允许单例。
5. **#G5 异步收口**：所有异步请求带 `requestId`；取消一律 `context.Context`；新局/悔棋/离开页面必须 cancel 且**迟到结果按 id 丢弃**（app 事件总线统一实现）；输入锁在失败/取消/dispose 时必须解锁。
6. **#G6 思维链强制关闭**：任何 LLM 请求不得携带开启思维链的参数（复制物构造层恒发按预设映射）；UI/配置不提供任何开启开关。
7. **#G7 掩码**：API Key 只经 keyring 落盘（0600 回退）；日志、异常消息、UI 一律掩码（`****`+末4位）；掩码字符串回写不覆盖真实 Key。
8. **#G8 外呼收口**：对外 HTTP 一律经 `internal/llm/transport`；internal/ui 与 internal/state 禁止直接网络调用。
9. **#G9 协议逐字**：LLM 提示词/解析正则/错误文案是调优过的协议（复制物与上游 05 §2 为权威），逐字对照，禁止意译改写。
10. **#G10 规则内核唯一事实源**：着法合法性以 `internal/rules`（复制物）为唯一事实源；LLM 回复必须与本地白名单精确匹配；`playMove` 始终保留最终校验。

## 开发规范

- 每个子任务一个 commit，格式 `feat(m0'): 描述` / `test(m1'): 描述` / `fix(m2'): 描述` / `docs: 描述`；涉及技术决策的改动在提交信息末尾加 `Decision: DR-Gxxx`，新决策先追加 `design_docs/decision_log.md`（引用上游写作 `上游-DR-xxx`）。
- **文档与代码同步**：任何行为变更先改 design_docs/ 对应章节再动代码（同一 commit 或文档先行）；每里程碑收尾更新 `docs/PROGRESS.md`；**禁止代码领先文档多个提交**。
- **测试先行**：状态层先写上游 spec 翻译用例（红→绿，09 §3 翻译纪律）；协议面先写快照测试；金标准只读，改动须上游依据并单独 commit。
- **里程碑验收流**：自动化测试通过 → commit → **暂停等待用户手动验收**（11 §5）→ 有 bug 修复并回归 → 验证通过再继续；进度记入 docs/PROGRESS.md。
- **迁移对照**：行为不确定时，先查上游 01 文档的 Flutter 源码锚点与 Electron 版源码确认；**禁止凭直觉发明行为**。
- **规则缺口口径**：长将判负与重复局面判和已实现（复制物）；**长捉与自然限着（60 回合无吃子）不实现**，保持与上游/原版一致。

## 常用命令

```bash
go run .               # Gio 应用开发模式（WSLg 窗口）
go test ./... -race    # Go 全量测试（质量门标准口径）
gofmt -l . && go vet ./...   # 质量门 lint 部分
go run ./cmd/eval      # MatchRunner 能力评估（LLM_BASE_URL/LLM_MODEL 环境变量）
go build -ldflags "-X main.version=..."   # 生产构建（单二进制）
```

## 质量门（每个任务收尾前必须全绿）

`gofmt -l` 空输出 + `go vet ./...` 0 问题 + `go test ./... -race` 全量通过（全平台无例外，上游-DR-012）；UI 任务另加：dev 模式手测对应交互（对照 08 文档防错清单）+ 用户手动验收（11 §5）。
