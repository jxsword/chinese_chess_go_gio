package solver

// 求解器协议层测试（T6.1，04 文档 §2/§9.4）：请求/响应 roundtrip、取消链路、
// 迟到收口、wire 形状对齐前端 solverProtocol.ts。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestHandleSolveRoundtrip(t *testing.T) {
	payload, err := json.Marshal(SolvePayload{Fen: fenA, TimeLimitMs: 10_000, MaxPlies: 3})
	if err != nil {
		t.Fatal(err)
	}
	resp := Handle(context.Background(), Request{ID: "id-1", Type: ReqSolve, Payload: payload})
	if !resp.OK {
		t.Fatalf("solve 应成功: %s", resp.Error)
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Status    string `json:"status"`
		Solutions []struct {
			Moves []map[string]any `json:"moves"`
		} `json:"solutions"`
		Elapsed       int64 `json:"elapsed"`
		SearchedPlies int   `json:"searchedPlies"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("结果非 SolveResult 形状: %v (%s)", err, raw)
	}
	// FEN-A 一着杀：迭代加深在第 1 轮即解出（searchedPlies=1，与 TS 一致）。
	if wire.Status != "solved" || len(wire.Solutions) < 2 || wire.SearchedPlies != 1 {
		t.Fatalf("结果字段不符: %s", raw)
	}
	// moves 为 {from:{col,row}, to:{col,row}} 形状（前端 formatIccs/chineseNotations 消费）。
	first := wire.Solutions[0].Moves[0]
	fromMap, ok := first["from"].(map[string]any)
	if !ok {
		t.Fatalf("moves[0].from 非坐标对象: %s", raw)
	}
	if _, ok := fromMap["col"].(float64); !ok {
		t.Fatalf("from.col 缺失: %s", raw)
	}
}

func TestHandleSolveInvalidFen(t *testing.T) {
	payload, _ := json.Marshal(SolvePayload{Fen: fenE, TimeLimitMs: 1_000, MaxPlies: 3})
	resp := Handle(context.Background(), Request{ID: "id-2", Type: ReqSolve, Payload: payload})
	if resp.OK || resp.Error == "" {
		t.Fatalf("非法 FEN 应以 error 结算: %+v", resp)
	}
}

func TestHandleIsWinningFirstMove(t *testing.T) {
	firstMove, err := json.Marshal(WireMove{
		From: WirePos{Col: 0, Row: 4},
		To:   WirePos{Col: 3, Row: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(IsWinningFirstMovePayload{
		Fen:       fenA,
		FirstMove: firstMove,
		Plies:     1,
	})
	resp := Handle(context.Background(), Request{ID: "id-3", Type: ReqIsWinningFirstMove, Payload: payload})
	if !resp.OK || resp.Result != true {
		t.Fatalf("(0,4)->(3,4) plies=1 应为 true: %+v", resp)
	}

	firstMove, err = json.Marshal(WireMove{
		From: WirePos{Col: 0, Row: 0},
		To:   WirePos{Col: 3, Row: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ = json.Marshal(IsWinningFirstMovePayload{
		Fen:       fenA,
		FirstMove: firstMove,
		Plies:     3,
	})
	resp = Handle(context.Background(), Request{ID: "id-4", Type: ReqIsWinningFirstMove, Payload: payload})
	if !resp.OK || resp.Result != false {
		t.Fatalf("不合法首着应为 false: %+v", resp)
	}
}

func TestHandleUnknownType(t *testing.T) {
	resp := Handle(context.Background(), Request{ID: "id-5", Type: "bogus"})
	if resp.OK || !strings.Contains(resp.Error, "Unknown solver request type") {
		t.Fatalf("未知类型应以错误结算: %+v", resp)
	}
}

func TestHandleEntryCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	payload, _ := json.Marshal(SolvePayload{Fen: fenA})
	resp := Handle(ctx, Request{ID: "id-6", Type: ReqSolve, Payload: payload})
	if resp.OK || resp.Error != ErrCanceled.Error() {
		t.Fatalf("入口已取消应回 canceled: %+v", resp)
	}
}

func TestRunnerCancelInflight(t *testing.T) {
	runner := NewRunner()
	payload, _ := json.Marshal(SolvePayload{Fen: fenC, TimeLimitMs: 60_000, MaxPlies: 13})
	ch := runner.Submit(Request{ID: "id-7", Type: ReqSolve, Payload: payload})
	time.Sleep(30 * time.Millisecond)
	runner.Cancel("id-7")
	select {
	case resp := <-ch:
		if resp.OK || resp.Error != ErrCanceled.Error() {
			t.Fatalf("取消后应以 canceled 结算: %+v", resp)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("取消后 5s 内未结算")
	}
	// 幂等：再取消未知/已结束 id 无副作用。
	runner.Cancel("id-7")
	runner.Cancel("id-unknown")
}

func TestRunnerCancelMsgType(t *testing.T) {
	runner := NewRunner()
	ch := runner.Submit(Request{ID: "id-8", Type: ReqCancel})
	resp := <-ch
	if !resp.OK || resp.ID != "id-8" {
		t.Fatalf("cancel 请求应幂等空响应: %+v", resp)
	}
}
