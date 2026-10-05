package engine

// ChessAi 三接口金标准对拍（09 §2.2，Electron 版 chessAi.spec.ts 的 Go 对应）。
//
// 金标准来源：Flutter 版 ChessAi（ai_engine.dart）在固定 FEN 上
// findBestMoveEx 逐层独立搜索的输出（testdata/golden/engine.json，dart run 提取，
// 与 TS 版共用）。根节点全窗口下每个着法的分数是精确 minimax 值（与走法生成
// 顺序无关），因此跨语言逐位可复现；同分着法间的排序不参与对拍（Dart sort 不稳定）。
//
// 铁律（DR-006）：全部对拍期望在不传 HistoryFens 前提下零改动成立。
// 慢速对拍集（initial/midgame 深层完整搜索耗时较长）由环境变量 RUN_SLOW=1 启用。
import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

type goldenMoveJSON struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Captured string `json:"captured,omitempty"`
}

type goldenTopEntry struct {
	Move goldenMoveJSON `json:"move"`
	Cp   int            `json:"cp"`
}

type goldenExReport struct {
	Best   *goldenMoveJSON  `json:"best"`
	BestCp *int             `json:"bestCp"`
	TopK   []goldenTopEntry `json:"topK"`
}

type goldenEvalEntry struct {
	Move  goldenMoveJSON `json:"move"`
	Depth int            `json:"depth"`
	Cp    *int           `json:"cp"`
}

type goldenEngineCase struct {
	Name     string                    `json:"name"`
	Fen      string                    `json:"fen"`
	Ex       map[string]goldenExReport `json:"ex"`
	Evaluate []goldenEvalEntry         `json:"evaluate,omitempty"`
}

type goldenEngineFile struct {
	Source     string             `json:"source"`
	Note       string             `json:"note"`
	SlowDepths map[string][]int   `json:"slowDepths"`
	Cases      []goldenEngineCase `json:"cases"`
}

func loadEngineGolden(t *testing.T) goldenEngineFile {
	t.Helper()
	data, err := os.ReadFile("../../testdata/golden/engine.json")
	if err != nil {
		t.Fatalf("读取金标准 engine.json 失败: %v", err)
	}
	var g goldenEngineFile
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatalf("解析金标准 engine.json 失败: %v", err)
	}
	return g
}

// parseGoldenPos 期望坐标 "[col,row]" → Position。
func parseGoldenPos(s string) rules.Position {
	s = strings.Trim(s, "[]")
	parts := strings.Split(s, ",")
	col, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	row, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	return rules.Pos(col, row)
}

func goldenMoveKey(m goldenMoveJSON) string {
	from := parseGoldenPos(m.From)
	to := parseGoldenPos(m.To)
	return fmt.Sprintf("%d,%d->%d,%d", from.Col, from.Row, to.Col, to.Row)
}

func moveKey(m rules.Move) string {
	return fmt.Sprintf("%d,%d->%d,%d", m.From.Col, m.From.Row, m.To.Col, m.To.Row)
}

func runSlow(t *testing.T) bool {
	t.Helper()
	if os.Getenv("RUN_SLOW") == "1" {
		return true
	}
	t.Skip("慢速对拍集：需 RUN_SLOW=1（09 §2.2）")
	return false
}

// findBestMoveEx 金标准对拍：bestCp 与分数序列逐位一致、逐着法分数一致
// （同分着法间的顺序不作断言，Dart sort 不稳定）。
func TestGoldenFindBestMoveEx(t *testing.T) {
	g := loadEngineGolden(t)
	for _, c := range g.Cases {
		for _, depthStr := range sortedDepthKeys(c.Ex) {
			report := c.Ex[depthStr]
			depth, _ := strconv.Atoi(depthStr)
			isSlow := containsInt(g.SlowDepths[c.Name], depth)
			name := fmt.Sprintf("%s/深度%d", c.Name, depth)
			if isSlow {
				runSlow(t)
			}
			t.Run(name, func(t *testing.T) {
				actual, err := FindBestMoveEx(c.Fen, FindBestMoveExOptions{Depth: depth, TopK: 16, TimeLimitMs: 60000})
				if err != nil {
					t.Fatalf("FindBestMoveEx 报错: %v", err)
				}
				if report.TopK == nil {
					// 被将死/困毙：无合法走法返回 null。
					if actual != nil {
						t.Fatalf("期望 null（被将死/困毙），got bestCp=%d", actual.BestCp)
					}
					return
				}
				if actual == nil {
					t.Fatal("期望报告非空")
				}
				if actual.BestCp != *report.BestCp {
					t.Errorf("bestCp = %d, want %d", actual.BestCp, *report.BestCp)
				}
				if len(actual.TopK) == 0 || actual.TopK[0].Cp != actual.BestCp {
					t.Errorf("topK[0].cp 应等于 bestCp")
				}
				if len(actual.TopK) < len(report.TopK) {
					t.Fatalf("topK 数量 %d < 期望 %d", len(actual.TopK), len(report.TopK))
				}
				// 分数序列逐位一致（降序）。
				for i, want := range report.TopK {
					if actual.TopK[i].Cp != want.Cp {
						t.Errorf("topK[%d].cp = %d, want %d", i, actual.TopK[i].Cp, want.Cp)
					}
				}
				// 逐着法分数一致（同分着法间的顺序不作断言）。
				goldenByMove := make(map[string]int, len(report.TopK))
				for _, e := range report.TopK {
					goldenByMove[goldenMoveKey(e.Move)] = e.Cp
				}
				for _, e := range actual.TopK {
					if want, ok := goldenByMove[moveKey(e.Move)]; ok && e.Cp != want {
						t.Errorf("着法 %s cp = %d, want %d", moveKey(e.Move), e.Cp, want)
					}
				}
			})
		}
	}
}

// evaluateMove 金标准对拍。
func TestGoldenEvaluateMove(t *testing.T) {
	g := loadEngineGolden(t)
	for _, c := range g.Cases {
		for _, e := range c.Evaluate {
			name := fmt.Sprintf("%s/%s->%s/深度%d", c.Name, e.Move.From, e.Move.To, e.Depth)
			t.Run(name, func(t *testing.T) {
				move := rules.Move{From: parseGoldenPos(e.Move.From), To: parseGoldenPos(e.Move.To)}
				cp, err := EvaluateMove(c.Fen, move, EvaluateMoveOptions{Depth: e.Depth})
				if err != nil {
					t.Fatalf("EvaluateMove 报错: %v", err)
				}
				if e.Cp == nil {
					if cp != nil {
						t.Errorf("期望 null，got %d", *cp)
					}
					return
				}
				if cp == nil {
					t.Errorf("期望 %d，got null", *e.Cp)
					return
				}
				if *cp != *e.Cp {
					t.Errorf("cp = %d, want %d", *cp, *e.Cp)
				}
			})
		}
	}
}

// sortedDepthKeys 深度键按数值升序（map 迭代无序，保证输出稳定）。
func sortedDepthKeys(m map[string]goldenExReport) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			a, _ := strconv.Atoi(keys[i])
			b, _ := strconv.Atoi(keys[j])
			if b < a {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
