# build/ 打包资产（M7' T7'.2，design_docs/10 §4 定案）

单二进制为基线（Gio 核心优势，上游 K35 的根治）；工具全部不进 go.mod（依赖白名单哲学，上游-DR-010 同构）。

## 资产清单

| 文件 | 用途 |
|---|---|
| `appicon.png` | 1024 应用图标（自上游同源复用；macOS icns 由 CI 用 sips/iconutil 生成） |
| `nfpm.yaml` | Linux deb 打包配置（nfpm，不写 depends 硬绑） |
| `linux/chinese-chess-ultra-gio.desktop` | 桌面入口（deb + AppImage 共用） |
| `linux/chinese-chess-ultra-gio-512.png` | linuxdeploy 只接受标准分辨率图标的 512 副本 |
| `linux/build-appimage.sh` | AppImage 组装（linuxdeploy，自动下载到 tmp/） |
| `darwin/Info.plist` | .app bundle 手工组装的静态清单 |

## 本地打包（以 Linux 为例）

```bash
go build -ldflags "-X main.version=1.0.0" -o build/bin/chinese-chess-ultra-gio .
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.1   # 工具，不进 go.mod
nfpm package -f build/nfpm.yaml -p deb \
  -t build/bin/chinese-chess-ultra-gio_1.0.0_amd64.deb
./build/linux/build-appimage.sh 1.0.0
tar -czf build/bin/chinese-chess-ultra-gio-1.0.0-linux-amd64.tar.gz -C build/bin chinese-chess-ultra-gio
```

macOS（.app 手工组装 + hdiutil dmg）与 Windows（裸 exe）步骤见 `.github/workflows/release.yml`（tag `v*` 触发的三平台矩阵即权威流程）。

## 产物矩阵（release.yml）

| 平台 | 产物 |
|---|---|
| Linux | `chinese-chess-ultra-gio_<ver>_amd64.deb` + `chinese-chess-ultra-gio-<ver>-linux-amd64.AppImage` + `chinese-chess-ultra-gio-<ver>-linux-amd64.tar.gz`（兜底） |
| Windows | `chinese-chess-ultra-gio-<ver>-windows-amd64.exe`（裸 exe 基线；NSIS 候选待裁决定案） |
| macOS | `chinese-chess-ultra-gio-<ver>-macos-universal.dmg`（amd64+arm64 lipo → .app → hdiutil UDZO） |

## Windows 裸 exe 使用说明（基线形态）

1. 下载 `chinese-chess-ultra-gio-<ver>-windows-amd64.exe`，放到任意目录双击运行；
2. 无需安装运行时（Gio Windows 后端纯 Go，无 WebView2 依赖）；
3. 首次运行 Windows SmartScreen 可能提示未签名——点"更多信息 → 仍要运行"；
4. LLM 功能的 API Key 经 OS 凭据库（Windows 凭据管理器）落盘，无 keyring 环境回退为同目录 0600 明文文件。
