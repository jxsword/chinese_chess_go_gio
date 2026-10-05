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

## 已知问题（新增）

- **KG-002**：gio List.ScrollBy 逐帧程序化滚动 → 文本绘制病理慢路径（黑屏）；规避=直接推进 Position.First，M5' 用手势滚动。
- **KG-003**：gio X11 后端无 XIM 客户端 → WSLg fcitx5 无法输入中文；规避=剪贴板粘贴（实测可用）；Windows IME 经 RDP（Wayland text-input-v3）待用户手测定论。
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
4. **POC-3 IME+字体（T0'.4）**：`go run ./cmd/poc ime` → 字体回退链四行中文正常渲染；输入框已自动聚焦：
   - fcitx5 直键入（预期：拼音字母原样出现，不出中文——KG-003 已知）；
   - **Ctrl+V 粘贴**（首帧已自动写入样本"Gio中文粘贴测试ABC123"）→ 预期出现中文（粘贴兜底）；
   - **Windows 侧切换中文输入法（微软拼音）在窗口内打字** → 若能出中文=Wayland text-input 经 RDP 桥接可用（此结论决定 M4' 表单口径，请务必记录现象）。
5. **POC-4 长列表（T0'.5）**：`POC_LIST_AUTOSCROLL=1 go run ./cmd/poc list` → 14 万局列表持续滚动不卡（状态条 FPS/帧开销）；不带该变量再跑 → 鼠标拖拽滚动手感正常。
6. **POC-5 渲染（T0'.6）**：上一步窗口即 Wayland 路径；`WAYLAND_DISPLAY=none go run ./cmd/poc board` 强制 X11 路径；`LIBGL_ALWAYS_SOFTWARE=1 WAYLAND_DISPLAY=none go run ./cmd/poc board` 软渲染路径——三者窗口均正常即通过。
7. **CI（T0'.7）**：push 后 GitHub Actions 三平台全绿（首次运行需在 GitHub 仓库开启 Actions）。

## 下一里程碑（M1'）

验收通过后：全新会话逐字粘贴 design_docs/11 §4.2 启动提示词（gameVm 翻译/fenHistory 四收口/gameStore 工厂/主页 7 入口）。
