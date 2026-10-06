package ui

// 重复裁决页面集成测试（翻译源 = 上游 frontend/test/pages/repetitionJudge.spec.tsx，
// T3.10，DR-018，08 防错 #11/#12；headless 形态：09 §4 "能在 state 层测的逻辑
// 不进 Gio 层测"——上游经 jsdom 点击棋盘走子，此处以 vm.PlayMove 驱动
//（与 BoardView 动画结束后的 vm.OnTap 同为 executeMove→OnHistoryGrow 触发链），
// toast/确认框呈现层断言转为记录回调与 DrawOffer 状态）。
//
// 两种手构环（残局 FEN 起盘，双人页逐手走子）：
// - SWING 长将环（车在底线来回将军）：k=2 → toast 警告；k=3 → 长将方判负；
// - QUIET 闲着环（车摆动、双方王闲走，无人将军）：k=3 → 和棋确认框
//   （拒绝可变着 / 接受判和）；k=4 → 强制判和。

import (
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
)

const swingFen = "4k4/R8/9/9/9/9/9/9/9/8K w - - 0 1" // 车 a9、黑王 e10、红王 h1

// SWING：Ra9-a10+ / Ke10-e9 / Ra10-a9+ / Ke9-e10 → 回到起盘（红方每手将军）。
var swingCycle = [][4]int{
	{0, 1, 0, 0},
	{4, 0, 4, 1},
	{0, 0, 0, 1},
	{4, 1, 4, 0},
}

// QUIET：Ra9-a8 / Ke10-d10 / Ra8-a9 / Kd10-e10 → 回到起盘（无人将军）。
var quietCycle = [][4]int{
	{0, 1, 0, 2},
	{4, 0, 3, 0},
	{0, 2, 0, 1},
	{3, 0, 4, 0},
}

type judgeFixture struct {
	vm     *state.GameVm
	judge  *RepetitionJudge
	toasts []string
}

func newJudgeFixture() *judgeFixture {
	vm := state.NewGameVm(swingFen)
	f := &judgeFixture{vm: vm}
	f.judge = NewRepetitionJudge(vm, func(rules.Side) bool { return true }, func(m string) {
		f.toasts = append(f.toasts, m)
	})
	vm.OnHistoryGrow = f.judge.OnHistoryGrow
	vm.OnHistoryRewound = f.judge.OnHistoryRewound
	return f
}

// playCycle 逐手走完一轮（vm.PlayMove = 合法性校验 + executeMove 收口触发）。
func (f *judgeFixture) playCycle(t *testing.T, cycle [][4]int) {
	t.Helper()
	for _, m := range cycle {
		from := rules.Pos(m[0], m[1])
		to := rules.Pos(m[2], m[3])
		if !f.vm.PlayMove(from, to) {
			t.Fatalf("着法非法: %v→%v", from, to)
		}
	}
}

// 上游 spec: "长将环：k=2 toast 非阻塞警告 → k=3 长将方判负（无弹窗）"
func TestRepetitionJudgePerpetualCheck(t *testing.T) {
	f := newJudgeFixture()

	// 第一轮：k=2 → toast 非阻塞警告（不弹确认框，防错 #11）。
	f.playCycle(t, swingCycle)
	if len(f.toasts) == 0 || f.toasts[len(f.toasts)-1] != "红方连续将军重复，再次将判负" {
		t.Fatalf("k=2 应 toast 警告: %v", f.toasts)
	}
	if f.judge.DrawOffer != nil {
		t.Fatal("k=2 不应弹确认框")
	}

	// 第二轮：k=3 → 长将方（红）判负，走既有结算横幅（ResultBanner）。
	f.playCycle(t, swingCycle)
	if r := f.vm.Current().Result; r == nil || *r != state.ResultBlackWins {
		t.Fatalf("k=3 应红方判负（黑胜）: %v", f.vm.Current().Result)
	}
	// k=2 期间每次历史增长重复警告（rules 语义，上游同款），终末为判负 toast
	if f.toasts[len(f.toasts)-1] != "红方长将，判负" {
		t.Fatalf("k=3 应 toast 判负: %v", f.toasts)
	}
}

// 上游 spec: "闲着环：k=2 无警告 → k=3 确认框（拒绝变着）→ k=4 强制判和"
func TestRepetitionJudgeQuietCycleDeclineThenForced(t *testing.T) {
	f := newJudgeFixture()

	// 第一轮：k=2 闲着重现 → 不警告不弹框。
	f.playCycle(t, quietCycle)
	if len(f.toasts) != 0 || f.judge.DrawOffer != nil {
		t.Fatalf("闲着 k=2 不应介入: toasts=%v offer=%v", f.toasts, f.judge.DrawOffer)
	}

	// 第二轮：k=3 → 三次重复判和确认框（防错 #12）。
	f.playCycle(t, quietCycle)
	if f.judge.DrawOffer == nil || *f.judge.DrawOffer != rules.Red {
		t.Fatalf("k=3 应置和棋确认框（面对方=红）: %v", f.judge.DrawOffer)
	}

	// 玩家拒绝（变着继续）→ 对局继续。
	f.judge.DeclineDraw()
	if f.judge.DrawOffer != nil || f.vm.IsFinished() {
		t.Fatal("拒绝后对局应继续")
	}

	// 第三轮：k=4 → 强制判和。
	f.playCycle(t, quietCycle)
	if r := f.vm.Current().Result; r == nil || *r != state.ResultDraw {
		t.Fatalf("k=4 应强制判和: %v", f.vm.Current().Result)
	}
	if len(f.toasts) != 1 || f.toasts[0] != "再次重复局面，强制判和" {
		t.Fatalf("k=4 应 toast 强制判和: %v", f.toasts)
	}
}

// 上游 spec: "闲着环：k=3 接受和棋 → result=draw 终局"
func TestRepetitionJudgeQuietCycleAccept(t *testing.T) {
	f := newJudgeFixture()

	f.playCycle(t, quietCycle)
	f.playCycle(t, quietCycle)
	if f.judge.DrawOffer == nil {
		t.Fatal("k=3 应置和棋确认框")
	}
	f.judge.AcceptDraw()
	r := f.vm.Current().Result
	if r == nil || *r != state.ResultDraw {
		t.Fatalf("接受和棋后 result 应为 draw: %v", r)
	}
	if f.judge.DrawOffer != nil {
		t.Fatal("接受后确认框应关闭")
	}
}

// 悔棋/新局重置 declined 与未决确认框（07 §3 OnHistoryRewound 收口；
// 上游 useEffect historyLen<prev 分支）。
func TestRepetitionJudgeRewoundResets(t *testing.T) {
	f := newJudgeFixture()
	f.playCycle(t, quietCycle)
	f.playCycle(t, quietCycle)
	if f.judge.DrawOffer == nil {
		t.Fatal("前置：k=3 应有确认框")
	}

	f.judge.DeclineDraw()
	f.vm.Undo() // 悔棋（pop 侧收口触发）
	if f.judge.DrawOffer != nil {
		t.Fatal("悔棋应关闭未决确认框")
	}
	// 再走回重复 → 重新弹框（k=3 重新达到）
	f.vm.PlayMove(rules.Pos(3, 0), rules.Pos(4, 0))
	if f.judge.DrawOffer == nil {
		t.Fatal("重新达到 k=3 应重新弹框")
	}

	f.vm.NewGame() // 新局（重置侧收口触发）
	if f.judge.DrawOffer != nil || f.vm.IsFinished() {
		t.Fatal("新局应重置裁决状态")
	}
}
