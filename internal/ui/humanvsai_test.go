package ui

// 人机对战页 headless 单测（T3'.2，09 §4：状态级等价用例；交互/视觉走手测清单）。
// 翻译锚点 = 上游 HumanVsAiPage.tsx：triggerAiMove（lockInput→搜索→无条件解锁
// P0-1、seq=作废在途）、newGame/switchSide/undoRound 的 AI 重触发、引擎失败判
// AI 负（防错 #7）、恢复定局后 AI 先行；requestId 形态下迟到回执由页面按 id
// 丢弃 + 总线按取消 id 丢弃（#4/#G5）。

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/engine"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
)

// aiEnvFixture 复用双人页 fake 环境，NewRequestID 换成计数器（AI 页需要
// 可区分的请求 id 做迟到丢弃断言）。
type aiEnvFixture struct {
	*pageEnvFixture
	ai   *fakeRunner
	page *HumanVsAiPage
}

func newAiEnvFixture(autoSave bool) *aiEnvFixture {
	f := &aiEnvFixture{pageEnvFixture: newPageEnvFixture(newPageRepo(), autoSave), ai: newFakeRunner()}
	env := f.pageEnvFixture.env
	var seq atomic.Int64
	env.NewRequestID = func(prefix string) string { return fmt.Sprintf("%s-%d", prefix, seq.Add(1)) }
	f.page = newHumanVsAiPage(env, HumanVsAiHooks{}, f.ai)
	return f
}

// replyNow 回填触发时刻（模拟引擎已搜满 minThinkDuration——测试延迟应用语义时
// 使回执立即生效）。
func (f *aiEnvFixture) replyNow() {
	f.page.aiStartedAt = time.Now().Add(-minThinkDuration)
}

// lastAiRequest 最近一次 AI 请求载荷。
func (f *aiEnvFixture) lastAiRequest(t *testing.T) engine.FindBestMovePayload {
	t.Helper()
	req := f.ai.lastRequest(t)
	if req.Type != engine.ReqFindBestMove {
		t.Fatalf("请求类型 = %q", req.Type)
	}
	var p engine.FindBestMovePayload
	if err := remarshal(jsonAny(t, req.Payload), &p); err != nil {
		t.Fatalf("载荷反解: %v", err)
	}
	return p
}

// canTapSelect 输入探针：点指定方未动过的炮（红(1,7)/黑(1,2)），未锁定且轮
// 该方时应产生选中。
func (f *aiEnvFixture) canTapSelect(side rules.Side) bool {
	if rules.IsRedSide(side) {
		f.page.store.VM.OnTap(1, 7)
	} else {
		f.page.store.VM.OnTap(1, 2)
	}
	return f.page.store.State().Selected != nil
}

// playPlayerMove 玩家走红炮（7,7）→（7,4）（中炮线，路径畅通恒合法）。
func (f *aiEnvFixture) playPlayerMove() bool {
	f.page.store.VM.OnTap(7, 7)
	snap := f.page.store.State()
	if snap.Selected == nil {
		return false
	}
	for _, p := range snap.LegalTargets {
		if p.Col == 7 && p.Row == 4 {
			f.page.store.VM.OnTap(7, 4)
			return len(f.page.store.State().MoveHistory) == len(snap.MoveHistory)+1
		}
	}
	return false
}

// aiReply 黑炮应手（7,2）→（7,3)：中炮开局后黑炮进一格恒合法。
func aiReply() *rules.Move {
	m := rules.Move{From: rules.Pos(7, 2), To: rules.Pos(7, 3)}
	return &m
}

// 上游挂载流：进页恢复（无存档）→ 解锁。
func TestAiPageRestoreNoSaveUnlocks(t *testing.T) {
	f := newAiEnvFixture(true)
	defer f.page.Dispose()

	if !f.page.restorePending {
		t.Fatal("进页应发起恢复取档")
	}
	if f.canTapSelect(rules.Red) {
		t.Fatal("取档回执前输入应锁定（#5）")
	}
	f.page.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsAi, Saved: nil})
	if !f.canTapSelect(rules.Red) {
		t.Fatal("回执后应解锁")
	}
	// 玩家执红：恢复/新局后轮红，不应触发 AI
	if len(f.ai.requests) != 0 {
		t.Fatal("执红开局不应触发 AI")
	}
}

// 上游 triggerAiMove：lockInput → 请求载荷（fen/difficulty/fenHistory 拷贝）→
// aiThinking；玩家走子完成后经 onPlayerMoved 触发（防错 #3：仅历史增长）。
func TestAiPagePlayerMoveTriggersAi(t *testing.T) {
	f := newAiEnvFixture(true)
	defer f.page.Dispose()
	f.page.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsAi})

	if !f.playPlayerMove() {
		t.Fatal("玩家走子应成行")
	}
	f.page.onPlayerMoved()

	if !f.page.aiThinking {
		t.Fatal("应进入思考态")
	}
	if f.canTapSelect(rules.Red) {
		t.Fatal("AI 思考期间输入应锁定")
	}
	p := f.lastAiRequest(t)
	snap := f.page.store.State()
	if p.Fen != snap.Fen {
		t.Fatalf("请求 fen = %q, 欲当前局面 %q", p.Fen, snap.Fen)
	}
	if p.Difficulty != 3 {
		t.Fatalf("缺省难度应 3: %d", p.Difficulty)
	}
	// DR-018：fenHistory 拷贝传入（长度=手数+1）
	if len(p.HistoryFens) != len(snap.MoveHistory)+1 {
		t.Fatalf("HistoryFens 长度 = %d, 欲 %d", len(p.HistoryFens), len(snap.MoveHistory)+1)
	}
	if p.HistoryFens[len(p.HistoryFens)-1] != snap.Fen {
		t.Fatal("HistoryFens 末项应为当前局面")
	}
}

// 上游 then 回执：无论作废与否一律解锁（P0-1）；ok 应手经 playMove 最终校验
// （铁律 #G10）落盘 + 视觉飞行层启动。
func TestAiPageAiMoveAppliesAndUnlocks(t *testing.T) {
	f := newAiEnvFixture(true)
	defer f.page.Dispose()
	f.page.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsAi})
	if !f.playPlayerMove() {
		t.Fatal("玩家走子应成行")
	}
	f.page.onPlayerMoved()
	id := f.page.aiRequestID
	historyBefore := len(f.page.store.State().MoveHistory)

	// 黑炮应手（合法着法，PlayMove 校验通过）
	move := *aiReply()
	f.replyNow()
	f.page.OnAppEvent(EngineMoveDone{RequestID: id, Move: &move})

	if f.page.aiThinking || f.page.aiRequestID != "" {
		t.Fatal("回执后应退出思考态")
	}
	if !f.canTapSelect(rules.Red) {
		t.Fatal("回执后应解锁（P0-1）")
	}
	if len(f.page.store.State().MoveHistory) != historyBefore+1 {
		t.Fatalf("AI 应手应落盘: %d", len(f.page.store.State().MoveHistory))
	}
	if !f.page.board.vis.active {
		t.Fatal("应启动视觉飞行层（已落盘走法）")
	}
}

// 迟到回执（新局/悔棋取消后到达）按 id 丢弃，不落盘（#4/#G5）。
func TestAiPageLateResponseDiscardedAfterNewGame(t *testing.T) {
	f := newAiEnvFixture(true)
	defer f.page.Dispose()
	f.page.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsAi})
	if !f.playPlayerMove() {
		t.Fatal("玩家走子应成行")
	}
	f.page.onPlayerMoved()
	staleID := f.page.aiRequestID

	// 新游戏：取消在途（Runner + 总线双收口）
	f.page.doNewGame()
	if f.page.aiRequestID != "" || f.page.aiThinking {
		t.Fatal("新局应作废在途请求")
	}
	if len(f.ai.cancels) != 1 || f.ai.cancels[0] != staleID {
		t.Fatalf("Runner 取消记录 = %v", f.ai.cancels)
	}
	found := false
	for _, id := range f.canceled {
		if id == staleID {
			found = true
		}
	}
	if !found {
		t.Fatalf("总线应取消 %q: %v", staleID, f.canceled)
	}

	// 迟到回执：不落盘、不解锁残留、不改思考态
	move := *aiReply()
	f.replyNow()
	f.page.OnAppEvent(EngineMoveDone{RequestID: staleID, Move: &move})
	if len(f.page.store.State().MoveHistory) != 0 {
		t.Fatal("迟到回执不得落盘")
	}
	if f.page.board.vis.active {
		t.Fatal("迟到回执不得触发视觉飞行层")
	}
}

// 引擎失败防软死锁：显式判 AI 负（防错 #7；上游 status==='failed' → resign）。
func TestAiPageEngineFailureResignsAi(t *testing.T) {
	f := newAiEnvFixture(true)
	defer f.page.Dispose()
	f.page.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsAi})
	if !f.playPlayerMove() {
		t.Fatal("玩家走子应成行")
	}
	f.page.onPlayerMoved()

	f.replyNow()
	f.page.OnAppEvent(EngineMoveDone{RequestID: f.page.aiRequestID, Err: errors.New("boom")})
	// 解锁由 vm.Resign 自带（state 层用例覆盖），此处断言判负与提示
	r := f.page.store.State().Result
	if r == nil || *r != state.ResultRedWins {
		t.Fatalf("AI（黑）失败应判红胜: %v", r)
	}
	if f.page.toastText == "" {
		t.Fatal("应 toast 失败提示")
	}
}

// noLegalMove 回执（Move=nil，Err=nil）：将死/困毙由棋盘状态呈现，仅解锁。
func TestAiPageNoLegalMoveUnlocksOnly(t *testing.T) {
	f := newAiEnvFixture(true)
	defer f.page.Dispose()
	f.page.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsAi})
	if !f.playPlayerMove() {
		t.Fatal("玩家走子应成行")
	}
	f.page.onPlayerMoved()

	f.replyNow()
	f.page.OnAppEvent(EngineMoveDone{RequestID: f.page.aiRequestID})
	if !f.canTapSelect(rules.Black) { // 仍轮黑方：以黑子选中探测解锁
		t.Fatal("应解锁")
	}
	if f.page.store.State().Result != nil {
		t.Fatal("无合法走法回执不改对局结果（由棋盘状态呈现）")
	}
}

// 上游 undoRound：思考中点悔棋 = 作废在途 + undoRound 因输入锁 no-op +
// 轮 AI 重新触发（"中止重想"）。
func TestAiPageUndoDuringThinkingRestartsSearch(t *testing.T) {
	f := newAiEnvFixture(true)
	defer f.page.Dispose()
	f.page.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsAi})
	if !f.playPlayerMove() {
		t.Fatal("玩家走子应成行")
	}
	f.page.onPlayerMoved()
	staleID := f.page.aiRequestID
	history := len(f.page.store.State().MoveHistory)

	f.page.undoMove()
	if len(f.ai.cancels) != 1 || f.ai.cancels[0] != staleID {
		t.Fatalf("应取消在途请求: %v", f.ai.cancels)
	}
	if len(f.page.store.State().MoveHistory) != history {
		t.Fatal("输入锁未解，undoRound 应 no-op（悔棋不生效）")
	}
	if !f.page.aiThinking || f.page.aiRequestID == "" || f.page.aiRequestID == staleID {
		t.Fatal("应重新触发 AI（新请求 id）")
	}
	if f.canTapSelect(rules.Red) {
		t.Fatal("重想期间输入应保持锁定")
	}
}

// 非思考路径悔棋：撤 AI 应手 + 玩家最近一手（undoRound 人机口径），
// 回到玩家回合不再触发。
func TestAiPageUndoRoundWhenIdle(t *testing.T) {
	f := newAiEnvFixture(true)
	defer f.page.Dispose()
	f.page.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsAi})
	if !f.playPlayerMove() {
		t.Fatal("玩家走子应成行")
	}
	f.page.onPlayerMoved()
	id := f.page.aiRequestID
	move := *aiReply()
	f.replyNow()
	f.page.OnAppEvent(EngineMoveDone{RequestID: id, Move: &move})

	f.page.undoMove()
	if len(f.page.store.State().MoveHistory) != 0 {
		t.Fatalf("应撤 AI 应手与玩家一手: %d", len(f.page.store.State().MoveHistory))
	}
	if f.page.aiThinking || f.page.aiRequestID != "" {
		t.Fatal("回到玩家回合不应触发 AI")
	}
	if !f.canTapSelect(rules.Red) {
		t.Fatal("悔棋后应解锁")
	}
}

// 上游 switchSide：执黑切换 → 重开新局 + AI（红）先行；再切回执红 → 新局无触发。
func TestAiPageSwitchSideAiFirst(t *testing.T) {
	f := newAiEnvFixture(true)
	defer f.page.Dispose()
	f.page.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsAi})

	f.page.switchSide(rules.Black)
	if f.page.playerSide != rules.Black {
		t.Fatal("执方应切换为黑")
	}
	if !f.page.aiThinking || len(f.page.store.State().MoveHistory) != 0 {
		t.Fatal("执黑应重开新局并 AI 先行")
	}
	firstID := f.page.aiRequestID
	if len(f.ai.cancels) != 0 {
		t.Fatalf("切换前无在途请求: %v", f.ai.cancels)
	}

	// 执黑时 AI（红）应手落盘 → 轮玩家（黑）：炮二平五（7,7)→(4,7)
	move := rules.Move{From: rules.Pos(7, 7), To: rules.Pos(4, 7)}
	f.replyNow()
	f.page.OnAppEvent(EngineMoveDone{RequestID: firstID, Move: &move})
	if f.page.store.VM.IsRedTurn() {
		t.Fatal("AI 落盘后应轮黑方")
	}
	if !f.canTapSelect(rules.Black) {
		t.Fatal("玩家回合应解锁")
	}

	// 切回执红：取消无在途（已结束）、新局、不触发
	f.page.switchSide(rules.Red)
	if f.page.playerSide != rules.Red || !f.page.store.VM.IsRedTurn() {
		t.Fatal("切回执红应新局红先")
	}
	if f.page.aiThinking || f.page.aiRequestID != "" {
		t.Fatal("执红不应触发 AI")
	}
}

// 上游挂载/恢复流：执黑时恢复定局后若轮 AI（红）→ AI 先行（必须等 restore
// 完成后再触发，防 restore 重置抹掉已落应手）。
func TestAiPageRestoreThenAiFirstWhenBlack(t *testing.T) {
	f := newAiEnvFixture(true)
	defer f.page.Dispose()
	f.page.playerSide = rules.Black // 测试注入：执黑进页（切换路径另行覆盖）

	f.page.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsAi}) // 恢复完成（无存档→新局）
	if !f.page.aiThinking || len(f.page.store.State().MoveHistory) != 0 {
		t.Fatal("执黑恢复定局后应 AI（红）先行")
	}
	if f.canTapSelect(rules.Black) {
		t.Fatal("AI 思考期间输入应锁定")
	}
}

// Dispose：取消在途 AI 请求（#4/#5）；迟到回执由总线丢弃后页面再防御。
func TestAiPageDisposeCancelsInFlight(t *testing.T) {
	f := newAiEnvFixture(true)
	f.page.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsAi})
	if !f.playPlayerMove() {
		t.Fatal("玩家走子应成行")
	}
	f.page.onPlayerMoved()
	id := f.page.aiRequestID

	f.page.Dispose()
	if len(f.ai.cancels) != 1 || f.ai.cancels[0] != id {
		t.Fatalf("离页应取消在途请求: %v", f.ai.cancels)
	}
	move := *aiReply()
	f.replyNow()
	f.page.OnAppEvent(EngineMoveDone{RequestID: id, Move: &move})
	if len(f.page.store.State().MoveHistory) != 1 {
		t.Fatal("离页后迟到回执不得落盘")
	}
}

// 裁决接线：isHuman=执方判定（玩家侧弹确认框 / AI 侧自动判和——AI 侧路径
// 用独立 judge 走闲着环验证，复用 repetition_test 的 QUIET 环）。
func TestAiPageJudgeWiringAndAiAutoDraw(t *testing.T) {
	f := newAiEnvFixture(true)
	defer f.page.Dispose()
	if !f.page.judge.isHuman(rules.Red) || f.page.judge.isHuman(rules.Black) {
		t.Fatal("执红时 isHuman 应仅红方为玩家")
	}

	// AI 侧重复判和：面对方为 AI（isHuman=false）→ 自动 agreeDraw，无确认框
	vm := state.NewGameVm(swingFen)
	toasts := []string{}
	judge := NewRepetitionJudge(vm, func(rules.Side) bool { return false }, func(m string) { toasts = append(toasts, m) })
	vm.OnHistoryGrow = judge.OnHistoryGrow
	vm.OnHistoryRewound = judge.OnHistoryRewound
	playQuietCycles(t, vm, 2)
	if r := vm.Current().Result; r == nil || *r != state.ResultDraw {
		t.Fatalf("面对 AI 时三次重复应自动判和: %v", r)
	}
	if judge.DrawOffer != nil || len(toasts) != 1 || toasts[0] != "三次重复局面，判和" {
		t.Fatalf("应自动判和无确认框: offer=%v toasts=%v", judge.DrawOffer, toasts)
	}
}

// playQuietCycles 用 QUIET 环走 n 轮（repetition_test 的 quietCycle 复用）。
func playQuietCycles(t *testing.T, vm *state.GameVm, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		for _, m := range quietCycle {
			if !vm.PlayMove(rules.Pos(m[0], m[1]), rules.Pos(m[2], m[3])) {
				t.Fatalf("着法非法: %v", m)
			}
		}
	}
}

// 手动保存回执（mode=humanVsAi）。
func TestAiPageManualSave(t *testing.T) {
	f := newAiEnvFixture(true)
	defer f.page.Dispose()
	f.page.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsAi})
	f.page.saveGame()

	if len(f.db.saves) != 1 || !f.db.saves[0].Manual || f.db.saves[0].Mode != state.ModeHumanVsAi {
		t.Fatalf("手动保存请求 = %+v", f.db.saves)
	}
	f.page.OnAppEvent(DbSaveDone{Mode: state.ModeHumanVsAi, Manual: true})
	if f.page.toastText != "棋局已保存" {
		t.Fatalf("应 toast 已保存: %q", f.page.toastText)
	}
}

// 难度选择即时生效（下一次思考用新档位）+ 难度名映射。
func TestAiPageDifficultySelection(t *testing.T) {
	if difficultyName(5) != "大师" || difficultyName(1) != "初级" {
		t.Fatal("难度名映射错误")
	}
	f := newAiEnvFixture(true)
	defer f.page.Dispose()
	f.page.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsAi})

	f.page.difficulty = 5
	if !f.playPlayerMove() {
		t.Fatal("玩家走子应成行")
	}
	f.page.onPlayerMoved()
	p := f.lastAiRequest(t)
	if p.Difficulty != 5 {
		t.Fatalf("下一次思考应用新档位: %d", p.Difficulty)
	}
}

// DR-G004：回执早于 minThinkDuration 到达 → 保持思考态与输入锁，延迟余量后
// 二次投递并应用（"AI 正在思考..."可感知）。
func TestAiPageMinThinkDefersApply(t *testing.T) {
	f := newAiEnvFixture(true)
	defer f.page.Dispose()
	f.page.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsAi})
	if !f.playPlayerMove() {
		t.Fatal("玩家走子应成行")
	}
	f.page.onPlayerMoved()
	id := f.page.aiRequestID
	historyBefore := len(f.page.store.State().MoveHistory)

	// 回执立即到达（远早于 300ms）：不得立即应用
	move := *aiReply()
	f.page.OnAppEvent(EngineMoveDone{RequestID: id, Move: &move})
	if len(f.page.store.State().MoveHistory) != historyBefore {
		t.Fatal("最小思考期内不得落盘")
	}
	if !f.page.aiThinking || f.page.aiRequestID != id {
		t.Fatal("最小思考期内应保持思考态")
	}
	if f.canTapSelect(rules.Red) {
		t.Fatal("最小思考期内输入应保持锁定")
	}

	// 等待 AfterFunc 余量二次投递 → 消费回执 → 应用
	deadline := time.Now().Add(2 * time.Second)
	var redelivered *EngineMoveDone
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for _, e := range f.emitted {
			if d, ok := e.(EngineMoveDone); ok && d.RequestID == id {
				redelivered = &d
			}
		}
		f.mu.Unlock()
		if redelivered != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if redelivered == nil {
		t.Fatal("延迟余量后应二次投递回执")
	}
	f.page.OnAppEvent(*redelivered)
	if len(f.page.store.State().MoveHistory) != historyBefore+1 {
		t.Fatal("二次投递后应落盘")
	}
	if f.page.aiThinking || f.page.aiRequestID != "" {
		t.Fatal("应用后应退出思考态")
	}
}

// DR-G004：延迟期取消（新局/悔棋/离页）→ 二次投递按 id 丢弃，不落盘（#G5）。
func TestAiPageMinThinkCancelDuringDeferral(t *testing.T) {
	f := newAiEnvFixture(true)
	defer f.page.Dispose()
	f.page.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsAi})
	if !f.playPlayerMove() {
		t.Fatal("玩家走子应成行")
	}
	f.page.onPlayerMoved()
	id := f.page.aiRequestID

	move := *aiReply()
	f.page.OnAppEvent(EngineMoveDone{RequestID: id, Move: &move}) // 进入延迟期
	f.page.doNewGame()                                            // 延迟期取消

	time.Sleep(minThinkDuration + 200*time.Millisecond)
	// 二次投递即使发生（总线/页面双层丢弃），也不得落盘
	f.page.OnAppEvent(EngineMoveDone{RequestID: id, Move: &move})
	if len(f.page.store.State().MoveHistory) != 0 {
		t.Fatal("延迟期取消后回执不得落盘")
	}
	if f.page.aiThinking || f.page.aiRequestID != "" {
		t.Fatal("取消后应退出思考态")
	}
}
