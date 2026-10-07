# 开发进度（PROGRESS）

## 当前状态

**M6' 工作室 + 求解器 + 识图对接——验收通过**（2026-10-07，tag v0.6.0-m6'；
T6'.1~T6'.5 完成 + §6 两轮复审 + 3 轮验收反馈修复，用户确认"全部验证通过"）。
下一里程碑 M7'（评估 + 打包发布，启动提示词=design_docs/11 §4.8，全新会话粘贴）。

## 里程碑总览

| 里程碑 | 状态 | 说明 |
|---|---|---|
| 设计文档阶段（含领域层复制） | ✅ | 本文件首条记录 |
| **M0' 工程骨架 + UI POC 排险** | ✅ 验收通过（tag v0.1.0-m0'） | T0'.1~T0'.7 完成；POC 五项结论回填 08 §12/DR-G003；两轮验收修复（KG-003~007） |
| **M1' 状态层核心 + 主页** | ✅ 验收通过 | T1'.1~T1'.3 完成；DR-G002 翻译用例全绿；验收反馈两项修复（居中/D-002 标题绕过）复验通过 |
| **M2' 双人对战 + 存储** | ✅ 验收通过（tag v0.2.0-m2'） | T2'.1~T2'.4 完成；翻译用例回归+13 项手测通过；验收反馈一项修复（KG-009 弹窗居中）复验通过 |
| **M3' 引擎对接 + 人机页** | ✅ 验收通过（tag v0.3.0-m3'） | T3'.1~T3'.2 完成；对拍回归全绿（复制物零改动）+ cancel/迟到丢弃用例 + 难度 5 实测 1.54s（≤7.5s）；验收反馈轮两项修复（chips 指针区/侧板溢出）+ 澄清（难度耗时）+ DR-G004 最小思考呈现 |
| **M4' LLM 全链路对接** | ✅ 验收通过（tag v0.4.0-m4'） | T4'.1~T4'.3 完成 + 12 轮反馈修复/优化（4 个 Gio 层真缺陷：每帧新建列表/嵌套列表事件盗取/列表漏设 Vertical/保存按钮未消费点击；界面缩放设置、Key 掩码显示、预设下拉式展开、百炼预设）；真实端点（百炼）一整局实测通过 |
| **M5' 语料 + 棋谱对接** | ✅ 验收通过（tag v0.5.0-m5'） | T5'.1~T5'.4 完成 + D-004 入口勘误落位（语料浏览→残局选关、棋谱库=记录库+保存为棋谱）+ D-005/D-006 对话框定案 + 3 轮反馈修复（下载 targetDir/启动器统一/剪贴板乱码/记录解码）；14 万局性能实测 FPS 76~87；导出快照与上游一致 |
| M6' 工作室 + 求解器 + 识图 | ✅ 验收通过（tag v0.6.0-m6'） | T6'.1~T6'.5 完成；6 验证 FEN 金标准回归全绿；§6 两轮复审修复 3 处 + 验收反馈 3 轮修复（结果面板死按钮→同帧 nil 绘制崩溃 ×2 处→辅助开关不可见/Tab 滚动化、棋谱库操作入口下拉菜单化） |
| M7' | ⬜ | 见 design_docs/10-实施路线图.md |

## M6' 完成清单（逐任务 commit）

| 任务 | 交付 | 验收结果 |
|---|---|---|
| 文档先行 | 00 §4 登记事件面：`solve:tick`（200ms 节拍）/`solve:done`/`solve:win:done`/`solve:assist:done`（提议回执）/`vision:done`/`dialog:pickfile`（文件选择，D-006 同型）+ timer:tick 行注记（识图已用时共用 1s 节拍） | — |
| T6'.1 solver Runner 直调 | `ui/solverclient.go`（复制物 solver.Runner 包装——SolveAsync/IsWinningFirstMoveAsync，响应 goroutine 经 env.Emit 回总线 #G3，Cancel=Runner ctx+总线双收口 #G5）+ events.go SolveDone/SolveWinDone 载荷（commit 154de81） | fake 4 用例 + 真复制物端到端（FEN-A 金标准求解+首着裁判）；emitCollector.waitPayload 改按序弹出 |
| T6'.2 工作室页 | rules 导出 InPalace/InOwnHalf 包装（**K9 处置**，KNOWN_ISSUES 已更新——原实现未动，一行登记）+ `state/studio.go`（setupRules.ts+studioValidate.ts 逐字翻译：PlacementIssue/CountIssue/ValidateStudioPosition 五条/SolveLabelOf/标题生成）+ `ui/studio.go` 三 Tab（摆盘校验含 FEN 导入并入/求解/识图）+ 非阻塞悬浮条（复用 DrawToast 无输入注册——TC-SOL-007）+ 结果面板置顶关闭（标题行常驻×+底部关闭兜底）+ 三种结论自动入库 + 进入对战联动 pendingBattle（commit f758c0f） | state 校验 4 用例（分支直译+金标准 FEN-A）+ 页面 8 用例（守卫/全链路/取消无残留/nil 防御/dispose/联动/入库失败）-race 全绿 |
| T6'.3 视觉识图 | `ui/filedialog.go` PickFileAsync（zenity/kdialog/osascript/powershell OpenFileDialog，PNG/JPEG 过滤，取消=空串）+ `ui/visionclient.go`（后台 goroutine 读文件+魔数 MIME+base64→复制物 VisionReader，AuthSlot 注入 DR-010）+ 识图 Tab（三槽位掩码加载/DR-009 借用注记/识别中按钮变灰+守卫防重入 K33/1s 已用时节拍）+ 人工校正流（[去摆盘校正]调色板=候选棋子选择器点击纠错/[重新识别]/[直接采用]）（commit 5089516） | 识图槽位/借用注记/K33 防重入/迟到丢弃/结果载入/失败路径 2 用例 |
| T6'.4 LLM 求解辅助 | `internal/llm/solveassist.go`（**新增翻译补全件**——上游 Wails 版此模块以前端 TS 直供；SOLVE_ASSIST_SYSTEM/user 逐字快照+ParseSolveProposal 三行解析+ProposeSolveFirstMove 重试链；diff 范围声明=本新增文件+K9 两包装）+ 工作室编排（提议 goroutine→solve:assist:done→裁判→四分支注释→求解照常）+ 研究助手配置弹窗（LlmConfigCard 视觉预设/掩码/KG-004 粘贴/测试连接/保存槽位重载）（commit 30f095e） | llm 8 用例（快照逐字/解析容错/重试/短路/enable_thinking:false 恒发断言）+ 页面 4 用例（借用全链路/失败注释/归一化/弹窗保存） |
| T6'.5 演示播放器 | `state/puzzledemo.go`（puzzleDemo.ts 逐字翻译——五态/interval 换算/保留速度/自定义间隔 clamp+倍率回 1x/循环重置/失步跳过防错 #8/坏 FEN error；Gio 形态：节拍由 UI ticker 按 CurrentInterval 驱动 Advance，注册备查）+ `ui/replayview.go` 完整版（速度档位 chips+自绘间隔滑块 widget.Float+循环 checkbox；跳转即停播回手动浏览）（commit d76b7b0） | state 11 用例（spec ①~⑪ 逐条）+ replayview 用例新语义适配（完成心跳→completed 停拍） |

### M6' 复审记录（§6.1 两轮）

- 第一轮·语义一致性：EndgameStudioPage 逐节对照（摆盘即时校验/求解设置/悬浮条/结果面板文案/入库标题/识图借用注记逐字）；studioValidate 五条与上游逐条对应；**修复**：SolveTick 迟到节拍缺代次守卫（补 Gen 丢弃）。
- 第二轮·缺陷扫描：**修复 1 个生产形态真竞态**——replayview ticker goroutine 每拍调 `player.CurrentInterval()` 直读主 goroutine 可写的 params（速度/间隔变更点），改捕获值 + 变更点重建 ticker（restartTickerIfPlaying）；求解中用户可改摆盘/参数 → **修复**：startSolve 捕获 redTurn/timeMs/plies 快照（结果面板与裁判口径一致）；runSolveAssist 无 Emit 防御（降级环境直接求解）。
- §6.3 铁律自检（机器 grep）：#G1 六包+state 无 gio/net-http（仅复制物既有 transport/vision/corpusDownloader）；#G2 复制物 diff=两新增翻译补全件（llm/solveassist*.go，上游 TS 直供模块补齐）+ rules/board.go 末尾 8 行 K9 导出包装（登记）；#G3 新增 goroutine（solver/vision/ticker/assist）只 emit 或读捕获值；#G4 工作室 RouteStudio 工厂页；#G5 solver/assist/verify/vision/dialog 请求全带 requestId，取消双收口，识图无取消入口以 disposed+id 双丢弃收口（K33）；#G6 配置 UI 无思维链开关（复用 LlmConfigCard，关闭参数恒发由复制物构造层保证）；#G7 掩码（assistantCard 掩码回显，ResolveAPIKey 只进传输层）；#G8 ui/state 无网络出口。

### M6' 语义偏离注记（有意，登记备查）

1. **三 Tab 收口**（08 §7 Gio 规格）：上游为摆盘/FEN 导入/识图三 Tab + 求解设置独立弹窗；Gio 版按 08 §7 定稿为摆盘校验/求解/识图三 Tab，FEN 导入并入摆盘校验 Tab（路径输入+解析按钮同功能），求解设置（限时/深度/大模型辅助）内联在求解 Tab（chips 呈现，M3' 起对 select 的既定偏离同型）。
2. **识图人工校正流形态**：上游在结果网格上点击纠错；Gio 版识图结果载入共享摆盘棋盘（T6'.2 摆盘编辑即纠错器——调色板选棋子放置/橡皮清除/点取走），校正流以三入口承接（去摆盘校正/重新识别/直接采用），语义等价。
3. **演示播放器节拍**：上游 puzzleDemo 内部 setInterval 心跳；Gio 版为纯状态机（Advance() 由 UI ticker 驱动，间隔变更点重建 ticker）——行为语义等价，测试以 Advance 注入替代 fake timers。
4. **识图已用时**：上游 setInterval 1s；Gio 版经事件总线 TimerTick（页面级计时共用载荷）驱动，reading 标志守卫。
5. 求解中允许继续摆盘编辑（非阻塞悬浮条语义，08 §3.6 优化 2）——编辑不影响在途求解（solveFen/参数为发起时快照），结果面板按快照呈现。

### M6' 验证门结果

- **6 验证 FEN 与上游一致**：✅（internal/solver 金标准回归全绿——多解/无解/超时/已将死/非法/缺王；复制物零改动）；
- 识图人工校正流手测：**待用户执行**（真实视觉模型端点，见手测清单）；
- 联动对战手测：**待用户执行**（工作室求解→进入对战）；
- 质量门：`gofmt -l` 空输出 + `go vet ./...` 0 问题 + `GOMAXPROCS=2 go test ./... -race` 全量通过；`go run .` X11 冒烟弹窗通过（MESA ZINK 告警为既有环境噪声，渲染正常）。

### M6' 验收反馈修复轮（2026-10-07，§6.2 流程）

1. **【修复】结果面板三按钮（×/进入对战/关闭）点击无响应**：layoutSheet 只渲染
   不消费 Clicked——死按钮（M4' 修复轮 11 同款教训）。补消费后引出：
2. **【修复·崩溃】点 ×/关闭直接退出应用**：关闭分支置 sheet=nil 后同帧仍继续
   layoutSheet 绘制 → nil deref panic（unwinding 时 defer clip Pop 二次 panic
   掩盖原错误）。改为关闭后本帧跳过绘制。
3. **【修复】求解 Tab"大模型辅助"开关不可见**：Tab 内容溢出裁切 + CheckBox 不
   显眼——Tab 内容区改页级滚动列表 + 开关 chips 化（开/关双 chip）。
4. **【修复·崩溃】助手配置弹窗"取消"同款退出**：assistantCard=nil 后同帧继续
   绘制——同款守卫修复；全仓排查同款模式（RecordSaveDialog/BattleLauncher 无
   风险）。
5. **【优化】棋谱库操作入口收拢**：记录卡单行化（标题+meta 左/"操作 ▾"右）+
   自绘右上锚定下拉菜单（遮罩点外关闭、删除红字、防穿透）——上游"每条菜单"
   同锚点，08 §8 注记；顺带修复既有真 bug：runAction 缺 actFile case（列表卡
   "导出文件"按钮无响应）。

### M6' 验收结论（2026-10-07，用户确认"全部验证通过"）

- 通过：摆盘校验（即时校验/数量上限/FEN 导入）、求解全链路（悬浮条不挡棋盘/
  防重发/结果面板置顶关闭/自动入库）、进入对战联动、识图（视觉模型+人工校正
  流）、LLM 求解辅助（借用+裁判注释）、助手配置弹窗、演示播放器（速度/间隔/
  循环）、离页取消、棋谱库操作下拉菜单；
- 验收反馈修复 3 轮（死按钮+2 处同帧 nil 崩溃+开关可见性 / 助手弹窗崩溃 /
  菜单化+actFile bug）复验通过；
- M6' 无新增遗留问题（K9 已按既定口径处置收口）。

## 请手测清单（M6'）

> 准备：`go run .`；研究助手需**视觉理解模型**（识图用）——"残局工作室 → 研究助手模型配置"弹窗：预设选 DashScope（qwen-vl-max）或智谱 glm-4.5v，填 Key → 测试连接 → 保存。无视觉模型时识图会失败（对话模型识图必败，错误提示会建议改用视觉模型）；求解辅助可借用对战配置（黑→红）。
> 图片：任一棋盘截图/正俯拍照片（PNG/JPEG）。

1. **摆盘校验（T6'.2）**：主页点"残局工作室"→ 摆盘校验 Tab：
   - 空盘 → 调色板选"帅"点九宫外 → toast"帅/将只能放在九宫内的 9 个位置"；
   - 摆齐一套子力（或点"初始局面"）→ 底部校验面板"校验通过（可保存 / 可求解）"；
   - 摆 3 枚红车 → toast"红方车最多 2 枚"；橡皮清除、点击已有棋子取走；
   - FEN 导入：粘贴 `3k5/9/9/9/R8/8R/9/9/9/4K4 w`（仅盘面字段）→ 解析并载入 → toast"已载入（红方行棋）"。
2. **求解与悬浮条（T6'.1/T6'.2 核心）**：求解 Tab → 限时/深度 chips 可选 → 点"AI 求破解"：
   - 底部出现深色悬浮条"求解中… 已用时 N.Ns"（**不挡棋盘**：悬浮条出现期间棋盘点击/摆盘仍可操作；悬浮条**无关闭入口**——TC-SOL-007 不可误关）；
   - 悬浮条出现期间再点"AI 求破解"应无反应（重复发起守卫）；
   - FEN-A 约 1~2s 出结果面板：**关闭按钮在标题行右上（×，恒可见）**，滚动解法列表后仍可点；底部"关闭"按钮兜底也可关；
   - 面板内容：用时、已保存到棋谱库、解法中文记谱（如"车一平四"）；
   - 回棋谱库页 → 列表出现"10-07 红方残局（唯一解）"（已破解筛选可见）。
3. **入库与进入对战联动**：结果面板点"进入对战"→ 四模式弹层 → 双人对弈 → 以求解局面开局（不写存档桶）；人机 AI → 默认玩家执红（求解方）先行。
4. **识图（T6'.3，需视觉模型）**：识图 Tab → "选择棋盘图片并识别"：
   - 识别中按钮变灰显示"识别中… 已用时 N 秒"（点击无反应——K33 无取消入口，单次超时 120s × 最多 2 次）；
   - 助手槽未配置时先测"借用"：清空助手槽三字段保存 → 识别 → 消息区"研究助手未配置，已临时借用黑方对战配置（不写入研究助手配置）——对战配置可能不支持识图"（对话模型识图必败并提示改用视觉模型——预期路径）；
   - 视觉模型识别成功 → 消息"识别到 N 枚棋子（红方行棋），已载入棋盘，请人工核对后再求解" + 三入口出现；
   - **人工校正流**："去摆盘校正" → 选棋子点棋盘纠错/橡皮清除错子 → 回识图 Tab"重新识别"或直接"AI 求破解"；"直接采用（去求解）"切求解 Tab。
5. **LLM 求解辅助（T6'.4，需对话或视觉模型）**：摆一个残局 → 求解 Tab 勾选"大模型辅助" → AI 求破解：
   - 助手槽空时 toast"研究助手未配置，已临时借用黑方对战配置"；
   - 结果面板出现"大模型首选 a4-d4（已验证为必胜着法）；思路: …"或"…未通过求解器验证，已忽略…"（模型水平决定，两态都算通过）；
   - 棋谱库详情信息卡"大模型注释"行同文案。
6. **研究助手配置弹窗（T6'.4）**：头部"研究助手模型配置" → 视觉预设 chips → Key 粘贴按钮 → 测试连接"连接成功" → 保存 → toast"模型配置已保存"（无 keyring 时如实提示明文回退）→ 重开弹窗 Key 显示 `****`+末 4 位。
7. **演示播放器（T6'.5，残局选关页）**：残局选关 → 任一残局详情：
   - 播放/暂停/继续/停止（停止回开局）；⇤◀▶⇥ 步进；
   - **速度档位 0.5x/1x/2x**：播放中切换立即变快/变慢（下一拍生效）；
   - **自定义间隔滑块**（200–4000ms）：拖动后 ms/步 数字变化、节奏跟随（拖动会把档位切回 1x——上游同语义）；
   - **循环**：勾选后播完自动从头重播不停止。
8. **离页取消（#G5）**：求解中返回主页再进 → 无旧结果闪现；识图中返回主页 → 迟到回执不落盘面（识图请求本身无法取消，K33 口径）。
9. **回归（自动）**：`go test ./... -race -count=1` 全绿。

## M5' 完成清单（逐任务 commit）

| 任务 | 交付 | 验收结果 |
|---|---|---|
| 文档先行 | 00 §4 登记语料/重放事件面（`corpus:scan`/`corpus:entries`/`corpus:batch`/`corpus:pgnindex`/`corpus:pgngame`/`corpus:progress`/`corpus:download`/`replay:tick`；parser:progress 修订为"Gio 直调不另设，批次级进度=corpus:batch"）+ 08 §8 M5' 落地注记（K4 纪律）（commit 21b778d） | — |
| T5'.1 状态层 | `state/corpusbrowser.go`（corpusBrowser.ts 372 行翻译：ParsedPuzzleView 投影/VisibleItems 筛选排序/PgnPageSlice 分页/CorpusIO+CorpusDriver 注入式异步编排/分批 128+generation 代次+协作取消通道）（commit 78ab697） | 上游 corpusBrowser.spec.ts 11 用例逐条翻译 + 4 条翻译扩展（分批 128/失败批保持 nil/目录缺失引导/迟到回执丢弃），`-race` 全绿 |
| T5'.1 语料库页 | `ui/corpusclient.go`（复制物直调 CorpusIO + 下载客户端）+ `ui/corpuspage.go`（下载引导含**取消按钮**防错 #10/分类列/XQF 面板/虚拟化行卡）+ `app/corpus.go`（corpusRoot 解析+工厂注册）+ cmd/poc corpus 冒烟模式（commit efb4fdf） | X11 截图取证 corpus_missing.png（缺失引导）/corpus_main.png（样例 XQF 端到端扫描→分批解析→77 着/难度/残局题投影） |
| T5'.2 PGN 面板 | 虚拟化连续长列表（KG-002 口径手势滚动）+ 搜索（Editor+IME，过滤缓存 dirty）+ 搜索跳转 + 行点击进详情 + `ui/corpusperf.go` 性能实测脚手架（CC_GIO_SYNTH_* 注入，生产零影响）+ 搜索框 KG-004 粘贴按钮（commit dc1f9e6） | **14 万局滚动实测（最坏口径：每帧重绘+每帧排帧）：稳态 FPS 76~87、帧开销 avg 3.2~5.7ms / max ≤6.5ms（预算 16.7ms）**；证据 corpus_pgn_perf*.png + perf 日志 |
| T5'.3 重放器+导出+进入对战 | `state/gamerecord.go`+`state/pgnwriter.go`（pgnWriter.ts 逐字翻译）+ `ui/replayview.go`（静态棋盘/步进/800ms 自动播放（goroutine+stop 通道+replay:tick，gen 迟到丢弃）/中文记谱走法列表/导出 PGN 复制（clipboard.WriteOp）/进入对战弹层）+ `ui/battle.go`（BattleMode 四选项+BattleStartFen）+ app pendingBattle 一次性消费 → 4 对局页构造器跳过恢复、以起点 FEN 开局、canSave=false（防错 #6）（commit f31e237） | **PGN 导出快照与上游逐字符一致**（2 快照用例）+ pgnwriter 8 用例 + 重放器状态机 8 用例 + 进入对战页用例 1，-race 全绿；截图 replay_detail4.png（77 着完整呈现）/replay_playing.png（4/77 自动推进） |
| T5'.4 文件对话框 | **D-006 定案（用户裁决：系统命令方案）+ 落地**（commit c86d469）：ui/filedialog.go（zenity/kdialog/FolderBrowserDialog/osascript，取消=空串）+ 语料页"选择其他棋谱目录"（→ 写 corpus.userPath → 重扫）+ 记录库"导出文件"操作；路径输入保留为兜底 | zenity 本机实测可启动；设置写入存储层独立验证 set ok；UI 全链路待用户手测 |

### M5' 复审记录（§6.1 两轮，commit c028ff8）

- 第一轮·语义一致性：corpusBrowser.ts 逐方法对照（可见列表/分批/generation/打开单局文案逐条）；CorpusBrowserPage/PuzzleDetailView 逐节对照；事件名与 00 §4 清单一致。修正：详情元信息补难度文案；parser:progress 登记行修订（Gio 直调形态不另设）。
- 第二轮·缺陷扫描：**修复 1 个生产形态真竞态**——`opDone` 通道字段被工作 goroutine（stale 探针）与主 goroutine（beginOp close+重建）并发读写，同步测试驱动掩盖；改为闭包值捕获 + staleChan。另：粘贴按钮 28→34dp（material.Button 内边距裁切文字，M3'/M4' 同类教训）；清理未用字段。
- §6.3 铁律自检（机器 grep）：#G1 六包+state 无 gio/net-http import（仅注释命中）；#G2 复制物 diff（v0.4.0-m4' 起）=零改动；#G3 新增 goroutine 仅 emit/自有通道；#G4 语料 store 为页面实例、对局页全部工厂；#G5 requestId+代次双收口（离页取消解析/下载）；#G8 ui/state 无网络出口。

### M5' 语义偏离注记（有意，登记备查）

1. **PGN 长列表虚拟化连续滚动替代上游 DOM 分页呈现**（08 §8 注记①，KG-002 口径）：上游 50 局/页翻页是 Web 渲染约束；Gio layout.List 虚拟化下连续滚动+搜索跳转等价且为 08 既定口径。pgnPageSlice 分页语义保留于状态层并有翻译用例（Gio UI 不消费分页控件）。
2. **重放器 M5' 为基础版**（06 附录 §7 既定）：步进+800ms 自动播放+停止回开局；速度档位/循环/自定义间隔随 M6' puzzleDemo 状态机（T6'.5）。
3. **离开语料页取消在途解析/下载**（#G5 口径，工厂页 Dispose）：上游为 WebView 后台继续+进度监听断开；本仓离页即取消（半成品+ETag sidecar 保留，重试续传——防错 #10）。
4. **导出 PGN 的语料投影**：语料无 result/solveStatus 面——Result 恒 `*`；mode 投影 endgame/humanVsHuman；SetFen/FEN 按初始局面是否标准判定（pgnWriter 语义不变）。
5. **进入对战起点**（recordBattle 语义语料等价）：残局题=initialFen；全局对局=终局局面（重放 moves）；上游"已分胜负不提供入口"分支不适用于语料。humanVsAi 续战默认玩家执红（FEN 决定行棋方）。
6. **目录选择按钮未实现**（T5'.4 待定案）：下载引导页以提示行替代（"手动将语料目录放置到期望路径后重新进入本页"）；导出 PGN 文件（保存对话框）同待定案，剪贴板导出先行。
7. 语义偏离 1~7 中涉及行为变更的文档面已同步（00 §4/08 §8，同 commit 或文档先行）。

### M5' 验收反馈修复轮（2026-10-07，§6.2 流程）

用户反馈两项（下载报错 + 入口归属），处置：

1. **【修复·D-004】入口归属勘误**：语料浏览（扫描/下载/XQF/PGN）应归属
   "残局选关"入口，"棋谱库"应是用户棋谱记录库。上游锚点核实：上游
   App.tsx——残局选关(/puzzle)=CorpusBrowserPage；棋谱库(/record-library)=
   RecordLibraryPage/RecordDetailPage；写入侧=对战页 RecordSaveDialog+
   工作室求解入库（M6'）。Gio 08 §8/10 M5' 行原表述为文档错误，实现按文档
   执行连带错位。落位：残局选关=语料库浏览页（标题沿上游 h2）、棋谱库=
   记录库页（列表/筛选/详情重放/进入对战/导出/分享/删除）+ 对局页
   "保存为棋谱"（RecordSaveDialog，标题留空自动生成/备注→game_records）；
   决策记录 docs/decision_log.md D-004。
2. **【修复】下载 rename ENOENT**：`rename corpus.tmp-<ts>: no such file
   or directory`——CorpusDownloader 漏掉上游绑定层语义（targetDir 空 =
   corpusRoot 解析），复制物收到空串致 staging 相对目录+rename 空目标；
   解压失败后复制物整体清理临时 zip（语义正确）表现为每次从头下载。
   修复+单测锚定；每次从头下载非续传缺陷，根因修复后取消/中断续传生效。

修复轮 2（记录库冒烟发现）：①RecordDataFromStorage 走法解码改 JSON 往返
（coordPair 仅识别 JSON 形状，直传 [2]int 全解码失败）；②详情页"返回"独立
点击器回列表（原误用主页返回）；③记录卡铺满宽+信息卡 360dp（chips 裁切）。

修复轮 3（验收反馈：语料重放器弹窗错位不可点/剪贴板中文乱码/导入本地语料）：
1. **【修复】进入对战启动器统一共用组件**（commit e018469）：语料重放器旧
   弹窗 layout.Center 在 Stack Stacked 子节点退化为左上角贴边（KG-009 同类，
   图1），且无关闭入口。提取 ui/battlelauncher.go（宏量测+op.Offset 居中+
   取消/返回+人机 AI 执方选择步），replayview/recordlibrary 两处同界面；
   poc 实测模式点击/取消均生效（日志取证）。
2. **【修复】剪贴板中文乱码**（commit 867aebd）：gio clipboard.WriteCmd 经
   WSLg 桥接写 CJK 乱码（KG-004 反方向）——导出 PGN/分享文本改走
   CopyToWindowsClipboardAsync（PowerShell Set-Clipboard，UTF-8 base64），
   回执经 clipboard:write 事件（00 §4 已登记）toast 提示，失败回退
   WriteCmd（非 WSL 面）。PGN 中文头（Event/棋手名）为上游快照锁定协议面，
   乱码系传输层。
3. **【新增】导入本地语料目录**（commit 7c68e34，D-005）：缺失引导页新增
   路径输入（Editor+粘贴按钮）+目录存在性校验+使用该目录/恢复默认 → 写
   corpus.userPath → 重扫；用户设置路径优先（ResolveCorpusDir），不复制
   不安装到下载位置——与用户指定语义逐字对应。

修复轮 3 补充（验收反馈：棋谱库选模式不进对局页）：**【修复】BattleLauncher
target 捕获时序**——l.Close() 先清空 target 再回调 onLaunch(l.target)，
记录库启动以 target 计算起点 FEN，收到 nil 静默不导航（语料页不用 target
故正常）；改为先捕获 target 再 Close。真实链路冒烟：棋谱库详情 → 启动器 →
双人对弈 → 实际导航并以记录起点开局。

### M5' 验收结论（2026-10-07，用户确认"全部测试通过"）

- 通过：语料下载/取消/续传、分类与 XQF/PGN 浏览、重放器（步进/自动播放/导出/进入对战）、
  保存为棋谱（自动标题落库）、棋谱库=记录库（列表/筛选/详情重放/进入对战/导出/分享/删除）、
  导入本地语料（zenity 对话框）、导出 PGN 文件（zenity 保存对话框）、剪贴板中文、
  离页取消、14 万局长列表性能；状态层/复制物自动回归。
- 验收反馈修复 3 轮（下载 targetDir 未解析 / D-004 入口归属勘误+记录库落地 /
  启动器统一+剪贴板乱码+导入对话框 D-005/D-006）复验通过。
- T5'.4 文件对话框决策项定案（D-006）并落地——M5' 无遗留开放项。

### M5' 验证门结果

- 语料下载/解析手测：**待用户执行**（真实 45MB 包下载/取消/半成品保留，见手测清单）；
- 长列表性能：✅（FPS 76~87 / avg 3.2~5.7ms / max ≤6.5ms，最坏口径实测）；
- 导出快照与上游一致：✅（逐字符，2 快照）；
- 质量门：`gofmt -l` 空输出 + `go vet ./...` 0 问题 + `go test ./... -race` 全量通过；`go run .` 冒烟通过。

## 请手测清单（M5'，D-004 修正后）

> 准备：`export PKG_CONFIG_PATH="$HOME/.local/lib/pkgconfig"`；
> 中文搜索词走搜索框旁"粘贴"按钮（KG-004，先在 Windows 复制）；
> 语料包约 45MB（GitHub Release），首次下载失败可重试。

1. **语料下载（残局选关入口，T5'.1 核心）**：主页点"残局选关"→"语料库浏览"（缺失引导）：
   - 期望路径 /home/<user>/Documents/ChineseChessUltra/corpus 显示正确；
   - 点"下载语料包（约 45MB）"→ 进度 MB 推进 → **中途"取消下载"** → **再下载应续传**（上轮失败根因已修：targetDir 解析；若仍整体重下请记录网络/ETag 情况）；
   - 完成后自动重扫进分类视图。
2. **分类与 XQF 浏览（残局选关入口）**：分类切换、解析进度、行卡投影、搜索/仅残局/难度/排序、中文粘贴搜索。
3. **PGN 大文件（残局选关入口）**：索引"共 N 局"、长列表滚动流畅、ASCII 搜索、行点击进重放器。
4. **重放器（残局选关入口）**：播放/暂停/继续/停止/步进、走法列表半着跳转、残局题与全局对局各一局、"导出 PGN（复制）"粘贴核对、"进入对战"四模式弹层。
5. **保存为棋谱（四对局页，D-004 新增）**：双人对弈走几步 → "保存为棋谱" → 标题留空保存 → toast"棋谱已保存"。
6. **棋谱库=记录库（D-004 核心）**：主页点"棋谱库"：
   - 列表显示刚保存的记录（标题自动生成"YYYY-MM-DD 模式名"、模式·日期·对局标签）；
   - 空态文案（无记录时）："暂无棋谱。可在对局中保存，或在残局工作室求解后自动入库。"（工作室入库随 M6'）；
   - 点标题进详情：对局类定位保存时局面、◀▶ 步进、记谱芯片跳转、信息卡；
   - 操作按钮：导出 PGN（复制粘贴核对七标签+ICCS）、分享文本（复制核对中文记谱）、删除（确认框→列表刷新）；
   - 筛选 chips（对局/已破解/无解/未决超时）toggle；
   - "进入对战"→ 四模式弹层 → 人机 AI 附执方选择（玩家执红/黑）→ 起点局面开局、不写存档桶（防错 #6）。
7. **离页取消（#G5）**：批量解析/下载中途返回主页再进，无旧内容闪现；下载取消半成品保留。
8. **性能复核（可选）**：`CC_GIO_SYNTH_PGN=140000 CC_GIO_SYNTH_SCROLL=1 go run ./cmd/poc corpus`，参考 FPS 76~87 / avg ≤6ms。
9. **回归（自动）**：`go test ./... -race -count=1` 全绿。
10. **T5'.4 决策项**：随本轮验收一并定案（决策矩阵见会话输出：A zenity 库 / B 系统命令对话框 / C 自绘输入 / D 维持现状，推荐 B）。

## M4' 完成清单（逐任务 commit）

| 任务 | 交付 | 验收结果 |
|---|---|---|
| T4'.1 | `ui/llmclient.go`（commit T4'.1）：复制物 transport 包装——moveTransport 做 AuthSlot 注入（DR-010）+ chunk/done/error 事件 tee 到 app 事件总线（外层页面级 requestId 标记）；MoveAsync=goroutine 生存期即请求生存期（NextMove 阻塞至结算，00 §4 "cc:llm:chat"行）；取消=走子 ctx + player.CancelCurrent + 总线 Cancel 三收口（#G5；StreamTransport.Cancel 幂等）；TestConnectionAsync 走复制物 Proxy.TestConnection；EngineRunnerAdapter 参谋同步面（Submit+等待，ctx 取消撤销引擎请求，**参谋一律不传 HistoryFens**——可复现铁律）；`app/llmrepo.go` ui.LlmStore 代理（凭据槽位异步读写、ResolveAPIKey 仅注入面、llm_settings 写侧后台 I/O）；events.go 新增载荷 + 00 §4 四行登记（文档先行） | mock SSE 回归全绿（internal/llm -count=1 复跑）；tee 事件/AuthSlot 三态/取消链/参谋适配器（含 ctx 取消）/httptest mock 测试连接 + 掩码 roundtrip/降级路径，`-race` 全绿；事件总线 requestId 过滤/迟到丢弃用例（M1' 既有）回归 |
| T4'.2 | `state/assistantconfig.go`（assistantConfig.ts 逐字翻译：助手槽全空→黑→红运行时借用，纯内存永不写助手槽，部分填写不借用 DR-012 边界，authSlot 随来源槽）+ `ui/llmconfigcard.go`（四字段表单 Editor+IME、预设 chips=presetFor 翻译、掩码 Key 回显、测试连接、**无思维链开关**——仅 DR-005 固定提示语）+ `ui/winclip.go`（KG-004 粘贴异步化，每字段定向回填）+ `ui/widgets.go` layoutOptionChips 共享化 | assistantConfig.spec.ts 9 用例逐条翻译全绿；配置卡状态用例 6（预设选择/掩码往返/粘贴回填/测试连接/无开关结构锚定）全绿；app 代理掩码 roundtrip（掩码回写不覆盖真实 Key，防错 #8）/降级路径 2 用例全绿 |
| T4'.3 | `ui/humanvsllm.go`（翻译源=HumanVsLlmPage.tsx：黑方大模型/内置 AI 二选一、配置卡+设置 chips、两路汇合触发、应手结算/failed 判负 #7/迟到丢弃、思考中悔棋=中止重想、DR-014 镜像视图、内置路径补 DR-G004、立即保存粘性置底）+ `ui/llmvsllm.go`（翻译源=LlmVsLlmPage.tsx：循环=主 goroutine 状态机 pump 步进（#G3 等价上游 async while）、开始校验 DR-012/DR-014、暂停作废在途、loopGen 代次收口、interval tick 经事件总线、双槽位保存、恢复不自动续跑）+ `ui/llmcommon.go`（流式消息区 chunk 追加+备注/否决链条目+置底；choiceRow consume/draw 分离）+ app 两页工厂（#G4）+ 两页 RepetitionJudge 裁决接线（人机页玩家侧弹框/引擎侧自动；LLM vs LLM 双方自动） | 页面用例 15（镜像/触发 spec/结算/取消链/流式 K21/保存镜像拦截/测试连接 override/内置对手/防抖代次/循环交替/interval tick/暂停作废/停止新游戏/failed 判负/迟到丢弃）`-race` 全绿；X11 截图取证两页渲染（~/poc_evidence/m4/home.png、humanvsllm.png、llmvsllm.png）；`go run .` 冒烟无 panic |

## M4' 验证门结果

- **mock SSE 回归全绿（复制物）**：`go test ./internal/llm/ -race -count=1`（含 GLM thinking 参数断言=恒发关闭参数、mock 写已断开连接守卫）+ 金标准对拍（internal/engine -count=1）全绿，复制物零改动；
- **事件总线 requestId 用例**：M1' 总线过滤/迟到丢弃回归 + moveTransport 外层 id 映射用例 + 页面级 K21 二次收口（chunk/MoveDone 双面）全绿；
- **掩码/借用用例**：Credentials 掩码语义（复制物既有）+ app 代理掩码 roundtrip/掩码回写合并 + DR-009 借用 9 用例全绿；
- **真实端点手测一整局**：待用户执行（GLM/DeepSeek 配置与手测指引见下方手测清单）；
- 质量门：`gofmt -l` 空输出 + `go vet ./...` 0 问题 + `go test ./... -race` 全量通过；`go run .` 冒烟通过。

## M4' 复审记录（§6.1 两轮）

- 第一轮·语义一致性：humanvsllm.go/llmvsllm.go 与上游两页逐行为对照——修正三处：①弹窗遮罩下对局设置 chips/引擎类型 chips 点击边沿未消费（KG-008 同类面，收口为全消费不生效）；②防抖保存字段面按上游 copyWith 对齐（对手引擎类型仅"立即保存"落盘）；③内置 AI 对手路径补 DR-G004 最小思考呈现 300ms（同人机页）。测试连接镜像 override（DR-014）与上游 testOverride 逐字对应。
- 第二轮·缺陷扫描：goroutine 面审计（moveTransport emit 只经 env.Emit；OnAttempt/paste/persist 定时器全部事件回主循环；参谋适配器阻塞仅在走子 goroutine 内合法）；迟到面审计（页面 requestId 防御 + 总线取消 id 双层 + loopGen 代次三面）；错误消息面无 Key 泄漏（ResolveAPIKey 返回值只进 Authorization 头）；choiceRow 虚拟化行消费面每帧全量（draw 仅可见行）；`-race` 全绿。
- §6.3 铁律自检（机器验证）：#G1 六包 import 行无 gioui.org、net/http 仅 llm/transport+vision 与 storage/corpusDownloader（复制物既有口径）；#G2 复制物+cmd/eval diff=零改动；#G3 新增代码后台 goroutine 只 Emit；#G4 两页 Router 工厂；#G5 llm/restore/save/test 请求均带 requestId，取消三收口；#G6 配置 UI 无思维链开关（结构用例锚定，关闭参数由复制物构造层恒发）；#G7 掩码回读/回写合并（代理层测试锚定）；#G8 ui/state 无网络出口。
- P3 登记：KG-010（流式 chunk 事件通道满非阻塞丢弃——#G3 语义使然，消息区可能缺段，结算回执不受影响）。

## M4' 已知问题与偏离注记

- KG-010（新增，P3）：见 docs/KNOWN_ISSUES.md。
- 语义偏离 1（有意，登记备查）：**DR-014 镜像的 authSlot 取红槽**——上游人机 LLM 页镜像态构造 HybridLlmPlayer 恒传黑槽，掩码 Key 下 Proxy 按黑槽注入会落空（红方 Key 在红槽）；本仓按同页 testConnection 的 testOverride 同口径取红槽（authSlot 随生效配置来源，DR-010 闭环）。镜像视图编辑不回写黑槽的 DR-014 语义不变。
- 语义偏离 2（呈现层）：**流式消息区**为人机 LLM/LLM vs LLM 两页新增（08 §5 Gio 规格；上游状态条仅显示 note）——chunk 正文追加渲染 + 〔参谋〕〔兜底〕〔判负〕〔异常〕条目（VetoFeedback 否决链经 result.Note 入列）；思维链正文不呈现（DR-005 恒关）。
- 语义偏离 3（布局层）：对局设置/引擎类型的 `<select>` 一律 chips 呈现（Gio 无 select；M3' 难度 chips 同款偏离）；预设选择 6 项折两行。
- M3'/M2' 遗留偏离注记（PlayMove 即时落盘+视觉飞行层、恢复取档先裁决、进页锁输入等）对 M4' 两页同样适用（复用同一骨架）。

## M4' 验收反馈修复轮（2026-10-06，§6.2 流程）

用户反馈三项（人机 LLM 对战页），处置：

1. **【修复】表单边界控制字符清洗（sanitizeField）**：百炼端点测试连接 404 回显
   "The model `qwen3.8-max:\u0000\u0000\u0000\u0000` does not exist"——请求体模型 ID
   夹带 4 个 NUL 控制字符（经键入/剪贴板路径进入 Editor，原样进入请求体；其他软件
   用同一模型+Key 正常即佐证请求不干净）。配置卡三字段（baseUrl/apiKey/model）在
   表单边界统一剥离 C0 控制字符与 DEL + TrimSpace（UI 层清洗，复制物协议构造不动，
   #G2）；SetConfig/ApplyPaste/Config() 三入口全覆盖 + 用例锚定。
   **复测指引**：重新打开配置卡，不改 Key 直接"测试连接"；若干净模型名仍 404，
   则该账号未开通 qwen3.8-max（复制物视觉预设示例模型，M6' 识图用）——换
   `qwen-max` / `qwen-plus`（百炼对话模型）验证。
2. **【修复】设置面板字号过小**：LLM 两页侧板整体加大——配置卡标题 13→15sp、
   字段标签 12→14sp、输入框 13→14sp、提示行 11→13sp、chips 12→13sp（共享
   layoutOptionChips，人机页同步受益）、小节标题 13→15sp、消息区 11→13sp+
   高度 120→140dp、选择行标签 12→14sp；侧板宽度 320→380dp。截图取证
   ~/poc_evidence/m4/humanvsllm_fontfix.png。
3. **【登记·KG-011】最大化后无法最小化**：应用不拦截标题栏按钮（无 app 侧代码
   路径）；gio x11 仅在 app 发起 Configure(Minimized) 时调 XIconifyWindow，
   最大化态下 WM 侧 iconify 请求不生效——疑 WSLg 窗口管理器对 gio 窗口的
   WM_STATE 交互缺陷。变通：先"还原"再最小化；升 gio 复核（与 KG-003 同项）。

回归：gofmt/vet/`go test ./... -race -count=1` 全绿（commit fix(m4')）。

### 验收反馈修复轮第 2 轮（2026-10-06，§6.2）

用户复测反馈：测试连接已成功，但**下棋仍报同款 404**；字号仍偏小。

1. **【根因补全·修复】走子管线路径的脏配置未清洗**：用户存储中的配置已带
   NUL（首轮修复前保存），首轮只在配置卡边界清洗——测试连接用表单清洗值
   （成功），走子管线用页面持有的存储原始值（`p.config`，仍带 NUL）→ 404。
   实证：读取用户存档 `credentials.enc`，model = `"qwen3.8-max` + 4 个 NUL 字节`
   （无冒号——报错里的冒号是百炼错误模板自己的回显格式）。
   **修复**：清洗上移到存储读写边界——`state.SanitizeTextField`（纯 Go，双端
   共用）+ app 凭据代理 LoadSlotAsync/slotToLlm（回读清洗）与 SaveSlotAsync
   （落盘清洗）双入口；**用户无需重填配置，重启即净**。用例：脏存盘 roundtrip
   （load 出 model 干净 + Key 掩码）锚定。
2. **【修复】字号第二轮加大**：标题 16sp/选择行与字段标签 15sp/输入框 15sp/
   chips 14sp/消息区与提示 14sp（消息区 150dp）；侧板 380→400dp。截图
   ~/poc_evidence/m4/humanvsllm_fontfix2.png（面板裁剪对照）。

回归：gofmt/vet/`go test ./... -race -count=1` 全绿。

### 验收反馈修复轮第 3 轮（2026-10-06，§6.2）

用户复测反馈：下棋已正常（NUL 清洗修复确认生效）；侧板字体/布局仍不满意
（面板窄、内容拥挤、行高小）；最大化后最小化/最大化按钮均无响应。

1. **【修复】侧板比例宽度**：定宽 400dp → max(420dp, 窗宽 30%)——最大化/
   宽窗下侧板随比例加宽（4K 最大化下约 600dp），内容不再拥挤；两页同口径。
2. **【修复】字号第三轮 + 行高放宽**：小节标题 17sp/字段标签与选择行 16sp/
   输入框 16sp（框高 40dp）/chips 15sp（高 30dp）/消息区与提示 15sp/
   备注 15sp；粘贴按钮 52×28dp；行距统一放宽。
3. **【修复】消息区首行被裁切**：删除手写 scrollToEnd（First=n-1 把末行顶到
   视口顶部，长行被上缘裁切），改用 gio layout.List 原生 `ScrollToEnd`
   （末条目贴底；用户上滚后 BeforeEnd 置位自动停止跟随——阅读不被打断）。
4. **【登记】KG-011 补充**：最大化后"最小化/最大化（还原）"按钮均无响应
   （仍为 WSLg WM × gio WM_STATE 交互缺陷，应用侧无代码路径）。

回归：gofmt/vet/`go test ./... -race -count=1` 全绿；截图
~/poc_evidence/m4/humanvsllm_fontfix3.png。

### 验收反馈修复轮第 4 轮（2026-10-06，§6.2）

用户复测反馈：面板变宽了但"字体完全没变化"；chips 文本换行裁切（"内置AI
代走"/"快(2层)"被切半）。

1. **【根因定位】显示缩放不足**：用户环境 2560×1600 + Xft.dpi=144（gio 已按
   1.5x 渲染），16sp≈24px 物理像素在该屏上仍偏小——sp 数值微调不可感知，
   需要全局缩放手段。
   **【修复】界面缩放设置（08 §9 Gio 版新增，文档先行）**：主页"全局设置"
   弹窗（补齐 M2' 预留项）= autoSave 开关 + 缩放档位 chips（100%~200%，键
   `global_ui_scale` 持久化）；app 装配层 FrameEvent 按乘数缩放 `unit.Metric`
   （dp/sp 全局换算，指针命中不受影响），**调整即刻生效**。缩放机制端到端
   验证（弹窗点击 150% → 全局变大 + settings.json 落盘 1.5 + 重读生效，
   截图 settings_modal.png/settings_150.png/llm_150.png）。
2. **【修复】chips 定宽换行裁切**（三轮字号问题下被掩盖的真布局缺陷）：
   layoutOptionChips 改为按标签内容自适应宽度（无约束测量遍丢弃 ops 后
   定宽），"内置AI代走"/"快(2层)"等长标签不再换行截断；行距放宽
   （choiceRow 上下 inset）。
3. 用例：UIScale 默认/持久化/回读（state）。

回归：GOMAXPROCS=2 go test ./... -race -count=1 全绿（首跑 TestProxyTotalLimit
为 KG-007 已登记单核饥饿偶发，复跑通过）。

### 验收反馈修复轮第 5 轮（2026-10-06，§6.2 收尾）

1. **【修复】侧板比例项改 dp 口径（缩放自洽）**：窗宽 30% 的比例项原为像素
   口径——界面缩放调高后 dp 空间变小，文字会再次拥挤；改按窗宽 dp 计算
   （同 humanvsllm/llmvsllm 两页），任意缩放档位下布局一致。截图
   ~/poc_evidence/m4/llm_panel_dp.png。
2. 排障注记：合成点击（xdotool）偶发未生效/误触（第 4 轮 settings.json 里的
   global_ui_scale=1 即误触 100% 所致）——机制本身已端到端验证，用户实测为准。

回归：gofmt/vet/go test（-race，GOMAXPROCS=2）全绿。

### 验收反馈修复轮第 6 轮（2026-10-06，§6.2）

用户复测反馈：①右侧设置面板应可垂直滚动（参数显示不全）且"立即保存"恒在
底部不随滚动；②缩放设置"又变小了"。

1. **【修复·真根因】侧板外层滚动列表每帧新建**：`layoutSidePanel` 在布局闭包
   内 `list := layout.List{...}`——立即模式下每帧新建即清零滚动状态（First/
   Offset 每帧归零），**拖拽/滚轮永远无效**。改页级字段 `p.sideList`（构造期
   初始化，两页同修）；"立即保存"粘性置底结构本就正确（Flex 竖排
   {Flexed(1) 滚动区, Rigid 按钮行}——列表可滚后按钮恒可见）。
2. **【考古结论·非缺陷】缩放回退=我的验证点击误触**：settings.json 的
   global_ui_scale 被覆写为 1 系本会话自动化点击（xdotool 合成点击不可靠）
   误触 100% 档所致——持久化机制本身已由状态层测试证明（SetUIScale→
   st.Set→Load roundtrip 1.5 正确落盘/回读，uiscale_test.go）。**已代为恢复
   1.5**，用户下次启动即为放大档；此后重设即持久。
3. 排障工具失效注记：本会话截图回读通道中途失效（media omitted），GUI 取证
   不可靠——本轮以代码审读+状态层测试为准；pkill 模式含命令行同名
   字面量会自匹配杀死执行 shell（三次踩坑，改字符类+拆分调用）。

回归：GOMAXPROCS=2 go test ./... -race -count=1 全绿。

### 验收反馈修复轮第 7 轮（2026-10-07，§6.2）

用户复测反馈：150% 缩放下"设置区只剩消息内容区，其他设置看不见了"。

1. **【修复·真根因 2】消息区内嵌列表盗取滚动事件**：消息卡片内嵌
   layout.List（上轮 ScrollToEnd 改造引入）——嵌套列表在消息卡片区域内把
   滚轮/拖拽全部消费（自己无内容可滚也不放行），外层侧板列表在消息区上
   永远收不到事件；150% 下消息区是侧板最大可见块 → 滚动"完全死亡"、
   设置区永远在折叠线下。**消息区去内嵌列表**：普通列随内容自然增高、
   随侧板单列滚动（无嵌套=无事件竞争）；填充改"先量内容再绘制"（原
   fill 用约束 Max.Y=剩余空间，视觉上越界盖住下方——KG-009 同族坑）。
2. 侧板滚动列表每帧新建的真根因（第 6 轮）+ 本轮合并后：侧板=单一
   layout.List（页级字段），全部行随滚动可达，"立即保存"恒底部。
3. GUI 取证注记：本会话截图回读通道失效+合成点击不可靠+多实例窗口
   干扰——视觉验收交还用户；代码层面结论均有单测/审读依据。

回归：GOMAXPROCS=2 go test ./... -race -count=1 全绿。

### 验收反馈修复轮第 8 轮（2026-10-07，§6.2）

用户复测反馈：150% 下"设置区只剩消息内容区，其他设置还是看不见"。

**像素级取证（用户截图程序化分析）定位三个真因，全部修复：**

1. **【修复】消息区无限增高把设置区挤出视口**：第 7 轮去内嵌列表后未设
   高度上限——一局的消息（流式正文+备注条目，2.25x 下每行 ~40px）把
   配置卡/设置区全部推到折叠线下；用户截图实测配置卡的白色编辑框在
   整屏内 **0 命中**（编辑框从未进入视口）。**修复**：①消息区固定高度
   220dp + 只显示最新 10 行（tail，超出从最旧侧丢弃、内存留 60 行——
   消息区是流式过程/否决链临时呈现，完整 note 仍在状态条）；②消息区
   **移到设置区之后**（行序：信息→对手→配置卡→设置行→消息区）——
   设置常驻上方无需深滚。
2. **【修复】PageTheme.ContrastBg 缺省**：material 默认 #3F51B5（indigo）
   与本仓色板无关——未显式着色的 Button（立即保存等）用它；用户截图
   indigo 命中 1131 采样点。**修复**：ContrastBg = ThemeSeed，截屏复测
   indigo 残留 0。
3. **【验证通过】侧板滚动**：第 6/7 轮两处根因（每帧新建列表+嵌套列表
   事件盗取）修复后实测——编辑框 18 段可见、滚轮滚动后内容位移
   （18 段→4236 白像素）、"立即保存"恒底部；布局行结构完整
   （信息/对手/配置卡/设置行/消息区/保存按钮）。

回归：GOMAXPROCS=2 go test ./... -race -count=1 全绿。

### 验收反馈修复轮第 9 轮（2026-10-07，§6.2）

用户反馈四项，处置：

1. **【修复·行索引冲突】"设置分两列/乱序"**：第 8 轮把消息区移到设置后时，
   行号 iota 常量 `sideRowMsg(4)` 与设置行 i=4（无效回复重试）**相撞**——
   消息卡嵌入设置行中间、部分设置行丢失。**修复**：行派发改 total 驱动
   （0=信息 1=对手 2=配置卡 3..n=设置行 末行=消息区；两页同修，
   `sideRowCount()` 替换常量索引）。
2. **【修复】Ctrl+V 粘贴追加杂字符**（key 尾部 "ache]On"、模型 ID 尾部
   "wr"）：WSLg 剪贴板桥接对混合剪贴板内容在边界处产生垃圾（KG-004
   已扩充记载）。**修复**：baseUrl/apiKey/model 三字段改 ASCII 域清洗
   （sanitizeASCIIField：非 ASCII + 控制字符全剥离——CJK mojibake 块
   随之消除）；ASCII 垃圾（"ache]On"）无法可靠识别，**指引性修复**：
   配置卡新增提示行"Ctrl+V 经剪贴板桥接可能损坏或追加杂字符，请优先
   用各字段旁的「粘贴」按钮"。
3. **【修复】API Key 掩码显示**：编辑器改 gio Editor.Mask='•' 默认掩码
   显示 + "显示/隐藏"切换按钮（点击明文，再点隐藏）；已保存 Key 的
   掩码回显（****+末4位）在明文态呈现，掩码回写合并语义不变。
4. baseurl 两路粘贴一致 ✓（无改动）。

实测（150%→用户自调 1.25）：编辑框 15 段可见、indigo 残留 0、
行结构完整（信息/对手/配置卡/设置/消息区/保存按钮钉底）。
**考古补记**：第 7 轮"巨型空白区"根因完整化——内嵌 List 在
`mctx.Constraints.Max.Y=1e6` 测量下返回整个视口高度，消息卡吞掉
侧板全部剩余空间，设置行被推到无穷远（第 8 轮固定高已修）。

回归：GOMAXPROCS=2 go test ./... -race -count=1 全绿。

### 验收反馈修复轮第 10 轮（2026-10-07，§6.2）——用户截图直读（图像通道恢复），真根因一击定位

用户复测反馈（两图）：①LLM vs LLM 页设置区只剩红方配置卡；②人机 LLM 页
设置区"分两列显示"（信息/对手并排）。

**【修复·总根因】侧板列表漏设 `Axis: layout.Vertical`**：第 6 轮把外层列表
改页级字段时写的是零值 `layout.List{}`——**gio 零值 List 的 Axis 是水平**！
侧板所有行从左到右横排：图2 的"信息/对手并排"（分两列）、图1 的"只剩第一
行红方配置卡"（其余行全部溢出到窗口右缘外不可见/不可滚）、编辑框被窗口
右缘裁切——前几轮所有"设置看不见"表象的最终根源（第 6/7/8 轮修的都是
叠加其上的次级缺陷）。**一行修复**：`sideList: layout.List{Axis: layout.Vertical}`
（两页）。

实测验证（150%/1.25 档）：编辑框恢复全面板宽（x 764-1520，756px）且纵向
堆叠；滚轮滚动内容位移（编辑框像素 4000→10060）；滚动到底"立即保存"
横贯面板底部（x 768-1520）钉住、消息区卡在其上方。

回归：GOMAXPROCS=2 go test ./... -race -count=1 全绿。

### 验收反馈修复轮第 11 轮（2026-10-07，§6.2）

用户反馈：LLM 对战页点"立即保存"似乎保存了参数到文件，但没有 toast。

**【修复·真根因】"立即保存"按钮从未消费点击（死按钮）**：llmvsllm.go 渲染了
saveNowBtn 但 handleEvents 从未调用其 Clicked（M3' chips 同款教训——渲染
不消费=死按钮）。用户看到的"保存到文件"是改设置 chips 触发的 800ms 防抖
自动保存（persistNow），并非按钮行为。**修复**：handleEvents 补
saveNowBtn.Clicked 消费（含弹窗遮罩分支）。

**实测验证（插桩+像素双证据）**：点击 → saveNow 触发 → 两槽位回执齐
（llm_config_red/black, stored=plainFallback——keyring 缺席，0600 明文
回退如实呈现）→ **toast 显示**（深色胶囊 y 1032-1107，像素取证 (86,75,59)
等胶囊色）→ 2.2s 自动消失（toast 区域恢复按钮带色）。
**此前几轮误报澄清**：r15/r18 的 toast 分析输出被 found[:6] 截断——
toast 实际显示过；用户当时点的是旧实例（多实例未清理，死按钮版）。

回归：GOMAXPROCS=2 go test ./... -race -count=1 全绿。

### M4' 验收结论与优化轮（2026-10-07）

- **验收通过（用户确认"测试通过"）**：LLM 全链路（配置/测试连接/流式消息区/
  走子管线/参谋否决链/暂停继续/立即保存+toast/掩码回写/DR-012 跟随/界面
  缩放）；真实端点（百炼千问）一整局实测。
- **优化轮（第 12 轮，用户建议）**：
  1. 预设选择改**下拉式展开**（折叠=仅当前预设+▾，点击展开全部候选、选中
     即收起——Gio 无 select；展开内容在卡片流内不受列表虚拟化裁切）；
  2. **百炼千问预设**（UI 层追加，复制物零改动 #G2）：baseUrl=
     dashscope compatible-mode/v1、示例模型 qwen3.8-max；ThinkingStyleFor
     未登记 → 兜底 enable_thinking:false = DashScope 语义同形（05 §3.1），
     行为正确。**注意**：选择该预设会回填示例模型，覆盖已填的模型 ID。

## M4' 手测清单（用户执行；真实端点一整局为验收门）

### 准备：端点配置（GLM / DeepSeek 任选其一，或都用）

1. `go run .` → 主页"人机 LLM"→"对手"区默认"大模型"；
2. 配置卡（黑方模型）：预设 chips 选端点 → 填 API Key → 测试连接 → "连接成功，模型 … 响应正常"；
   - **智谱 GLM**：预设"智谱 GLM"（端点/示例模型自动回填），API Key 从 <https://open.bigmodel.cn> 控制台获取，模型 ID 可改 `glm-4-flash`（免费）或 `glm-4.6`；关闭参数自动走 `thinking:{type:disabled}`；
   - **DeepSeek**：预设"DeepSeek"，Key 从 <https://platform.deepseek.com> 获取，模型 `deepseek-chat`；关闭参数走 `enable_thinking:false` 兜底；
   - **输入口径（KG-003/KG-004）**：Key/URL 为 ASCII，Ctrl+V 可用；每字段旁"粘贴"按钮走 powershell 管道（Windows 先复制），中文场景用它；
   - 点**立即保存**（侧板底部粘性按钮）→ toast"模型配置已保存"（WSL 无 keyring 时如实提示"已明文保存到本地"——0600 回退，复制物 DR-011 语义）；
   - 【上游-DR-005 核对项】配置卡内**不存在任何思维链开关**，仅固定提示"思维链已强制关闭"。
3. LLM vs LLM 页可复用同一配置：进入后"立即保存"写红黑两槽；或把一侧留空（显示"对局时将使用另一方的模型配置"提示——DR-012 跟随）。

### 界面缩放（M4' 验收反馈新增，08 §9）

0. **主页 → 全局设置**：弹出设置弹窗（自动保存开关 + 界面缩放档位）→ 点
   **150%** → 整个应用文字/间距立即变大（棋盘大小不变）→ 关闭；重启应用
   缩放档位保持（settings.json `global_ui_scale`）。高分屏若仍偏小选 175%/200%。

### 人机 LLM 页（对照 08 防错 #4/#5/#7/#8/#11/#12）

1. **流式消息区**：走一着红炮（7,7)→(7,4) → 状态条"黑方 <模型ID> 正在思考…（已思考 Ns，第 1/3 次尝试）"→ 消息区实时追加模型回复正文（chunk 级）→ 应手落盘带 220ms 飞行动画 + 状态条切回等待玩家。
2. **参谋/否决链**：引擎参谋选"候选"或"护航"→ 走子后消息区出现"〔参谋〕参谋评分: …"条目；护航模式模型选劣着时可观察到"〔兜底〕已由参谋否决…"或引擎代走注记（观察项）。
3. **重试与降级**：无效回复重试=3 次、持续失败=内置 AI 代走（默认）→ 故意把模型 ID 改成不存在值 → 走子 → 消息区"〔兜底〕…已由内置 AI 兜底走子"+ 状态条注记（或改回"该方判负"观察"〔判负〕…判红方胜"）。
4. **思考中悔棋=中止重想**：模型思考中点"悔棋" → 在途请求作废、模型重新应手（防错 #4）；新游戏确认框弹出期间 chips/设置不可点（遮罩拦截）。
5. **保存/恢复/掩码（#8）**：走 2~3 手 → 返回主页 → 重进 → 恢复存档且轮黑时模型自动续手；重启应用进配置卡 → API Key 显示 `****`+末 4 位（掩码回读），不动它直接"立即保存" → 再测连接仍成功（掩码回写不覆盖真实 Key）。
6. **测试连接镜像（DR-014）**：先在大模型对战页（或 LLM vs LLM 红方）保存红方配置，清空黑方槽（把三字段全清空+立即保存）→ 进人机 LLM 页 → 配置卡显示红方配置+提示"黑方未配置——已使用红方的模型配置"→ 测试连接应测红方配置且成功；立即保存 → toast"当前为红方配置的镜像视图，请在红方一侧修改配置"。
7. **内置 AI 对手（DR-014）**：对手切"内置 AI"→ 走子后 AI 直接应手（无需 LLM 配置），行为同人机对战页（含 300ms 最小思考呈现）。
8. **三次重复裁决（#11/#12）**：构造闲着重复环 → k=2 toast 不阻塞 / k=3 判和确认框（玩家侧）——与人机页一致。

### LLM vs LLM 页

1. **自动对局一整局**：双槽位配置好后点"开始对战" → 状态条"红方（模型ID）思考中…（已思考 Ns，第 N/M 次尝试）"与黑方交替 → 每手落盘+飞行动画、消息区双方流式正文与备注条目、信息区"红方/黑方：note"+"最新走法"→ 至将死/困毙自动停（或"停止"手动终止）。
2. **走棋间隔**：间隔选 1/2/5 秒 → 每手之间明显停顿；选"不等待"→ 连续快走。
3. **暂停/继续/停止**：思考中点"暂停" → 在途回复作废、状态条"已暂停（模型回复已作废，继续后重新思考）"→ "继续" → 重新思考；"停止" → 解锁棋盘（点击可选中）、可再"开始对战"续走。
4. **空侧跟随（DR-012）**：把黑方三字段全清空+立即保存（红方保留配置）→ 开始 → 黑方实际用红方配置应手（状态条黑方括号内显示红方模型 ID）；黑侧引擎切"内置 AI"→ 黑方无需 LLM 配置直接应手。
5. **恢复不自动续跑**：对局中途返回主页 → 重进 → 局面恢复但停在当前局面，点"开始对战"续走。
6. **一方失败终止（#7 观察项）**：把一侧模型 ID 改错+持续失败=该方判负 → 开始 → 该侧 3 次重试耗尽 → 状态条"X方走子失败，对局终止"+ 结算横幅判对方胜。
7. **双槽位保存回执**："立即保存" → 两回执齐后 toast"双方模型配置已保存"；红黑任一 per-field 粘贴按钮独立回填本卡字段。


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

## M3' 验收结论（2026-10-06，用户确认"其他验证通过"后收尾）

- 通过：完整人机对局（AI 应手落盘/回合流转/lastMove 高亮）、执黑切换（AI 红先）、
  难度档切换与即时生效、新游戏确认框、保存/恢复、每局一实例；
- 验收反馈修复轮两项（chips 指针区注册/侧板宽度溢出裁切）复测通过；
- 行为澄清：难度档位映射实测正确（开局 2ms→1.33s 单调），低档快为引擎特性
  （timeMs 上限语义，与上游一致）；用户要求增加可感知思考态 → DR-G004
  最小思考呈现 300ms（复测确认）；
- M3' 无新增遗留问题（KG-008 双人页注记对 chips 同类面已在人机页收口）。

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
