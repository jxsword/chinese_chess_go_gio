package app

// WSLg 检测单测（决策记录 D-002：KG-005 标题绕过的探测口径）。

import (
	"errors"
	"os"
	"testing"
)

func TestUnderWSLg(t *testing.T) {
	cases := []struct {
		name  string
		probe envProbe
		want  bool
	}{
		{
			name: "/mnt/wslg 存在 → 命中",
			probe: envProbe{
				stat:        func(string) (os.FileInfo, error) { return nil, nil },
				procVersion: func() ([]byte, error) { return []byte("Linux version 6.6"), nil },
			},
			want: true,
		},
		{
			name: "/mnt/wslg 缺失但 /proc/version 含 microsoft → 兜底命中",
			probe: envProbe{
				stat:        func(string) (os.FileInfo, error) { return nil, errors.New("not found") },
				procVersion: func() ([]byte, error) { return []byte("Linux version 5.15-microsoft-standard-WSL2"), nil },
			},
			want: true,
		},
		{
			name: "原生 Linux → 不命中",
			probe: envProbe{
				stat:        func(string) (os.FileInfo, error) { return nil, errors.New("not found") },
				procVersion: func() ([]byte, error) { return []byte("Linux version 6.6.114 generic"), nil },
			},
			want: false,
		},
		{
			name: "两路探测均失败 → 不命中",
			probe: envProbe{
				stat:        func(string) (os.FileInfo, error) { return nil, errors.New("boom") },
				procVersion: func() ([]byte, error) { return nil, errors.New("boom") },
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := underWSLg(tc.probe); got != tc.want {
				t.Fatalf("underWSLg = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWindowTitle(t *testing.T) {
	wslg := envProbe{
		stat:        func(string) (os.FileInfo, error) { return nil, nil },
		procVersion: func() ([]byte, error) { return nil, errors.New("n/a") },
	}
	native := envProbe{
		stat:        func(string) (os.FileInfo, error) { return nil, errors.New("not found") },
		procVersion: func() ([]byte, error) { return []byte("Linux generic"), nil },
	}
	if got := windowTitle(wslg); got != "Chinese Chess Ultra (Gio)" {
		t.Fatalf("WSLg 标题 = %q, want ASCII 绕过", got)
	}
	if got := windowTitle(native); got != "中国象棋 Ultra（Gio 版）" {
		t.Fatalf("原生环境标题 = %q, want 中文", got)
	}
}
