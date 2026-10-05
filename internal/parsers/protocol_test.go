package parsers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// 解析协议等价用例（parserProtocol.ts dispatch 语义 + 取消语义，06 文档 §6）。

func mustPayload(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestHandleParseBatchResultsAndProgress(t *testing.T) {
	// 批量解析：结果与 files 等长一一对应；每文件一次进度回调。
	xqfBytes := buildXqf(t, xqfBuildOptions{version: 0x0a, fen: rules.FENInitial, moves: []string{"h2e2"}})
	pgnBytes := []byte("1. 炮二平五 马8进7\n")
	req := Request{
		ID:   "r1",
		Type: ReqParseBatch,
		Payload: mustPayload(t, ParseBatchPayload{Files: []ParseFileInput{
			{Name: "a.xqf", Source: "残局", Bytes: xqfBytes},
			{Name: "b.pgn", Source: "全局", Bytes: pgnBytes},
			{Name: "bad.xqf", Source: "残局", Bytes: []byte("garbage")},
		}}),
	}
	var progress []Progress
	resp := Handle(t.Context(), req, func(done, total int) { progress = append(progress, Progress{Done: done, Total: total}) })
	if !resp.OK {
		t.Fatalf("resp = %+v", resp)
	}
	result := resp.Result.(ParseBatchResult)
	if len(result.Puzzles) != 3 {
		t.Fatalf("puzzles = %d", len(result.Puzzles))
	}
	if result.Puzzles[0] == nil || result.Puzzles[0].Format != "xqf" || result.Puzzles[0].Source != "残局" {
		t.Fatalf("puzzles[0] = %+v", result.Puzzles[0])
	}
	if result.Puzzles[1] == nil || result.Puzzles[1].SolutionMoves[0] != "h2e2" {
		t.Fatalf("puzzles[1] = %+v", result.Puzzles[1])
	}
	if result.Puzzles[2] != nil {
		t.Fatal("损坏文件位应为 null")
	}
	// 进度 n/m：3 次回调（1/3, 2/3, 3/3）。
	if len(progress) != 3 || progress[2].Done != 3 || progress[2].Total != 3 {
		t.Fatalf("progress = %+v", progress)
	}
}

func TestHandleParseBatchEmptyAndUnknownType(t *testing.T) {
	resp := Handle(t.Context(), Request{ID: "r", Type: ReqParseBatch, Payload: mustPayload(t, ParseBatchPayload{})}, nil)
	if !resp.OK || len(resp.Result.(ParseBatchResult).Puzzles) != 0 {
		t.Fatalf("空批 = %+v", resp)
	}
	resp = Handle(t.Context(), Request{ID: "r", Type: "bogus"}, nil)
	if resp.OK || !strings.Contains(resp.Error, "Unknown parser request type") {
		t.Fatalf("未知类型 = %+v", resp)
	}
}

func TestRunnerCancelMidBatch(t *testing.T) {
	// 取消语义：批中途 ctx 取消 → 以 canceled 结算；已解析文件位保持。
	runner := NewRunner()
	files := make([]ParseFileInput, 0, 64)
	for i := 0; i < 64; i++ {
		files = append(files, ParseFileInput{Name: "a.xqf", Source: "残局", Bytes: buildXqf(t,
			xqfBuildOptions{version: 0x0a, fen: rules.FENInitial, moves: []string{"h2e2", "h9g7"}})})
	}
	req := Request{ID: "batch-1", Type: ReqParseBatch, Payload: mustPayload(t, ParseBatchPayload{Files: files})}
	ch := runner.Submit(req, func(done, total int) {
		if done == 10 {
			runner.Cancel("batch-1") // 第 10 个文件完成后取消
		}
	})
	resp := <-ch
	if resp.OK || resp.Error != "canceled" {
		t.Fatalf("取消后 resp = %+v", resp)
	}
	// 取消后的迟到再取消/未知 id：幂等无 panic。
	runner.Cancel("batch-1")
	runner.Cancel("unknown-id")
}

func TestRunnerCancelBeforeSubmit(t *testing.T) {
	// 请求到达前已取消：Submit(cancel 类型) 幂等回 ok（对齐 dispatch cancel 分支）。
	runner := NewRunner()
	ch := runner.Submit(Request{ID: "x", Type: ReqCancel}, nil)
	resp := <-ch
	if !resp.OK || resp.ID != "x" {
		t.Fatalf("cancel 提前 = %+v", resp)
	}
}

func TestHandleProgressNilSafe(t *testing.T) {
	// onProgress 为 nil 时不回调（绑定层测试注入用）。
	req := Request{ID: "r", Type: ReqParseBatch, Payload: mustPayload(t, ParseBatchPayload{Files: []ParseFileInput{
		{Name: "a.pgn", Source: "s", Bytes: []byte("1. 炮二平五\n")},
	}})}
	resp := Handle(t.Context(), req, nil)
	if !resp.OK {
		t.Fatalf("resp = %+v", resp)
	}
}
