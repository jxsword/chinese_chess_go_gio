# 开发进度（PROGRESS）

## 当前状态

**v1.0.0 已发布**（2026-10-07，M0'~M7' 全部里程碑验收通过；发布步骤见
[PROGRESS-archive.md](PROGRESS-archive.md) §"正式发布步骤"）。

**M7' 维护收口轮**（2026-10-08，全项目审核后的修复与归档，`go test ./... -race`
全量回归全绿）：

| # | 交付 | commit |
|---|---|---|
| 1 | OnAttempt 跨 goroutine 竞态收口（走子 goroutine 读页面字段→id 值捕获闭包，两 LLM 页）+ LlmTestDone 按 id 收口（LLM vs LLM 双卡定向） | `5f54a10` |
| 2 | Records*/SecureSlot* 六族事件载荷补 RequestID + 消费端按 id 收口（记录库 list/get/delete、RecordSaveDialog 四页、工作室入库与三槽位、LLM 两页槽位加载/保存）+ openRecordByID 降级卡死修复 + 00 §4 载荷描述同步 | `cad0214` |
| 3 | OnClose 降级 nil DB 防御（humanvsai/humanvshuman）+ EventBus.cancelled TTL 清理（防泄漏，#G5 语义不变）+ 关闭期 DAO 收口（Window.ioWG，Wait 后再关 DAO）+ advisor id 前缀重复修正 | `5cbb56f` |
| 4 | 文档收官归档：本文件重构 + 历史拆 [PROGRESS-archive.md](PROGRESS-archive.md) + KNOWN_ISSUES 补 K13/KG-012/KG-013 + README 发布口径 + 决策日志判据 | `docs` |

## 里程碑总览

| 里程碑 | 状态 | 说明 |
|---|---|---|
| 设计文档阶段（含领域层复制） | ✅ | [PROGRESS-archive.md](PROGRESS-archive.md) 首条记录 |
| **M0' 工程骨架 + UI POC 排险** | ✅ 验收通过（tag v0.1.0-m0'） | T0'.1~T0'.7 完成；POC 五项结论回填 08 §12/DR-G003；两轮验收修复（KG-003~007） |
| **M1' 状态层核心 + 主页** | ✅ 验收通过 | T1'.1~T1'.3 完成；DR-G002 翻译用例全绿；验收反馈两项修复（居中/D-002 标题绕过）复验通过 |
| **M2' 双人对战 + 存储** | ✅ 验收通过（tag v0.2.0-m2'） | T2'.1~T2'.4 完成；翻译用例回归+13 项手测通过；验收反馈一项修复（KG-009 弹窗居中）复验通过 |
| **M3' 引擎对接 + 人机页** | ✅ 验收通过（tag v0.3.0-m3'） | T3'.1~T3'.2 完成；对拍回归全绿（复制物零改动）+ cancel/迟到丢弃用例 + 难度 5 实测 1.54s（≤7.5s）；验收反馈轮两项修复 + DR-G004 最小思考呈现 |
| **M4' LLM 全链路对接** | ✅ 验收通过（tag v0.4.0-m4'） | T4'.1~T4'.3 完成 + 12 轮反馈修复/优化；真实端点（百炼）一整局实测通过 |
| **M5' 语料 + 棋谱对接** | ✅ 验收通过（tag v0.5.0-m5'） | T5'.1~T5'.4 完成 + D-004 入口勘误落位 + D-005/D-006 对话框定案 + 3 轮反馈修复；14 万局性能实测 FPS 76~87；导出快照与上游一致 |
| M6' 工作室 + 求解器 + 识图 | ✅ 验收通过（tag v0.6.0-m6'） | T6'.1~T6'.5 完成；6 验证 FEN 金标准回归全绿；验收反馈 3 轮修复复验通过 |
| M7' 评估 + 打包发布 | ✅ 验收通过（tag v1.0.0） | T7'.1~T7'.3 完成 + Release 演练 4 轮修复全绿 + 真实端点四项手测 + eval suite 逐档对比；Windows 维持裸 exe 基线（DR-G005） |
| M7' 维护收口轮 | ✅ | 2026-10-08：全项目审核 → 三轮代码修复（见上表）+ 文档收官归档；全量 -race 回归全绿 |

> 各里程碑逐任务清单、验收记录、修复轮全文与手测清单：见
> [PROGRESS-archive.md](PROGRESS-archive.md)（M0'~M7' 完整存档）。

## 已知问题

累计表见 [KNOWN_ISSUES.md](KNOWN_ISSUES.md)（K1~K36 上游继承 + KG-001 起
本仓新增）。核心项：KG-003/KG-004（WSLg 中文输入/剪贴板——粘贴按钮可靠
路径）、KG-005/KG-011（WSLg 窗口管理观感缺陷）、KG-002/KG-006（gio 库
行为坑，规避模式已固化）、KG-007（CI GOMAXPROCS=2 规避）、KG-012/KG-013
（复制物 corpusDownloader/eval 文案留档，登记不修改）。
