package rules

// L3 规则裁决层（DR-006，前置 electron-DR-018 final 设计 §5；02 文档 §7）。
//
// 无状态纯函数：每次真实落子后由页面以完整 fenHistory 调用，内部现算出现次数
// 与环内将军归责——悔棋/读档/新局回滚即截断重算，无任何可被污染的增量状态。
//
// v1 范围（DR-006 裁决）：长将判负 + 重复局面判和（含双方长将不变作和、
// 第 4 次出现强制判和）；长捉判定不做（复杂度高、误判即判负）。
// 长将归责为亚洲棋规的近似：以"重现计数 + 环内该方每手均将军"替代连续长将追踪。
//
// 判定细节：
//   - 局面键 = 完整 FEN（含轮走方，fen.go 恒 halfMove 0）；
//   - 第 i 手的走子方 = fenHistory[i] 的轮走方；该手"将军" = fenHistory[i+1] 局面下
//     其对手被 Board.IsCheck 判将（含将帅照面）；
//   - 环 = 最近两次重现之间的着法；归责 = 环内该方每手均将军（且该方有手）。
//
// 纯 Go：禁止 import Wails / net/http / frontend 任何符号（铁律 #1）。

// VerdictKind 裁决类别（02 文档 §7.1）。
type VerdictKind int

const (
	VerdictNone VerdictKind = iota
	// VerdictPerpetualCheckWarning k=2 单方全程将军 → 非阻塞警告。
	VerdictPerpetualCheckWarning
	// VerdictPerpetualCheckLoss k=3 单方全程将军 → 违规方判负。
	VerdictPerpetualCheckLoss
	// VerdictBothPerpetualCheckDraw k=3 双方全程将军 → 不变作和。
	VerdictBothPerpetualCheckDraw
	// VerdictRepetitionDraw k=3 无人全程将军 → 三次重复判和。
	VerdictRepetitionDraw
	// VerdictForcedRepetitionDraw k≥4 → 强制判和（拒绝和棋后不再询问）。
	VerdictForcedRepetitionDraw
)

// Verdict 裁决结果：警告/判负时 Side 为归责方，其余类别 Side 为空。
type Verdict struct {
	Kind VerdictKind
	Side Side
}

// cycleClass 环内将军归责：red | black | both | none。
type cycleClass string

const (
	cycleRed   cycleClass = "red"
	cycleBlack cycleClass = "black"
	cycleBoth  cycleClass = "both"
	cycleNone  cycleClass = "none"
)

// JudgeRepetition 裁决入口：以完整局面 FEN 序列（含初始局面与轮走方）调用。
// 返回 nil = 无重复/无需介入。
func JudgeRepetition(fenHistory []string) *Verdict {
	if len(fenHistory) < 2 {
		return nil
	}
	last := fenHistory[len(fenHistory)-1]
	occurrences := []int{}
	for i := 0; i < len(fenHistory); i++ {
		if fenHistory[i] == last {
			occurrences = append(occurrences, i)
		}
	}
	k := len(occurrences)
	if k <= 1 {
		return nil
	}

	// 最近一环：最近两次重现之间的着法（fenHistory[prev+1 .. len-1]）。
	prev := occurrences[k-2]
	cls := classifyCycle(fenHistory, prev)

	if k == 2 {
		if cls == cycleRed {
			return &Verdict{Kind: VerdictPerpetualCheckWarning, Side: Red}
		}
		if cls == cycleBlack {
			return &Verdict{Kind: VerdictPerpetualCheckWarning, Side: Black}
		}
		return nil // 闲着/双方将军重现：仅长将形态才警告
	}
	if k == 3 {
		if cls == cycleRed {
			return &Verdict{Kind: VerdictPerpetualCheckLoss, Side: Red}
		}
		if cls == cycleBlack {
			return &Verdict{Kind: VerdictPerpetualCheckLoss, Side: Black}
		}
		if cls == cycleBoth {
			return &Verdict{Kind: VerdictBothPerpetualCheckDraw}
		}
		return &Verdict{Kind: VerdictRepetitionDraw}
	}
	// k≥4：仅当玩家在 k=3 拒绝过和棋才会走到（页面在 k=3 弹确认框）。
	return &Verdict{Kind: VerdictForcedRepetitionDraw}
}

// classifyCycle 环内将军归责：每手走子方按其"将军"标志统计，全将军者为长将方。
func classifyCycle(fenHistory []string, prevIdx int) cycleClass {
	// isCheck 结果缓存（同一调用内同一局面只算一次）。
	checkCache := map[string]bool{}
	total := map[Side]int{Red: 0, Black: 0}
	checks := map[Side]int{Red: 0, Black: 0}
	for i := prevIdx; i < len(fenHistory)-1; i++ {
		moverIsRed := ParseTurnFen(fenHistory[i])
		mover := Black
		if moverIsRed {
			mover = Red
		}
		after := fenHistory[i+1]
		gaveCheck, ok := checkCache[after]
		if !ok {
			b, err := FromFen(after)
			if err != nil {
				// 无效 FEN（理论上不出现——历史由本方对局采集）：该手不计将军。
				gaveCheck = false
			} else {
				// 走子后轮走方 = mover 的对手；其被将军即该手为"将军"。
				gaveCheck = b.IsCheck(OpponentOf(mover))
			}
			checkCache[after] = gaveCheck
		}
		total[mover]++
		if gaveCheck {
			checks[mover]++
		}
	}
	redAll := total[Red] > 0 && checks[Red] == total[Red]
	blackAll := total[Black] > 0 && checks[Black] == total[Black]
	if redAll && blackAll {
		return cycleBoth
	}
	if redAll {
		return cycleRed
	}
	if blackAll {
		return cycleBlack
	}
	return cycleNone
}
