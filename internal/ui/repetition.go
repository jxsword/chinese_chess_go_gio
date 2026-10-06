package ui

// 重复裁决接线（翻译源 = 上游 features/board/useRepetitionJudge.ts；
// DR-018，08 防错 #11/#12，07 §3 收口点口径）。
//
// 监听历史增长 → rules.JudgeRepetition(fenHistory) → 按裁决驱动：
//   - 长将第 2 次出现 → toast 非阻塞警告（不锁输入、不打断思考，#11）；
//   - 长将第 3 次 → 违规方判负（vm.Resign，结算由终局横幅呈现，#12）；
//   - 双方长将 / 第 4 次重复 → 判和（vm.AgreeDraw）；
//   - 三次重复判和 → 面对和棋的一方为玩家时置 DrawOffer（页面画确认框，
//     可变着继续），为 AI/LLM 时自动接受（M3'/'/' 后续页复用本组件）。
//
// 触发口径（07 §3，DR-G002 落位）：上游 React useEffect 依赖 moveHistory.length
// 的"仅增长时裁决"由 state 层收口点承接——OnHistoryGrow（executeMove push 侧 /
// restore 重放完成侧）= 增长触发；OnHistoryRewound（悔棋 pop / 新局）= 重置
// declined 与未决和棋框。悔棋/新局不触发裁决，语义等价且触发点确定。

import (
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
)

// RepetitionJudge 重复裁决（页面级纯逻辑组件，无 Gio 依赖，可 headless 单测）。
type RepetitionJudge struct {
	vm        *state.GameVm
	isHuman   func(side rules.Side) bool
	showToast func(message string)

	declined  bool
	DrawOffer *rules.Side // 待玩家确认的三次重复和棋（面对方），nil = 无待确认
}

// NewRepetitionJudge 创建裁决接线（isHuman：三方对局页注入 AI/LLM 执方判定）。
func NewRepetitionJudge(vm *state.GameVm, isHuman func(side rules.Side) bool, showToast func(string)) *RepetitionJudge {
	return &RepetitionJudge{vm: vm, isHuman: isHuman, showToast: showToast}
}

// OnHistoryGrow 历史增长裁决（挂 vm.OnHistoryGrow；fenHistory=完整局面序列）。
func (j *RepetitionJudge) OnHistoryGrow(fenHistory []string) {
	if len(fenHistory) < 2 {
		return
	}
	verdict := rules.JudgeRepetition(fenHistory)
	if verdict == nil {
		return
	}
	sideToMove := rules.Red
	if !j.vm.IsRedTurn() {
		sideToMove = rules.Black
	}
	switch verdict.Kind {
	case rules.VerdictPerpetualCheckWarning:
		j.showToast(sideName(verdict.Side) + "方连续将军重复，再次将判负")
	case rules.VerdictPerpetualCheckLoss:
		j.showToast(sideName(verdict.Side) + "方长将，判负")
		j.vm.Resign(verdict.Side)
	case rules.VerdictBothPerpetualCheckDraw:
		j.showToast("双方长将，不变作和")
		j.vm.AgreeDraw()
	case rules.VerdictRepetitionDraw:
		if j.isHuman != nil && j.isHuman(sideToMove) {
			side := sideToMove
			j.DrawOffer = &side // 玩家确认（防错 #12：拒绝后 k=4 强制判和）
		} else {
			j.showToast("三次重复局面，判和")
			j.vm.AgreeDraw()
		}
	case rules.VerdictForcedRepetitionDraw:
		j.showToast("再次重复局面，强制判和")
		j.declined = false
		j.vm.AgreeDraw()
	}
}

// OnHistoryRewound 悔棋/新局：拒绝状态与未决和棋确认框一并重置（上游 historyLen
// 减少分支；触发点=state 收口点 OnHistoryRewound，07 §3）。
func (j *RepetitionJudge) OnHistoryRewound() {
	j.declined = false
	j.DrawOffer = nil
}

// AcceptDraw 接受和棋（确认框确认）。
func (j *RepetitionJudge) AcceptDraw() {
	j.DrawOffer = nil
	j.vm.AgreeDraw()
}

// DeclineDraw 拒绝（变着继续；下次重复即 k=4 强制判和）。
func (j *RepetitionJudge) DeclineDraw() {
	j.DrawOffer = nil
	j.declined = true
}

// sideName 裁决文案用方名（红/黑）。
func sideName(side rules.Side) string {
	if rules.IsRedSide(side) {
		return "红"
	}
	return "黑"
}
