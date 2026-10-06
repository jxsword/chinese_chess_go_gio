# 开发进度（PROGRESS）

## 当前状态

**M3' 引擎对接 + 人机页——开发完成，待用户手动验收**（2026-10-06）。
验收通过后下一里程碑 M4'（LLM 全链路对接，启动提示词=design_docs/11 §4.5，全新会话粘贴）。

## 里程碑总览

| 里程碑 | 状态 | 说明 |
|---|---|---|
| 设计文档阶段（含领域层复制） | ✅ | 本文件首条记录 |
| **M0' 工程骨架 + UI POC 排险** | ✅ 验收通过（tag v0.1.0-m0'） | T0'.1~T0'.7 完成；POC 五项结论回填 08 §12/DR-G003；两轮验收修复（KG-003~007） |
| **M1' 状态层核心 + 主页** | ✅ 验收通过 | T1'.1~T1'.3 完成；DR-G002 翻译用例全绿；验收反馈两项修复（居中/D-002 标题绕过）复验通过 |
| **M2' 双人对战 + 存储** | ✅ 验收通过（tag v0.2.0-m2'） | T2'.1~T2'.4 完成；翻译用例回归+13 项手测通过；验收反馈一项修复（KG-009 弹窗居中）复验通过 |
| **M3' 引擎对接 + 人机页** | 🔶 待手测验收 | T3'.1~T3'.2 完成；对拍回归全绿（复制物零改动）+ cancel/迟到丢弃用例 + 难度 5 实测 1.54s（≤7.5s）；复审两轮修正两项 |
| M4'~M7' | ⬜ | 见 design_docs/10-实施路线图.md |

## M3' 完成清单（逐任务 commit）

| 任务 | 交付 | 验收结果 |
|---|---|---|
| T3'.1 | `ui/engineclient.go`（commit c3c29ea）：包装复制物 engine.Runner（NewRunner/Submit/Cancel 语义，goroutine+ctx 内建）——FindBestMove/FindBestMoveEx/EvaluateMove 三接口异步封装，响应 goroutine 经 env.Emit 回 app 事件总线（#G3 正方向）；**HistoryFens 仅 FindBestMove 接受**（对局方 fenHistory 拷贝传入，DR-018；参数缺省=关闭重复治理），参谋接口一律不传历史（03 §6.3 可复现铁律，无传入途径）；Cancel 双收口（Runner ctx 取消 + 总线迟到丢弃，#G5）；events.go 新增 EngineMoveDone/EngineReportDone/EngineEvalDone 回执载荷（00 §4 engine Worker 行） | 引擎客户端 7 用例（载荷形状/历史拷贝/参谋无历史/双收口取消/错误透传/真复制物端到端合法着法）全绿；**对拍回归全绿**（internal/engine -race 56s，engine.json 不传 HistoryFens 逐位一致，复制物基线零改动） |
| T3'.2 | `ui/humanvsai.go`（commit bd955b9，翻译源=上游 HumanVsAiPage.tsx）：执红/执黑切换（重开新局+AI 先行）、难度 1-5 chips（即时生效）、状态条四态（对局结束/AI 思考中/被将军/等待玩家）、AI 应手 PlayMove 最终校验落盘（#G10）+ 视觉飞行层、引擎失败判 AI 负（#7）、恢复定局后 AI 先行、新游戏确认框 + 三次重复判和确认框（玩家侧弹框/AI 侧自动判和）、自动保存/恢复/手动保存/用时计时（复用 M2' 骨架）+ `ui/boardview.go` 已落盘走法视觉飞行层（08 §4 注记）+ app RouteHumanVsAi 工厂页（#G4） | 页面级 14 用例（恢复/触发/落盘/迟到丢弃/失败判负/undo 重触发/执方切换/dispose 取消/裁决接线/保存/难度）+ 视觉层 1 用例，`-race` 全绿；`go run .` 冒烟通过 |

## M3' 验证门结果

- **对拍回归全绿**：复制物基线不动（`git diff c96e8f3..HEAD -- internal/ 六包+cmd/eval` 为空），engine.json 不传 HistoryFens 逐位一致（`go test ./internal/engine/ -race` 56s 全绿）；
- **cancel/迟到丢弃用例**：引擎客户端双收口取消 + 页面级迟到回执按 id 丢弃（新局/悔棋/dispose 三路径）+ app 事件总线 requestId 过滤（M1' 既有用例回归）全绿；
- **难度 5 应答 ≤7.5s 性能门**：`RUN_SLOW=1 TestPerformanceGateDifficulty5` **实测 1.54s**（初始局面，WSL2 环境）；
- 人机一局手测：待用户执行（见下方手测清单）；
- 质量门：`gofmt -l` 空输出 + `go vet ./...` 0 问题 + `go test ./... -race` 全量通过。

## M3' 复审记录（§6.1 两轮）

- 第一轮·语义一致性：humanvsai.go 与上游 HumanVsAiPage.tsx 逐行为对照（triggerAiMove 的 lockInput→搜索→无条件解锁 P0-1、newGame/switchSide/undoRound 的 seq 作废→requestId 形态、引擎失败 resign(AI 方)、恢复后 AI 先行须等 restore 完成、isHuman=执方判定）——**修正状态条被将军方名反转**（上游 `isRedTurn ? '红' : '黑'`）；undoRound 语义核实：上游 undoRound 有 inputLocked 守卫（gameVm.ts:245），思考中点悔棋=作废在途+undo no-op+轮 AI 重触发（"中止重想"），实现照此落位并加防残锁兜底。
- 第二轮·缺陷扫描：**修正弹窗遮罩下执方/难度 chips 可点击**（判和确认框打开时可切执方重开新局——KG-008 同类面，一并收口点击边沿）；goroutine 面审计（EngineClient 响应 goroutine 生存期=请求生存期、fake 均带超时兜底、toast/timer 有界可停）；迟到面审计（页面按 id 防御 + 总线按取消 id 丢弃双层）；无错误消息泄 Key；`-race` 全绿。
- §6.3 铁律自检：#G1 六包 import 面无 gio/新增 net-http（grep 机器验证，net/http 命中均为复制物既有口径）；#G2 六包+cmd/eval M3' diff=零改动（机器验证）；#G3 EngineClient goroutine 只经 env.Emit 回总线（env 回调仅构造期写定）；#G4 RouteHumanVsAi 工厂页；#G5 AI/restore/save 请求均带 requestId，取消=Runner ctx+总线双收口；#G8 ui 无网络出口。
- P3 登记：无新增（KG-008 双人页注记对 chips 同样适用，已在人机页收口）。

## M3' 验收反馈修复轮（2026-10-06，§6.2 流程）

用户反馈三项（执黑不可点/难度不可点/AI 不显示走棋疑似绑低难度），X11+xdotool
截图驱动真实应用实证（证据链 ~/poc_evidence/m3/），两项真缺陷 + 一项行为澄清：

1. **【修复】执黑/难度 chips 不可点击**：自绘 chip 只画了圆角矩形，从未注册
   指针输入区——widget.Clickable 须经 `Clickable.Layout`（或 clip 内显式
   Add）注册手势，否则是死按钮。改经 Clickable.Layout 注册后实测：执黑切换
   （AI 红先走子）、专家/大师档切换（高亮+对手名联动）均生效。
2. **【修复】侧板卡片右缘被窗口裁掉**（大师 chip 半可见，M2' 双人页同款）：
   layoutBody 的 Rigid 子项把 280dp 约束加在 UniformInset(8) 内层，实际占位
   280dp+16dp，整页横向溢出 ~24px，卡片右缘溢出窗口。约束移到 Inset 外层
   （280dp=含内边距总宽），两页一并修复；chip 宽 44→42dp 后五个难度 chip
   完整可见。
3. **【澄清·非缺陷】"AI 不走棋/疑似绑定低难度"**：插桩（boardclick 日志）+
   截图实证 AI 链路正常——玩家走子→Runner 搜索→PlayMove 落盘→回合回玩家
   （步数 2→4、lastMove 高亮、执黑时 AI 红先均截图验证）。高级/专家应答快是
   引擎特性：timeMs 是上限不是下限，深度 4/5 约 0.2~1s 即完成（与上游引擎
   行为一致）；"AI 正在思考"为短暂时呈（大师档实测约 1.5s 肉眼可见）；难度档
   不持久化（页级状态，刷新回默认高级，上游同款）。

排障副产物：WSLg Xwayland 下 xdotool 注入点击 = 请求坐标 +12px（窗口左边框），
截屏取证链用 `env -u WAYLAND_DISPLAY -u XDG_RUNTIME_DIR` 强制 X11 后端；
应用在真实鼠标输入下无此问题。

**难度档耗时实测（用户追问"仅大师明显慢，是否档位与算法不对应"）**：临时探针
直调复制物 FindBestMove（应用同一路径，探针已删）逐档测量——开局局面
初级 2ms / 中级 33ms / 高级 99ms / 专家 349ms / 大师 1.33s，随深度单调递增，
映射正确（levelParams 逐值随复制物锁定：d1-d5 深度 2~6，timeMs 300/800/1600/
3000/5000 为上限非下限）。主观"初级~专家几乎一样快"是算法固有特性：α-β+置换
表下深度 2~5 在常规局面均在 ~0.4s 内完成，只有深度 6 越过 1s 阈值；初级/中级
的档位差异体现在**着法随机窗口**（120/50 厘兵）而非耗时。与上游引擎行为一致。
链路佐证：TestAiPageDifficultySelection 验证页面难度值逐字传入载荷。

回归：gofmt/vet/`go test ./internal/{ui,app,state}/ -race` 全绿（commit 4b71c4c）。

## M3' 已知问题与偏离注记

- 语义偏离 1（有意，对齐上游语义经核实）：**取消入口=无独立取消按钮**——上游人机页无独立"取消思考"按钮，思考中悔棋/新游戏/返回即取消在途请求（防错 #4）；08 §5 已同步注记。
- 语义偏离 2（呈现层）：**AI 应手=PlayMove 即时落盘 + 终点棋子 220ms 纯视觉飞行层**（等价上游 CSS transition；状态时序不变、不吞输入；CancelAnim 一并作废）——08 §4 已同步注记。
- 语义偏离 3（布局层）：执方/难度选择放侧板"对手"区 chips（上游为头部 `<select>`；Gio 头部按钮行宽度限制）——08 §5 已同步注记。
- M2' 遗留偏离注记（恢复取档先裁决/进页锁输入/blur 覆盖 minimize）对 M3' 同样适用（复用同一骨架）。

## M3' 手测清单（用户执行；对照 08 §11 防错 #1~#6/#11/#12）

准备：`go run .`；数据目录 `~/.config/chinese-chess-ultra-gio/`。

1. **主页 → 人机对战**：入口卡进入；空库直接开新局，状态条显示"等待玩家（红方）走棋"；侧板"对手"区显示"内置 AI（高级）"（默认难度 3）、执红高亮。
2. **完整对局一局（默认难度）**：走红炮（7,7）→（7,4）→ 状态条切"AI 正在思考..."（棋盘输入锁定）→ AI 应手落盘带终点 220ms 飞行动画 + lastMove 高亮 → 状态条切回"等待玩家（红方）"→ 交替走子至终局或主动新游戏。
3. **执黑可选**：点"执黑" chip → 重开新局且 AI（红）先行（状态条立即"AI 正在思考..."）→ AI 落盘后玩家走黑子。
4. **难度选择**：点"大师" chip → 走子后 AI 应手明显变慢（深度 6）；点"初级" → 明显变快且有随机性；高亮 chip 随点随换。
5. **思考中悔棋=中止重想（上游语义）**：AI 思考中点"悔棋" → AI 换个思路重新应手（步数不变——悔棋本身不生效）；AI 未思考时点悔棋 → 正常撤 AI 应手+玩家一手（步数 -2）。
6. **新游戏确认框**：思考中/任意时刻点"新游戏" → 确认框弹出（遮罩拦截：此时点执方/难度 chips 应无效）→ 确定=清空计时归零（执黑则 AI 先行）。
7. **AI 判负路径（观察项，正常对局不应出现）**：正常对局不应出现"引擎计算失败"toast；如出现，状态条应显示"对局结束：红方获胜"（防软死锁 #7）。
8. **三次重复裁决（#12，AI 侧自动判和）**：构造闲着重复环（车王摆动）→ 三次重复 → 自动 toast"三次重复局面，判和"+ 横幅"和棋"（面对方为 AI 时不弹确认框）；玩家自己走出重复时（面对方=玩家）→ 弹确认框（接受和棋/变着继续）。
9. **保存/恢复**：走 2~3 手 → 返回主页 → 重进人机对战 → 恢复存档（执红恢复后轮玩家不触发 AI；存档若保存在 AI 思考前则恢复后 AI 先行）；点"保存棋局" → toast"棋局已保存"。
10. **每局一实例**：主页→人机对战→返回→再进 → 新局；执黑再进 → AI 先行不残留。
11. **终局禁手（#6）**：将死/困毙后 → 状态条"对局结束：X 方获胜！"横幅，棋盘点击无选中。
12. **双人页回归（视觉飞行层改动面）**：进双人对弈走一手 → 点击路径 220ms 动画与 M2' 验收一致（skipFrom/skipTo 互不干扰）。

## M2' 完成清单（逐任务 commit）

| 任务 | 交付 | 验收结果 |
|---|---|---|
| T2'.1 | `internal/app/datastore.go`：数据目录=os.UserConfigDir()/chinese-chess-ultra-gio、库文件 chinese_chess_ultra_gio.sqlite（07 §1，与上游应用不互写）、**DAO 懒打开**（失败记忆错误→"本地存储不可用"降级）+ settings.json/凭据回退路径装配（07 §4/§5；凭据槽 M4' 配置卡接入）+ 全局设置单例加载接线（铁律 #G4 例外面）。复制物 internal/storage 零改动 | datastore_test 3 用例（roundtrip/降级/错误记忆）全绿；DAO/凭据复制物回归全绿 |
| T2'.2 | `state/autosave.go`（gameAutoSave.ts 翻译：SaveOnExit 开关门控/SaveManual 不受开关/Dispose 先存后注销/CanSave 防错 #6）+ `state/restore.go`（gameRestore.ts 翻译：Restored/死局删档开新局/FEN 无效/存储异常/无存档五路径；同步形态+异步拆分形态）+ `state/lifecycle.go`（lifecycleRegistry.ts 翻译：LifecycleBus 订阅/广播/注销）+ app 侧 `repo.go`（repo 异步代理：自动保存 fire-and-forget、手动保存回执、关闭同步写=K16 有界等待）+ `ui/events.go`/`ui/env.go`（00 §4 内部事件行：db:save/db:load/timer:tick/toast:hide）+ autosaveBridge（blur/minimize→总线、close→页面同步保存）；00 §4/07 §2 文档先行 | autoSaveRestore.spec.ts 9 用例逐条翻译全绿 + 拆分形态 1 + repo 2 + 桥接 1，`-race` 全绿 |
| T2'.3 | **BoardView 正式版**（`ui/boardview.go`）：cell=min(w/9.6,h/10.6) 布局/命中换算/选中与合法目标高亮（08 §3）+ 220ms 两阶段飞行动画帧时间戳权威结束（08 §4，防错 #1/#2/#4）+ OnMoved 历史增长守卫（M3' AI 挂接点）+ CancelAnim（悔棋/新局/离页作废）。POC 绘制库上收为共享 `boardart.go`/`boardlayout.go`/`draw.go`（绘制公式经 POC-1 Electron 对照验证，POC demo 同源引用不删）；坐标口径定案**纵线号**（docs/decision_log.md D-003，08 §12 回填同步） | boardlayout 几何/命中 5 用例 + easeOutCubic/animProgress 权威结束/CancelAnim 作废 3 新用例全绿；防错 #1/#2/#6 手测列入清单 |
| T2'.4 | **双人页全量**（`ui/humanvshuman.go`）：头部按钮行（返回/新游戏/悔棋/保存棋局）+ 信息卡（结算横幅/回合/将军/步数/用时/走法记录开关+双列中文记谱列表）+ 新游戏确认框 + 三次重复判和确认框 + toast（2.2s 非阻塞）+ 用时 1s 计时（终局暂停累计）+ 进页恢复流（锁输入→异步取档→RestoreOrNewGame→解锁/死局删档）+ OnClose 同步保存；`ui/widgets.go`（ModalDialog/Toast/ResultBanner）+ `ui/repetition.go`（useRepetitionJudge.ts 翻译：k=2 toast/k=3 判负/判和确认框/k≥4 强制和/rewound 重置）+ Router 工厂页（每局一实例，#G4） | repetitionJudge.spec.tsx 3 用例逐条翻译 + rewound 重置 1 + 页面级 8 用例（恢复四路径/手动保存/自动保存错误提示/新游戏重置/OnClose/dispose 取消）+ Router 工厂 1，`-race` 全绿；`go run .` 启动冒烟通过 |

**M2' 范围注记**：双人页"保存为棋谱"/"分享棋局"两按钮随 M5' 棋谱对接落地——其协议面
（writeShareText/shareText 快照、RecordSaveDialog、recordFromSession）属 09 §1 pgnWriter
翻译清单，快照基准须上游同源，M2' 抢先实现会脱离快照护栏。08 §5 按钮行语义不变。

## M2' 验证门结果

- 翻译用例回归：autoSaveRestore.spec.ts 9 用例 + repetitionJudge.spec.tsx 3 用例逐条翻译全绿（`-race`）；
- 页面级/装配级新增用例全绿（恢复四路径、手动保存、自动保存错误提示、新游戏重置、OnClose、dispose 取消、Router 工厂）；
- `go run .` 启动冒烟通过（WSLg 弹窗，无 panic）；
- 质量门：`gofmt -l` 空输出 + `go vet ./...` 0 问题 + `go test ./... -race` 全量通过。

## M2' 复审记录（§6.1 两轮）

- 第一轮·语义一致性：BoardView 与上游 BoardView.tsx 逐条对照（动画结束 vm.OnTap(to)、历史增长守卫、飞行层隐藏起点格/被吃子保持可见）——修正动画期间高亮层被隐藏的偏差（上游保持显示）；裁决门槛对齐上游 historyLen<2（fenHistory<3）；vm.OnTap 复核与 M1' gamevm 翻译一致；恢复/保存语义与 07 §2 落地口径一致（文档先行）。
- 第二轮·缺陷扫描：修复 timer goroutine 与 Dispose 的 `p.timerStop` 字段竞态（-race 暴露，改闭包捕获）；修复模态弹窗遮罩下头部按钮穿透（悔棋可在弹窗下执行——上游遮罩全屏阻断，补页面级守卫）；迟到事件面审计（跨页 DbSaveDone/ToastHide 载荷按 Mode/Seq 防御，主页不实现 EventTarget 天然忽略）；无 goroutine 泄漏面（timer/toast/repo goroutine 均有界、可停）；无错误消息泄 Key；`-race` 全绿。
- §6.3 铁律自检：#G1 六包 import 面无 gio/net-http（grep 机器验证，仅注释命中）；#G2 六包 diff=零改动（git diff --stat M1'..HEAD）；#G3 新增代码无后台直写 UI/state（I/O 均经 repo 代理+事件回执，timer/toast goroutine 只 Emit）；#G4 对局页经 Router 工厂每局一实例（globalSettings 单例为例外面）；#G5 restore/save 请求均带 requestId，dispose Cancel 后迟到丢弃；#G8 ui/state 无网络出口。
- P3 登记：KG-008（弹窗遮罩下侧板 checkbox 仍可点击，影响极小）。

## M2' 已知问题与偏离注记

- KG-008（新增，P3）：见 docs/KNOWN_ISSUES.md。
- 语义偏离 1（有意，已收敛验证）：恢复取档回执先于死局判定——restore 触发 OnHistoryGrow 裁决，若恢复的存档恰为重复判和局，Gio 版在恢复决策内即判和→按死局删档开新局；上游（React effect 异步裁决）每次进入都会恢复同一盘"已判和"存档再判和，形成僵尸循环。Gio 版行为更收敛，登记备查。
- 语义偏离 2（有意）：进页恢复前锁输入（上游无此守卫，恢复完成前点击会被恢复覆盖）——防错 #5 口径的加强。
- minimize 事件源核实结论（07 §2 表遗留项）：gio 无直接窗口事件（system.ActionMinimize 仅为发起动作）；WSLg 实测最小化伴随失焦，blur 相位覆盖自动保存场景；派发点保留（autosaveBridge.OnMinimize→总线），其余平台覆盖面留后续里程碑手测。

## M2' 验收结论（2026-10-06，用户确认"验证通过"）

- 通过：双人完整对局（选中/高亮/动画/吞输入）、存档恢复/死局清理、悔棋、新游戏确认框、
  用时计时、长将 k=2/k=3 与闲着重复判和确认框、手动保存回执、离开自动保存、每局一实例；
- 验收反馈修复轮一项（KG-009 弹窗居中）复验通过，X11 截图取证
  （~/poc_evidence/m2/m2_dialog.png、m2_newgame.png）；
- M2' 新增遗留问题：KG-008（P3 弹窗遮罩下侧板开关可点）、KG-009（layout.Center 语义坑，
  已固化规避模式）。

## M2' 手测清单（存档——验收通过，2026-10-06）

数据目录：`~/.config/chinese-chess-ultra-gio/`（库 chinese_chess_ultra_gio.sqlite、settings.json）。

1. **主页 → 双人对弈**：入口卡可进入；空库时直接开新局（红先）。
2. **完整对局一局**（防错 #1/#2）：点红炮选中（选中圈+绿点/绿环目标高亮）→ 点目标格 → 棋子 220ms 飞行动画 → 动画结束才落子（lastMove 两圆高亮）；动画期间狂点棋盘应无任何反应（#1 吞输入）；点空白/再点选中子=取消或换选；吃子后被吃子随落子消失。
3. **走子连走至将军/将死**：被将方帅/将格红环提示 + 信息卡"将军！"；将死或困毙 → 结算横幅（红方胜！/黑方胜！），终局后棋盘禁手（#6：点棋子无选中）、按钮仍可悔棋。
4. **悔棋**：动画中点悔棋 → 动画作废不落子（#4）；悔棋后步数/走法记录同步回退，中文记谱正确（红方汉字、黑方数字口径）。
5. **新游戏确认框**：点新游戏 → 模态确认框（遮罩+面板），遮罩下头部按钮/棋盘不可穿透；取消=不变，确定=棋局清空+计时归零。
6. **自动保存/恢复**：走 2~3 手 → 切走窗口焦点（blur）→ 关闭应用重进双人对弈 → 应恢复到存档时点（局面/历史/悔棋链完整）；信息区步数与走法记录一致（防错 #7：存档含脏数据时跳过——可用文本编辑器改库文件 move_stack_json 注入非法记录验证）。
7. **死局清理**：走至将死 → 离开页面（触发存档）→ 重进双人对弈 → 应删档开新局（而非恢复已结束的棋）。
8. **用时计时**：信息卡用时每秒 +1；终局后停止累计；新游戏归零。
9. **重复裁决 k=2/k=3（#11/#12）**：构造长将环（车在底线连续将军来回，如车 a9 与黑王 e10 同线来回拉锯）→ 第二次重复出现非阻塞 toast"红方连续将军重复，再次将判负"（棋盘可继续操作）；第三次 → toast"红方长将，判负"+ 结算横幅"黑方胜！"。
10. **重复判和确认框**：构造闲着重复环（无将军的局面重复，如车与王各自行走摆动）→ 三次重复 → 确认框"三次重复局面"（接受和棋/变着继续）；拒绝变着后再重复 → 强制判和 toast+横幅"和棋"；接受 → 横幅"和棋"。
11. **手动保存回执**：点保存棋局 → toast"棋局已保存"；改库文件为只读/换损坏目录后保存 → toast"保存失败：本地存储不可用"（不吞错）。
12. **离开页面自动保存**：走几手 → 返回主页 → 检查库文件 saved_games 表有 humanVsHuman 记录（DR-008：fen=起始局面，moves=完整着法栈）。
13. **每局一实例**：主页→双人对弈→返回→再进入 → 应为新局（不残留上一局）。

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
