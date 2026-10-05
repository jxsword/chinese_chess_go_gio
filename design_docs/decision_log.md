# 决策记录（Decision Records）— ChineseChessUltra Gio

> 格式沿用上游约定（背景/全部选项/结论/理由/影响）。
> 本仓从 **DR-G001** 重新编号；引用上游决策一律写作 `上游-DR-xxx`；引用 Electron 版写作 `electron-DR-xxx`。
> 工作区协作/流程层决策见 `docs/decision_log.md`（D-xxx）。

## DR-G001 领域层整体复制：基线 v1.0.0-rc1 + 映照纪律（2026-10-06）

- 背景：本仓是上游 Wails v2 版（`/home/ssy/proj/chinese_chess_go`，tag v1.0.0-rc1，M0~M7 全部用户验收，全平台 -race 绿基线）的 Gio UI 重构版。重构策略="领域层整体复制、UI 层 Gio 从零自绘、状态层 TS 迁 Go"。复制对象：`internal/{rules,engine,solver,llm,parsers,storage}` 六包全部源码与测试（78 个 .go + parsers 测试夹具）、`testdata/golden/` 四份金标准、`cmd/eval/`（MatchRunner CLI，上游-DR-011 stderr 逐手进度）。已证实复制范围零 wails import、零 go:embed——六包本就是纯 Go（上游铁律 #1 的成果）。
- 选项：
  - A. 整体复制零修改（采纳）——`git archive v1.0.0-rc1` 提取 + module 改名/import 前缀改写/依赖收缩（D-001，去 wails + tidy）。优点：金标准对拍基线与全平台 -race 绿基线直接继承；行为漂移风险为零；复制物可被 `go test`、Gio UI、cmd/eval 三端直调（上游铁律 #1 语义原样保留）。缺点：模块前缀全量改写（51 处）；上游后续修正需同步机制。代价：复制 commit 一次。
  - B. 参照重写——优点：可顺手清理 K7~K12 等留档差异。缺点：重新引入逐行翻译风险与对拍回归成本，违背"上游已验收资产直接复用"的立项前提；交付最慢。弃。
  - C. 复制 + 顺手重构——优点：代码更"本仓化"。缺点：破坏映照纪律（复制物必须可与上游逐文件 diff 审计），对拍归因失去"代码同源"前提。弃。
- 结论：A。**映照纪律**：复制物是"上游映照"，缺陷修正回上游或登记差异（docs/KNOWN_ISSUES.md），禁止就地静默改；本仓每笔涉及复制物的 commit 必须可陈述与上游的 diff 范围。
- 理由：A 的风险面最小且审计面最强——归一 import 前缀后与上游 diff 零差异 + 金标准 sha256 同源（engine=63556ce6…/fen=26b5bece…/moves=15a96357…/notation=0b1ea301…），是"行为逐位一致"的机器可验证凭据。
- 影响：复制 commit（feat(domain)）记录基线 tag/hash；同步机制见下；AGENTS.md 铁律 #G2。
- **上游后续修正的同步机制**：上游已收尾（v1.0 归档），预期低频。若上游出现新修正：①先评估是否适用于本仓（若属 Wails 绑定层则不适用）；②按上游 commit hash 摘樱桃应用（人工 diff + 前缀归一）；③复制物 diff 审计口径更新为"上游 v1.0.0-rc1 + 已摘樱桃清单"；④同步记录进 docs/PROGRESS.md 与 KNOWN_ISSUES。禁止整包重拷覆盖本仓已对接的复制物。

## DR-G002 状态层 TS→Go 迁移：上游 stores 为行为锚点，逐条翻译为 Go 测试（2026-10-06）

- 背景：上游状态层是 `frontend/src/stores/` 的 10 个 TS 文件（共 1,244 行）：gameVm(322)/createGameStore(41)/gameAutoSave(64)/gameRestore(41)/lifecycleRegistry(22)/globalSettings(38)/llmSettings(58)/corpusBrowser(372)/corpusTypes(17)/puzzleDemo(269)。上游 `frontend/test/` 实测 **27 个 spec 文件 / 249 个用例**（立项提示词原口径"29 个用例"按实测校正：29 ≈ spec 文件数量级，非用例数；本 DR 记双口径，实测以 27 spec/249 用例为准）。Gio 版无 WebView/无 TS，状态层必须迁 Go；上游 08 §1"stores 原样复制"的资产转为**行为锚点**而非复制物。
- 选项：
  - A. 逐文件翻译为 internal/state 纯 Go + 上游 vitest 用例逐条翻译为 Go 表驱动测试（采纳）——优点：行为锚点可执行（上游 249 用例中 stores/api/llm/storage/studio 相关者为翻译源，page/boardView 的 76 条 UI 用例由 08 手测清单制替代）；fenHistory 四收口（gameVmFenHistory.spec.ts）、自动保存恢复状态机（autoSaveRestore.spec.ts，含上游-DR-008 起始 FEN 语义）、异步编排收口（api/contract、engineClient/parserClient/solverClient spec 的 requestId 取消/迟到丢弃语义）三组重心用例逐条对应；`go test -race` 兜底。缺点：翻译工作量集中在 M1'/M2'。代价：一个新包。
  - B. 保留 TS 状态层（嵌入 WebView 或独立的 ts 运行时）——优点：锚点零翻译。缺点：违背"无 WebView、单二进制"目标，引入 node 运行时或把 Gio 退化为 Wails 同构。弃。
  - C. 采用第三方 Go 状态管理库——优点：现成响应式。缺点：白名单外依赖（须走 DR 流程）；上游 stores 的语义（每局一实例工厂、fenHistory 收口、generation 防陈旧）是领域语义，通用库帮不上。弃。
- 结论：A。落位 `internal/state`；每局一实例（上游铁律 #6 对应：createGameStore 工厂语义 → Go 工厂函数，禁止全局单例）；允许单例的仅 globalSettings（上游同口径）。corpusBrowser 的 14 万局索引/分批 128 懒解析/generation 防陈旧与 puzzleDemo 的 idle→playing→paused/completed/error 状态机为独立翻译单元。
- 理由：状态层是 Gio 版行为正确性的重心（存档/恢复/重复裁决数据基础都在这里），可执行锚点比人工对照可靠；A 不引入新依赖且与上游-DR-003 的 goroutine+context 并发模型同构。
- 影响：07 文档（状态层迁移设计为重心）、09 文档（测试映射表）、10 文档 M1'/M2' 落位、AGENTS.md 铁律 #G4。

## DR-G003 Gio UI 技术要点：gioui.org + go-text/typesetting、立即模式保留态封装（2026-10-06）

- 背景：UI 层从"React + 原生 WebView"（上游-DR-001）换为 Go 从零自绘。约束：三平台桌面端、WSLg 开发、单二进制分发（上游 AppImage 80MB 教训 K35 的反面诉求）、上游 08 文档的交互规格（棋盘/动画/防错 #1~#12）全部需要重新实现。
- 选项：
  - A. **Gio（gioui.org）+ go-text/typesetting（采纳）**——优点：纯 Go 立即模式 GUI（单二进制、无 WebView/无 CGO 系统库拖累的目标形态）；go-text/typesetting 是 Gio 官方文本整形栈（中文 shaping/字体回退）；layout.List 天然虚拟化（14 万局长列表）；`widget.Editor` 带输入法接入点（中文 IME）；绘制 ops（clip/stroke/图片/文本）足以实现棋盘自绘与 220ms 帧循环动画。缺点：立即模式心智与 React 声明式差异大，页面/组件全部从零；Gio v0.x API 仍在演进（锁版+升级走 DR 流程缓解）。代价：08 文档成为最大写作项 + M0' POC 排险五项。
  - B. Fyne——优点：widget 化、上手快。缺点：保留模式框架，棋盘自绘/动画帧循环同样要手写 canvas，且单文件分发与文本整形能力弱于 Gio+typesetting；Gio 的立即模式与"每帧重算、状态在外"的模型更贴合"UI=纯函数(状态)"的上游状态层架构。弃。
  - C. 保留 Wails/WebView 方案——优点：上游 UI 资产直接复用。缺点：与本仓立项目标（去 WebView、纯 Go 单二进制）冲突；等于不重写。弃。
  - D. Walk（Windows 专用）/ 自绘 X11——优点：无。缺点：平台覆盖不符（需三平台）、工程量失控。弃。
- 结论：A。技术要点（细节回填 08 文档，标注 POC 项）：
  1. **立即模式保留态封装**：Gio 每帧重算布局，无框架保留态——每页一个 state struct（internal/ui），由 UI 主 goroutine 独占读写；Gio widget 自带状态（widget.Clickable/Editor/Enum 等）按上游组件树粒度组织；领域状态在 internal/state，UI 状态绝不反向流入。
  2. **UI 线程模型**：单事件循环（main goroutine：`window.Event()` 阻塞收 FrameEvent/InputEvent/SystemCommand）；后台领域计算在独立 goroutine（复制物 Runner 语义），结果经 channel 送回，主循环以非阻塞 select 消费 + `window.Invalidate()` 排帧——禁止后台 goroutine 直写 UI 状态（铁律 #G3）。
  3. **渲染后端**：各平台图形 API 与 WSLg（X11/Wayland）路径、软件渲染兜底（Mesa llvmpipe）、Vulkan 后端可用性——**以 M0' POC 实测为准回填**，本文不预设结论。
  4. **IME**：widget.Editor + 输入法协议；WSLg fcitx5 场景为 M0' POC 排险项（08 §IME）。
  5. **长列表**：layout.List 虚拟化 + internal/state 分页索引（上游 corpusBrowser 分批 128 语义保留）。
- 理由：Gio 是 Go 生态中最贴合"单二进制 + 全自绘 + 中文文本"约束的成熟方案；B~D 均在关键约束上失分。POC 前不预设渲染/IME 细节结论，是诚实口径。
- 影响：00 文档（架构/线程模型）、08 文档（全部绘制与交互实现）、09 文档（Gio 层测试方式）、10 文档 M0'（POC 排险五项）、AGENTS.md 依赖白名单（gioui.org、go-text/typesetting）。

## 附：沿用上游不做重裁的决策清单

| 上游-DR | 主题 | 本仓沿用方式 |
|---|---|---|
| DR-002 | modernc.org/sqlite 纯 Go 驱动 | 复制物随迁，零改动 |
| DR-003 | goroutine + context 并发模型 | 复制物随迁；Gio 侧事件经 channel+Invalidate 对接同一模型 |
| DR-004 | 外呼收口 internal/llm/transport | 复制物随迁；Gio UI 同样禁直连 |
| DR-005 | LLM 思维链强制关闭 | 复制物随迁（请求构造恒发关闭参数）；Gio 配置 UI 无开关 |
| DR-006 | 重复治理三层前置 | 复制物随迁（L0~L3 已在）；UI 裁决接线随 M2'/M4' 挂接 |
| DR-008 | 存档序列化=起始 FEN+着法栈 | 复制物随迁（storage/gameVm 语义已含） |
| DR-009 | 研究助手配置运行时借用（黑→红） | 语义随状态层迁 Go（07 文档） |
| DR-011 | eval CLI stderr 逐手进度 | 复制物随迁，零改动 |
| DR-012 | 全平台 -race + Windows 平台性测试修复 | 复制物测试随迁；质量门无例外 |
| D-001（本仓） | go.mod 去 wails + tidy | 已落地于复制 commit |
