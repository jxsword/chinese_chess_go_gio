// POC-4 数据合成器单元测试：规模、确定性、字段形状。
package ui

import "testing"

// spec: 08 §8——合成 14 万局数据集（POC-4 验收口径）。
func TestPocSynthGames(t *testing.T) {
	rows := pocSynthGames()
	if len(rows) != pocGameCount {
		t.Fatalf("rows=%d want %d", len(rows), pocGameCount)
	}
	// 确定性：同种子两次生成逐行一致
	again := pocSynthGames()
	for i := range rows {
		if rows[i] != again[i] {
			t.Fatalf("第 %d 行两次生成不一致", i)
		}
	}
	// 字段形状：标题/日期/步数/分类非空，步数在合理区间
	for _, i := range []int{0, 1, pocGameCount / 2, pocGameCount - 1} {
		r := rows[i]
		if r.title == "" || r.date == "" || r.category == "" {
			t.Fatalf("第 %d 行字段为空: %+v", i, r)
		}
		if r.moves < 20 || r.moves >= 300 {
			t.Fatalf("第 %d 行步数越界: %d", i, r.moves)
		}
	}
}
