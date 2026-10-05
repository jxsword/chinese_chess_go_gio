package storage

// 最小 ZIP 读取器（主进程专用，06 文档 §5 解压安全设计的载体；
// 等价翻译 Electron 版 src/main/services/corpusZip.ts）。
//
// 为什么不手写解析：zip 解压只用到「标准无加密条目 + store/deflate」，Go
// 标准库 archive/zip 已封装 EOCD/中央目录/local header 解析与 CRC 校验，
// 不必为此引入新依赖（AGENTS 依赖纪律）；同时把 zip-slip 防护收敛在调用方
// corpusDownloader 的同一遍历里（zip.EntryLite 的判定字段与本文件等价）。
//
// 能力：store(0)/deflate(8)、符号链接条目识别（Unix mode 位）、加密条目拒绝。
// 不支持特性（加密/分卷）遇到即报 ZipFormatError（语料包场景不可能出现）。
// 单条目内容解出失败（CRC/截断）不中止整体：经 content 的 err 参数交调用方
// 跳过并计数（对齐 TS 版逐条目 try/catch 语义；Go 侧因标准库校验 CRC，
// 坏条目被跳过而 TS 照写坏数据——刻意加强，见 KNOWN_ISSUES M5）。

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
)

// ZipEntryLite 单个 zip 条目（名称 / 是否目录 / 是否符号链接）。
type ZipEntryLite struct {
	Name        string
	IsDirectory bool
	IsSymlink   bool
}

// ZipFormatError 解析失败（非 zip / 截断 / 不支持特性）。
var ZipFormatError = errors.New("ZIP 格式错误")

// zipEntries 遍历 zip 条目（中央目录顺序）。目录条目产出 ZipEntryLite（IsDirectory，
// data 为 nil）；符号链接条目产出 ZipEntryLite（IsSymlink）；文件条目经 content
// 回调解出内容。entryErr 非 nil 表示该条目内容读取失败（CRC/截断），调用方应
// 跳过并继续。content 返回错误时中止整体遍历（调用方主动中止）。
// 格式级错误（加密/不支持方法）以返回错误中止整体（对应 TS readZipEntries 抛错）。
func zipEntries(zipPath string, content func(entry ZipEntryLite, data []byte, entryErr error) error) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("%w: %v", ZipFormatError, err)
	}
	defer reader.Close()
	for _, f := range reader.File {
		entry := ZipEntryLite{
			Name:        f.Name,
			IsDirectory: f.FileInfo().IsDir(),
			IsSymlink:   f.Mode()&os.ModeSymlink != 0,
		}
		if f.Flags&0x1 != 0 {
			return fmt.Errorf("%w: 条目已加密，不支持", ZipFormatError)
		}
		switch f.Method {
		case zip.Store, zip.Deflate:
		default:
			return fmt.Errorf("%w: 压缩方法不支持: %d", ZipFormatError, f.Method)
		}
		if entry.IsDirectory || entry.IsSymlink {
			if err := content(entry, nil, nil); err != nil {
				return err
			}
			continue
		}
		rc, err := f.Open()
		var data []byte
		entryErr := error(nil)
		if err != nil {
			entryErr = err
		} else {
			data, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				data, entryErr = nil, err
			}
		}
		if err := content(entry, data, entryErr); err != nil {
			return err
		}
	}
	return nil
}
