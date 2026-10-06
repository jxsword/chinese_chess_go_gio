# 开发进度（PROGRESS）

## 当前状态

**M2' 双人对战 + 存储——进行中**（T2'.1 已完成）。

## 里程碑总览

| 里程碑 | 状态 | 说明 |
|---|---|---|
| 设计文档阶段（含领域层复制） | ✅ | 本文件首条记录 |
| **M0' 工程骨架 + UI POC 排险** | ✅ 验收通过（tag v0.1.0-m0'） | T0'.1~T0'.7 完成；POC 五项结论回填 08 §12/DR-G003；两轮验收修复（KG-003~007） |
| **M1' 状态层核心 + 主页** | ✅ 验收通过 | T1'.1~T1'.3 完成；DR-G002 翻译用例全绿；验收反馈两项修复（居中/D-002 标题绕过）复验通过 |
| **M2' 双人对战 + 存储** | 🔄 进行中 | T2'.1 ✅ / T2'.2 ⬜ / T2'.3 ⬜ / T2'.4 ⬜ |
| M3'~M7' | ⬜ | 见 design_docs/10-实施路线图.md |

## M2' 完成清单（逐任务 commit）

| 任务 | 交付 | 验收结果 |
|---|---|---|
| T2'.1 | `internal/app/datastore.go`：数据目录=os.UserConfigDir()/chinese-chess-ultra-gio、库文件 chinese_chess_ultra_gio.sqlite（07 §1，与上游应用不互写）、**DAO 懒打开**（失败记忆错误→"本地存储不可用"降级）+ settings.json/凭据回退路径装配（07 §4/§5；凭据槽 M4' 配置卡接入）+ 全局设置单例加载接线（铁律 #G4 例外面）。复制物 internal/storage 零改动 | datastore_test 3 用例（roundtrip/降级/错误记忆）全绿；DAO/凭据复制物回归全绿 |

## M1' 完成清单（T1'.1~T1'.3，逐任务 commit）

| 任务 | 交付 | 验收结果 |
|---|---|---|
| T1'.1 | `internal/state/gamevm.go`（翻译源=上游 gameVm.ts 322 行，commit 5c04537）：GameVm/GameSnapshot/SerializedGame + **fenHistory 四收口**（初始/newGame/newGameFromFen 重置、executeMove push、undo pop、restore 重放逐手采集含跳脏，长度恒=手数+1）+ agreeDraw/resign 首版即有 + serialize 存起始 FEN+完整着法栈（DR-008）+ **裁决触发内联收口点**（07 §3：`OnHistoryGrow`=executeMove push 侧+restore 完成侧、`OnHistoryRewound`=undo pop+新局） | gameVmFenHistory.spec.ts 7 用例逐条翻译全绿 + 收口点 5 子用例 |
| T1'.2 | `gamestore.go`（工厂**每局一实例**，铁律 #G4）+ `globalsettings.go`（单例允许，缺省开启/写失败保内存）+ `llmsettings.go`（fromRaw clamp/K15、枚举 index 存档、字段子集保存；键名复用复制物 internal/llm 常量）（commit 2239fd0） | gameStore.spec.ts 19 用例逐条翻译全绿（12 等价集 + 7 扩展 + 订阅桥接）；设置用例全绿 |
| T1'.3 | internal/app 装配（commit 632dd01 + 复审修正 0a9388e）：**EventBus**（Cancel 后迟到结果按 requestId 丢弃 #G5，空 id 直通，通道满非阻塞丢弃）+ **Router**（主页+7 入口，离开页 `Disposer.Dispose` 挂接 07 §2）+ **生命周期段**（失焦=FocusEvent、关闭=ClosingEvent 可 Abort——v0.10.3 实测口径，07 §2 表已同步修订；minimize 事件源 M2' 核实）+ `ui/theme.go`（Electron global.css :root 色板 + Noto CJK 四件显式注册，poc_palette/placeholder 删除）+ **主页 7 入口**（08 §2：RRect 入口卡+hover+点击导航+全局设置入口）+ 7 入口占位页（可返回主页） | 事件总线 6 用例/路由 5 用例/生命周期 3 用例全绿（09 §4 纯 Go 表驱动） |

## M1' 验证门结果

- gameVmFenHistory/gameStore 翻译用例全绿（上游 26 用例逐条对应，用例名注释保留 spec 对应关系）；
- 事件总线/装配单测全绿（`go test ./internal/app/ -race`）；
- 主页可导航（待手测确认）；
- 质量门：`gofmt -l` 空输出 + `go vet ./...` 0 问题 + `go test ./... -race` 全量通过。

## M1' 验收结论（2026-10-06，用户确认"全部通过"）

- 通过：主页 7 入口居中渲染与悬停高亮、导航、窗口关闭、状态层/总线/路由/生命周期自动回归；
- 验收反馈修复轮两项（居中根因=根约束 Min 撑满、KG-005 D-002 WSLg ASCII 标题）复验通过；
- M1' 无新增遗留问题。

## M1' 验收反馈修复轮（2026-10-06，§6.2 流程）

1. **【修复】主页 7 入口卡/标题/副标题/设置按钮未居中**：根因=gio 根约束 Min=Max=窗宽，
   material 文本撑满 Min 使 Flex cross 轴 Middle 偏移归零；改用 `layout.Center`
   （清 Min 后在原 Min 范围内居中）。X11 实拍截图验证（commit 6c602ac）。
2. **【修复·D-002】标题栏中文乱码（KG-005）**：用户决策 B——运行时检测 WSLg（/mnt/wslg
   + /proc/version 兜底）改用 ASCII 标题，其余平台保持中文；`internal/app/wslg.go`
   注入式单测四分支全绿；WSLg 内实测 `_NET_WM_NAME` = "Chinese Chess Ultra (Gio)"。
   决策矩阵与记录见 docs/decision_log.md D-002。

其余手测项全部通过。回归：gofmt/vet/`go test ./... -race` 全绿。

## M1' 复审记录（§6.1 两轮）

- 第一轮·语义一致性：gamevm.go 与上游 gameVm.ts 逐方法对照（含 undoRound 三段 pop 语义、restore 跳脏条件、resign/agreeDraw 解锁）；修正 executeMove 内联快照→复用 buildSnapshot（终局判定单一出口）；副标题去里程碑术语。
- 第二轮·缺陷扫描：EventBus mutex 覆盖 cancelled map（Emit/Cancel 任意 goroutine，Drain 单消费者）；新增代码无后台直写 UI/state、无 goroutine 泄漏面、无错误消息泄 Key；-race 全绿。
- §6.3 铁律自检：#G1 六包无 gio/net-http 新增（vision.go/corpusDownloader.go 为复制物既有口径，#G2 禁动）；#G3 新增代码无后台直写；#G4 无对局全局单例（globalSettings 尚未实例化，M2' 装配）；#G8 ui/state 无网络出口。
- 07 §2 事件源映射表实测修订（同 commit 632dd01）：`system.Command{CommandClose}` 不存在 → `*app.ClosingEvent`；FocusEvent 字段名为 `Focus`。

## 已知问题（累计）

- **KG-002**：gio List.ScrollBy 逐帧程序化滚动 → 文本绘制病理慢路径（黑屏）；规避=直接推进 Position.First，M5' 用手势滚动。
- **KG-003**：WSLg 下中文键盘输入全路径不可用（用户验收定论）；中文输入走 KG-004 按钮。
- **KG-004**：WSLg 剪贴板桥接 CJK 乱码且有损；可靠路径=powershell 管道粘贴按钮（已验证），M4' 沿用。
- **KG-005**：WSLg 窗口标题栏中文 □（WSLg 缺陷，不可修，仅观感）。
- **KG-006**：gio `InvalidateCmd{At: 未来时刻}` 在 WSLg 不排帧；帧循环一律立即排帧。
- **KG-007**：复制物 transport 超时测试单核饥饿偶发，CI GOMAXPROCS=2 规避（§6.2 不改复制物）。
- M1' 新增：无（生命周期 minimize 事件源待 M2' 核实，属任务挂接点非缺陷）。
- 详见 docs/KNOWN_ISSUES.md。

## M0' 完成清单（存档）

| 任务 | 交付 | 验收结果 |
|---|---|---|
| T0'.1 | go.mod 锁版（**gioui.org v0.10.3 + go-text/typesetting v0.3.5**，白名单内）+ main.go 薄入口 + internal/{app,ui,state} 骨架 + 单事件循环（#G3） | X11 弹窗验证 ✓ |
| T0'.2 | POC-1 棋盘自绘（cmd/poc board） | 与 Electron 参照渲染并排对照一致；无离屏缓存需求 |
| T0'.3 | POC-2 动画帧循环（cmd/poc anim） | 实测 225~235ms（≤1 帧偏差）；帧时间戳权威 |
| T0'.4 | POC-3 IME+字体（cmd/poc ime） | XIM 不可用（KG-003）+粘贴兜底可用（KG-004） |
| T0'.5 | POC-4 长列表（cmd/poc list） | **FPS 80，帧开销 avg 680µs / max 1.315ms**（达标）；KG-002 登记 |
| T0'.6 | POC-5 WSLg 渲染 | 三路径可用+软渲染兜底；结论入 08 §12 |
| T0'.7 | ci.yml 三平台矩阵 | 三平台全绿 |

（M0' POC 实测数据与验收修复轮记录见 git 历史 v0.1.0-m0' 版本的 PROGRESS.md；已知问题累计表见 docs/KNOWN_ISSUES.md。）

## 请手测清单（M1'，存档——已执行完毕）

> 准备：`export PKG_CONFIG_PATH="$HOME/.local/lib/pkgconfig"`（建议写入 ~/.bashrc）；
> 中文输入一律走"从 Windows 剪贴板粘贴"路径（KG-003/KG-004 既定口径）；
> 窗口标题栏中文 □ 为 KG-005 已知，不影响验收。

1. **主页（T1'.3）**：`go run .` → 弹出"中国象棋 Ultra（Gio 版）"窗口：
   - 标题区"中国象棋 Ultra / Gio 全自绘单二进制桌面版"正常渲染（Noto CJK 中文）；
   - **7 张入口卡**齐全（残局选关/人机对战/人机 LLM/LLM vs LLM/双人对弈/残局工作室/棋谱库），标题+副标题两行居中；
   - 鼠标悬停入口卡 → 卡片底色加深（hover 高亮）；移出恢复；
   - 底部"全局设置"按钮可见（M1' 点击仅日志输出，设置弹窗 M2' 落地）。
2. **导航（T1'.3）**：依次点击 7 张入口卡 → 每次都切换到对应占位页（标题+“本页面将在后续里程碑落地”+“返回主页"按钮）；点"返回主页"回主页；快速连续切换无卡死无崩溃。
3. **窗口关闭（T1'.3 生命周期段）**：任意页面点标题栏 × → 应用正常退出（终端无 panic）。
4. **失焦事件通道（T1'.3，观察项）**：应用运行中切换到其他窗口再切回 → 无异常（blur 派发为 M2' 自动保存挂接点，本期仅通道就位，无可见行为）。
5. **状态层回归（T1'.1/T1'.2，自动）**：`go test ./internal/state/ ./internal/app/ -race -v` → 全部 PASS（gameVmFenHistory 7 例 + 裁决收口 5 例 + gameStore 20 例 + 总线 6 例 + 路由 5 例 + 生命周期 3 例）。
6. **POC 回归（不改行为）**：`go run ./cmd/poc board` 与 `go run ./cmd/poc anim` → 色板迁移到 theme.go 后渲染与 M0' 验收时一致。

## 下一里程碑（M2'）

验收通过后：全新会话逐字粘贴 design_docs/11 §4.3 启动提示词（storage 接线/自动保存恢复状态机/BoardView 正式版/双人页全量 + UI 裁决接线双人页）。
