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
