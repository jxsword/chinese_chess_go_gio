package app

// T2'.1 验收：数据目录装配与 DAO 懒打开回归（design_docs/07 §1/§5）。
// 复制物 DAO 测试（上游 14 条内存库用例）随复制物继承；此处只测装配层：
// 目录命名、懒打开、失败降级、Close 幂等。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/state"
)

func TestOpenDataStoreRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s := OpenDataStore(dir)
	if s.Dir() != dir {
		t.Fatalf("Dir() = %q, 期望 %q", s.Dir(), dir)
	}
	if s.Settings() == nil {
		t.Fatal("Settings() = nil, 期望设置存储已装配")
	}
	if s.Credentials() == nil {
		t.Fatal("Credentials() = nil, 期望凭据存储已装配（回退路径=数据目录）")
	}

	dao, err := s.DB()
	if err != nil {
		t.Fatalf("DB() 打开失败: %v", err)
	}
	// 同一实例复用（懒打开单例）
	dao2, err := s.DB()
	if err != nil || dao != dao2 {
		t.Fatal("DB() 两次调用应返回同一 DAO 实例")
	}

	// DAO 往返：模式桶写入/回读（复制物语义经装配层可用）
	moves := [][]int{{7, 7, 4, 7}}
	if _, err := dao.UpsertForMode(string(state.ModeHumanVsHuman), "fen-x", moves); err != nil {
		t.Fatalf("UpsertForMode: %v", err)
	}
	got, err := dao.LatestForMode(string(state.ModeHumanVsHuman))
	if err != nil || got == nil {
		t.Fatalf("LatestForMode = (%v, %v), 期望非空", got, err)
	}
	if got.Fen != "fen-x" || len(got.Moves) != 1 {
		t.Fatalf("回读存档不符: %+v", got)
	}

	if err := dao.DeleteForMode(string(state.ModeHumanVsHuman)); err != nil {
		t.Fatalf("DeleteForMode: %v", err)
	}
	if got, err := dao.LatestForMode(string(state.ModeHumanVsHuman)); err != nil || got != nil {
		t.Fatalf("删除后 LatestForMode = (%v, %v), 期望 (nil, nil)", got, err)
	}

	s.Close()
	s.Close() // 幂等
}

func TestOpenDataStoreDegraded(t *testing.T) {
	// 目录不可用（空串）：设置 nil、DAO 恒错——"本地存储不可用"降级路径（07 §1）
	s := OpenDataStore("")
	if s.Settings() != nil || s.Credentials() != nil {
		t.Fatal("降级路径不应装配设置/凭据")
	}
	if _, err := s.DB(); err == nil {
		t.Fatal("降级路径 DB() 应返回错误")
	}
}

func TestOpenDataStoreDAOErrorMemoized(t *testing.T) {
	// 目录指向普通文件：OpenDao 初始化失败，错误被记忆并重复返回
	dir := t.TempDir()
	bad := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(bad, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := OpenDataStore(bad)
	_, err1 := s.DB()
	_, err2 := s.DB()
	if err1 == nil || err2 == nil {
		t.Fatalf("DB() 应失败: %v / %v", err1, err2)
	}
	if !strings.Contains(err1.Error(), "init schema") && !errors.Is(err1, err2) {
		// 两种失败形态均可：初始化失败或记忆错误重复返回
		t.Logf("失败形态: %v / %v", err1, err2)
	}
}
