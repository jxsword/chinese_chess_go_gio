// Package storage 持久化层（07 文档）：SQLite DAO（§1）、设置存储（§5）、凭据存储（§4）。
//
// 纯 Go 包（铁律 #1）：仅依赖 stdlib 与白名单驱动 modernc.org/sqlite、zalando/go-keyring，
// 可被 go test、cmd/eval、Wails 后端三端直接调用。
//
// Schema 与 Electron 版 src/main/services/db.ts 逐字段一致（07 §1.1）；两套存档语义
// （saved_games 自动存档 vs game_records 棋谱库）与存档 JSON Schema 不变。
package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	_ "modernc.org/sqlite"
)

// sqliteDriverName database/sql 驱动名（modernc.org/sqlite 纯 Go 实现，DR-002）。
const sqliteDriverName = "sqlite"

// savedGamesDDL / gameRecordsDDL 07 文档 §1.1 两表 Schema（逐字段对照 Electron 版 db.ts）。
const (
	savedGamesDDL = `
  CREATE TABLE IF NOT EXISTS saved_games (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    mode            TEXT NOT NULL,
    fen             TEXT NOT NULL,
    move_stack_json TEXT NOT NULL,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
  )`

	gameRecordsDDL = `
  CREATE TABLE IF NOT EXISTS game_records (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    title          TEXT NOT NULL,
    mode           TEXT NOT NULL,
    initial_fen    TEXT NOT NULL,
    moves_json     TEXT NOT NULL,
    result         TEXT,
    solve_status   TEXT,
    solutions_json TEXT,
    llm_note       TEXT,
    note           TEXT,
    created_at     INTEGER NOT NULL
  )`
)

// migrateV1 V1 迁移（game_dao.dart:123-131）：一期建表无 mode 列 → 补列并标 'legacy'，
// 任何模式读不到（与 Electron 版 db.ts migrateV1 一致）。
func migrateV1(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(saved_games)")
	if err != nil {
		return err
	}
	defer rows.Close()
	hasMode := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull, pk int
		var dfltValue any
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dfltValue, &pk); err != nil {
			return err
		}
		if name == "mode" {
			hasMode = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasMode {
		_, err := db.Exec("ALTER TABLE saved_games ADD COLUMN mode TEXT NOT NULL DEFAULT 'legacy'")
		return err
	}
	return nil
}

// InitSchema 建表 + 迁移（供测试注入内存库；game_dao.dart:86-100）。
func InitSchema(db *sql.DB) error {
	if _, err := db.Exec(savedGamesDDL); err != nil {
		return err
	}
	if err := migrateV1(db); err != nil {
		return err
	}
	_, err := db.Exec(gameRecordsDDL)
	return err
}

// toEpochMs 旧库时间戳兼容：epoch 毫秒数值或 ISO 文本均可（db.ts toEpochMs）。
func toEpochMs(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		if !math.IsInf(t, 0) && !math.IsNaN(t) {
			return int64(t)
		}
	case string:
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
			if parsed, err := time.Parse(layout, t); err == nil {
				return parsed.UnixMilli()
			}
		}
	}
	return 0
}

// parseMovesJson move_stack_json 解析：数组 of 长度 4 的数字数组；脏数据按空历史处理
// （恢复链路还有第二道跳脏闸——GameVm.restore）。与 db.ts parseMovesJson 的唯一偏差：
// 非整数值（如 1.5）在此丢弃而非保留——恢复端 restore 对非整数本就跳过，净效果一致。
func parseMovesJson(jsonText string) [][]int {
	moves := [][]int{}
	var parsed any
	if err := json.Unmarshal([]byte(jsonText), &parsed); err != nil {
		return moves
	}
	arr, ok := parsed.([]any)
	if !ok {
		return moves
	}
	for _, item := range arr {
		row, ok := item.([]any)
		if !ok || len(row) != 4 {
			continue
		}
		quad := make([]int, 4)
		valid := true
		for i, n := range row {
			f, ok := n.(float64)
			if !ok || f != math.Trunc(f) || math.IsInf(f, 0) {
				valid = false
				break
			}
			quad[i] = int(f)
		}
		if valid {
			moves = append(moves, quad)
		}
	}
	return moves
}

// resultValues / solveStatusValues 合法枚举（db.ts RESULT_VALUES / parseSolveStatus）。
var (
	resultValues      = []string{"redWins", "blackWins", "draw"}
	solveStatusValues = []string{"none", "solved", "noSolution", "timeout"}
)

// parseResult result 列解析：非字符串 → null；非法字符串 → 'draw'（db.ts parseResult）。
func parseResult(v any) *string {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	for _, valid := range resultValues {
		if s == valid {
			return &s
		}
	}
	fallback := "draw"
	return &fallback
}

// parseSolveStatus solve_status 列解析：非字符串 → null；非法字符串 → 'none'。
func parseSolveStatus(v any) *string {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	for _, valid := range solveStatusValues {
		if s == valid {
			return &s
		}
	}
	fallback := "none"
	return &fallback
}

// SavedGame saved_games 行（驼峰契约面，07 §1.1；shared/ipc/types.SavedGame 同形）。
type SavedGame struct {
	ID        int64   `json:"id"`
	Mode      string  `json:"mode"`
	Fen       string  `json:"fen"`
	Moves     [][]int `json:"moves"`
	CreatedAt int64   `json:"createdAt"`
	UpdatedAt int64   `json:"updatedAt"`
}

// savedGamesRow saved_games 列扫描（created_at/updated_at 扫进 any 以兼容旧库 TEXT 时间戳）。
type savedGamesRow struct {
	id            int64
	mode          sql.NullString
	fen           string
	moveStackJSON string
	createdAt     any
	updatedAt     any
}

func savedGameFromRow(row savedGamesRow) *SavedGame {
	// mode ?? 'legacy'：仅 NULL 回退（TS ?? 不处理空串，保持一致）
	mode := "legacy"
	if row.mode.Valid {
		mode = row.mode.String
	}
	return &SavedGame{
		ID:        row.id,
		Mode:      mode,
		Fen:       row.fen,
		Moves:     parseMovesJson(row.moveStackJSON),
		CreatedAt: toEpochMs(row.createdAt),
		UpdatedAt: toEpochMs(row.updatedAt),
	}
}

// RecordMove 棋谱契约面走法条目（moves_json 契约形状，07 §1.3）：
// p 恒非空字符串（渲染层组装时恒有棋子）；x nil 表示无吃子。
type RecordMove struct {
	F [2]int  `json:"f"`
	T [2]int  `json:"t"`
	P string  `json:"p"`
	X *string `json:"x"`
}

// GameRecord game_records 行（07 §1.1 表结构驼峰化）。
// Solutions 为 JSON 值透传（nil ↔ SQL NULL；数组形状在读取侧校验，与 db.ts 一致）。
type GameRecord struct {
	ID          int64        `json:"id"`
	Title       string       `json:"title"`
	Mode        string       `json:"mode"`
	InitialFen  string       `json:"initialFen"`
	Moves       []RecordMove `json:"moves"`
	Result      *string      `json:"result"`
	SolveStatus *string      `json:"solveStatus"`
	Solutions   any          `json:"solutions"`
	LlmNote     *string      `json:"llmNote"`
	Note        *string      `json:"note"`
	CreatedAt   int64        `json:"createdAt"`
}

// GameRecordSummary 棋谱库列表行（07 §5：列表筛选 SolveStatus）。
type GameRecordSummary struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	Mode        string  `json:"mode"`
	Result      *string `json:"result"`
	SolveStatus *string `json:"solveStatus"`
	CreatedAt   int64   `json:"createdAt"`
}

// RawRecordMove moves_json 落盘原始形状（p/x 允许 null，与 Flutter 版逐字段一致；
// storage-schema/recordMoves.ts RawRecordMove 同形）。
type RawRecordMove struct {
	F [2]int  `json:"f"`
	T [2]int  `json:"t"`
	P *string `json:"p"`
	X *string `json:"x"`
}

// EncodeRecordMove 领域走法 → 存档条目（game_record.dart:_encodeMove）。
func EncodeRecordMove(m *rules.Move) RawRecordMove {
	raw := RawRecordMove{F: [2]int{m.From.Col, m.From.Row}, T: [2]int{m.To.Col, m.To.Row}}
	if m.Piece != nil {
		fen := rules.PieceFenChar(m.Piece)
		raw.P = &fen
	}
	if m.Captured != nil {
		fen := rules.PieceFenChar(m.Captured)
		raw.X = &fen
	}
	return raw
}

// DecodeRecordMove 存档条目 → 领域走法（game_record.dart:decodeMoveJson）。
// 字段缺失 / 坐标越界 / FEN 字符非法返回 nil（防御旧库脏数据）。
// 空串与 null 同义（旧库行存在 x:"" 形态），均视为"无棋子"；多字符串取首字符判定
// （与 TS 字符串索引语义一致：'ZZ'[0] === 'Z'）。
func DecodeRecordMove(raw any) *rules.Move {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	fc, fr, okF := coordPair(obj["f"])
	tc, tr, okT := coordPair(obj["t"])
	if !okF || !okT || !rules.InBoard(fc, fr) || !rules.InBoard(tc, tr) {
		return nil
	}
	piece := pieceFromFenAny(obj["p"])
	if obj["p"] != nil {
		// 键存在且非 null：非字符串（如数值）或非空但解析不出棋子 → 脏数据
		if s, isStr := obj["p"].(string); !isStr || (s != "" && piece == nil) {
			return nil
		}
	}
	captured := pieceFromFenAny(obj["x"])
	if obj["x"] != nil {
		if s, isStr := obj["x"].(string); !isStr || (s != "" && captured == nil) {
			return nil
		}
	}
	return &rules.Move{From: rules.Pos(fc, fr), To: rules.Pos(tc, tr), Piece: piece, Captured: captured}
}

// coordPair [col,row] 数组解析：长度 2 且均为数值。
func coordPair(v any) (int, int, bool) {
	arr, ok := v.([]any)
	if !ok || len(arr) != 2 {
		return 0, 0, false
	}
	c, okC := arr[0].(float64)
	r, okR := arr[1].(float64)
	if !okC || !okR {
		return 0, 0, false
	}
	return int(c), int(r), true
}

// pieceFromFenAny FEN 字符串 → 棋子；nil/空串 → nil（recordMoves.ts pieceFromFenOrNull）。
func pieceFromFenAny(v any) *rules.Piece {
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	return rules.PieceFromFenChar(s[0])
}

// RecordMoveToRaw 契约面走法（p 恒 string）→ 落盘原始条目（recordMoves.ts recordMoveToRaw）。
func RecordMoveToRaw(m RecordMove) RawRecordMove {
	p := m.P
	return RawRecordMove{F: m.F, T: m.T, P: &p, X: m.X}
}

// FillMovePieces 对走法序列重放补齐棋子/吃子信息；局面不符（源格无子）即截断
// （防御式，game_record.dart:225-240）。
func FillMovePieces(initialFen string, moves []rules.Move) []rules.Move {
	board, err := rules.FromFen(initialFen)
	if err != nil {
		return nil
	}
	filled := []rules.Move{}
	for _, m := range moves {
		piece := board.PieceAtP(m.From)
		if piece == nil {
			break
		}
		applied := board.ApplyMove(rules.Move{From: m.From, To: m.To})
		filled = append(filled, rules.Move{From: applied.From, To: applied.To, Piece: piece, Captured: applied.Captured})
	}
	return filled
}

// FinalFenOf 从 initialFen 重放 moves 求终局 FEN；遇到与局面不符的走法即止损
// （game_record.dart:89-96）。
func FinalFenOf(initialFen string, moves []rules.Move) string {
	board, err := rules.FromFen(initialFen)
	if err != nil {
		return initialFen
	}
	for _, m := range moves {
		if board.PieceAtP(m.From) == nil {
			break
		}
		board.ApplyMove(rules.Move{From: m.From, To: m.To})
	}
	return board.ToFen()
}

// gameRecordsRow game_records 列扫描。
type gameRecordsRow struct {
	id            int64
	title         string
	mode          string
	initialFen    string
	movesJSON     string
	result        any
	solveStatus   any
	solutionsJSON sql.NullString
	llmNote       sql.NullString
	note          sql.NullString
	createdAt     any
}

// recordFromRow game_records 行 → 领域对象（db.ts recordFromRow）。
func recordFromRow(row gameRecordsRow) (*GameRecord, error) {
	moves := []RecordMove{}
	var rawMoves any
	if err := json.Unmarshal([]byte(row.movesJSON), &rawMoves); err == nil {
		if arr, ok := rawMoves.([]any); ok {
			for _, item := range arr {
				m := DecodeRecordMove(item)
				if m == nil {
					continue
				}
				r := EncodeRecordMove(m)
				// 契约面 p 允许空串不可把 null 强转 ''——DecodeRecordMove 会把空串
				// 视为"无棋子"，但 p 键缺失的行重编码后 p 为 nil，落回空串
				//（db.ts：`p: r.p ?? ''`，实机缺陷修复注释）。
				p := ""
				if r.P != nil {
					p = *r.P
				}
				moves = append(moves, RecordMove{F: r.F, T: r.T, P: p, X: r.X})
			}
		}
	}
	var solutions any
	if row.solutionsJSON.Valid && row.solutionsJSON.String != "" {
		var parsed any
		if err := json.Unmarshal([]byte(row.solutionsJSON.String), &parsed); err == nil {
			if arr, ok := parsed.([]any); ok && allArrays(arr) {
				solutions = parsed
			}
		}
	}
	return &GameRecord{
		ID:          row.id,
		Title:       row.title,
		Mode:        row.mode,
		InitialFen:  row.initialFen,
		Moves:       moves,
		Result:      parseResult(row.result),
		SolveStatus: parseSolveStatus(row.solveStatus),
		Solutions:   solutions,
		LlmNote:     nullStringPtr(row.llmNote),
		Note:        nullStringPtr(row.note),
		CreatedAt:   toEpochMs(row.createdAt),
	}, nil
}

// allArrays 数组的数组判定（db.ts：parsed.every((s) => Array.isArray(s))）。
func allArrays(arr []any) bool {
	for _, item := range arr {
		if _, ok := item.([]any); !ok {
			return false
		}
	}
	return true
}

func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func stringPtr(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

// ChessDao 数据访问对象（saved_games 自动存档 + game_records 棋谱库，07 §1）。
type ChessDao struct {
	db *sql.DB
}

// OpenDao 打开并初始化数据库（07 §1：文件位于 Documents/chinese_chess_ultra_go.sqlite）。
// busy_timeout 对齐 better-sqlite3 默认 5000ms（并发写等待而非立即 SQLITE_BUSY）。
func OpenDao(dbPath string) (*ChessDao, error) {
	db, err := sql.Open(sqliteDriverName, "file:"+dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if err := InitSchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &ChessDao{db: db}, nil
}

// OpenDaoInMemory 测试辅助：内存库（单连接保持 :memory: 数据存活）。
func OpenDaoInMemory() (*ChessDao, error) {
	db, err := sql.Open(sqliteDriverName, ":memory:")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := InitSchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &ChessDao{db: db}, nil
}

// Close 关闭底层连接。
func (d *ChessDao) Close() error { return d.db.Close() }

// ---------------------------------------------------------------------------
// saved_games（自动存档）
// ---------------------------------------------------------------------------

// Upsert 裸 upsert（game_dao.dart:142-163；id 为 nil 时插入并标 'legacy'，
// 与原版 NULL 模式同样不可经任何正式模式读出）。
func (d *ChessDao) Upsert(id *int64, fen string, moves [][]int) (int64, error) {
	jsonBytes, err := json.Marshal(moves)
	if err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	if id == nil {
		res, err := d.db.Exec(
			"INSERT INTO saved_games(mode, fen, move_stack_json, created_at, updated_at) VALUES ('legacy', ?, ?, ?, ?)",
			fen, string(jsonBytes), now, now)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	_, err = d.db.Exec(
		"UPDATE saved_games SET fen = ?, move_stack_json = ?, updated_at = ? WHERE id = ?",
		fen, string(jsonBytes), now, *id)
	if err != nil {
		return 0, err
	}
	return *id, nil
}

// Latest 最近一条（按更新时间倒序，game_dao.dart:166-175）；空库返回 nil。
func (d *ChessDao) Latest() (*SavedGame, error) {
	rows, err := d.db.Query(
		"SELECT id, mode, fen, move_stack_json, created_at, updated_at FROM saved_games ORDER BY updated_at DESC LIMIT 1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	row, err := scanSavedGamesRow(rows)
	if err != nil {
		return nil, err
	}
	return savedGameFromRow(row), nil
}

// UpsertForMode 按模式 upsert：每模式只保留最近一局（game_dao.dart:180-205）。
func (d *ChessDao) UpsertForMode(mode, fen string, moves [][]int) (int64, error) {
	jsonBytes, err := json.Marshal(moves)
	if err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	var existingID int64
	err = d.db.QueryRow("SELECT id FROM saved_games WHERE mode = ? LIMIT 1", mode).Scan(&existingID)
	if errors.Is(err, sql.ErrNoRows) {
		res, err := d.db.Exec(
			"INSERT INTO saved_games(mode, fen, move_stack_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
			mode, fen, string(jsonBytes), now, now)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	if err != nil {
		return 0, err
	}
	_, err = d.db.Exec(
		"UPDATE saved_games SET fen = ?, move_stack_json = ?, updated_at = ? WHERE id = ?",
		fen, string(jsonBytes), now, existingID)
	if err != nil {
		return 0, err
	}
	return existingID, nil
}

// LatestForMode 指定模式最近一局；legacy 数据不属于任何模式（game_dao.dart:208-218）。
func (d *ChessDao) LatestForMode(mode string) (*SavedGame, error) {
	rows, err := d.db.Query(
		"SELECT id, mode, fen, move_stack_json, created_at, updated_at FROM saved_games WHERE mode = ? ORDER BY updated_at DESC LIMIT 1", mode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	row, err := scanSavedGamesRow(rows)
	if err != nil {
		return nil, err
	}
	return savedGameFromRow(row), nil
}

// DeleteForMode 删除指定模式存档（恢复后死局清理等场景）。
func (d *ChessDao) DeleteForMode(mode string) error {
	_, err := d.db.Exec("DELETE FROM saved_games WHERE mode = ?", mode)
	return err
}

// All 全部存档（按更新时间倒序）。
func (d *ChessDao) All() ([]*SavedGame, error) {
	rows, err := d.db.Query(
		"SELECT id, mode, fen, move_stack_json, created_at, updated_at FROM saved_games ORDER BY updated_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	games := []*SavedGame{}
	for rows.Next() {
		row, err := scanSavedGamesRow(rows)
		if err != nil {
			return nil, err
		}
		games = append(games, savedGameFromRow(row))
	}
	return games, rows.Err()
}

// Delete 按 id 删除存档。
func (d *ChessDao) Delete(id int64) error {
	_, err := d.db.Exec("DELETE FROM saved_games WHERE id = ?", id)
	return err
}

// Clear 清空全部存档。
func (d *ChessDao) Clear() error {
	_, err := d.db.Exec("DELETE FROM saved_games")
	return err
}

type rowScanner interface{ Scan(dest ...any) error }

func scanSavedGamesRow(rs rowScanner) (savedGamesRow, error) {
	var row savedGamesRow
	err := rs.Scan(&row.id, &row.mode, &row.fen, &row.moveStackJSON, &row.createdAt, &row.updatedAt)
	return row, err
}

// ---------------------------------------------------------------------------
// game_records（棋谱库）
// ---------------------------------------------------------------------------

// InsertRecord 插入棋谱，返回 id（game_dao.dart:250-274）；CreatedAt 为 0 时取当前时间。
func (d *ChessDao) InsertRecord(record *GameRecord) (int64, error) {
	rawMoves := make([]RawRecordMove, 0, len(record.Moves))
	for _, m := range record.Moves {
		rawMoves = append(rawMoves, RecordMoveToRaw(m))
	}
	movesJSON, err := json.Marshal(rawMoves)
	if err != nil {
		return 0, err
	}
	var solutionsJSON any
	if record.Solutions != nil {
		b, err := json.Marshal(record.Solutions)
		if err != nil {
			return 0, err
		}
		solutionsJSON = string(b)
	}
	createdAt := record.CreatedAt
	if createdAt == 0 {
		createdAt = time.Now().UnixMilli()
	}
	res, err := d.db.Exec(
		`INSERT INTO game_records(
          title, mode, initial_fen, moves_json, result,
          solve_status, solutions_json, llm_note, note, created_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Title, record.Mode, record.InitialFen, string(movesJSON), stringPtr(record.Result),
		stringPtr(record.SolveStatus), solutionsJSON, stringPtr(record.LlmNote), stringPtr(record.Note),
		createdAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AllRecords 全部棋谱（按创建时间倒序，game_dao.dart:284-289）。
func (d *ChessDao) AllRecords() ([]*GameRecord, error) {
	rows, err := d.db.Query("SELECT * FROM game_records ORDER BY created_at DESC, id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []*GameRecord{}
	for rows.Next() {
		row, err := scanGameRecordsRow(rows)
		if err != nil {
			return nil, err
		}
		rec, err := recordFromRow(row)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}

// RecordByID 按 id 读单条棋谱；不存在返回 nil。
func (d *ChessDao) RecordByID(id int64) (*GameRecord, error) {
	rows, err := d.db.Query("SELECT * FROM game_records WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	row, err := scanGameRecordsRow(rows)
	if err != nil {
		return nil, err
	}
	return recordFromRow(row)
}

// RecordSummaries 棋谱库列表摘要（07 §5）。
func (d *ChessDao) RecordSummaries() ([]GameRecordSummary, error) {
	all, err := d.AllRecords()
	if err != nil {
		return nil, err
	}
	summaries := make([]GameRecordSummary, 0, len(all))
	for _, r := range all {
		summaries = append(summaries, GameRecordSummary{
			ID:          r.ID,
			Title:       r.Title,
			Mode:        r.Mode,
			Result:      r.Result,
			SolveStatus: r.SolveStatus,
			CreatedAt:   r.CreatedAt,
		})
	}
	return summaries, nil
}

// DeleteRecord 删除棋谱。
func (d *ChessDao) DeleteRecord(id int64) error {
	_, err := d.db.Exec("DELETE FROM game_records WHERE id = ?", id)
	return err
}

// UpdateRecord 更新可变字段（标题/备注/求解结论；initial_fen/mode/created_at 不动，
// game_dao.dart:306-326）。
func (d *ChessDao) UpdateRecord(record *GameRecord) error {
	rawMoves := make([]RawRecordMove, 0, len(record.Moves))
	for _, m := range record.Moves {
		rawMoves = append(rawMoves, RecordMoveToRaw(m))
	}
	movesJSON, err := json.Marshal(rawMoves)
	if err != nil {
		return err
	}
	var solutionsJSON any
	if record.Solutions != nil {
		b, err := json.Marshal(record.Solutions)
		if err != nil {
			return err
		}
		solutionsJSON = string(b)
	}
	_, err = d.db.Exec(
		`UPDATE game_records SET
          title = ?, moves_json = ?, result = ?, solve_status = ?,
          solutions_json = ?, llm_note = ?, note = ?
        WHERE id = ?`,
		record.Title, string(movesJSON), stringPtr(record.Result), stringPtr(record.SolveStatus),
		solutionsJSON, stringPtr(record.LlmNote), stringPtr(record.Note), record.ID)
	return err
}

func scanGameRecordsRow(rs rowScanner) (gameRecordsRow, error) {
	var row gameRecordsRow
	err := rs.Scan(&row.id, &row.title, &row.mode, &row.initialFen, &row.movesJSON,
		&row.result, &row.solveStatus, &row.solutionsJSON, &row.llmNote, &row.note, &row.createdAt)
	return row, err
}
