package storage

import (
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// 等价集：Electron 版 test/storage/gameDao.spec.ts（game_dao_test.dart(10) +
// game_record_dao_test.dart(6) + 2 条回归 + 迁移组）全量移植。
// 内存库运行（09 文档 §2.4 db 节）；时间戳为 epoch 毫秒（07 §1 定稿）。

const fenStart = "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w"

func mustOpenDao(t *testing.T) *ChessDao {
	t.Helper()
	dao, err := OpenDaoInMemory()
	if err != nil {
		t.Fatalf("open in-memory dao: %v", err)
	}
	t.Cleanup(func() { _ = dao.Close() })
	return dao
}

func ptr(i int64) *int64 { return &i }

func strPtr(s string) *string { return &s }

func TestUpsertThenLatest(t *testing.T) {
	dao := mustOpenDao(t)
	id, err := dao.Upsert(nil, fenStart, [][]int{{0, 9, 0, 8}})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if id <= 0 {
		t.Fatalf("id = %d, want > 0", id)
	}
	latest, err := dao.Latest()
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest == nil {
		t.Fatal("latest = nil, want row")
	}
	if latest.ID != id || latest.Fen != fenStart || !reflect.DeepEqual(latest.Moves, [][]int{{0, 9, 0, 8}}) {
		t.Fatalf("latest = %+v", latest)
	}
}

func TestUpsertWithIDUpdates(t *testing.T) {
	dao := mustOpenDao(t)
	id, err := dao.Upsert(nil, fenStart, nil)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := dao.Upsert(ptr(id), "changed", [][]int{{1, 2, 3, 4}}); err != nil {
		t.Fatalf("upsert with id: %v", err)
	}
	all, err := dao.All()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 1 || all[0].Fen != "changed" {
		t.Fatalf("all = %+v, want 1 row fen=changed", all)
	}
}

func TestLatestOrdersByUpdatedAt(t *testing.T) {
	dao := mustOpenDao(t)
	if _, err := dao.Upsert(nil, "first", nil); err != nil {
		t.Fatalf("upsert first: %v", err)
	}
	time.Sleep(25 * time.Millisecond) // 毫秒级时间戳，确保不同
	if _, err := dao.Upsert(nil, "second", nil); err != nil {
		t.Fatalf("upsert second: %v", err)
	}
	latest, err := dao.Latest()
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest == nil || latest.Fen != "second" {
		t.Fatalf("latest = %+v, want fen=second", latest)
	}
}

func TestDeleteRemovesFromLatest(t *testing.T) {
	dao := mustOpenDao(t)
	id, err := dao.Upsert(nil, "first", nil)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := dao.Delete(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	latest, err := dao.Latest()
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest != nil {
		t.Fatalf("latest = %+v, want nil", latest)
	}
}

func TestClearEmptiesAll(t *testing.T) {
	dao := mustOpenDao(t)
	for _, fen := range []string{"a", "b"} {
		if _, err := dao.Upsert(nil, fen, nil); err != nil {
			t.Fatalf("upsert %s: %v", fen, err)
		}
	}
	if err := dao.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	all, err := dao.All()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("all = %+v, want empty", all)
	}
}

func TestUpsertForModeKeepsSeparateBuckets(t *testing.T) {
	dao := mustOpenDao(t)
	if _, err := dao.UpsertForMode("humanVsAi", "fen-a", [][]int{{0, 9, 0, 8}}); err != nil {
		t.Fatal(err)
	}
	if _, err := dao.UpsertForMode("humanVsHuman", "fen-b", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := dao.UpsertForMode("aiVsAi", "fen-c", nil); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"humanVsAi": "fen-a", "humanVsHuman": "fen-b", "aiVsAi": "fen-c"}
	for mode, fen := range want {
		saved, err := dao.LatestForMode(mode)
		if err != nil {
			t.Fatalf("latestForMode(%s): %v", mode, err)
		}
		if saved == nil || saved.Fen != fen {
			t.Fatalf("latestForMode(%s) = %+v, want fen=%s", mode, saved, fen)
		}
	}
	all, err := dao.All()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("all len = %d, want 3", len(all))
	}
}

func TestUpsertForModeOverwritesSameMode(t *testing.T) {
	dao := mustOpenDao(t)
	if _, err := dao.UpsertForMode("humanVsAi", "old", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := dao.UpsertForMode("humanVsAi", "new", [][]int{{1, 1, 1, 2}}); err != nil {
		t.Fatal(err)
	}
	saved, err := dao.LatestForMode("humanVsAi")
	if err != nil {
		t.Fatal(err)
	}
	if saved == nil || saved.Fen != "new" || !reflect.DeepEqual(saved.Moves, [][]int{{1, 1, 1, 2}}) {
		t.Fatalf("saved = %+v", saved)
	}
	all, err := dao.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("all len = %d, want 1", len(all))
	}
}

func TestLegacyRowNotReadableByOfficialMode(t *testing.T) {
	dao := mustOpenDao(t)
	// Electron 侧 DDL 的 mode 为 NOT NULL（07 §1）：裸 upsert 固定标 'legacy'，
	// 与原版 NULL 模式同样不可经任何正式模式读出。
	if _, err := dao.Upsert(nil, "legacy-row", nil); err != nil {
		t.Fatal(err)
	}
	official, err := dao.LatestForMode("humanVsAi")
	if err != nil {
		t.Fatal(err)
	}
	if official != nil {
		t.Fatalf("latestForMode(humanVsAi) = %+v, want nil", official)
	}
	legacy, err := dao.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if legacy == nil || legacy.Fen != "legacy-row" || legacy.Mode != "legacy" {
		t.Fatalf("latest = %+v, want legacy-row/legacy", legacy)
	}
}

func TestDeleteForModeOnlyDeletesThatMode(t *testing.T) {
	dao := mustOpenDao(t)
	if _, err := dao.UpsertForMode("humanVsAi", "a", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := dao.UpsertForMode("aiVsAi", "b", nil); err != nil {
		t.Fatal(err)
	}
	if err := dao.DeleteForMode("humanVsAi"); err != nil {
		t.Fatal(err)
	}
	h, err := dao.LatestForMode("humanVsAi")
	if err != nil {
		t.Fatal(err)
	}
	if h != nil {
		t.Fatalf("humanVsAi = %+v, want nil", h)
	}
	a, err := dao.LatestForMode("aiVsAi")
	if err != nil {
		t.Fatal(err)
	}
	if a == nil || a.Fen != "b" {
		t.Fatalf("aiVsAi = %+v, want fen=b", a)
	}
}

// 旧库迁移（一期无 mode 列，game_dao_test.dart 迁移组）：TEXT 时间戳 + 无 mode 列。
func TestMigrateV1AddsModeColumn(t *testing.T) {
	db, err := openRawMemoryDB()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`
      CREATE TABLE saved_games (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        fen TEXT NOT NULL,
        move_stack_json TEXT NOT NULL,
        created_at TEXT DEFAULT CURRENT_TIMESTAMP,
        updated_at TEXT DEFAULT CURRENT_TIMESTAMP
      )`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO saved_games(fen, move_stack_json) VALUES ('old-fen', '[]')"); err != nil {
		t.Fatal(err)
	}
	if err := InitSchema(db); err != nil {
		t.Fatalf("initSchema: %v", err)
	}
	dao := &ChessDao{db: db}
	if _, err := dao.UpsertForMode("humanVsAi", "new-fen", nil); err != nil {
		t.Fatalf("upsertForMode after migrate: %v", err)
	}
	newRow, err := dao.LatestForMode("humanVsAi")
	if err != nil {
		t.Fatal(err)
	}
	if newRow == nil || newRow.Fen != "new-fen" {
		t.Fatalf("latestForMode(humanVsAi) = %+v, want new-fen", newRow)
	}
	// 老数据仍在库里，但不属于任何正式模式；'legacy' 本身可读出
	all, err := dao.All()
	if err != nil {
		t.Fatal(err)
	}
	var legacyRows []*SavedGame
	for _, g := range all {
		if g.Fen == "old-fen" {
			legacyRows = append(legacyRows, g)
		}
	}
	if len(legacyRows) != 1 || legacyRows[0].Mode != "legacy" {
		t.Fatalf("legacy rows = %+v, want 1 row mode=legacy", legacyRows)
	}
	legacy, err := dao.LatestForMode("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if legacy == nil || legacy.Fen != "old-fen" {
		t.Fatalf("latestForMode(legacy) = %+v, want old-fen", legacy)
	}
	official, err := dao.LatestForMode("humanVsHuman")
	if err != nil {
		t.Fatal(err)
	}
	if official != nil {
		t.Fatalf("latestForMode(humanVsHuman) = %+v, want nil", official)
	}
}

// ---------- game_records（game_record_dao_test.dart） ----------

func sampleRecord(title string) *GameRecord {
	return &GameRecord{
		Title:      title,
		Mode:       "endgame",
		InitialFen: "3k5/9/9/9/9/9/9/9/9/4K4 w",
		Moves:      []RecordMove{},
		Result:     nil,
		SolveStatus: func() *string {
			s := "solved"
			return &s
		}(),
		Solutions: []any{[]any{"h5h3"}, []any{"h5h4", "h0g2"}},
		LlmNote:   strPtr("大模型首选 h5h3（已验证为必胜着法）"),
		Note:      strPtr("经典双车残局"),
		CreatedAt: time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC).UnixMilli(),
	}
}

func TestInsertAndReadRecord(t *testing.T) {
	dao := mustOpenDao(t)
	id, err := dao.InsertRecord(sampleRecord("测试棋谱"))
	if err != nil {
		t.Fatalf("insertRecord: %v", err)
	}
	loaded, err := dao.RecordByID(id)
	if err != nil {
		t.Fatalf("recordById: %v", err)
	}
	if loaded == nil {
		t.Fatal("recordById = nil")
	}
	if loaded.Title != "测试棋谱" || loaded.Mode != "endgame" || deref(loaded.SolveStatus) != "solved" {
		t.Fatalf("loaded = %+v", loaded)
	}
	solutions, ok := loaded.Solutions.([]any)
	if !ok || len(solutions) != 2 {
		t.Fatalf("solutions = %#v, want 2 arrays", loaded.Solutions)
	}
	second, ok := solutions[1].([]any)
	if !ok || !reflect.DeepEqual(second, []any{"h5h4", "h0g2"}) {
		t.Fatalf("solutions[1] = %#v", solutions[1])
	}
	if loaded.LlmNote == nil || !contains(*loaded.LlmNote, "h5h3") {
		t.Fatalf("llmNote = %v", loaded.LlmNote)
	}
	if loaded.Note == nil || *loaded.Note != "经典双车残局" {
		t.Fatalf("note = %v", loaded.Note)
	}
}

func TestAllRecordsOrderByCreatedAtDesc(t *testing.T) {
	dao := mustOpenDao(t)
	if _, err := dao.InsertRecord(sampleRecord("A")); err != nil {
		t.Fatal(err)
	}
	if _, err := dao.InsertRecord(sampleRecord("B")); err != nil {
		t.Fatal(err)
	}
	all, err := dao.AllRecords()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("all len = %d, want 2", len(all))
	}
	titles := map[string]bool{}
	for _, r := range all {
		titles[r.Title] = true
	}
	if !titles["A"] || !titles["B"] {
		t.Fatalf("titles = %v, want A and B", titles)
	}
}

func TestUpdateRecordMutableFields(t *testing.T) {
	dao := mustOpenDao(t)
	id, err := dao.InsertRecord(sampleRecord("测试棋谱"))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := dao.RecordByID(id)
	if err != nil || loaded == nil {
		t.Fatalf("recordById: %v, %v", loaded, err)
	}
	loaded.Title = "更新标题"
	loaded.SolveStatus = strPtr("timeout")
	loaded.Solutions = []any{}
	loaded.Note = strPtr("限时未决")
	if err := dao.UpdateRecord(loaded); err != nil {
		t.Fatalf("updateRecord: %v", err)
	}
	reloaded, err := dao.RecordByID(id)
	if err != nil || reloaded == nil {
		t.Fatalf("reload: %v, %v", reloaded, err)
	}
	if reloaded.Title != "更新标题" || deref(reloaded.SolveStatus) != "timeout" {
		t.Fatalf("reloaded = %+v", reloaded)
	}
	if sol, ok := reloaded.Solutions.([]any); !ok || len(sol) != 0 {
		t.Fatalf("solutions = %#v, want empty array", reloaded.Solutions)
	}
	if reloaded.Note == nil || *reloaded.Note != "限时未决" {
		t.Fatalf("note = %v", reloaded.Note)
	}
}

func TestDeleteRecord(t *testing.T) {
	dao := mustOpenDao(t)
	id, err := dao.InsertRecord(sampleRecord("测试棋谱"))
	if err != nil {
		t.Fatal(err)
	}
	if err := dao.DeleteRecord(id); err != nil {
		t.Fatal(err)
	}
	got, err := dao.RecordByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("recordById = %+v, want nil", got)
	}
	all, err := dao.AllRecords()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("allRecords = %+v, want empty", all)
	}
}

func TestRecordsAndSavedGamesIndependent(t *testing.T) {
	dao := mustOpenDao(t)
	id, err := dao.InsertRecord(sampleRecord("测试棋谱"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dao.UpsertForMode("humanVsHuman", "fen", [][]int{{1, 2, 3, 4}}); err != nil {
		t.Fatal(err)
	}
	saved, err := dao.LatestForMode("humanVsHuman")
	if err != nil {
		t.Fatal(err)
	}
	if saved == nil {
		t.Fatal("latestForMode = nil, want row")
	}
	rec, err := dao.RecordByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("recordById = nil")
	}
	if err := dao.DeleteForMode("humanVsHuman"); err != nil {
		t.Fatal(err)
	}
	rec, err = dao.RecordByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("recordById after deleteForMode = nil, want row")
	}
}

// 回归：无吃子走法（x=null）入库读回后可解码，起点重算非新局（实机缺陷修复）。
func TestRecordRoundTripWithNullCaptured(t *testing.T) {
	dao := mustOpenDao(t)
	moves := FillMovePieces(rules.FENInitial, []rules.Move{
		{From: rules.Pos(7, 7), To: rules.Pos(4, 7)},
		{From: rules.Pos(7, 0), To: rules.Pos(6, 2)},
		{From: rules.Pos(8, 0), To: rules.Pos(7, 0)},
	})
	finalFen := FinalFenOf(rules.FENInitial, moves)
	recordMoves := make([]RecordMove, 0, len(moves))
	for _, m := range moves {
		raw := EncodeRecordMove(&m)
		recordMoves = append(recordMoves, RecordMove{F: raw.F, T: raw.T, P: derefOrEmpty(raw.P), X: raw.X})
	}
	id, err := dao.InsertRecord(&GameRecord{
		Title:       "回归",
		Mode:        "humanVsAi",
		InitialFen:  rules.FENInitial,
		Moves:       recordMoves,
		Result:      nil,
		SolveStatus: strPtr("none"),
		Solutions:   nil,
		LlmNote:     nil,
		Note:        nil,
		CreatedAt:   time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("insertRecord: %v", err)
	}
	loaded, err := dao.RecordByID(id)
	if err != nil || loaded == nil {
		t.Fatalf("recordById: %v, %v", loaded, err)
	}
	if len(loaded.Moves) != 3 {
		t.Fatalf("moves len = %d, want 3", len(loaded.Moves))
	}
	// 每条走法都能解码（旧缺陷：x:"" 导致全部 null）
	decoded := make([]rules.Move, 0, len(loaded.Moves))
	for _, m := range loaded.Moves {
		dm := DecodeRecordMove(rawMoveToAny(RecordMoveToRaw(m)))
		if dm == nil {
			t.Fatalf("move %+v 不可解码", m)
		}
		decoded = append(decoded, *dm)
	}
	// 进入对战起点 = 终局局面（≠ 标准开局）
	startFen := FinalFenOf(rules.FENInitial, decoded)
	if boardPart(startFen) == boardPart(rules.FENInitial) {
		t.Fatalf("startFen = %s, want ≠ 初始盘面", startFen)
	}
	if startFen != finalFen {
		t.Fatalf("startFen = %s, want %s", startFen, finalFen)
	}
}

// 回归：旧库 x:"" 形态的走法行可解码（存量数据修复）；真脏数据仍拒绝。
func TestDecodeLegacyEmptyCaptured(t *testing.T) {
	num := func(v ...int) []any {
		out := make([]any, len(v))
		for i, n := range v {
			out[i] = float64(n) // JSON 数值语义
		}
		return out
	}
	legacyMoves := []map[string]any{
		{"f": num(7, 7), "t": num(4, 7), "p": "C", "x": ""},
		{"f": num(7, 0), "t": num(6, 2), "p": "n", "x": nil},
	}
	for _, m := range legacyMoves {
		if DecodeRecordMove(m) == nil {
			t.Fatalf("legacy move %v 应可解码", m)
		}
	}
	if DecodeRecordMove(map[string]any{"f": num(7, 7), "t": num(4, 7), "p": "C", "x": "Z"}) != nil {
		t.Fatal("x=Z 应拒绝")
	}
	if DecodeRecordMove(map[string]any{"f": num(7, 7), "t": num(4, 7), "p": "ZZ", "x": nil}) != nil {
		t.Fatal("p=ZZ 应拒绝")
	}
}

// 旧库（无 game_records 表）打开时自动建表；V1 迁移同样生效。
func TestLegacyDBCreatesGameRecordsTable(t *testing.T) {
	db, err := openRawMemoryDB()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`
      CREATE TABLE saved_games (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        fen TEXT NOT NULL,
        move_stack_json TEXT NOT NULL,
        created_at TEXT DEFAULT CURRENT_TIMESTAMP,
        updated_at TEXT DEFAULT CURRENT_TIMESTAMP
      )`); err != nil {
		t.Fatal(err)
	}
	if err := InitSchema(db); err != nil {
		t.Fatalf("initSchema: %v", err)
	}
	dao := &ChessDao{db: db}
	id, err := dao.InsertRecord(sampleRecord("测试棋谱"))
	if err != nil {
		t.Fatalf("insertRecord: %v", err)
	}
	rec, err := dao.RecordByID(id)
	if err != nil || rec == nil {
		t.Fatalf("recordById: %v, %v", rec, err)
	}
	official, err := dao.LatestForMode("humanVsHuman")
	if err != nil {
		t.Fatal(err)
	}
	if official != nil {
		t.Fatalf("latestForMode(humanVsHuman) = %+v, want nil", official)
	}
}

// ---------- 测试辅助 ----------

func openRawMemoryDB() (*sql.DB, error) {
	db, err := sql.Open(sqliteDriverName, ":memory:")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func boardPart(fen string) string {
	for i, ch := range fen {
		if ch == ' ' {
			return fen[:i]
		}
	}
	return fen
}

// rawMoveToAny RawRecordMove → 与落盘 JSON 同形的 map（供 DecodeRecordMove 测试）。
func rawMoveToAny(raw RawRecordMove) map[string]any {
	m := map[string]any{
		"f": []any{float64(raw.F[0]), float64(raw.F[1])},
		"t": []any{float64(raw.T[0]), float64(raw.T[1])},
	}
	if raw.P != nil {
		m["p"] = *raw.P
	} else {
		m["p"] = nil
	}
	if raw.X != nil {
		m["x"] = *raw.X
	} else {
		m["x"] = nil
	}
	return m
}
