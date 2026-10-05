package engine

// 协议层与棋手接口单测（09 §2.4 goroutine/绑定集成的 Go 侧对应；
// Electron 版 engineWorker.spec.ts 协议 roundtrip/取消语义的 DR-003 移植）。
import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

const protocolInitialFen = "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1"

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal 载荷失败: %v", err)
	}
	return raw
}

// 协议 roundtrip：findBestMove / findBestMoveEx / evaluateMove 三类型。
func TestProtocolRoundtrip(t *testing.T) {
	runner := NewRunner()

	// findBestMove：OK + WireMove。
	resp := <-runner.Submit(Request{
		ID:      "fbm-1",
		Type:    ReqFindBestMove,
		Payload: mustJSON(t, FindBestMovePayload{Fen: protocolInitialFen, Difficulty: 1}),
	})
	if !resp.OK {
		t.Fatalf("findBestMove 应成功: %s", resp.Error)
	}
	move, ok := resp.Result.(WireMove)
	if !ok {
		t.Fatalf("result 应为 WireMove，got %T", resp.Result)
	}
	if move.From.Col < 0 || move.From.Col > 8 || move.From.Row < 0 || move.From.Row > 9 {
		t.Errorf("from 越界: %+v", move.From)
	}

	// findBestMove：无合法走法 → result 缺省（null 语义）。
	resp = <-runner.Submit(Request{
		ID:      "fbm-2",
		Type:    ReqFindBestMove,
		Payload: mustJSON(t, FindBestMovePayload{Fen: "R3k4/9/9/9/9/4R4/9/9/9/4K4 b - - 0 1", Difficulty: 1}),
	})
	if !resp.OK || resp.Result != nil {
		t.Errorf("被将死应 OK+null，got %+v", resp)
	}

	// findBestMoveEx：OK + WireReport（topK 二元组 JSON 形状另测）。
	resp = <-runner.Submit(Request{
		ID:      "ex-1",
		Type:    ReqFindBestMoveEx,
		Payload: mustJSON(t, FindBestMoveExPayload{Fen: protocolInitialFen, Depth: 2, TopK: 3}),
	})
	if !resp.OK {
		t.Fatalf("findBestMoveEx 应成功: %s", resp.Error)
	}
	report, ok := resp.Result.(WireReport)
	if !ok {
		t.Fatalf("result 应为 WireReport，got %T", resp.Result)
	}
	if len(report.TopK) != 3 {
		t.Errorf("topK 长度 = %d, want 3", len(report.TopK))
	}

	// evaluateMove：OK + 分数。
	resp = <-runner.Submit(Request{
		ID:   "ev-1",
		Type: ReqEvaluateMove,
		Payload: mustJSON(t, map[string]any{
			"fen":   protocolInitialFen,
			"move":  map[string]any{"from": map[string]any{"col": 1, "row": 7}, "to": map[string]any{"col": 4, "row": 7}},
			"depth": 3,
		}),
	})
	if !resp.OK {
		t.Fatalf("evaluateMove 应成功: %s", resp.Error)
	}
	if _, ok := resp.Result.(int); !ok {
		t.Errorf("evaluateMove 结果应为 int 分数，got %T", resp.Result)
	}
}

// wire JSON 形状对齐前端协议：topK 为 [move, cp] 二元组、captured 按需省略。
func TestProtocolWireJSONShape(t *testing.T) {
	report := WireReport{
		Best:   WireMove{From: WirePos{Col: 0, Row: 6}, To: WirePos{Col: 0, Row: 4}, Captured: &WirePiece{Kind: "rook", Side: "black"}},
		BestCp: 900,
		TopK:   []WireTopKEntry{{Move: WireMove{From: WirePos{Col: 0, Row: 6}, To: WirePos{Col: 0, Row: 4}}, Cp: 900}},
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	topK, ok := generic["topK"].([]any)
	if !ok || len(topK) != 1 {
		t.Fatalf("topK 应为数组")
	}
	pair, ok := topK[0].([]any)
	if !ok || len(pair) != 2 {
		t.Fatalf("topK 表项应为 [move, cp] 二元组")
	}
	if _, ok := pair[1].(float64); !ok {
		t.Errorf("二元组第二位应为 cp 数值")
	}
	bestJSON, _ := json.Marshal(report.Best)
	if !strings.Contains(string(bestJSON), `"captured"`) {
		t.Error("吃子着法应含 captured")
	}
	quiet := WireMove{From: WirePos{Col: 1, Row: 2}, To: WirePos{Col: 1, Row: 3}}
	quietJSON, _ := json.Marshal(quiet)
	if strings.Contains(string(quietJSON), "captured") {
		t.Error("非吃子着法不应含 captured 字段")
	}
}

// cancel 后立即 canceled 结算（取消=丢弃语义收口，03 §7）。
func TestProtocolCancelDuringSearch(t *testing.T) {
	runner := NewRunner()
	ch := runner.Submit(Request{
		ID:      "cancel-1",
		Type:    ReqFindBestMove,
		Payload: mustJSON(t, FindBestMovePayload{Fen: protocolInitialFen, Difficulty: 5}), // 长搜索
	})
	deadline := time.After(2 * time.Second)
	// 轮询等待搜索进入（探针存在即已开始），随后取消。
	time.Sleep(50 * time.Millisecond)
	runner.Cancel("cancel-1")
	select {
	case resp := <-ch:
		if resp.OK {
			t.Fatal("取消后不应返回有效结果")
		}
		if resp.Error != ErrCanceled.Error() {
			t.Errorf("error = %q, want %q", resp.Error, ErrCanceled.Error())
		}
	case <-deadline:
		t.Fatal("取消后 2s 内未结算（搜索未被中止）")
	}
}

// cancel 未知 id 幂等无操作。
func TestProtocolCancelIdempotent(t *testing.T) {
	runner := NewRunner()
	runner.Cancel("nonexistent") // 不 panic
	resp := <-runner.Submit(Request{ID: "x", Type: ReqCancel, Payload: mustJSON(t, map[string]any{})})
	if !resp.OK {
		t.Errorf("cancel 请求应幂等 OK，got %+v", resp)
	}
}

// 迟到响应按 id 丢弃由前端 pending 表收口（00 §3.2）；Go 侧保证：cancel 后
// 同 id 重新 Submit 可正常受理（注册表已清理，无悬挂状态）。
func TestProtocolResubmitAfterCancel(t *testing.T) {
	runner := NewRunner()
	ch := runner.Submit(Request{
		ID:      "reuse-1",
		Type:    ReqFindBestMove,
		Payload: mustJSON(t, FindBestMovePayload{Fen: protocolInitialFen, Difficulty: 5}),
	})
	time.Sleep(20 * time.Millisecond)
	runner.Cancel("reuse-1")
	<-ch // canceled 结算
	ch2 := runner.Submit(Request{
		ID:      "reuse-1",
		Type:    ReqFindBestMove,
		Payload: mustJSON(t, FindBestMovePayload{Fen: "3k5/9/9/9/r8/9/R8/9/9/4K4 w - - 0 1", Difficulty: 1}),
	})
	select {
	case resp := <-ch2:
		if !resp.OK {
			t.Fatalf("同 id 重提应成功: %s", resp.Error)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("重提请求未结算")
	}
}

// 无效 FEN / 未知类型 → ok:false 错误响应。
func TestProtocolErrorPaths(t *testing.T) {
	runner := NewRunner()
	resp := <-runner.Submit(Request{
		ID:      "bad-fen",
		Type:    ReqFindBestMove,
		Payload: mustJSON(t, FindBestMovePayload{Fen: "not a fen", Difficulty: 1}),
	})
	if resp.OK || resp.Error == "" {
		t.Errorf("无效 FEN 应报错，got %+v", resp)
	}
	resp = <-runner.Submit(Request{ID: "bad-type", Type: "bogus", Payload: mustJSON(t, map[string]any{})})
	if resp.OK || !strings.Contains(resp.Error, "Unknown engine request type") {
		t.Errorf("未知类型应报错，got %+v", resp)
	}
}

// Handle：ctx 已取消时入口即回 canceled（不空算）。
func TestProtocolHandleCanceledCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resp := Handle(ctx, Request{
		ID:      "pre-cancel",
		Type:    ReqFindBestMove,
		Payload: mustJSON(t, FindBestMovePayload{Fen: protocolInitialFen, Difficulty: 5}),
	})
	if resp.OK || resp.Error != ErrCanceled.Error() {
		t.Errorf("已取消 ctx 应立即回 canceled，got %+v", resp)
	}
}

// -----------------------------------------------------------------------------
// ChessAiPlayer（MoveSource 实现，03 §8）
// -----------------------------------------------------------------------------

// NextMove：初始局面返回合法走法 + 展示名。
func TestChessAiPlayerNextMoveOk(t *testing.T) {
	player := NewChessAiPlayer(3)
	if player.DisplayName() != "内置 AI（高级）" {
		t.Errorf("DisplayName = %q", player.DisplayName())
	}
	result, err := player.NextMove(context.Background(), rules.Initial(), nil)
	if err != nil {
		t.Fatalf("NextMove 报错: %v", err)
	}
	if result.Status != StatusOK || result.Move == nil {
		t.Fatalf("应返回 ok + move，got %+v", result)
	}
}

// NextMove：被将死 → noLegalMove。
func TestChessAiPlayerNoLegalMove(t *testing.T) {
	player := NewChessAiPlayer(1)
	b := mustBoard(t, "R3k4/9/9/9/9/4R4/9/9/9/4K4 b - - 0 1")
	result, err := player.NextMove(context.Background(), b, nil)
	if err != nil {
		t.Fatalf("NextMove 报错: %v", err)
	}
	if result.Status != StatusNoLegalMove {
		t.Errorf("应 noLegalMove，got %q", result.Status)
	}
}

// NextMove：ctx 已取消 → canceled 错误上抛（调用方丢弃语义）。
func TestChessAiPlayerCanceled(t *testing.T) {
	player := NewChessAiPlayer(1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := player.NextMove(ctx, rules.Initial(), nil)
	if err == nil {
		t.Fatal("已取消 ctx 应上抛错误")
	}
}

// NextMove/HistoryFensFromBoard：从 board+history 推导的 fenHistory 与页面侧
// （VM fenHistory，重放式采集）逐项一致——L2 历史入参引擎侧单源的接口契约（03 §8）。
func TestChessAiPlayerHistoryFensDerived(t *testing.T) {
	startFen := "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1"
	board := mustBoard(t, startFen)
	// 三手对局：炮二平五 / 马8进7 / 车一平二（各含被吃子快照口径，此处无吃子）。
	history := make([]rules.Move, 0, 3)
	for _, mv := range []rules.Move{
		{From: rules.Pos(7, 7), To: rules.Pos(4, 7)}, // 红炮平中
		{From: rules.Pos(7, 0), To: rules.Pos(6, 2)}, // 黑马上
		{From: rules.Pos(8, 9), To: rules.Pos(7, 9)}, // 红车平一
	} {
		snapshot := board.ApplyMove(mv)
		history = append(history, rules.Move{From: mv.From, To: mv.To, Captured: snapshot.Captured})
	}

	// 页面侧口径：重放式采集每个中间局面（start → ... → 当前）。
	pageFens := []string{startFen}
	replay := mustBoard(t, startFen)
	for _, mv := range history {
		replay.ApplyMove(mv)
		pageFens = append(pageFens, replay.ToFen())
	}
	if replay.ToFen() != board.ToFen() {
		t.Fatal("重放终局应与当前盘一致")
	}

	// 引擎侧单源：HistoryFensFromBoard(board, history) 应逐项等于 pageFens。
	derived := HistoryFensFromBoard(board, history)
	if len(derived) != len(pageFens) {
		t.Fatalf("推导长度 %d ≠ 页面长度 %d", len(derived), len(pageFens))
	}
	for i := range pageFens {
		if derived[i] != pageFens[i] {
			t.Errorf("fenHistory[%d] = %q, want %q", i, derived[i], pageFens[i])
		}
	}

	// 等价性：NextMove 的 L2 入参与页面口径一致 → 两次 FindBestMove 输出一致。
	player := NewChessAiPlayer(3)
	result, err := player.NextMove(context.Background(), board, history)
	if err != nil {
		t.Fatalf("NextMove 报错: %v", err)
	}
	if result.Status != StatusOK || result.Move == nil {
		t.Fatalf("应返回 ok，got %+v", result)
	}
	pageResult, err := FindBestMove(board.ToFen(), FindBestMoveOptions{Difficulty: 3, HistoryFens: pageFens})
	if err != nil || pageResult == nil {
		t.Fatalf("FindBestMove(页面口径) 报错: %v", err)
	}
	if moveKey(*result.Move) != moveKey(*pageResult) {
		t.Errorf("引擎推导与页面口径应产生一致应手: %s vs %s",
			moveKey(*result.Move), moveKey(*pageResult))
	}
}
