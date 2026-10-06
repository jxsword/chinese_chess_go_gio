# 决策记录（工作区协作决策）

本文件记录**工作区协作/流程层**决策（编号 D-xxx，遵循用户级全局协作约定）。
技术设计决策见 `design_docs/decision_log.md`（编号 DR-Gxxx，引用上游写作"上游-DR-xxx"）。
未被选中的选项必须连同弃用理由一并保留。

## D-001 复制 commit 中 go.mod 依赖清单的处理（2026-10-06）

- 背景：上游 go.mod 有 4 个直接依赖（wails v2、go-keyring、golang.org/x/text、modernc.org/sqlite）。
  经 grep 证实复制范围（internal/ 六包 + cmd/eval）零处 import wails——wails 只服务上游 main.go/app.go（本仓不复制）。
  但复制指令限定"仅允许的机械性改动：go.mod module 改名 + import 前缀改写"，go.mod 依赖清单是否随 import 现实收缩需定案。
- 选项：
  - A. 去 wails + `go mod tidy`——优点：go.mod/go.sum 精确反映被复制代码真实依赖集，wails 及其 30+ 间接依赖不进仓，M0' 免一次清理提交，tidy 与 import 前缀改写属同族机械适配；缺点：超出指令字面"仅允许"半步，需用户授权，tidy 理论上可能触网（实测模块缓存已命中全部所需版本）；代价：无实质代价。
  - B. 原样保留全部 4 直接依赖（含 wails）——优点：最严格贴合"零修改"字面，绝无争议；缺点：go.sum 携带 wails 全家 30+ 无关间接依赖，模块图含死依赖，M0' 需多一笔清理 commit；代价：仓库噪声。
  - C. 去 wails 但手工编辑 go.sum（不跑 tidy）——优点：无触网风险；缺点：手工维护易错、无收益，纯劣化；代价：人工出错风险。
- 结论：**A（去 wails + go mod tidy）**，用户在决策矩阵提问中明确选择。
- 理由：复制纪律的本意是**行为零改动**（源码/测试/金标准逐字节同源）；go.mod 依赖清单是对被复制代码的"陈述"而非复制物本体，随 import 现实收缩属机械适配的应有之义。映照纪律校验口径不受损：diff 校验以"除 go.mod 与 import 前缀行外零差异"为准。弃 B 因仓库噪声与 M0' 额外清理成本；弃 C 因无收益且易错。
- 影响：仅 go.mod/go.sum 两文件；测试结果不变（复制 commit 实测 `go test ./... -race` 全绿）；M0' 无需依赖清理提交。

## D-002 KG-005 标题栏中文乱码的处置（2026-10-06）

- 背景：M1' 验收手测再次提出——WSLg 窗口标题栏"中国象棋 Ultra（Gio 版）"显示为 □□□□。
  M0' 已 xprop 核实应用侧 `_NET_WM_NAME` 写入正确 UTF-8，乱码发生在 WSLg→RDP 桥接的
  标题解码环节，应用无法修正解码本身，只能改标题内容绕过。处置属产品决策（影响各平台观感）。
- 选项：
  - A. 维持现状（KG-005 留档）——优点：零改动，真实 X11/Wayland/Windows 原生环境标题正常，WSLg 未来修复自动受益；缺点：WSLg 下观感持续乱码；代价：无。
  - B. 运行时检测 WSLg→ASCII 标题（"Chinese Chess Ultra (Gio)"），其余平台保持中文——优点：只在有缺陷的环境绕过，中文环境体验不受损；缺点：多约 30 行检测代码，双语标题策略需固化；代价：`internal/app/wslg.go`（检测函数纯 Go 可单测）+ 装配一行。
  - C. 全平台 ASCII 标题——优点：最简单、全平台一致；缺点：中文用户在原生环境也失去中文标题，与产品定位不符；代价：产品体验损失。
- 结论：**B（WSLg 内 ASCII 标题）**，用户在决策矩阵提问中明确选择。
- 理由：缺陷面仅存在于 WSLg（/mnt/wslg 标记目录 + /proc/version 含 microsoft 兜底，误报无害——仅影响标题语言）；绕过不触及 gio 复制物与协议面，检测函数注入式单测覆盖四分支。弃 A 因用户日常开发环境即 WSLg，观感问题持续存在；弃 C 因不必要地牺牲原生环境体验。
- 影响：internal/app/{wslg.go,wslg_test.go,app.go Run 标题行}；KNOWN_ISSUES KG-005 处置栏更新；
  实测 `_NET_WM_NAME` = "Chinese Chess Ultra (Gio)"（WSLg 内）。
