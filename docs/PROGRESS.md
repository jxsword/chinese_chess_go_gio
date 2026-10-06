# 开发进度（PROGRESS）

## 当前状态

**M4' LLM 全链路对接——开发完成，待用户手测验收**（2026-10-06；T4'.1~T4'.3 完成，
质量门全绿，X11 截图取证两页渲染；真实端点手测一整局由用户执行）。
验收通过后进入 M5'（语料 + 棋谱对接，启动提示词=design_docs/11 §4.6）。

## 里程碑总览

| 里程碑 | 状态 | 说明 |
|---|---|---|
| 设计文档阶段（含领域层复制） | ✅ | 本文件首条记录 |
| **M0' 工程骨架 + UI POC 排险** | ✅ 验收通过（tag v0.1.0-m0'） | T0'.1~T0'.7 完成；POC 五项结论回填 08 §12/DR-G003；两轮验收修复（KG-003~007） |
| **M1' 状态层核心 + 主页** | ✅ 验收通过 | T1'.1~T1'.3 完成；DR-G002 翻译用例全绿；验收反馈两项修复（居中/D-002 标题绕过）复验通过 |
| **M2' 双人对战 + 存储** | ✅ 验收通过（tag v0.2.0-m2'） | T2'.1~T2'.4 完成；翻译用例回归+13 项手测通过；验收反馈一项修复（KG-009 弹窗居中）复验通过 |
| **M3' 引擎对接 + 人机页** | ✅ 验收通过（tag v0.3.0-m3'） | T3'.1~T3'.2 完成；对拍回归全绿（复制物零改动）+ cancel/迟到丢弃用例 + 难度 5 实测 1.54s（≤7.5s）；验收反馈轮两项修复（chips 指针区/侧板溢出）+ 澄清（难度耗时）+ DR-G004 最小思考呈现 |
| **M4' LLM 全链路对接** | 🔶 开发完成待手测 | T4'.1~T4'.3 完成；mock SSE 回归+掩码/借用/总线用例全绿；真实端点手测待用户执行（GLM/DeepSeek 配置指引见手测清单） |
| M5'~M7' | ⬜ | 见 design_docs/10-实施路线图.md |

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
