package app

// 语料目录装配（T5'.1，06 文档 §1 + 上游 app.go corpusRoot/documentsDir 同款）：
// 当前生效语料目录按 用户设置(corpus.userPath) > legacy 相对目录 > 平台默认
// <Documents>/ChineseChessUltra/corpus 解析（storage.ResolveCorpusDir）。

import (
	"os"
	"path/filepath"

	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
	"github.com/jxsword/chinese_chess_go_gio/internal/ui"
)

// corpusUserPathKey 语料目录设置键（07 文档 §3 corpus.userPath）。
const corpusUserPathKey = "corpus.userPath"

// corpusRoot 解析当前生效语料目录（可能尚不存在，由引导下载/手动放置创建）。
func (w *Window) corpusRoot() string {
	user := ""
	if w.store != nil && w.store.Settings() != nil {
		if v, ok := w.store.Settings().Get(corpusUserPathKey).(string); ok {
			user = v
		}
	}
	legacy, _ := os.Getwd()
	return storage.ResolveCorpusDir(storage.CorpusDirOptions{
		UserSetting:    user,
		DocumentsPath:  documentsDir(),
		LegacyBasePath: legacy,
	})
}

// documentsDir 文档目录（对齐 Electron app.getPath('documents') 语义；上游
// app.go documentsDir 同款：WSL 默认无 ~/Documents 时创建，失败退回主目录）。
func documentsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	docs := filepath.Join(home, "Documents")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		return home
	}
	return docs
}

// corpusEnv 构造语料页环境（T5'.1：ui 不 import app 的解耦点）。
func (w *Window) corpusEnv() ui.CorpusEnv {
	return ui.CorpusEnv{
		Emit:         w.emitFunc(),
		Cancel:       w.Cancel,
		NewRequestID: newRequestID,
		Root:         w.corpusRoot,
		Settings:     w.store.Settings(),
	}
}
