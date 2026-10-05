package parsers

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// PGN（中国象棋变体）棋谱解析器（对应 pgn_parser.dart，06 文档 §4；逐行翻译 pgnParser.ts）。
//
// 支持两种着法文本：
// - ICCS 坐标：`H2-E2` / `h2e2`（列 a-i、行 0-9，0 为红方底线）；
// - 中文纵线记谱：`炮二平五`、`马8进7`、`前炮退二` 等，随局面逐着消解。
//
// 标签对支持 `[FEN]`（自定义起始局面）、`[Event]`、`[Red]`、`[Black]` 等；
// 无 `[FEN]` 时使用标准初始局面。注释 `{...}`、行注释 `;...`、NAG `$n`、
// 变着 `(...)` 被跳过；多局文件按局切分。
//
// 大文件（多局合一 `.pgns`，可达百 MB）不要整读内存：用 ScanGameOffsets
// 流式建立按局偏移索引，再按需读取单局交给 ParseGame。文件访问经
// PgnFileSource 注入（本包零 fs 依赖，Dart 侧 RandomAccessFile 的等价抽象）。

// 标签行：`[Key "Value"]`（pgn_parser.dart:34）。
var tagPattern = regexp.MustCompile(`^\s*\[(\w+)\s+"(.*)"\]\s*$`)

// 步数序号：`1.` `12...`（pgn_parser.dart:35）。
var moveNumberPattern = regexp.MustCompile(`\d+\s*\.+`)

// 结果标记（pgn_parser.dart:36）。
var resultPattern = regexp.MustCompile(`(1-0|0-1|1/2-1/2|\*)`)

// NAG（pgn_parser.dart:37）。
var nagPattern = regexp.MustCompile(`\$\d+`)

// 最内层变着括号（pgn_parser.dart:38）。
var innermostVarPattern = regexp.MustCompile(`\([^()]*\)`)

// movePattern 着法 token（ICCS 与中文纵线记谱交替扫描，pgn_parser.dart:44-46）。
var movePattern = regexp.MustCompile(
	`([a-iA-I]\d{1,2}-?[a-iA-I]\d{1,2})|([前后中]?[车马炮兵卒帅将仕士相象砲][一二三四五六七八九\d０-９]?[平进退][一二三四五六七八九\d０-９])`)

// cnDigits 红方汉字数字（一~九，pgn_parser.dart:49-52）。
var cnDigits = map[rune]int{
	'一': 1, '二': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9,
}

// pieceKindByChar 棋子中文字符 → 种类（颜色由轮走方决定，车/马/炮等红黑同形；pgn_parser.dart:55-61）。
var pieceKindByChar = map[rune]rules.Kind{
	'车': rules.Rook,
	'马': rules.Knight,
	'炮': rules.Cannon,
	'砲': rules.Cannon,
	'兵': rules.Pawn,
	'卒': rules.Pawn,
	'帅': rules.King,
	'将': rules.King,
	'仕': rules.Advisor,
	'士': rules.Advisor,
	'相': rules.Minister,
	'象': rules.Minister,
}

// isLinearKind 直线走子（进/退后跟格数）；其余为斜走子（进/退后跟目标纵线）。
func isLinearKind(kind rules.Kind) bool {
	switch kind {
	case rules.Rook, rules.Cannon, rules.King, rules.Pawn:
		return true
	}
	return false
}

// PgnGameIndex 多局 PGN 文件中单局的索引条目（偏移 + 摘要，pgn_parser.dart:580-597）。
type PgnGameIndex struct {
	Offset int64   `json:"offset"`
	Length int64   `json:"length"`
	Event  *string `json:"event"`
	Red    *string `json:"red"`
	Black  *string `json:"black"`
}

// PgnGameIndexTitle 单局标题（event 优先，缺省 `红 vs 黑`）。
func PgnGameIndexTitle(index PgnGameIndex) string {
	if index.Event != nil {
		return *index.Event
	}
	return index.derefOr(index.Red, "?") + " vs " + index.derefOr(index.Black, "?")
}

func (i PgnGameIndex) derefOr(v *string, dflt string) string {
	if v != nil {
		return *v
	}
	return dflt
}

// PgnFileSource 大文件按局索引的文件源抽象（本包零 fs 依赖；storage 层用 os.File 实现）。
// Read 语义：返回 [offset, min(offset+length, byteLength)) 的字节；
// offset ≥ byteLength 时返回空切片（同 Dart readSync 的 EOF 行为）。
type PgnFileSource interface {
	ByteLength() int64
	Read(offset int64, length int) []byte
}

// hasNonWhitespaceByte 单字节序非空白的行内字节判断（pgn_parser.dart:449-450/482-483）。
func hasNonWhitespaceByte(b []byte) bool {
	for _, v := range b {
		if v != 0x0d && v != 0x0a && v != 0x20 && v != 0x09 {
			return true
		}
	}
	return false
}

// DecodeUtf8Lossy 有损 UTF-8 解码（等价 Dart utf8.decode(allowMalformed: true)）。
func DecodeUtf8Lossy(b []byte) string {
	return strings.ToValidUTF8(string(b), "\uFFFD")
}

func nonEmpty(s string) (string, bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return "", false
	}
	return t, true
}

// -----------------------------------------------------------------------------
// 多局切分与整段解析
// -----------------------------------------------------------------------------

// ParseGames 解析整段 PGN 文本（可含多局），返回全部棋局；单局失败跳过不影响其余（pgn_parser.dart:73-84）。
func ParseGames(content string, source string) ([]*ParsedPuzzle, error) {
	games := make([]*ParsedPuzzle, 0)
	for _, gameText := range SplitGames(content) {
		puzzle, err := ParseGame(gameText, source)
		if err != nil {
			var fenErr *rules.FenFormatError
			if errors.As(err, &fenErr) {
				continue // PGN 局解析失败，已跳过
			}
			return nil, err
		}
		games = append(games, puzzle)
	}
	return games, nil
}

// SplitGames 把多局 PGN 文本按局切分：以"出现着法之后再次遇到标签行"作为新一局的开始（pgn_parser.dart:89-105）。
func SplitGames(content string) []string {
	games := make([]string, 0)
	var lines []string
	inMoves := false
	flush := func() {
		games = append(games, strings.Join(lines, "\n")+"\n")
		lines = lines[:0]
	}
	for _, line := range splitLines(content) {
		isTag := strings.HasPrefix(line, "[") && tagPattern.MatchString(line)
		if isTag && inMoves {
			flush()
			inMoves = false
		}
		lines = append(lines, line)
		if !isTag && strings.TrimSpace(line) != "" {
			inMoves = true
		}
	}
	if strings.TrimSpace(strings.Join(lines, "\n")) != "" {
		flush()
	}
	return games
}

// splitLines 按 \r?\n 切行（保留空行）。
func splitLines(content string) []string {
	return strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
}

// ReadPgnTags 读取标签对（key 小写化；pgn_parser.dart:179-188）。
func ReadPgnTags(gameText string) map[string]string {
	tags := make(map[string]string)
	for _, line := range splitLines(gameText) {
		if m := tagPattern.FindStringSubmatch(line); m != nil {
			tags[strings.ToLower(m[1])] = m[2]
		}
	}
	return tags
}

// -----------------------------------------------------------------------------
// 单局解析
// -----------------------------------------------------------------------------

// ParseGame 解析单局 PGN 文本为 ParsedPuzzle（pgn_parser.dart:113-168）。
// source 为来源标注（如语料子目录名）。着法无法消解或局面非法时返回
// *rules.FenFormatError（等价 Dart FormatException）。
func ParseGame(gameText string, source string) (*ParsedPuzzle, error) {
	tags := ReadPgnTags(gameText)
	moveSection := ExtractMoveSection(gameText)

	// 起始局面：优先 FEN 标签，否则标准初始局面。
	fenTag, hasFen := nonEmpty(tags["fen"])
	initialBoard := rules.Initial()
	isRedTurn := true
	if hasFen {
		b, err := rules.FromFen(fenTag)
		if err != nil {
			return nil, err
		}
		initialBoard = b
		isRedTurn = rules.ParseTurnFen(fenTag)
	}
	initialGrid, err := rules.ParseBoardFen(initialBoard.ToFen())
	if err != nil {
		return nil, err
	}
	initialFen := rules.BuildFen(initialGrid, isRedTurn)

	// 逐着消解（在棋盘上验证合法性）。
	board, err := rules.FromFen(initialFen)
	if err != nil {
		return nil, err
	}
	moves := make([]string, 0)
	for _, token := range moveSection {
		resolved := ResolveToken(token, board, isRedTurn)
		if resolved == nil {
			// 着法无法消解：停在当前处，保留已解析的合法前缀（pgn_parser.dart:136-141）。
			break
		}
		iccs := FormatIccs(resolved.From, resolved.To)
		if iccs == "" {
			break
		}
		moves = append(moves, iccs)
		board.ApplyMove(rules.Move{From: resolved.From, To: resolved.To})
		isRedTurn = !isRedTurn
	}
	if len(moves) == 0 {
		return nil, &rules.FenFormatError{Message: "PGN 局不含任何可解析着法"}
	}

	title := defaultPgnTitle(tags["red"], tags["black"])
	if t, ok := nonEmpty(tags["event"]); ok {
		title = t
	}
	red, hasRed := nonEmpty(tags["red"])
	black, hasBlack := nonEmpty(tags["black"])
	date, hasDate := nonEmpty(tags["date"])
	site, hasSite := nonEmpty(tags["site"])
	descParts := make([]string, 0, 3)
	if hasRed || hasBlack {
		if !hasRed {
			red = "?"
		}
		if !hasBlack {
			black = "?"
		}
		descParts = append(descParts, red+" vs "+black)
	}
	if hasDate {
		descParts = append(descParts, date)
	}
	if hasSite {
		descParts = append(descParts, site)
	}

	return &ParsedPuzzle{
		ID:            fmt.Sprintf("pgn/%s/%s/%d", source, title, len(moves)),
		InitialFen:    initialFen,
		SolutionMoves: moves,
		Title:         &title,
		Description:   optionalText(strings.Join(descParts, " · ")),
		Source:        source,
		Format:        "pgn",
		Difficulty:    DifficultyFromMoveCount(len(moves)),
	}, nil
}

// optionalText 空串归一为 null（TS descParts.join 的 ” 与 Dart null 的口径对齐）。
func optionalText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func defaultPgnTitle(red, black string) string {
	r, ok := nonEmpty(red)
	if !ok {
		r = "?"
	}
	b, ok := nonEmpty(black)
	if !ok {
		b = "?"
	}
	return r + " 对 " + b
}

// ExtractMoveSection 抽取着法文本并切分为 token 列表（pgn_parser.dart:194-217）。
// 处理顺序：剔标签行 → 去块注释 → 去变着（嵌套）→ 去序号/NAG/结果 → 全文匹配着法。
func ExtractMoveSection(gameText string) []string {
	// 先剔除标签行（FEN 棋盘串里的字母数字会被误认为着法）。
	var kept []string
	for _, line := range splitLines(gameText) {
		if tagPattern.MatchString(line) {
			continue
		}
		kept = append(kept, line)
	}
	text := strings.Join(kept, "\n")
	// 单遍扫描去注释：{} 块注释内可含 ;，; 行注释内可有未闭合 {。
	noComments := stripComments(text)
	// 变着：反复删除最内层括号以处理嵌套。
	stripped := noComments
	for {
		next := innermostVarPattern.ReplaceAllString(stripped, " ")
		if next == stripped {
			break
		}
		stripped = next
	}
	cleaned := moveNumberPattern.ReplaceAllString(stripped, " ")
	cleaned = nagPattern.ReplaceAllString(cleaned, " ")
	cleaned = resultPattern.ReplaceAllString(cleaned, " ")
	cleaned = strings.Join(strings.FieldsFunc(cleaned, isSpaceRune), "")

	return movePattern.FindAllString(cleaned, -1)
}

// isSpaceRune \s+ 等价的空白判定（全文去空白用）。
func isSpaceRune(r rune) bool { return unicode.IsSpace(r) }

// stripComments 单遍扫描去注释：块注释 `{...}` 与行注释 `;...`（到行尾）（pgn_parser.dart:222-245）。
func stripComments(text string) string {
	var out strings.Builder
	inBrace := false
	i := 0
	for i < len(text) {
		r, size := utf8.DecodeRuneInString(text[i:])
		if inBrace {
			if r == '}' {
				inBrace = false
			}
			i += size
		} else if r == '{' {
			inBrace = true
			out.WriteByte(' ')
			i += size
		} else if r == ';' {
			for i < len(text) && text[i] != '\n' {
				i++
			}
			out.WriteByte(' ')
			// '\n' 本身留给下一轮正常写入（对齐 TS 的 continue 跳过 i+=1）。
		} else {
			out.WriteString(text[i : i+size])
			i += size
		}
	}
	return out.String()
}

// -----------------------------------------------------------------------------
// 着法消解（pgn_parser.dart:253-422）
// -----------------------------------------------------------------------------

// ResolveToken 把单个着法 token 消解为棋盘上的起止坐标；ICCS 直查、中文按候选+合法走法唯一匹配。
func ResolveToken(token string, board *rules.Board, isRedTurn bool) *FromTo {
	if iccs := ParseIccs(token); iccs != nil {
		if board.PieceAtP(iccs.From) != nil {
			return iccs
		}
		return nil
	}
	return resolveChineseMove(token, board, isRedTurn)
}

// parseNumberChar 解析数字字符（汉字、半角或全角阿拉伯数字——部分生成器黑方用全角；pgn_parser.dart:378-385）。
func parseNumberChar(ch rune) (int, bool) {
	if n, ok := cnDigits[ch]; ok {
		return n, true
	}
	// 全角 ０-９ → 半角。
	if ch >= 0xff10 && ch <= 0xff19 {
		return int(ch - 0xff10), true
	}
	if ch >= '0' && ch <= '9' {
		return int(ch - '0'), true
	}
	return 0, false
}

// numberToCol 纵线号 → 列号：红方从右起一~九（col = 9-n），黑方从其右手起 1~9（col = n-1）。
func numberToCol(n int, side rules.Side) int {
	if side == rules.Red {
		return 9 - n
	}
	return n - 1
}

func resolveChineseMove(token string, board *rules.Board, isRedTurn bool) *FromTo {
	chars := []rune(token)
	if len(chars) == 0 {
		return nil
	}
	side := rules.Black
	if isRedTurn {
		side = rules.Red
	}

	// 结构解析：[前后中]? 棋子 [列号?] 平/进/退 数字
	idx := 0
	modifier := rune(0)
	if chars[idx] == '前' || chars[idx] == '后' || chars[idx] == '中' {
		modifier = chars[idx]
		idx++
	}
	kind, ok := pieceKindByChar[chars[idx]]
	if !ok {
		return nil
	}
	idx++

	colNumber := -1 // 列号（可能省略；-1 = 未给）
	if idx < len(chars) {
		if n, ok := parseNumberChar(chars[idx]); ok {
			colNumber = n
			idx++
		}
	}
	if idx >= len(chars) {
		return nil
	}
	action := chars[idx] // 平 / 进 / 退
	if action != '平' && action != '进' && action != '退' {
		return nil
	}
	idx++
	if idx >= len(chars) {
		return nil
	}
	targetNumber, ok := parseNumberChar(chars[idx])
	if !ok {
		return nil
	}
	idx++
	if idx != len(chars) {
		return nil // 应恰好消费完
	}

	isLinear := isLinearKind(kind)

	// 候选棋子：按列号或前/后/中修饰筛选。
	candidates := make([]rules.Position, 0)
	for row := 0; row < 10; row++ {
		for col := 0; col < 9; col++ {
			if p := board.PieceAt(col, row); p != nil && p.Kind == kind && p.Side == side {
				candidates = append(candidates, rules.Pos(col, row))
			}
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	fromChoices := make([]rules.Position, 0)
	switch {
	case modifier != 0 && colNumber >= 0:
		// 非标准组合"前兵九平八"：先限定列，再在同列多子中取前/后/中。
		col := numberToCol(colNumber, side)
		var inCol []rules.Position
		for _, p := range candidates {
			if p.Col == col {
				inCol = append(inCol, p)
			}
		}
		if len(inCol) < 2 {
			return nil
		}
		rows := make([]int, 0, len(inCol))
		for _, p := range inCol {
			rows = append(rows, p.Row)
		}
		sortInts(rows)
		if side != rules.Red {
			reverseInts(rows)
		}
		pick, ok := pickByModifier(modifier, rows)
		if !ok {
			return nil
		}
		chosen := findPosByRow(inCol, pick)
		if chosen == nil {
			return nil
		}
		fromChoices = append(fromChoices, *chosen)
	case modifier != 0:
		// 前/后/中：用于同列同类多子，省略列号；找到有多个同类子的列。
		byCol := make(map[int][]rules.Position)
		for _, c := range candidates {
			byCol[c.Col] = append(byCol[c.Col], c)
		}
		for _, entry := range byCol {
			if len(entry) < 2 {
				continue
			}
			rows := make([]int, 0, len(entry))
			for _, p := range entry {
				rows = append(rows, p.Row)
			}
			sortInts(rows)
			// 红方 row 小者为"前"，黑方相反。
			if side != rules.Red {
				reverseInts(rows)
			}
			if pick, ok := pickByModifier(modifier, rows); ok {
				if chosen := findPosByRow(entry, pick); chosen != nil {
					fromChoices = append(fromChoices, *chosen)
				}
			}
		}
	case colNumber >= 0:
		col := numberToCol(colNumber, side)
		for _, p := range candidates {
			if p.Col == col {
				fromChoices = append(fromChoices, p)
			}
		}
	default:
		return nil // 无列号也无前后修饰，无法消解
	}

	// 在候选棋子的合法走法中寻找唯一匹配。
	matches := make(map[string]FromTo)
	for _, from := range fromChoices {
		for _, move := range board.LegalMovesFor(from) {
			if matchesAction(move.To, from, action, targetNumber, isLinear, side) {
				matches[fmt.Sprintf("%d,%d", move.To.Col, move.To.Row)] = FromTo{From: from, To: move.To}
			}
		}
	}
	if len(matches) != 1 {
		return nil
	}
	for _, m := range matches {
		return &m
	}
	return nil
}

// pickByModifier 前→排首；后→排尾；中→中间位（≥3 子才有意义，pgn_parser.dart:322-327）。
func pickByModifier(modifier rune, ordered []int) (int, bool) {
	switch modifier {
	case '前':
		if len(ordered) == 0 {
			return 0, false
		}
		return ordered[0], true
	case '后':
		if len(ordered) == 0 {
			return 0, false
		}
		return ordered[len(ordered)-1], true
	case '中':
		if len(ordered) >= 3 {
			return ordered[len(ordered)/2], true
		}
		return 0, false
	}
	return 0, false
}

func matchesAction(to, from rules.Position, action rune, number int, isLinear bool, side rules.Side) bool {
	red := side == rules.Red
	switch action {
	case '平':
		// 平移：目标纵线，行不变。
		return to.Col == numberToCol(number, side) && to.Row == from.Row
	case '进':
		if isLinear {
			// 直线子进：列不变，前进 number 格。
			delta := number
			if red {
				delta = -number
			}
			return to.Col == from.Col && to.Row == from.Row+delta
		}
		// 斜走子进：数字为目标纵线，行向前。
		if to.Col != numberToCol(number, side) {
			return false
		}
		if red {
			return to.Row < from.Row
		}
		return to.Row > from.Row
	case '退':
		if isLinear {
			delta := -number
			if red {
				delta = number
			}
			return to.Col == from.Col && to.Row == from.Row+delta
		}
		if to.Col != numberToCol(number, side) {
			return false
		}
		if red {
			return to.Row > from.Row
		}
		return to.Row < from.Row
	}
	return false
}

func sortInts(xs []int) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

func reverseInts(xs []int) {
	for i, j := 0, len(xs)-1; i < j; i, j = i+1, j-1 {
		xs[i], xs[j] = xs[j], xs[i]
	}
}

func findPosByRow(ps []rules.Position, row int) *rules.Position {
	for i := range ps {
		if ps[i].Row == row {
			return &ps[i]
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// 大文件按局索引（pgn_parser.dart:430-559）
// -----------------------------------------------------------------------------

// chunkSize 块大小 1MB（pgn_parser.dart:435）。
const chunkSize = 1 << 20

// maxPendingBytes 单行字节缓冲上限：超过后该行按非标签行（moves 行）处理并
// 丢弃剩余内容，pending 不无限累积（P2-5 语义，pgn_parser.dart:438）。
const maxPendingBytes = 8 << 20

type scanState struct {
	gameStart        int64
	gameTags         map[string]string
	inMoves          bool
	pendingTruncated bool
}

// ScanGameOffsets 流式扫描多局合一 PGN 文件，返回每局的偏移与摘要信息（pgn_parser.dart:430-547）。
//
// 不把文件读入内存；按字节定位行（UTF-8 多字节字符跨块安全），以
// "出现着法后再次遇到标签行"分界。仅标签行被解码，其余保持字节。
// maxGames ≤ 0 表示不限数量。
func ScanGameOffsets(source PgnFileSource, maxGames int) []PgnGameIndex {
	result := make([]PgnGameIndex, 0)
	state := &scanState{gameStart: -1, gameTags: make(map[string]string)}
	processLine := func(lineBytes []byte, lineStart int64) {
		if state.pendingTruncated {
			// 超长行按非标签行处理（计为 moves 行），不解析内容。
			if hasNonWhitespaceByte(lineBytes) {
				state.inMoves = true
				if state.gameStart < 0 {
					state.gameStart = lineStart
				}
			}
			return
		}
		isTagLine := len(lineBytes) > 0 && lineBytes[0] == 0x5b // '['
		if isTagLine {
			if state.inMoves {
				result = append(result, PgnGameIndex{
					Offset: state.gameStart,
					Length: lineStart - state.gameStart,
					Event:  tagOrNil(state.gameTags["event"]),
					Red:    tagOrNil(state.gameTags["red"]),
					Black:  tagOrNil(state.gameTags["black"]),
				})
				state.gameTags = make(map[string]string)
				state.inMoves = false
				state.gameStart = lineStart
			} else if state.gameStart < 0 {
				state.gameStart = lineStart
			}
			lineText := DecodeUtf8Lossy(lineBytes)
			if m := tagPattern.FindStringSubmatch(lineText); m != nil {
				state.gameTags[strings.ToLower(m[1])] = m[2]
			}
		} else if hasNonWhitespaceByte(lineBytes) {
			state.inMoves = true
		}
	}

	filePos := int64(0)      // 已读完的字节数
	pendingStart := int64(0) // pending[0] 在文件中的位置
	var pending []byte       // 未成行的剩余字节

	for {
		// Read 语义：返回 [offset, min(offset+length, byteLength)) 的字节；EOF 返回空。
		chunk := source.Read(filePos, chunkSize)
		if len(chunk) == 0 {
			break
		}
		segStart := 0
		for i := 0; i < len(chunk); i++ {
			if chunk[i] == 0x0a {
				var lineBytes []byte
				var lineStart int64
				if len(pending) > 0 {
					lineBytes = append(append(make([]byte, 0, len(pending)+i-segStart), pending...), chunk[segStart:i]...)
					lineStart = pendingStart
				} else {
					lineBytes = chunk[segStart:i]
					lineStart = filePos + int64(segStart)
				}
				processLine(lineBytes, lineStart)
				pending = pending[:0]
				state.pendingTruncated = false
				segStart = i + 1
				if maxGames > 0 && len(result) >= maxGames {
					return result
				}
			}
		}
		if len(pending) == 0 {
			pendingStart = filePos + int64(segStart)
		}
		if segStart < len(chunk) {
			remainder := chunk[segStart:]
			room := maxPendingBytes - len(pending)
			if len(remainder) > room {
				if room > 0 {
					pending = append(pending, remainder[:room]...)
				}
				state.pendingTruncated = true
			} else {
				pending = append(pending, remainder...)
			}
		}
		filePos += int64(len(chunk))
	}
	// 文件末尾最后一行（若非空）。
	if len(pending) > 0 {
		processLine(pending, pendingStart)
		state.pendingTruncated = false
	}
	if state.gameStart >= 0 {
		end := source.ByteLength()
		if end > state.gameStart {
			result = append(result, PgnGameIndex{
				Offset: state.gameStart,
				Length: end - state.gameStart,
				Event:  tagOrNil(state.gameTags["event"]),
				Red:    tagOrNil(state.gameTags["red"]),
				Black:  tagOrNil(state.gameTags["black"]),
			})
		}
	}
	return result
}

// tagOrNil 标签值空白归一为 null（TS nonEmpty）。
func tagOrNil(v string) *string {
	if t, ok := nonEmpty(v); ok {
		return &t
	}
	return nil
}
