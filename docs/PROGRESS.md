# 开发进度（PROGRESS）

## 当前状态

**设计文档阶段完成**（2026-10-06）：领域层复制基线建立 + Gio 版设计文档集齐备；等待用户审阅清单确认后，按 11 手册 §4.1 提示词进入 M0'。

## 里程碑总览

| 里程碑 | 状态 | 说明 |
|---|---|---|
| 设计文档阶段（含领域层复制） | ✅ | 本文件首条记录 |
| M0' 工程骨架 + UI POC 排险 | ⬜ | POC 五项：棋盘自绘/动画帧循环/IME/长列表/WSLg 渲染 |
| M1'~M7' | ⬜ | 见 design_docs/10-实施路线图.md |

## 变更日志

### 设计文档阶段（2026-10-06）

**复制基线（feat(domain) commit 48056f3）**：

- 基线：上游 `/home/ssy/proj/chinese_chess_go` tag **v1.0.0-rc1**（commit `a3cd7db3c892e5617ce6bd3ae0bf581712268376`），`git archive` 提取；
- 范围：internal/ 六包（78 .go + parsers 测试夹具）+ testdata/golden/ 四份金标准 + cmd/eval/（main.go 431 行 + main_test.go）；
- 机械性改动（D-001）：module 改名 + 51 处 import 前缀改写 + 去 wails 依赖收缩（go mod tidy）；
- **映照校验**：80 个 .go 归一前缀后与上游 diff 零差异；金标准逐字节同源（sha256：engine=63556ce6…/fen=26b5bece…/moves=15a96357…/notation=0b1ea301…）；
- **质量门**：gofmt -l 空 / go vet 0 / `go test ./... -race` 全绿（7 包 ok，本机 go1.27.1 linux-amd64；rules 1.0s / solver 2.1s / llm 4.3s / parsers 4.0s / storage 1.2s / engine 52.4s / eval 38.3s）。

**文档集（docs commit 8 笔）**：

- docs/decision_log.md：D-001（go.mod 去 wails+tidy，含决策矩阵）；
- design_docs/decision_log.md：DR-G001（复制策略/映照纪律/同步机制）、DR-G002（状态层迁 Go，锚点=10 stores 1,244 行 + 27 spec/249 用例，**"29"实测校正记录**）、DR-G003（Gio 技术要点，POC 项不预设结论）；
- design_docs/00：单事件循环/无绑定层/调用与事件清单（M1'~M6' 对接锚点）/Wails 专属 K 替代方案表；
- design_docs/02~06：差异附录 ×5（事实源=上游同名文档，零行为差异）；
- design_docs/07：状态层迁移设计（迁移映射总表/四收口/生命周期事件源映射/并发所有权口径）；
- design_docs/08：逐页自绘规格（主页 7 入口/棋盘 ops 含楚河汉界与纵线号/220ms 帧循环/长列表/IME/防错 #1~#12 映射/POC 回填区）；
- design_docs/09：测试方案（状态层翻译纪律/Gio 层轻量测试/E2E 差异声明/CI 教训五条内置）；
- design_docs/10：M0'~M7' 路线图（POC 排险先行/Gio 风险表 R-G1~R6/打包矩阵/DoD）；
- design_docs/11：执行手册（M0'~M7' 任务表 + **八段完整启动提示词**/验收流/复审/跑偏表含 Gio 专属 3 行）；
- design_docs/README.md：导航 + 上游对照表（含 01 行）+ 术语表 + 模块映射；
- AGENTS.md：铁律 #G1~#G10（新增 #G2 映照纪律、#G3 单事件循环）+ 依赖白名单；
- docs/KNOWN_ISSUES.md：上游 36 条三分类（①复制继承 25 条/②替代消解 11 条/③设计输入 6 条跨类）+ **KG-001**（keyring service 共存）。

**决策记录**：D-001（docs/decision_log.md）、DR-G001~G003（design_docs/decision_log.md）。

## 下一里程碑（M0'）

启动方式：全新会话逐字粘贴 design_docs/11 §4.1 启动提示词。POC 排险五项（08 §12 回填表）：棋盘自绘/220ms 动画帧循环/中文 IME/长列表虚拟化（合成 14 万局数据集）/WSLg 渲染。
