# 铭杰终端机 MJ Terminal

便携式个人终端：在公用/共享电脑上提供隔离浏览器 + 节点出口，不改动宿主机的系统网络设置。双击启动后守护进程在后台常驻，关闭桌面不影响连接；在系统内「设置 → 退出系统」才会完全停止。

## 下载（64 位 Windows）

到本仓库的 **Releases** 页面下载 `MJ-System-64bit.zip`，解压后运行 `MJ-System.exe` 即可，第一次运行会自动释放内置核心。本仓库为私有仓库，下载前先登录本账号。

## 自动构建

源码推送到 `main` 后，GitHub Actions 自动完成构建并更新 Release 中的 `MJ-System-64bit.zip`：

- Go 构建单文件 EXE（内嵌桌面页面与 sing-box 核心）
- sing-box 使用官方 1.14.4 版 `windows-amd64-legacy-windows-7` 构建，构建时自动从 SagerNet 官方 Release 下载并校验嵌入
