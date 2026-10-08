package app

// 对局页 repo 异步代理（T2'.2，design_docs/07 §2 落地口径）：
// 把 state.GameRepo / ui.GameDB 的同步签名落到 DataStore 的 DAO I/O 上，
// I/O 一律后台 goroutine（铁律 #G3）；结果经事件总线回主循环。
//
//   - SaveGame（自动保存路径）：fire-and-forget（K3）——错误记日志 + DbSaveDone
//     回执（界面状态条提示，不吞错、不阻塞交互）；
//   - SaveAsync（手动保存）：回执 Manual=true，成功/失败均提示；
//   - SaveSync（关闭路径）：主循环内同步写——K16"300ms best-effort 有界等待"的
//     Go 对应（本地 sqlite 单行写完成或失败即返回），唯一主循环 I/O 例外；
//   - LoadLatest（同步签名）：仅测试锚点/内部复用；页面取档走 LoadLatestAsync；
//   - DeleteForMode（死局清理）：fire-and-forget。

import (
	"log"
	"sync"

	"github.com/jxsword/chinese_chess_go_gio/internal/state"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
	"github.com/jxsword/chinese_chess_go_gio/internal/ui"
)

// gameRepo repo 异步代理（同时满足 state.GameRepo 与 ui.GameDB）。
type gameRepo struct {
	store *DataStore
	emit  func(requestID string, payload any, err error)
	wg    *sync.WaitGroup // 后台 I/O 计数（进程退出前 Wait 收口）
}

func newGameRepo(store *DataStore, emit func(requestID string, payload any, err error), wg *sync.WaitGroup) *gameRepo {
	return &gameRepo{store: store, emit: emit, wg: wg}
}

// goIO 后台 I/O 计数启动（调用方持 wg；退出前 Wait 收口——M7' 维护轮）。
func (r *gameRepo) goIO(fn func()) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		fn()
	}()
}

// ---- state.GameRepo ----

func (r *gameRepo) SaveGame(mode state.GameMode, fen string, moves [][]int) error {
	r.goIO(func() {
		if err := r.saveSync(mode, fen, moves); err != nil {
			log.Println("app: 自动保存失败:", err)
			r.emit("", ui.DbSaveDone{Mode: mode, Err: err}, nil)
		}
	})
	return nil
}

func (r *gameRepo) LoadLatest(mode state.GameMode) (*storage.SavedGame, error) {
	dao, err := r.store.DB()
	if err != nil {
		return nil, err
	}
	return dao.LatestForMode(string(mode))
}

func (r *gameRepo) DeleteForMode(mode state.GameMode) error {
	r.goIO(func() {
		dao, err := r.store.DB()
		if err != nil {
			log.Println("app: 死局存档清理失败:", err)
			return
		}
		if err := dao.DeleteForMode(string(mode)); err != nil {
			log.Println("app: 死局存档清理失败:", err)
		}
	})
	return nil
}

// ---- ui.GameDB ----

func (r *gameRepo) LoadLatestAsync(requestID string, mode state.GameMode) {
	r.goIO(func() {
		saved, err := r.LoadLatest(mode)
		if err != nil {
			log.Println("app: 取档失败（开新局降级）:", err)
		}
		r.emit(requestID, ui.DbLoadDone{Mode: mode, Saved: saved}, err)
	})
}

func (r *gameRepo) SaveAsync(requestID string, mode state.GameMode, fen string, moves [][]int, manual bool) {
	r.goIO(func() {
		err := r.saveSync(mode, fen, moves)
		if err != nil {
			log.Println("app: 保存棋局失败:", err)
		}
		r.emit(requestID, ui.DbSaveDone{Mode: mode, Manual: manual, Err: err}, nil)
	})
}

func (r *gameRepo) SaveSync(mode state.GameMode, fen string, moves [][]int) error {
	return r.saveSync(mode, fen, moves)
}

// saveSync 同步写模式桶（内部原语）。
func (r *gameRepo) saveSync(mode state.GameMode, fen string, moves [][]int) error {
	dao, err := r.store.DB()
	if err != nil {
		return err
	}
	_, err = dao.UpsertForMode(string(mode), fen, moves)
	return err
}

// ---- ui.RecordRepo（T5'.3 记录库，00 §4 `cc:db:records*` 行；
// D-004 勘误：棋谱库入口=记录库页，写入面=对局页"保存为棋谱"）----

// RecordsListAsync 记录列表（后台读 → ui.RecordsListDone）。
func (r *gameRepo) RecordsListAsync(requestID string) {
	r.goIO(func() {
		dao, err := r.store.DB()
		if err != nil {
			r.emit(requestID, ui.RecordsListDone{RequestID: requestID, Records: []storage.GameRecordSummary{}}, nil)
			return
		}
		records, err := dao.RecordSummaries()
		if err != nil {
			log.Println("app: 记录列表读取失败:", err)
			r.emit(requestID, ui.RecordsListDone{RequestID: requestID, Records: []storage.GameRecordSummary{}}, nil)
			return
		}
		r.emit(requestID, ui.RecordsListDone{RequestID: requestID, Records: records}, nil)
	})
}

// RecordsGetAsync 单条记录 → ui.RecordGetDone（Err 非 nil = 读取失败）。
func (r *gameRepo) RecordsGetAsync(requestID string, id int64) {
	r.goIO(func() {
		dao, err := r.store.DB()
		if err != nil {
			r.emit(requestID, ui.RecordGetDone{RequestID: requestID, Err: err}, nil)
			return
		}
		record, err := dao.RecordByID(id)
		if err != nil {
			log.Println("app: 记录读取失败:", err)
			r.emit(requestID, ui.RecordGetDone{RequestID: requestID, Err: err}, nil)
			return
		}
		r.emit(requestID, ui.RecordGetDone{RequestID: requestID, Record: record}, nil)
	})
}

// RecordsSaveAsync 写入棋谱记录 → ui.RecordSaveDone（成功/失败均回执）。
func (r *gameRepo) RecordsSaveAsync(requestID string, record state.GameRecordData) {
	r.goIO(func() {
		dao, err := r.store.DB()
		if err != nil {
			r.emit(requestID, ui.RecordSaveDone{RequestID: requestID, Err: err}, nil)
			return
		}
		st := state.RecordDataToStorage(record)
		if _, err := dao.InsertRecord(&st); err != nil {
			log.Println("app: 保存棋谱失败:", err)
			r.emit(requestID, ui.RecordSaveDone{RequestID: requestID, Err: err}, nil)
			return
		}
		r.emit(requestID, ui.RecordSaveDone{RequestID: requestID}, nil)
	})
}

// RecordsDeleteAsync 删除棋谱 → ui.RecordDeleteDone。
func (r *gameRepo) RecordsDeleteAsync(requestID string, id int64) {
	r.goIO(func() {
		dao, err := r.store.DB()
		if err != nil {
			r.emit(requestID, ui.RecordDeleteDone{RequestID: requestID, Err: err}, nil)
			return
		}
		err = dao.DeleteRecord(id)
		if err != nil {
			log.Println("app: 删除棋谱失败:", err)
		}
		r.emit(requestID, ui.RecordDeleteDone{RequestID: requestID, Err: err}, nil)
	})
}
