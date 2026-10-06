# 已知问题（KNOWN_ISSUES）

> 记录各里程碑复审产出的 P2/P3 项与按计划后置的冲突面；P0/P1 修复后即移除；每里程碑收尾更新。
> **编号约定**：K1~K36 为上游（chinese_chess_go v1.0.0-rc1）36 条的分类继承；**本仓新增条目自 KG-001 起**。
> 分类允许跨类标注：① 复制继承 / ② Wails·打包·前端专属→Gio 替代或消解 / ③ 预防性设计输入（同时保留①继承标注）。

## 总表（上游 36 条 → 本仓处置）

| K | 标题 | 层 | 处置分类 | 状态 | Gio 版处置 |
|---|---|---|---|---|---|
| K1 | 思维链 UI 开关与 DR-005 冲突 | 领域+前端 | ② | 已注销（复制物已含修复） | 配置 UI 无开关，铁律 #G6 |
| K2 | bundle 含 630KB TS 死代码 | 前端/打包 | ② | 消解 | 无 WebView/无前端 bundle |
| K3 | 自动保存 write 发即忘 | 前端 | ② | 已注销 | Go 侧错误记日志+UI 提示不吞错（07 §2） |
| K4 | `parser:progress` 事件名未登记 | Wails 事件 | ②+③ | 转化 | 事件通道沿用"域：动作"，**全部先登记 00 §4 再实现** |
| K5 | WSLg xwd 截图全黑 | 环境/取证 | ② | 待 POC | Gio 自绘直出窗口表面，POC-5 验证截图链 |
| K6 | node_modules 内 Go 代码污染质量门 | CI/工具链 | ② | 消解 | 无 node_modules |
| K7 | buildFen 接口收敛 | 领域 | ① | 继承 | 复制物同态，无需处置 |
| K8 | Board.grid 用 BoardGrid 二维切片 | 领域 | ① | 继承 | 复制物同态 |
| K9 | inPalace 等未导出 | 领域 | ① | 继承 | 摆盘如需导出=一行改动，登记差异（08 §7） |
| K10 | 裁决对无效 FEN 行为差 | 领域 | ① | 继承 | 复制物同态，跨语言审计点 |
| K11 | moveNotation 退化输入 panic | 领域 | ① | 继承 | 复制物同态，调用侧校验 |
| K12 | 凭据槽位契约面字段差 | 领域+前端 | ① | 已注销（复制物已含修复） | 槽位 JSON 四字段终态 |
| K14 | parseMovesJson 非整数值丢弃 | 领域(存储) | ① | 继承 | 复制物同态 |
| K15 | settings clamp 边界差 | 领域(存储) | ①+③ | 继承 | 消费者注意 nil→默认兜底（07 §5） |
| K16 | beforeClose 300ms best-effort | Wails | ② | 替代 | `system.Command` close 挂自动保存（07 §2），POC 核实 |
| K17 | Runner 同 id 并发后到者为准 | 领域(引擎) | ①+③ | 继承 | requestId 全局唯一契约保留（00 §4） |
| K18 | 随机路径跨语言伪随机源不同 | 领域(引擎) | ①+③ | 继承 | 对拍仅 randomness=0 路径（09 §2.2） |
| K19 | 构造器缺省语义差 | 领域(LLM) | ① | 继承 | cmd/eval 直构造时传显式值 |
| K20 | excerpt rune vs UTF-16 计数 | 领域(LLM) | ①+③ | 继承 | 错误消息断言口径（09 §3） |
| K21 | OnChunk 与取消结算 TOCTOU | 领域(LLM) | ①+③ | 继承 | UI 侧 requestId 二次收口（00 §4/08 §11） |
| K22 | chunk.error 消息键序差 | 领域(LLM) | ① | 继承 | 断言"含前缀+含 message"口径 |
| K23 | ParserCancel 无粘性记忆 | 领域(解析) | ①+③ | 继承 | 直调下窗口更小，守卫语义保留 |
| K24 | zip 条目错误语义刻意偏离 | 领域(语料) | ① | 继承 | 属安全加强，测试锁定 |
| K25 | PGN 正则 Unicode 空白差 | 领域(解析) | ① | 继承 | 实际语料 ASCII 空白，无影响 |
| K26 | DecodeUtf8Lossy 替换符粒度差 | 领域(解析) | ① | 继承 | 仅坏字节展示差异 |
| K27 | 歧义 token 的 from 选择序 | 领域(解析) | ① | 继承 | 病态局面才可观测 |
| K28 | readXqfString 越界分支差 | 领域(解析) | ① | 继承 | ParseXqf 最小长度约束下不可达 |
| K29 | 语料扫描三处边缘差 | 领域(语料) | ① | 继承 | UI 结果同型 |
| K30 | 下载器四处边缘差 | 领域(语料) | ① | 继承 | Go 侧总体更安全 |
| K31 | 求解器置换表键 Zobrist vs FEN | 领域(求解) | ① | 继承 | 上游 04 §2 明定差异 |
| K32 | 识图错误消息两处微差 | 领域(识图) | ① | 继承 | 错误前缀口径断言 |
| K33 | 识图无取消通道 | 领域+Wails | ① | 继承 | Gio 侧同口径：按钮 disabled 防重入（08 §7） |
| K34 | Windows CI 平台性测试差异 | CI | ② | 随迁已修复 | 复制物测试含修复；质量门规范继承（09 §6） |
| K35 | AppImage 体积 ~80MB | 打包 | ② | 显著缓解 | Gio 单二进制；M7' 实测体积 |
| K36 | eval 质量评估深度口径提示 | 领域(评估) | ① | 继承 | 复制物随迁，DoD 口径解读提示 |

## ① 复制继承（领域层条目，随复制物继续有效）

K7~K11、K14、K15、K17~K33 共 25 条为上游领域层 P3 留档（K12 已注销、复制物已含修复）。**处置原则：复制物零修改（DR-G001），这些条目随代码同态继承，无需本仓处置**；其中 K9（摆盘可能需导出未导出函数）与 K15（设置消费者兜底）在对应里程碑落地时按表中注记执行；K17/K18/K20/K21/K23 同时为设计输入（见 ③）。

## ② Wails/打包/前端专属 → Gio 替代或消解

- **天然消解**：K2（无 bundle）、K6（无 node_modules）、K3（状态层迁 Go 后错误可见）。
- **替代方案**：K4（事件通道命名登记制，00 §4）；K13+K16（`key.FocusEvent`/`system.Command` 原生窗口事件替代 Wails 无 blur/minimize 缺口与 OnBeforeClose，映射表在 07 §2，**各平台覆盖面 POC-4 核实**）；K5（截图链 POC-5 验证）；K35（单二进制根治，M7' 实测）。
- **随迁已修复/直接沿用**：K1、K12（复制物已含修复）；K34（质量门规范：全平台 -race 无例外）；K36（口径提示随复制物）。
- 上游两处已知瑕疵（release.yml L142 引用不存在的 K37、design_docs/README 索引表过时注记）**仅知悉不修**（上游已收尾）；本仓不复现悬空引用类问题。

## ③ 预防性设计输入（跨类标注，已转化为设计约束）

- **K4** → 00 §4 事件清单登记制：新增事件先改文档再实现；
- **K17** → requestId 全局唯一是调用方契约（00 §4），Gio 事件总线按 id 过滤；
- **K18** → 对拍口径仅 randomness=0；确定性测试注入随机源（09 §2.2）；
- **K20** → 错误消息断言用"≤160 字符+前缀"口径，不逐位锁（09 §3）；
- **K21** → UI 层 requestId 二次收口（00 §4、08 §11 #4）；
- **K23** → Cancel 守卫语义保留，批次进度按 requestId 丢弃兜底。

## 本仓初始新增条目

| KG | 标题 | 层 | 说明 | 处置 |
|---|---|---|---|---|
| KG-001 | 与上游应用共存时共享 keyring service 槽位 | 凭据 | 复制物常量 `KeyringServiceName="chinese-chess-ultra-go"` 零修改（DR-G001）；两版应用共存时读写同三槽位（同用户同服务，API Key 语义可视为共享） | 接受并留档；若用户提出隔离需求，走 DR 评估改 service 名的映照差异成本 |
| KG-002 | gio `List.ScrollBy` 逐帧程序化滚动触发文本绘制病理慢路径 | Gio 库（M0' POC-4 实测） | 每帧调用 `ScrollBy` + 每帧 Invalidate 的场景下，text 绘制进入病理性慢路径（paintGlyph→Shape/Bitmaps 巨型化，帧永不成→窗口黑屏；goroutine 栈留证于 M0' 会话记录）。直接推进 `Position.First`（同样的状态变化）不触发。疑 gio v0.10.3 内部缺陷，未深挖上游根因 | 规避：POC-4 用 `Position.First` 直接推进；M5' 棋谱库用自然手势滚动与 `ScrollTo`（搜索跳转），禁逐帧 ScrollBy；若 M5' 需程序化平滑滚动再评估升级 gio |
| KG-003 | gio 无 IME 支持：X11 无 XIM 客户端、Wayland text-input 未被 WSLg RDP 桥接——WSLg 下中文输入全路径不可用 | Gio 库 × 环境（M0' POC-3 实测 + M0' 验收用户确认） | gio v0.10.3 X11 后端仅调 XFilterEvent、无 XOpenIM（源码核实），fcitx5(XIM) 键入原样进入 Editor；**用户手测确认 Windows IME 经 RDP 亦无法输入**（Wayland text-input-v3 未被 WSLg 桥接或 gio 未激活） | 见 KG-004：中文内容经"从 Windows 剪贴板粘贴"按钮（powershell 管道）可靠输入；ASCII（API Key/URL）Ctrl+V 可用；升 gio 版本跟踪 X11/Wayland IME 支持 |
| KG-004 | WSLg 剪贴板桥接对 CJK 文本乱码且有损（Windows→WSL 方向） | WSLg 环境（M0' 验收实测） | Ctrl+V 粘贴 Windows 复制的中文收到 GB18030 mojibake（"中文粘贴测试"→"涓\ue15f枃绮樿创娴嬭瘯"，UTF-8 字节被按 GB18030 双字节语义重解码）；且桥接在 CJK/ASCII 边界处 (续字节，ASCII) 对非法、**字节被丢弃不可逆**（实测复现）。WSL→WSL 方向（应用 WriteCmd 自写自读）不受影响 | **可靠路径已落地并三轮验证**：绕过桥接，经 `powershell.exe Get-Clipboard` 读取 Windows 剪贴板。两个关键坑（复验轮实测）：①powershell stdout 默认按系统 ANSI/OEM 代码页（zh-CN=GBK）编码→WSL 按 UTF-8 解读仍乱码，必须前置 `[Console]::OutputEncoding=[System.Text.Encoding]::UTF8`；②同步 exec 不可行——从 Gio 主 goroutine（线程亲和 cgo）内起 interop 进程会挂起（`Cmd.Wait` 永阻塞，goroutine 栈留证；shell/纯 Go 探针均正常），且同步执行违反 #G3——已异步化（后台 goroutine→Window.Emit 通道→主循环 OnAppEvent，app.EventTarget 接口）。端到端实证：Windows Set-Clipboard 正确中文→按钮→编辑器完整正确。M4' 表单沿用此异步按钮；纯中文段可由 `clipboardMojibakeRecover`+cp936Pua 表抢救（禁止自动应用）；ASCII Ctrl+V 不受影响 |
| KG-005 | WSLg 窗口标题栏中文显示为 □ | WSLg 环境（M0' 验收实测） | gio X 侧 `_NET_WM_NAME` 为正确 UTF-8（xprop 核实），但 WSLg RDP 窗口标题栏显示 □□□——WSLg 标题解码缺陷，应用侧不可修 | 影响仅观感；M1' 主窗口标题如需规避可改 ASCII（走 DR 记录差异），或等 WSLg 更新 |
| KG-006 | gio `InvalidateCmd{At: 未来时刻}` 在 WSLg 不排帧 | Gio 库 × WSLg（M0' POC 实测） | 按未来时刻调度的重绘事件不产生 FrameEvent（POC-1 自动演示与 POC-3 自动粘贴两处复现，改立即 `InvalidateCmd{}` 后正常）；编辑器光标闪烁等 gio 内部 At 用法待观察 | 帧循环一律用立即 `InvalidateCmd{}`（POC-2/3/4 口径）；升级 gio 时复核 |
