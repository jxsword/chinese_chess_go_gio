# 中国象棋 Ultra（Gio 版）

中国象棋应用的 **Gio 全自绘单二进制桌面版**——上游 Wails v2 版（[chinese_chess_go](https://github.com/jxsword/chinese_chess_go)，tag v1.0.0-rc1，M0~M7 全部用户验收）的重构版：

> **领域层整体复制、状态层 TS 迁 Go、UI 层 Gio 从零自绘。**

- **领域层**：上游 internal/ 六包（rules/engine/solver/llm/parsers/storage）+ 四份跨语言金标准 + MatchRunner CLI 逐文件复制（映照纪律，零修改，DR-G001）；
- **状态层**：上游 10 个 TS store（1,244 行）以 249 个 vitest 用例为行为锚点，逐用例翻译为 `internal/state` 纯 Go（DR-G002）；
- **UI 层**：gioui.org 立即模式自绘（棋盘 ops/220ms 帧循环动画/长列表虚拟化/中文 IME），单事件循环 + 事件总线（DR-G003）。

## 功能总览（对齐上游全部功能）

五类对局模式（双人/人机/人机 LLM/LLM vs LLM/残局挑战）、规则内核（含 L3 重复局面裁决）、内置 AI 引擎（Zobrist+L1/L2 重复治理）、大模型对弈（SSE 流式、思维链强制关闭、否决链）、残局求解器、棋谱生态（XQF/PGN/语料库 14 万局）、能力评估（MatchRunner 4 profile）、三平台打包。

## 技术栈

| 项 | 选型 |
|---|---|
| GUI | gioui.org（立即模式全自绘，无 WebView）+ go-text/typesetting |
| 语言 | Go 1.26+（internal/ 纯 Go 分层） |
| 数据库 | modernc.org/sqlite（纯 Go 零 CGO） |
| 凭据 | OS keyring + 0600 明文回退 |
| LLM | net/http + SSE（transport 包收口；思维链恒关） |
| 测试 | go test -race + 金标准对拍 + 状态层翻译测试 + 手测清单制 |
| 打包 | go build 单二进制 + Linux AppImage/deb + macOS dmg（M7'） |

## 仓库结构

```text
chinese_chess_go_gio/
├── design_docs/            # 设计文档集（00~11 + decision_log DR-G）
├── docs/                   # PROGRESS / KNOWN_ISSUES / decision_log(D-)
├── internal/               # 纯 Go 层
│   ├── rules/ engine/ solver/ llm/ parsers/ storage/   # ★ 上游映照复制物（零修改）
│   └── state/              # 状态层（上游 stores 行为锚点迁 Go）
├── internal/app/           # 装配：窗口/路由/生命周期/事件总线（M0' 建）
├── internal/ui/            # Gio 自绘页面/组件/绘制（M0' 建）
├── cmd/eval/               # MatchRunner CLI（复制物）
├── main.go                 # 薄入口（M0' 建）
├── testdata/golden/        # 跨语言金标准（复制物，sha256 同源）
└── .github/workflows/      # ci.yml（M0'）/ release.yml（M7'）
```

## 设计文档

见 [design_docs/README.md](design_docs/README.md)（导航、上游对照表、阅读路径、术语表）与 [decision_log](design_docs/decision_log.md)（DR-G001~G003）。领域文档（02~06）事实源=上游同名文档，本仓仅存差异附录。

## 开发（WSL ubuntu / WSLg）

```bash
go run .               # 开发模式弹窗（依赖以 M0' POC 结论为准）
go test ./... -race    # 全量测试（质量门）
gofmt -l . && go vet ./...
go run ./cmd/eval      # 能力评估（LLM_BASE_URL/LLM_MODEL 环境变量）
```

## 路线图

设计文档阶段（本阶段）→ **M0'** 工程骨架+UI POC 排险五项 → M1' 状态层核心+主页 → M2' 双人对战+存储 → M3' 引擎对接+人机页 → M4' LLM 全链路 → M5' 语料+棋谱 → M6' 工作室+求解器+识图 → M7' 打包发布。详见 [10-实施路线图](design_docs/10-实施路线图.md)。

## 许可

待定（与上游一致）。
