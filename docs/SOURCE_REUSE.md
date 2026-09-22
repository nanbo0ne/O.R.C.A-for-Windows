# 从此源码创建新项目 / Reusing this source

此包是 O.R.C.A. Desktop 3.0.12 发布提交的源码，不是安装器或用户数据备份。归档只含已提交文件，不包含 `.git`、用户对话、本机配置、密钥、模型权重、依赖安装目录或编译缓存。提交编号与 ZIP 的 SHA-256 见包外同名交付记录；依赖需要联网安装。

This is the source at the Desktop 3.0.12 release commit, not an installer or user-data backup. The accompanying delivery record identifies the commit and ZIP digest. Dependencies must be installed separately.

## 本地构建 / Build

1. 安装 `go.mod` 指定的 Go、Node.js 22、npm 和 Wails v2.12.0。按 `desktop/README.md` 安装目标系统依赖。
2. 在 `desktop/frontend` 执行 `npm ci`、`npm run test:all`、`npm run build`。
3. 根目录执行 `go test ./... -p=1`；`desktop` 中执行 `go test .`、`wails build`。Windows 安装器另需 NSIS。
4. 正式打包流程见 `docs/build/desktop-v3.0.12.md` 与 `.github/workflows/release-desktop.yml`。发布工作流依赖你自己的仓库权限及签名 secrets，源码包不含这些凭据。

Install the declared Go version, Node.js 22 and Wails v2.12.0. Run frontend installation/tests/build, root and desktop Go tests, then `wails build` in `desktop`. Native packaging dependencies and release secrets are separate.

## 新项目必须单独设置 / New-project configuration

- 在自己的目录初始化新 Git 仓库。不要把它指向原项目的发布远端。
- 根据实际新产品名称更改 `desktop/wails.json`、Windows 资源、安装器应用标识、图标、Go module 路径及对应 imports；不要仅盲目替换显示名称。
- 为配置、会话、缓存、单实例和安装器退出通道设立独立标识与目录，避免覆盖已安装的 O.R.C.A. 数据。
- 更换内置更新源、GitHub Release 仓库、Minisign 公钥和签名私钥。测试前禁用原产品发布工作流，防止下载或推送错误产品。
- 保留明确禁用的 Computer Use 和托管本地 AI 状态，除非新项目单独完成相关权限与平台验收。
- 不携带真实 API Key 或个人配置。用户在新应用内单独配置供应商。

Create a new repository and independent product, storage, IPC and installer identities. Replace update origins and signing keys, and review workflow destinations before enabling releases. Do not bundle personal API keys. Disabled features require their own validation before enabling.

## 许可 / Licensing

项目根 `LICENSE` 为 MIT；再分发或用于新项目时保留版权及许可声明。第三方代码、资源、字体、运行时依赖和模型可能采用不同条款，参阅 `THIRD-PARTY-NOTICES.txt` 及各组件许可证。MIT 不代表所有第三方依赖都采用 MIT，也不要求新项目沿用 O.R.C.A. 品牌。

Keep the MIT copyright/license notice and review third-party notices and component licenses separately. Third-party assets, runtimes and models may have different terms.
