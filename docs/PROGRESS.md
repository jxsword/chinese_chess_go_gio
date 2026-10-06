# 开发进度（PROGRESS）

## 当前状态

**M0' 工程骨架 + UI POC 排险——代码与文档完成，等待用户手动验收**（2026-10-06）。
验收通过后进入 M1'（状态层核心 + 主页，启动提示词=design_docs/11 §4.2）。

## 里程碑总览

| 里程碑 | 状态 | 说明 |
|---|---|---|
| 设计文档阶段（含领域层复制） | ✅ | 本文件首条记录 |
| **M0' 工程骨架 + UI POC 排险** | 🔶 待手测验收 | T0'.1~T0'.7 全部完成；POC 五项结论已回填 08 §12 与 DR-G003 |
| M1'~M7' | ⬜ | 见 design_docs/10-实施路线图.md |

## M0' 完成清单（T0'.1~T0'.7，逐任务 commit）

| 任务 | 交付 | 验收结果 |
|---|---|---|
| T0'.1 | go.mod 锁版（**gioui.org v0.10.3 + go-text/typesetting v0.3.5**，白名单内）+ main.go 薄入口 + internal/{app,ui,state} 骨架 + 单事件循环（FrameEvent→drain→Layout→提交帧；后台 goroutine 经 Window.Emit 非阻塞通道 + Invalidate 排帧，#G3） | X11 弹窗验证 ✓ |
| T0'.2 | POC-1 棋盘自绘（poc_boardlayout/boardart/board.go + cmd/poc board）：08 §3.2 全要素 + Electron boardArt 逐元素映照 + 几何表驱动单测 | 与 Electron 参照渲染并排对照一致（poc1_compare.png）；无离屏缓存需求 |
| T0'.3 | POC-2 动画帧循环（poc_anim.go）：220ms 两阶段 easeOutCubic、帧时间戳 t≥1 权威结束、吞输入计数、220ms/1s 慢速切换 | 实测 225~235ms（≤1 帧偏差）；单测覆盖缓动+权威判定 |
| T0'.4 | POC-3 IME+字体（poc_ime.go）：Editor 自动聚焦+ChangeEvent 回读+首帧写剪贴板+四配置字体链 | XIM 不可用（KG-003）+粘贴兜底可用（xdotool 自动化探针实证） |
| T0'.5 | POC-4 长列表（poc_list.go）：合成 14 万局 + layout.List 虚拟化 + FPS/帧开销统计 + 自动滚动压测 | **FPS 80，帧开销 avg 680µs / max 1.315ms**（达标）；KG-002（ScrollBy 病理）登记 |
| T0'.6 | POC-5 WSLg 渲染：X11/Wayland 运行时回退、软渲染兜底（LIBGL_ALWAYS_SOFTWARE=1 渲染正确）、xwd 逐窗截图取证链（K5 不复现） | 结论入 08 §12；构建依赖记 AGENTS.md |
| T0'.7 | ci.yml：gofmt（ubuntu）+ vet/test -race 矩阵（ubuntu/windows/macos）+ setup-go 缓存 | 本地交叉编译 windows ✓；darwin 需 macOS runner（CI 首跑覆盖） |

## POC 实测数据（回填明细=design_docs/08 §12）

- **动画**：220ms 请求实测 225~235ms（1s 慢速实测 1.003~1.024s），帧时间戳权威；动画期 26~28 FPS（RDP 合成节流）。
- **长列表**：140,000 局程序化滚动最坏情况 FPS 80；帧开销 avg 680µs / max 1.315ms（预算 16.7ms）。
- **渲染**：默认=Wayland 后端（运行时选择）；WAYLAND_DISPLAY 失效→X11；软渲染兜底 ✓。
- **版本锁定**：gioui.org v0.10.3、go-text/typesetting v0.3.5（R-G5 缓解）。

## 验收反馈修复轮（2026-10-06，§6.2 流程：修复→回归→再提交）

用户首轮手测反馈（图1~4）：棋盘/动画项通过；发现 3 项缺陷 + 1 项定论：

1. **【修复】Ctrl+V 粘贴 Windows 中文乱码（KG-004 新建）**：本地复现定位——WSLg 剪贴板桥接把 Windows 文本按 GB18030 双字节语义重解码（"中文粘贴测试"→"涓\ue15f枃绮樿创娴嬭瘯"），且 CJK/ASCII 边界处桥接丢字节不可逆（静默自动反解码方案被否决并留测试固化契约）。落地可靠路径：**"从 Windows 剪贴板粘贴"按钮**（powershell.exe Get-Clipboard 管道，UTF-8 正确；含 /mnt/c 兜底路径），端到端实证 clip.exe→按钮→编辑器完整中文；纯中文段抢救工具 clipboardMojibakeRecover+cp936Pua 表（2068 条生成表）保留但禁止自动应用。
2. **【定论】Windows 输入法无法输入中文（KG-003 更新）**：用户手测确认——WSLg 下中文键盘输入全路径不可用（X11 无 XIM + Wayland text-input 未被 RDP 桥接）；中文输入口径统一走 KG-004 按钮。
3. **【留档】窗口标题栏中文 □（KG-005 新建）**：xprop 核实应用侧 `_NET_WM_NAME` 正确，WSLg RDP 标题解码缺陷，不可修，仅观感。
4. **【留档】`InvalidateCmd{At: 未来时刻}` 不排帧（KG-006 新建）**：两处复现，帧循环一律立即排帧。

回归：gofmt/vet/`go test ./... -race` 全绿（恢复契约用例 4 个：实测样本/正常不误改/垃圾拒绝/混排有损拒绝）。

**复验轮（同日二次反馈"按钮仍乱码"）——两项根因补修**：
1. powershell.exe stdout 默认按系统 ANSI/OEM 代码页（zh-CN=GBK）编码→WSL 侧按 UTF-8 解读即乱码（复现：直接 Get-Clipboard 输出 `��Windows�и...`；修法：前置 `[Console]::OutputEncoding=UTF8`，复验输出正确）。首轮端到端测试为假阳性——clip.exe 按 GBK 解码 stdin 与 powershell 按 GBK 编码 stdout 恰好互逆。
2. 同步 exec 从 Gio 主 goroutine 内起 interop 进程挂起（`Cmd.Wait` 永阻塞；shell/纯 Go 探针正常，栈留证）——粘贴改异步：后台 goroutine→`Window.Emit` 通道→主循环 `OnAppEvent`（app 新增 `EventTarget` 页面接口），顺带满足 #G3。
端到端（Set-Clipboard 正确中文→点按钮→编辑器完整正确中文）与 POC_WINPASTE 自动触发均验证通过。

## 已知问题（新增）

- **KG-002**：gio List.ScrollBy 逐帧程序化滚动 → 文本绘制病理慢路径（黑屏）；规避=直接推进 Position.First，M5' 用手势滚动。
- **KG-003**：WSLg 下中文键盘输入全路径不可用（X11 无 XIM + Wayland text-input 未被 RDP 桥接，用户验收定论）；中文输入走 KG-004 按钮。
- **KG-004**：WSLg 剪贴板桥接 CJK 乱码且有损；可靠路径=powershell 管道粘贴按钮（已验证），M4' 沿用。
- **KG-005**：WSLg 窗口标题栏中文 □（WSLg 缺陷，不可修，仅观感）。
- **KG-006**：gio `InvalidateCmd{At: 未来时刻}` 在 WSLg 不排帧；帧循环一律立即排帧。
- 详见 docs/KNOWN_ISSUES.md"本仓初始新增条目"表。

## 请手测清单（M0' 验收门，§5——按顺序执行）

> 准备：`export PKG_CONFIG_PATH="$HOME/.local/lib/pkgconfig"`（建议写入 ~/.bashrc）；
> 若尚未安装系统依赖：`sudo apt install -y libvulkan-dev libx11-xcb-dev`（装了可不导出该变量）。

1. **骨架弹窗（T0'.1）**：`go run .` → 弹出"中国象棋 Ultra（Gio 版）"窗口，显示"M0' 骨架就绪 — 版本 dev"，关闭按钮正常退出。
2. **POC-1 棋盘（T0'.2）**：`go run ./cmd/poc board`
   - 棋盘全要素：楚河漢界居中、纵线河界断开、九宫斜线、红黑棋子+汉字（对照 `~/poc_evidence/m0/poc1_compare.png`）；
   - 交互：点击红炮（b2 位）→ 出现蓝色选中圈+绿点/绿环合法目标 → 点绿点落子 → 黄色 lastMove 双圆；连续对弈几手；点己方另一子改选、再点同子取消；
   - 右上"切换坐标口径"按钮：ICCS（a-i/0-9）↔ 08 口径（红方一~九右→左/黑方 1~9 左→右）——**请在验收意见中告知 M2' 采用哪种**（08 §3.2 与 Electron 锚点存在口径差异）；
   - 自动演示参考：`POC_AUTODEMO=1 go run ./cmd/poc board`（1s 炮二平五→2s 选中黑炮，3s 停在终态）。
3. **POC-2 动画（T0'.3）**：`go run ./cmd/poc anim` → 三场景循环自动演示（炮二平五/黑马8进7/炮五进四吃卒）；点"1000ms 慢速"→ 观察两阶段（飞行中被吃子仍在 → 落定才消失）；**慢速下动画期间快速点击棋盘 → 工具条"吞输入"计数增长**；点"220ms 标准"恢复。
4. **POC-3 IME+字体（T0'.4，修复后复验）**：`go run ./cmd/poc ime` → 字体回退链四行中文正常渲染；输入框已自动聚焦：
   - **点"从 Windows 剪贴板粘贴（KG-004 可靠路径）"按钮**（先在 Windows 侧复制任意中文）→ 预期完整中文进入编辑器与"已提交文本"行（修复项回归）；
   - Ctrl+V 走 WSLg 桥接：ASCII 正常、中文乱码（KG-004 已知，不再作为验收项）；
   - 窗口标题栏 □ 为 KG-005 已知（应用侧 X 标题正确），不影响验收。
5. **POC-4 长列表（T0'.5）**：`POC_LIST_AUTOSCROLL=1 go run ./cmd/poc list` → 14 万局列表持续滚动不卡（状态条 FPS/帧开销）；不带该变量再跑 → 鼠标拖拽滚动手感正常。
6. **POC-5 渲染（T0'.6）**：上一步窗口即 Wayland 路径；`WAYLAND_DISPLAY=none go run ./cmd/poc board` 强制 X11 路径；`LIBGL_ALWAYS_SOFTWARE=1 WAYLAND_DISPLAY=none go run ./cmd/poc board` 软渲染路径——三者窗口均正常即通过。
7. **CI（T0'.7）**：push 后 GitHub Actions 三平台全绿（首次运行需在 GitHub 仓库开启 Actions）。

## 下一里程碑（M1'）

验收通过后：全新会话逐字粘贴 design_docs/11 §4.2 启动提示词（gameVm 翻译/fenHistory 四收口/gameStore 工厂/主页 7 入口）。
