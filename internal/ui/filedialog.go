package ui

// 文件对话框（T5'.4，D-006 定案：系统命令方案，零 go.mod 依赖；翻译锚点 =
// 上游 App.CorpusPickDirectory / api.dialog.saveFile 语义）。
//
// 平台分支：Linux zenity（kdialog 兜底）、Windows FolderBrowserDialog
// （powershell，KG-004 管道同型外呼）、macOS osascript。取消 = 空串输出
//（上游"取消返回空串"语义）；I/O 在后台 goroutine，回执经事件总线
//（00 §4 `dialog:pickdir`/`dialog:savefile`，铁律 #G3）。

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// errDialogUnavailable 无可用对话框工具（降级页面内路径输入——D-005 C 兜底）。
var errDialogUnavailable = errors.New("ui: 无可用对话框工具")

// dialogCandidates 按平台的对话框命令（惰性探测）。
func pickDirectoryCmd() (*exec.Cmd, func(output string) string, error) {
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("zenity"); err == nil {
			return exec.Command("zenity", "--file-selection", "--directory"),
				func(out string) string { return strings.TrimSpace(out) }, nil
		}
		if _, err := exec.LookPath("kdialog"); err == nil {
			return exec.Command("kdialog", "--getexistingdirectory", string(os.Getenv("HOME"))),
				func(out string) string { return strings.TrimSpace(out) }, nil
		}
	case "darwin":
		return exec.Command("osascript", "-e", "POSIX path of (choose folder)"),
			func(out string) string { return strings.TrimSpace(out) }, nil
	case "windows":
		script := "Add-Type -AssemblyName System.Windows.Forms; " +
			"$f = New-Object System.Windows.Forms.FolderBrowserDialog; " +
			"if ($f.ShowDialog() -eq 'OK') { $f.SelectedPath }"
		return exec.Command("powershell", "-NoProfile", "-Command", script),
			func(out string) string { return strings.TrimSpace(out) }, nil
	}
	return nil, nil, errDialogUnavailable
}

// PickDirectoryAsync 目录选择对话框（阻塞 I/O 在后台 goroutine；done 调用一次）。
// 取消/关闭 = ("" , nil)——上游 CorpusPickDirectory"取消返回空串"语义。
func PickDirectoryAsync(done func(dir string, err error)) {
	if done == nil {
		return
	}
	go func() {
		cmd, parse, err := pickDirectoryCmd()
		if err != nil {
			done("", err)
			return
		}
		hideWindow(cmd)
		out, err := cmd.Output()
		if err != nil {
			// zenity 取消=exit 1 + 空输出：视作取消（非错误）。
			if _, ok := err.(*exec.ExitError); ok && len(parse(string(out))) == 0 {
				done("", nil)
				return
			}
			done("", err)
			return
		}
		dir := parse(string(out))
		if dir != "" {
			if st, serr := os.Stat(dir); serr != nil || !st.IsDir() {
				done("", fmt.Errorf("所选目录不可读: %s", dir))
				return
			}
		}
		done(dir, nil)
	}()
}

// SaveFileAsync 保存文件对话框（PGN 导出；defaultName + content 落盘；
// done 的 savedPath 为空 = 用户取消——上游 api.dialog.saveFile 语义）。
func SaveFileAsync(defaultName, content string, done func(savedPath string, err error)) {
	if done == nil {
		return
	}
	go func() {
		cmd, parse, err := saveFileCmd(defaultName)
		if err != nil {
			done("", err)
			return
		}
		hideWindow(cmd)
		out, err := cmd.Output()
		if err != nil {
			if _, ok := err.(*exec.ExitError); ok && len(parse(string(out))) == 0 {
				done("", nil) // 取消
				return
			}
			done("", err)
			return
		}
		path := parse(string(out))
		if path == "" {
			done("", nil)
			return
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			done("", err)
			return
		}
		done(filepath.Clean(path), nil)
	}()
}

// saveFileCmd 保存对话框命令（Linux zenity --save --confirm-overwrite；
// Windows SaveFileDialog；macOS ossascript）。
func saveFileCmd(defaultName string) (*exec.Cmd, func(output string) string, error) {
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("zenity"); err == nil {
			return exec.Command("zenity", "--file-selection", "--save",
					"--confirm-overwrite", "--filename", defaultName),
				func(out string) string { return strings.TrimSpace(out) }, nil
		}
		if _, err := exec.LookPath("kdialog"); err == nil {
			return exec.Command("kdialog", "--getsavefilename", defaultName, "*.pgn"),
				func(out string) string { return strings.TrimSpace(out) }, nil
		}
	case "darwin":
		return exec.Command("osascript", "-e",
				fmt.Sprintf("POSIX path of (choose file name default name %q default location (path to desktop))", defaultName)),
			func(out string) string { return strings.TrimSpace(out) }, nil
	case "windows":
		script := "Add-Type -AssemblyName System.Windows.Forms; " +
			"$f = New-Object System.Windows.Forms.SaveFileDialog; " +
			fmt.Sprintf("$f.FileName = '%s'; $f.Filter = 'PGN (*.pgn)|*.pgn'; ", defaultName) +
			"if ($f.ShowDialog() -eq 'OK') { $f.FileName }"
		return exec.Command("powershell", "-NoProfile", "-Command", script),
			func(out string) string { return strings.TrimSpace(out) }, nil
	}
	return nil, nil, errDialogUnavailable
}

// hideWindow Windows 下隐藏对话框控制台宿主窗（非 Windows no-op）。
func hideWindow(cmd *exec.Cmd) {
	if runtime.GOOS == "windows" && cmd.SysProcAttr == nil {
		cmd.SysProcAttr = hideWindowAttr()
	}
}
