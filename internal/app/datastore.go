package app

// 数据目录与存储装配（design_docs/07 §1/§4/§5，T2'.1）：复制物 internal/storage
// 零改动（铁律 #G2），仅由本层决定 Gio 版数据目录与文件名：
//   - 目录 = os.UserConfigDir()/chinese-chess-ultra-gio
//   - 库文件 = chinese_chess_ultra_gio.sqlite——与上游 Wails 应用共存时不互写库文件
//     （07 §1，同上游 M2 勘误逻辑）；
//   - 凭据回退文件 = 复制物常量 CredentialsFallbackFilename（07 §4，M4' 配置卡接入）。
// 懒打开（07 §1）：DAO 首次访问时建立；磁盘异常返回错误，调用方按
// "本地存储不可用"降级、新局兜底（沿上游语义）。

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

// dataDirName Gio 版数据目录名（07 §1；区别于上游应用目录，不互写库文件）。
const dataDirName = "chinese-chess-ultra-gio"

// dbFilename SQLite 库文件名（07 §1）。
const dbFilename = "chinese_chess_ultra_gio.sqlite"

// DataDir 返回 Gio 版数据目录（os.UserConfigDir()/chinese-chess-ultra-gio）。
func DataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("app: 用户配置目录不可用: %w", err)
	}
	return filepath.Join(base, dataDirName), nil
}

// DataStore 存储装配：设置/凭据即时装配，DAO 懒打开（07 §1）。
// DB() 可被主循环与后台保存 goroutine 并发调用，mu 保护懒初始化；
// 打开后的 *sql.DB 自身并发安全（复制物语义）。
type DataStore struct {
	dir      string
	mu       sync.Mutex
	dao      *storage.ChessDao
	daoErr   error
	settings *storage.Settings // nil = 设置存储不可用（读失败按默认值，07 §5）
	creds    *storage.Credentials
}

// OpenDataStore 装配数据目录（dir 为空串 = 目录不可用，全部降级：
// 设置按默认值、DAO 打开恒错、凭据不装配）。
func OpenDataStore(dir string) *DataStore {
	s := &DataStore{dir: dir}
	if dir == "" {
		return s
	}
	settings, err := storage.OpenSettings(dir)
	if err != nil {
		log.Println("app: 设置存储打开失败（按默认值降级）:", err)
	} else {
		s.settings = settings
	}
	s.creds = storage.NewCredentials(storage.OSKeyring{},
		filepath.Join(dir, storage.CredentialsFallbackFilename))
	return s
}

// Dir 数据目录（空串 = 不可用）。
func (s *DataStore) Dir() string { return s.dir }

// Settings 设置存储（nil = 不可用，07 §5 降级口径）。
func (s *DataStore) Settings() *storage.Settings { return s.settings }

// Credentials 凭据存储（keyring 主存 + 0600 明文回退，07 §4；M4' 配置卡接入）。
func (s *DataStore) Credentials() *storage.Credentials { return s.creds }

// DB 懒打开 DAO（07 §1）；打开失败记忆错误，后续调用直接返回同一错误。
func (s *DataStore) DB() (*storage.ChessDao, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dao == nil && s.daoErr == nil {
		if s.dir == "" {
			s.daoErr = fmt.Errorf("app: 数据目录不可用，本地存储降级")
		} else {
			s.dao, s.daoErr = storage.OpenDao(filepath.Join(s.dir, dbFilename))
			if s.daoErr != nil {
				log.Println("app: 存档库打开失败（本地存储不可用）:", s.daoErr)
			}
		}
	}
	return s.dao, s.daoErr
}

// Close 关闭已打开的 DAO（进程退出时调用；未打开为空操作）。
func (s *DataStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dao != nil {
		if err := s.dao.Close(); err != nil {
			log.Println("app: 存档库关闭异常:", err)
		}
		s.dao = nil
	}
}
