# 铭杰终端机 MJ Terminal

便携式个人终端：在公用/共享电脑上提供隔离浏览器 + 节点出口，不改动宿主机的系统网络设置。双击启动后守护进程在后台常驻，关闭桌面不影响连接；在系统内「设置 → 退出系统」才会完全停止。

## 下载（64 位 Windows）

到本仓库的 **Releases** 页面下载 `MJ-System-64bit.zip`，解压后运行 `MJ-System.exe` 即可，第一次运行会自动释放内置核心。本仓库为私有仓库，下载前先登录本账号。

## 应用内更新（v0.6 起）

程序自带 OTA：`设置 → 软件更新`。仓库是私有的，首次使用需粘贴一次只读令牌（GitHub → Settings → Developer settings → Fine-grained tokens → 新建：仓库仅选 MJ-Terminal，权限仅 Contents 只读）。之后守护进程会在启动时静默检查新版并预下载，在设置里点「重启并更新」即可完成升级（节点、数据、壁纸都不动）。构建时会同时发布 `update.json`（版本/大小/SHA-256）供程序校验更新包。

## 自动构建

源码推送到 `main` 后，GitHub Actions 自动完成构建并更新 Release 中的 `MJ-System-64bit.zip` 与 `update.json`：

- Go 构建单文件 EXE（内嵌桌面页面与 sing-box 核心）
- sing-box 使用官方 1.14.4 版 `windows-amd64-legacy-windows-7` 构建，构建时自动从 SagerNet 官方 Release 下载并校验嵌入
