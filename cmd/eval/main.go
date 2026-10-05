// 五期能力评估 CLI（05 文档 §9；Electron 版 tools/eval.ts 逐行翻译）。
//
// 用法（真实 LLM 对抗赛，密钥只从环境变量读取，不落仓库）：
//
//	LLM_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1 \
//	LLM_MODEL=qwen3-max \
//	LLM_API_KEY=sk-xxx \
//	go run ./cmd/eval -- --games 2 --profile hybrid-candidate
//
// 或一条命令跑四档对比（基线/P0/候选/护航，每档红黑各一局对抗内置 AI 难度 3）：
//
//	LLM_BASE_URL=... LLM_MODEL=... go run ./cmd/eval -- --suite
//
// 场景：
//   - profile vs 内置 AI（"人 vs 大模型"，引擎代打人类侧）
//   - profile vs profile（"大模型对战"，可用 --red/--black 组合）
//
// 输出 JSON 报告到 stdout 并落盘（--out，默认 tmp/eval-report-<时间戳>.json）：
// 胜负/手数/每手耗时/失误率/兜底率，可直接归档对比。
//
// 【DR-005】eval 的 LLM 请求同样恒关思维链：请求体由 internal/llm BuildChatRequest
// 统一组装，按预设映射恒发关闭参数，本 CLI 不提供任何开关。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/engine"
	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// ---------------------------------------------------------------------------
// Node 侧 MoveSource 构造的 Go 对应（ChessAiMoveSource / EngineClient 的 CLI 直连版）
// ---------------------------------------------------------------------------

// chessAiSource CLI 直连引擎的内置 AI 棋手（eval.ts chessAiSource 同构：
// 不传 HistoryFens，对局可复现）。
func chessAiSource(difficulty int) engine.MoveSource { return cliAiSource{difficulty: difficulty} }

type cliAiSource struct{ difficulty int }

func (s cliAiSource) DisplayName() string {
	return fmt.Sprintf("内置 AI（%s）", engine.DifficultyName(s.difficulty))
}

func (s cliAiSource) NextMove(ctx context.Context, b *rules.Board, _ []rules.Move) (engine.MoveSourceResult, error) {
	move, err := engine.FindBestMove(b.ToFen(), engine.FindBestMoveOptions{Difficulty: s.difficulty, Ctx: ctx})
	if err != nil {
		return engine.MoveSourceResult{}, err
	}
	if move == nil {
		return engine.MoveSourceResult{Status: engine.StatusNoLegalMove}, nil
	}
	return engine.MoveSourceResult{Status: engine.StatusOK, Move: move}, nil
}

// cliAdvisor CLI 直连引擎的参谋适配器（AdvisorEngine 接口实现）。
type cliAdvisor struct{}

func (cliAdvisor) FindBestMoveEx(ctx context.Context, fen string, depth, topK, timeLimitMs int) (*engine.EngineReport, error) {
	return engine.FindBestMoveEx(fen, engine.FindBestMoveExOptions{
		Depth: depth, TopK: topK, TimeLimitMs: timeLimitMs, Ctx: ctx,
	})
}

func (cliAdvisor) EvaluateMove(ctx context.Context, fen string, m rules.Move, depth int) (*int, error) {
	return engine.EvaluateMove(fen, m, engine.EvaluateMoveOptions{Depth: depth, Ctx: ctx})
}

// ---------------------------------------------------------------------------
// stderr 逐手进度（DR-011，工具层增强：Electron 版 eval.ts 静默，长跑不可观测；
// 仅写 stderr 人读面——stdout JSON 报告协议面逐字节不变，对拍/快照不受影响）
// ---------------------------------------------------------------------------

// iccs 着法坐标（"h7e7" 形态；与 engine.MatchReport movesIccs 同口径）。
func iccs(m *rules.Move) string {
	return fmt.Sprintf("%c%d%c%d", rune('a'+m.From.Col), m.From.Row, rune('a'+m.To.Col), m.To.Row)
}

// moveLogger 逐手进度装饰器：包装 MoveSource，每次 NextMove 结算后向 stderr
// 打一行（含耗时、着法/失败形态、兜底标记）。对局内两座位共享同一 ply 计数器。
// 注：单手超时路径下本行可能迟到打印（Promise.race 败者语义），不影响报告。
type moveLogger struct {
	inner engine.MoveSource
	label string // 对局标签，如 "baseline-v1·局1/2"
	seat  string // 座位名，如 "红"
	ply   *atomic.Int64
}

func (l *moveLogger) DisplayName() string { return l.inner.DisplayName() }

func (l *moveLogger) NextMove(ctx context.Context, b *rules.Board, history []rules.Move) (engine.MoveSourceResult, error) {
	n := l.ply.Add(1)
	start := time.Now()
	res, err := l.inner.NextMove(ctx, b, history)
	elapsed := time.Since(start).Milliseconds()
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "[%s P%02d %s] 异常：%v（%dms）\n", l.label, n, l.seat, err, elapsed)
	case res.Status == engine.StatusOK && res.Move != nil:
		marker := ""
		if res.FromFallback {
			marker = " ⚠兜底"
		}
		fmt.Fprintf(os.Stderr, "[%s P%02d %s] %s（%dms）%s\n", l.label, n, l.seat, iccs(res.Move), elapsed, marker)
	case res.Status == engine.StatusNoLegalMove:
		fmt.Fprintf(os.Stderr, "[%s P%02d %s] 无着（%dms）\n", l.label, n, l.seat, elapsed)
	default:
		note := res.Note
		if len(note) > 60 {
			note = note[:60] + "…"
		}
		fmt.Fprintf(os.Stderr, "[%s P%02d %s] 失败：%s（%dms）\n", l.label, n, l.seat, note, elapsed)
	}
	return res, err
}

// runLoggedMatch 带进度的一场对局：开局头 + 逐手行 + 终局摘要。
func runLoggedMatch(ctx context.Context, red, black engine.MoveSource, label string, opts engine.MatchRunnerOptions) (engine.MatchReport, error) {
	var ply atomic.Int64
	redSeat := &moveLogger{inner: red, label: label, seat: "红", ply: &ply}
	blackSeat := &moveLogger{inner: black, label: label, seat: "黑", ply: &ply}
	fmt.Fprintf(os.Stderr, "[%s 开局] 红=%s 黑=%s · maxPlies=%d\n", label, red.DisplayName(), black.DisplayName(), opts.MaxPlies)
	report, err := engine.RunMatch(ctx, redSeat, blackSeat, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s 中断] %v\n", label, err)
		return report, err
	}
	fmt.Fprintf(os.Stderr, "[%s 终局] winner=%s endReason=%s plies=%d 红耗时=%dms 黑耗时=%dms 兜底=红%d/黑%d 质量评估=%d手\n",
		label, report.Winner, report.EndReason, report.Plies, report.RedTimeMs, report.BlackTimeMs,
		report.RedFallbacks, report.BlackFallbacks, report.EvaluatedPlies)
	return report, nil
}

// ---------------------------------------------------------------------------
// profile 集（llm_match_runner.dart:41-59 的 1:1 同源移植）
// ---------------------------------------------------------------------------

type profileEntry struct {
	name  string
	build func() engine.MoveSource
}

func buildProfiles(config llm.LlmEndpointConfig, transport llm.Transport, blend int, builtinAi func() engine.MoveSource,
	onAttempt func(profile string, attempt, totalAttempts int)) []profileEntry {
	attemptCb := func(string, int, int) {}
	if onAttempt != nil {
		attemptCb = onAttempt
	}
	return []profileEntry{
		{"baseline-v1", func() engine.MoveSource {
			return llm.NewLlmPlayer(config, transport, llm.LlmPlayerOptions{
				UsePromptV2:     false,
				MaxAttempts:     3,
				Fallback:        llm.FallbackBuiltinAI,
				BuiltinAiSource: builtinAi,
				OnAttempt:       func(attempt, total int) { attemptCb("baseline-v1", attempt, total) },
			}, llm.ChatClientOptions{})
		}},
		{"p0-prompt-v2", func() engine.MoveSource {
			return llm.NewLlmPlayer(config, transport, llm.LlmPlayerOptions{
				UsePromptV2:     true,
				MaxAttempts:     3,
				Fallback:        llm.FallbackBuiltinAI,
				BuiltinAiSource: builtinAi,
				OnAttempt:       func(attempt, total int) { attemptCb("p0-prompt-v2", attempt, total) },
			}, llm.ChatClientOptions{})
		}},
		{"hybrid-candidate", func() engine.MoveSource {
			return llm.NewHybridLlmPlayer(config, transport, cliAdvisor{}, llm.HybridLlmPlayerOptions{
				AdvisorMode:       llm.AdvisorCandidate,
				StrengthBlend:     blend,
				AdvisorDifficulty: 5,
				MaxAttempts:       3,
				Fallback:          llm.FallbackBuiltinAI,
				BuiltinAiSource:   builtinAi,
				OnAttempt:         func(attempt, total int) { attemptCb("hybrid-candidate", attempt, total) },
			}, llm.ChatClientOptions{})
		}},
		{"hybrid-gate", func() engine.MoveSource {
			return llm.NewHybridLlmPlayer(config, transport, cliAdvisor{}, llm.HybridLlmPlayerOptions{
				AdvisorMode:       llm.AdvisorGate,
				StrengthBlend:     blend,
				AdvisorDifficulty: 5,
				MaxAttempts:       3,
				Fallback:          llm.FallbackBuiltinAI,
				BuiltinAiSource:   builtinAi,
				OnAttempt:         func(attempt, total int) { attemptCb("hybrid-gate", attempt, total) },
			}, llm.ChatClientOptions{})
		}},
	}
}

// reportToJson Dart MatchReport.toJson 的等价映射：movesIccs → moves 键。
func reportToJson(r engine.MatchReport) map[string]any {
	raw, err := json.Marshal(r)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return map[string]any{}
	}
	delete(m, "movesIccs")
	m["moves"] = r.MovesIccs
	return m
}

// matchVsEngine profile vs 内置 AI（难度 3）：红黑换边各一局，EvaluateQuality 开
// （Dart _matchVsEngine）。source 单实例复用（eval.ts 同款）；label 为 stderr 进度
// 前缀（DR-011）。
func matchVsEngine(ctx context.Context, source engine.MoveSource, games, maxPlies, qualityDepth int, label string) ([]map[string]any, error) {
	reports := make([]map[string]any, 0, games)
	for i := 0; i < games; i++ {
		// 红黑换边：偶数局 LLM 执红，奇数局执黑。
		llmIsRed := i%2 == 0
		var red, black engine.MoveSource
		if llmIsRed {
			red, black = source, chessAiSource(3)
		} else {
			red, black = chessAiSource(3), source
		}
		report, err := runLoggedMatch(ctx, red, black, fmt.Sprintf("%s·局%d/%d", label, i+1, games), engine.MatchRunnerOptions{
			MaxPlies:        maxPlies,
			EvaluateQuality: true,
			QualityDepth:    qualityDepth,
		})
		if err != nil {
			return reports, err
		}
		entry := reportToJson(report)
		if llmIsRed {
			entry["llmSide"] = "red"
		} else {
			entry["llmSide"] = "black"
		}
		reports = append(reports, entry)
	}
	return reports, nil
}

// evalReport 顶层报告（suite 模式为四档分键，单局模式落 match）。
type evalReport struct {
	Games           int              `json:"games"`
	MaxPlies        int              `json:"maxPlies"`
	Match           []map[string]any `json:"match,omitempty"`
	BaselineV1      []map[string]any `json:"baseline-v1,omitempty"`
	P0PromptV2      []map[string]any `json:"p0-prompt-v2,omitempty"`
	HybridCandidate []map[string]any `json:"hybrid-candidate,omitempty"`
	HybridGate      []map[string]any `json:"hybrid-gate,omitempty"`
}

// ---------------------------------------------------------------------------
// 参数解析与主流程（eval.ts main 逐行对应）
// ---------------------------------------------------------------------------

func intArg(args []string, name string, def int) int {
	for i, arg := range args {
		if arg == name && i+1 < len(args) {
			v, err := strconv.Atoi(args[i+1])
			if err != nil {
				return def
			}
			return v
		}
	}
	return def
}

func stringArg(args []string, name, def string) string {
	for i, arg := range args {
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return def
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name {
			return true
		}
	}
	return false
}

func profileNames(profiles []profileEntry) []string {
	names := make([]string, 0, len(profiles))
	for _, p := range profiles {
		names = append(names, p.name)
	}
	return names
}

func findProfile(profiles []profileEntry, name string) func() engine.MoveSource {
	for _, p := range profiles {
		if p.name == name {
			return p.build
		}
	}
	return nil
}

func run(args []string) int {
	baseURL := os.Getenv("LLM_BASE_URL")
	model := os.Getenv("LLM_MODEL")
	apiKey := os.Getenv("LLM_API_KEY")
	if baseURL == "" || model == "" {
		fmt.Fprintln(os.Stderr, "请设置环境变量 LLM_BASE_URL、LLM_MODEL（可选 LLM_API_KEY）。")
		return 2
	}
	config := llm.LlmEndpointConfig{BaseURL: baseURL, APIKey: apiKey, Model: model}

	suite := hasFlag(args, "--suite")
	games := intArg(args, "--games", 2)
	maxPlies := intArg(args, "--max-plies", 120)
	blend := intArg(args, "--blend", 50)
	llmTimeoutSeconds := intArg(args, "--llm-timeout", 60)
	outPath := stringArg(args, "--out", fmt.Sprintf("tmp/eval-report-%d.json", time.Now().UnixMilli()))
	if abs, err := filepath.Abs(outPath); err == nil {
		outPath = abs
	}

	transport := llm.NewStreamTransport(func() int { return llmTimeoutSeconds })
	profiles := buildProfiles(config, transport, blend, func() engine.MoveSource { return chessAiSource(3) },
		func(profile string, attempt, total int) {
			fmt.Fprintf(os.Stderr, "[%s] 模型第 %d/%d 次尝试\n", profile, attempt, total)
		})

	report := evalReport{Games: games, MaxPlies: maxPlies}
	ctx := context.Background()

	if suite {
		// 四档对比：每档以红/黑两视角各对抗内置 AI（难度 3）一局。
		for k, p := range profiles {
			fmt.Fprintf(os.Stderr, "==== [%d/4] %s ====\n", k+1, p.name)
			reports, err := matchVsEngine(ctx, p.build(), 2, maxPlies, 4, p.name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "对局中断：%v\n", err)
				return 1
			}
			switch p.name {
			case "baseline-v1":
				report.BaselineV1 = reports
			case "p0-prompt-v2":
				report.P0PromptV2 = reports
			case "hybrid-candidate":
				report.HybridCandidate = reports
			case "hybrid-gate":
				report.HybridGate = reports
			}
		}
	} else {
		profileName := stringArg(args, "--profile", "hybrid-candidate")
		build := findProfile(profiles, profileName)
		if build == nil {
			fmt.Fprintf(os.Stderr, "未知 profile: %s（可选：%s）\n", profileName, joinNames(profileNames(profiles)))
			return 2
		}
		redName := stringArg(args, "--red", profileName)
		blackName := stringArg(args, "--black", "chessai-3")
		if redName == profileName && blackName == "chessai-3" {
			reports, err := matchVsEngine(ctx, build(), games, maxPlies, 4, profileName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "对局中断：%v\n", err)
				return 1
			}
			report.Match = reports
		} else {
			// 大模型对战：profile vs profile。
			redBuild := findProfile(profiles, redName)
			blackBuild := findProfile(profiles, blackName)
			if redBuild == nil || blackBuild == nil {
				missing := redName
				if blackBuild == nil {
					missing = blackName
				}
				fmt.Fprintf(os.Stderr, "未知 profile: %s（可选：%s、chessai-3）\n", missing, joinNames(profileNames(profiles)))
				return 2
			}
			reports := make([]map[string]any, 0, games)
			for i := 0; i < games; i++ {
				redSeat, blackSeat := redBuild(), blackBuild()
				if i%2 != 0 {
					redSeat, blackSeat = blackBuild(), redBuild()
				}
				rep, err := runLoggedMatch(ctx, redSeat, blackSeat, fmt.Sprintf("%s vs %s·局%d/%d", redName, blackName, i+1, games),
					engine.MatchRunnerOptions{MaxPlies: maxPlies})
				if err != nil {
					fmt.Fprintf(os.Stderr, "对局中断：%v\n", err)
					return 1
				}
				reports = append(reports, reportToJson(rep))
			}
			report.Match = reports
		}
	}

	text, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "报告序列化失败：%v\n", err)
		return 1
	}
	fmt.Println(string(text))
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "报告目录创建失败：%v\n", err)
		return 1
	}
	if err := os.WriteFile(outPath, append(text, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "报告写盘失败：%v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "报告已落盘：%s\n", outPath)
	return 0
}

func joinNames(names []string) string { return strings.Join(names, ", ") }
