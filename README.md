# 中国象棋 Ultra（Gio 版）

中国象棋应用的 **Gio 全自绘单二进制桌面版**——上游 Wails v2 版（[chinese_chess_go](https://github.com/jxsword/chinese_chess_go)，tag v1.0.0-rc1，M0~M7 全部用户验收）的重构版，**v1.0.0 已发布**：

> **领域层整体复制、状态层 TS 迁 Go、UI 层 Gio 从零自绘。**

- **领域层**：上游 internal/ 六包（rules/engine/solver/llm/parsers/storage）+ 四份跨语言金标准 + MatchRunner CLI 逐文件复制（映照纪律，零修改，DR-G001）；
- **状态层**：上游 10 个 TS store（1,244 行）以 249 个 vitest 用例为行为锚点，逐用例翻译为 `internal/state` 纯 Go（DR-G002）；
- **UI 层**：gioui.org 立即模式自绘（棋盘 ops/220ms 帧循环动画/长列表虚拟化/中文 IME），单事件循环 + 事件总线（DR-G003）。

## 下载与安装

从 GitHub **Releases 页**下载对应平台产物（v1.0.0 起，tag 触发 [release.yml](.github/workflows/release.yml) 自动构建并上传 Draft）：

| 平台 | 产物 | 安装方式 |
|---|---|---|
| Windows | `chinese-chess-ultra-gio-<ver>-windows-amd64.exe` | 裸 exe 直接运行（DR-G005 基线，无安装器） |
| Linux | `.AppImage` / `.deb` / `.tar.gz` | AppImage 直接运行（需 FUSE，或 `--appimage-extract-and-run`）；deb `sudo dpkg -i` 后从应用菜单启动 |
| macOS | `.dmg` / `.app` | dmg 挂载后将 .app 拖入 Applications |

本地打包命令与产物说明见 [build/README.md](build/README.md)。完整中文功能说明见 [docs/soft_intro.md](docs/soft_intro.md)。

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
| 打包 | go build 单二进制 + Linux AppImage/deb + macOS dmg + Windows 裸 exe（M7'） |

## 仓库结构

```text
chinese_chess_go_gio/
├── design_docs/            # 设计文档集（00~11 + decision_log DR-G）
├── docs/                   # PROGRESS（+archive）/ KNOWN_ISSUES / decision_log(D-) / soft_intro
├── internal/               # 纯 Go 层
│   ├── rules/ engine/ solver/ llm/ parsers/ storage/   # ★ 上游映照复制物（零修改）
│   ├── state/              # 状态层（上游 stores 行为锚点迁 Go）
│   ├── app/                # 装配：窗口/路由/生命周期/事件总线
│   └── ui/                 # Gio 自绘页面/组件/绘制
├── cmd/eval/               # MatchRunner CLI（复制物）
├── cmd/poc/                # POC/冒烟入口（M0' 遗产：board/anim/ime/list/corpus）
├── build/                  # 打包资产（nfpm/desktop/图标/Info.plist/README）
├── main.go                 # 薄入口
├── testdata/golden/        # 跨语言金标准（复制物，sha256 同源）
└── .github/workflows/      # ci.yml（M0'）/ release.yml（M7'）
```

## 设计文档

见 [design_docs/README.md](design_docs/README.md)（导航、上游对照表、阅读路径、术语表）与 [decision_log](design_docs/decision_log.md)（DR-G001 起）。领域文档（02~06）事实源=上游同名文档，本仓仅存差异附录。

## 开发（WSL ubuntu / WSLg）

```bash
go run .               # 开发模式弹窗（依赖以 M0' POC 结论为准）
go test ./... -race    # 全量测试（质量门）
gofmt -l . && go vet ./...
go run ./cmd/eval      # 能力评估（LLM_BASE_URL/LLM_MODEL 环境变量）
go run ./cmd/poc board # POC/冒烟（board/anim/ime/list/corpus）
```

## 版本历史

**v0.1.0-m0'** 工程骨架+UI POC 排险五项 → **v0.2.0-m2'** 双人对战+存储 → **v0.3.0-m3'** 引擎对接+人机页 → **v0.4.0-m4'** LLM 全链路 → **v0.5.0-m5'** 语料+棋谱 → **v0.6.0-m6'** 工作室+求解器+识图 → **v1.0.0** 打包发布（全里程碑验收记录见 [docs/PROGRESS.md](docs/PROGRESS.md) 与 [docs/PROGRESS-archive.md](docs/PROGRESS-archive.md)）。

## 许可

待定（与上游一致）。
