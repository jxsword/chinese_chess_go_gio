package ui

// 语料下载客户端用例（T5'.1 修复锚定）：targetDir 空值必须解析为 corpusRoot
//（上游绑定层 App.CorpusDownload 的 a.corpusRoot("") 语义——验收实测：空串
// 直传复制物导致 staging 相对目录 + rename 空目标 ENOENT）。

import "testing"

func TestCorpusDownloaderResolveTargetDir(t *testing.T) {
	called := ""
	d := &CorpusDownloader{root: func() string { called = "resolved"; return "/corpus/root" }}
	if got := d.resolveTargetDir(""); got != "/corpus/root" || called != "resolved" {
		t.Fatalf("空 targetDir 应经 root() 解析，实际 %q", got)
	}
	if got := d.resolveTargetDir("/explicit"); got != "/explicit" {
		t.Fatalf("显式 targetDir 应原样直传，实际 %q", got)
	}
	if got := (&CorpusDownloader{}).resolveTargetDir(""); got != "" {
		t.Fatalf("无 root 注入时保持原值（测试场景），实际 %q", got)
	}
}
