package rules

// 中文记法快照测试（09 §2.1 之 3：炮二平五/马8进7/兵五进一 等逐字快照；
// Electron 版 test/rules/moveNotation.spec.ts 的 Go 对应）。
// 覆盖 02 §4 三分支 × 红黑双方：平移 / 进退直线 / 斜走。
//
// 红方列号 = HAN[col]（col 0→九 … col 8→一）；黑方列号 = col+1。
// 经典对照：马二进三（红马 col7→col6）、相三进五（红相 col6→col4）、
// 仕四进五（红仕 col5→col4）。
import "testing"

// notation [FEN字符, from, to] → ChineseNotation 输出。
func notation(t *testing.T, fenChar byte, fc, fr, tc, tr int) string {
	t.Helper()
	piece := PieceFromFenChar(fenChar)
	if piece == nil {
		t.Fatalf("非法棋子字符: %c", fenChar)
	}
	return ChineseNotation(piece, Pos(fc, fr), Pos(tc, tr))
}

func TestNotationRedSideways(t *testing.T) {
	// 红方平移：炮二平五 / 炮八平五 / 兵九平八。
	cases := []struct {
		ch             byte
		fc, fr, tc, tr int
		want           string
	}{
		{'C', 7, 7, 4, 7, "炮二平五"},
		{'C', 1, 7, 4, 7, "炮八平五"},
		{'P', 0, 4, 1, 4, "兵九平八"},
	}
	for _, c := range cases {
		if got := notation(t, c.ch, c.fc, c.fr, c.tc, c.tr); got != c.want {
			t.Errorf("notation(%c) = %q, 期望 %q", c.ch, got, c.want)
		}
	}
}

func TestNotationBlackSideways(t *testing.T) {
	// 黑方平移：将5平4。
	if got := notation(t, 'k', 4, 0, 3, 0); got != "将5平4" {
		t.Errorf("notation = %q, 期望 将5平4", got)
	}
}

func TestNotationRedForwardLine(t *testing.T) {
	// 红方直线进：兵五进一 / 车一进一 / 车九进一 / 炮二进四 / 帅五进一。
	cases := []struct {
		ch             byte
		fc, fr, tc, tr int
		want           string
	}{
		{'P', 4, 6, 4, 5, "兵五进一"},
		{'R', 8, 9, 8, 8, "车一进一"},
		{'R', 0, 9, 0, 8, "车九进一"},
		{'C', 7, 7, 7, 3, "炮二进四"},
		{'K', 4, 9, 4, 8, "帅五进一"},
	}
	for _, c := range cases {
		if got := notation(t, c.ch, c.fc, c.fr, c.tc, c.tr); got != c.want {
			t.Errorf("notation(%c) = %q, 期望 %q", c.ch, got, c.want)
		}
	}
}

func TestNotationRedBackwardLine(t *testing.T) {
	// 红方直线退：车一退二 / 车九退二。
	if got := notation(t, 'R', 8, 7, 8, 9); got != "车一退二" {
		t.Errorf("notation = %q, 期望 车一退二", got)
	}
	if got := notation(t, 'R', 0, 7, 0, 9); got != "车九退二" {
		t.Errorf("notation = %q, 期望 车九退二", got)
	}
}

func TestNotationBlackLine(t *testing.T) {
	// 黑方直线进退：卒1进1 / 车1退1。
	if got := notation(t, 'p', 0, 3, 0, 4); got != "卒1进1" {
		t.Errorf("notation = %q, 期望 卒1进1", got)
	}
	if got := notation(t, 'r', 0, 2, 0, 1); got != "车1退1" {
		t.Errorf("notation = %q, 期望 车1退1", got)
	}
}

func TestNotationRedDiagonal(t *testing.T) {
	// 红方斜走：马二进三 / 仕四进五 / 仕六进五 / 相三进五 / 相七进九。
	cases := []struct {
		ch             byte
		fc, fr, tc, tr int
		want           string
	}{
		{'N', 7, 9, 6, 7, "马二进三"},
		{'A', 5, 9, 4, 8, "仕四进五"},
		{'A', 3, 9, 4, 8, "仕六进五"},
		{'B', 6, 9, 4, 7, "相三进五"},
		{'B', 2, 9, 0, 7, "相七进九"},
	}
	for _, c := range cases {
		if got := notation(t, c.ch, c.fc, c.fr, c.tc, c.tr); got != c.want {
			t.Errorf("notation(%c) = %q, 期望 %q", c.ch, got, c.want)
		}
	}
}

func TestNotationBlackDiagonal(t *testing.T) {
	// 黑方斜走：马8进7 / 马8退7 / 士4进5 / 士6进5。
	// 黑马在己方底线 row 0 前进（row 增大）为进。
	if got := notation(t, 'n', 7, 0, 6, 2); got != "马8进7" {
		t.Errorf("notation = %q, 期望 马8进7", got)
	}
	// 黑马在 row 9 向 row 减小方向移动为退。
	if got := notation(t, 'n', 7, 9, 6, 7); got != "马8退7" {
		t.Errorf("notation = %q, 期望 马8退7", got)
	}
	if got := notation(t, 'a', 3, 0, 4, 1); got != "士4进5" {
		t.Errorf("notation = %q, 期望 士4进5", got)
	}
	if got := notation(t, 'a', 5, 0, 4, 1); got != "士6进5" {
		t.Errorf("notation = %q, 期望 士6进5", got)
	}
}
