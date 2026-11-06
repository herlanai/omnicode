# Omnicode

一个安静、专注的原生代码编辑器。采用 Codex 风格的浅色布局，以 Golet 的原生 UI、Forma 组件、代码编辑器和 Markdown 渲染器构建。应用代码全部为 Go，不使用 WebView、Electron、HTML 或 JavaScript。

## 运行

需要 Go 1.27.3+，并在同级目录放置 [Golet](https://github.com/HyperparameterSI/golet) 源码：

```text
workspace/
├── golet/
└── omnicode/
```

当前 `go.mod` 使用 `replace github.com/HyperparameterSI/golet => ../golet`。本版验证所用 Golet 提交为 `efa603f5aca8233d958755581ad68c866e840ecb`。首次安装可执行：

```sh
git clone https://github.com/HyperparameterSI/golet ../golet
git -C ../golet checkout efa603f5aca8233d958755581ad68c866e840ecb
go mod download
go run .
```

已有 Golet 工作目录时请保留其修改，无需重新克隆或切换提交。其他位置可通过 `go mod edit -replace=github.com/HyperparameterSI/golet=/absolute/path/to/golet` 指定。

```sh
go run . /path/to/project                    # 打开项目
go run . /path/to/project /path/to/main.go   # 同时打开文件
make build                                 # bin/omnicode
make bundle                                # macOS: .app + bin/Omnicode-macOS-arm64.dmg
make test
```

macOS 原生窗口使用 Golet 的 GPU 渲染。纯 Go 构建不要求 Node.js 或 CGO。当前已在 macOS arm64 上构建、测试；其他平台尚未验证。

## 已实现

- 可调整宽度、可隐藏的文件树；目录按需展开，手动刷新。
- 文件树右键菜单：新建文件/文件夹、重命名、复制、剪切、粘贴、复制绝对/相对路径、在文件管理器中显示、移到废纸篓。根目录和空白区也支持新建与粘贴。
- 复制/剪切使用应用内部文件剪贴板，支持文件夹；遇到同名目标会拒绝覆盖。移动和重命名同步已打开文件的路径；移到废纸篓前保存待写入修改，保存失败则停止操作。
- 多标签编辑、未保存标记、关闭标签或窗口时的保存确认。
- 原生代码编辑器：语法高亮、行号、缩进参考线、括号配对、多光标、折叠、撤销重做、查找替换。
- 快速打开：按名称或相对路径筛选，Enter 打开首项，点击打开其他结果。
- 独立的原生 Markdown 编辑与实时分栏预览，可调整左右比例。
- 独立图片查看器：PNG、JPEG、GIF（首帧）、WebP、BMP、TIFF、SVG，支持缩放、100% 和适应窗口。
- 独立 PDF 阅读器：macOS Core Graphics 渲染，上一页/下一页、缩放和适应窗口。当前为页面图片预览，暂不支持文本选择、搜索、批注和密码解锁。单页渲染最长边限制为 1800 像素。
- 图片与 PDF 只读，不会进入自动保存流程。媒体文件上限 128 MiB，位图上限 4000 万像素；HEIC、AVIF、ICO 等暂不支持。
- Golet 原生终端支持最多 16 个独立 Shell，每个标签可上下或左右分为两个窗格。Ctrl+反引号显示或隐藏终端，隐藏时命令继续运行。点 + 从当前项目新建标签，文件树右键可指定启动目录。窗格菜单支持分屏、重命名、清屏、重启、关闭及把当前屏幕文字带入 AI 草稿。放大镜可搜索屏幕和滚动历史。
- 终端使用 Golet 自带的 libghostty-vt（动态加载、无 CGO）；`make bundle` 将运行库复制进应用，成品不依赖首次联网下载。
- 打开、自动保存、另存为；已有文件停止输入 750 ms 后自动保存，关闭前立即保存；新文件使用 ⌘ S 首次选择路径。保存失败保留修改并在状态栏提示，不会反复重试同一版本。同目录临时文件加原子替换，保留权限和 CRLF/LF，检测外部修改并拒绝覆盖。
- Go 源码格式化、自动换行、光标位置和编码状态。
- 系统文件对话框、应用菜单和快捷键。

## 快捷键

macOS 使用 ⌘，Windows / Linux 使用 Ctrl。

| 操作 | 快捷键 |
| --- | --- |
| 打开文件 / 文件夹 | ⌘ O / ⌘ Shift O |
| 新建文件 | ⌘ N |
| 保存 / 另存为 | ⌘ S / ⌘ Shift S |
| 关闭标签 | ⌘ W |
| 快速打开 | ⌘ P |
| 显示或隐藏侧栏 | ⌘ B |
| 查找 | ⌘ F |
| Markdown 预览 | ⌘ Shift V |
| Go 格式化 | Alt Shift F |
| 撤销 / 重做 | ⌘ Z / ⌘ Shift Z |

文件树底部的换行图标可切换自动换行。查找替换、多光标等编辑命令由 Golet 原生编辑器提供，也可通过编辑区右键菜单访问。

## 验证与原生截图

```sh
go test ./...
go vet ./...
go run . -screenshot artifacts/editor.png . main.go
go run . -preview -screenshot artifacts/markdown.png . README.md
```

`-screenshot` 使用同一份原生 UI 的软件渲染器输出 PNG，不启动浏览器。输出目录需要预先创建。测试覆盖编辑和撤销、快速打开、Markdown 预览、侧栏、标签关闭，以及自动保存防抖、后台标签保存、保存冲突、CRLF、权限、符号链接、二进制和大文件拒绝。

## 当前范围

以下为基础文件编辑约束；后续版本功能见各节。文件内容支持 UTF-8，单文件上限 8 MiB；快速打开最多索引 20,000 个文件并显示 100 个结果，跳过 `.git`、`node_modules`、`vendor` 等目录。目录更改后点击刷新更新索引；文件被外部修改时保存会报错，需另存为或先处理未保存修改再重新打开。

## 代码结构

- `main.go`：窗口生命周期、原生菜单、命令行和截图入口。
- `app.go`：工作区状态、文件树、标签页、快捷操作和布局。
- `viewer.go`、`viewer_text.go`、`viewer_pdf_*.go`：文件类型分流与独立查看器。
- `terminal_panel.go`：原生 PTY 终端与分栏布局。
- `workspace.go`：文档加载、保存、文件树与快速索引。
- `fileops.go`：文件树右键菜单、命名对话框与文件操作。
- `fileops_test.go`：菜单交互、新建、重命名、复制移动和废纸篓流程测试。
- `icons.go`：小型线框 SVG 图标，由原生绘图引擎绘制。
- `workspace_test.go`：磁盘安全和原生交互测试。

工具栏设计约定：优先使用克制的线框图标，说明放在悬停提示中，同时保留无障碍名称。macOS 打包从全新目录生成并进行本地签名，生成 DMG 安装包，再挂载校验包内的签名、图标与 Applications 快捷方式。打开 DMG 后将 Omnicode 拖入 Applications 即可安装。

## 最近项目与本地数据库

欢迎页显示最近打开的项目，点击项目行重新打开。按最近打开时间排序、路径去重，最多保留 20 项；列表可滚动。右侧移除图标仅删除历史记录，不删除磁盘项目。路径失效时提示错误，仍可移除记录。

使用 Golet 内置 SQLite 插件（动态加载、无 CGO），数据库在 macOS 的 `~/Library/Application Support/Omnicode/cache.sqlite`。当前表为 `recent_projects`，后续缓存可以添加独立表。首次运行新版后开始记录，旧版未持久化的历史无法自动恢复。数据库不可用时编辑器继续工作并显示提示。测试和截图不会写入用户数据库。

SQLite 与终端运行库均随 `.app` 打包，不需要额外安装数据库或联网下载。

## Office 与数据文件

- **XLSX**：独立原生表格视图，切换工作表、按文本筛选行、复制表格。点击单元格后在上方输入内容，按 Enter 或点击勾选图标应用并写入文件；这是明确提交操作，未按 Enter 的输入仅为草稿。数字自动作为数值写入，以 `=` 开头输入公式；以英文单引号开头强制按文本写入（例如 `'00123`）。修改使用同目录临时文件和原子替换，并检查外部修改冲突。
- **XLSM / XLTX**：只读工作表数据浏览，不运行宏。
- **CSV / TSV**：UTF-8 表格浏览、筛选和复制，支持带引号的换行字段；当前只读。
- **PPTX**：macOS 打开后默认在编辑区内嵌原生 Quick Look 原文稿视图；工具栏的播放预览按钮可另外打开独立窗口，保留系统可呈现的布局、背景、图片、图形和字体样式，支持窗口缩放和滚动浏览。无需安装 Office，不上传或修改文稿。可切换到文字与备注视图，按演示顺序提取文字、备注和复制内容。特殊字体、复杂图表、动画等效果取决于系统预览支持，不能保证与 PowerPoint 逐像素一致；可使用默认应用打开。其他平台保留文字视图。

Excel 使用纯 Go 的 [Excelize](https://xuri.me/excelize/en/) 解析。表格视图不是 Excel 的保真排版：暂不显示图表、格式、合并单元格效果，也不进行完整公式重算；公式结果是文档中的缓存值，无缓存时可能为空，选择单元格可查看公式。单文件限制 64 MiB，解压限制 256 MiB；工作表仅展示前 10,000 行、100 列，XLSX 保存保留未展示的数据。旧 `.xls`、`.xlsb`、`.ppt` 需转换为 XLSX/PPTX 后打开。

### 设置与 AI 引擎配置

左下角齿轮、系统状态栏菜单或 `⌘ ,` 打开主窗口内的设置页面。支持跟随系统/浅色/深色主题、界面与编辑器字体、界面/编辑器/终端字号；修改即时生效并自动保存到本地 SQLite。

内置 pi-coding-agent、CodeBuddy CLI、DSH 三个配置入口，可选择默认引擎并记录各自的 CLI 路径。Pi 和 Node 已随 Omnicode 安装包内置，只需配置模型 API Key；CodeBuddy CLI 与 DSH 需自行安装并配置凭据。打开设置时自动识别默认 CLI 路径，保留手动指定的路径；DSH 也支持直接识别 /Applications 或 ~/Applications 中 DeepSeek Harness.app 的内置启动器，无需额外注册全局命令。右上角星芒图标打开右侧 AI 对话区，发送任务时启动所选引擎，工作目录绑定当前项目。pi 模型支持 deepseek-flash、glm-5.3 模板及自定义模型，可设置默认模型、Provider ID、实际 Model ID、API 地址、协议和API Key。模板不代表服务商一定提供同名 API，请按实际服务填写。

参考 [pi 模型文档](https://pi.dev/docs/latest/models)、[CodeBuddy CLI](https://www.codebuddy.cn/cli/)、[DSH](https://deepseek-harness.github.io/deepseek-harness/guide/quickstart)。

原生 PPTX 预览回归检查（macOS 图形会话）：`OMNICODE_PPTX_NATIVE_TEST=/absolute/path/to/fixture.pptx go test -run ^TestQuickLookNativePreview$ -count=1`。测试检查内嵌原版视图、切换标签的隐藏与恢复、关闭标签的释放，并将原生视图渲染写入 `artifacts/pptx-inline-preview.png`。

### AI 对话

右侧 AI 面板支持多轮对话、Markdown 回复、停止任务、新建会话和隐藏面板。Pi 使用配置的默认模型和 API Key；临时模型配置隔离在应用创建的目录中，不修改用户的 Pi 配置。CodeBuddy 使用其自己的登录与模型设置，并在每次调用中临时授权当前项目的文件读写，DSH 使用 headless profile，并通过本次任务的临时 patch 启用其凭据存储与账号服务，读取桌面 profile 中的 DeepSeek 默认模型选择；不直接启动被桌面端独占的 desktop profile。每个请求通过参数数组传给 CLI，不经过 shell 拼接；前八条历史消息作为上下文传入，完整消息与草稿保存到本地 SQLite，按项目恢复最近会话，历史图标可切换最近 100 个会话（不会删除更早的数据）。

AI 可以按用户发送的任务操作项目文件和调用 CLI 所支持的工具；Omnicode 不设置跳过权限检查的参数。停止会终止当前引擎进程组，已完成的修改不会回滚。任务结束后刷新文件树和未修改的已打开文本文件，正在手动编辑的缓冲区仍保留并使用原有保存冲突检查。切换项目后需新建会话，避免任务在旧项目执行。服务商模型 ID、凭据及 CLI 版本需有效。CodeBuddy 已通过实际创建并读取测试文件的验证。

AI 对话与项目记录共用 `os.UserConfigDir()/Omnicode/cache.sqlite`（macOS 为 `~/Library/Application Support/Omnicode/cache.sqlite`）。发送、完成、关闭窗口时保存，流式回复与草稿最多每 500 ms 写入一次；新建会话保留旧记录，重启恢复中断内容但不自动执行任务。存储错误会在 AI 面板中提示。

CodeBuddy 非交互权限：使用 `--permission-mode dontAsk` 和进程级 `--settings`，按项目的真实绝对路径添加 `Read` / `Write` / `Edit` 规则，并允许 Glob、Grep 和少量固定的查看命令（pwd、ls、ls -la、git status、git status --short、git diff、git diff --stat）。不添加裸 Bash/Write/Edit 全局授权、不使用 bypassPermissions，也不修改磁盘上的 CodeBuddy 配置。项目以外的操作、受保护路径和已有 deny 规则仍由 CodeBuddy 权限系统决定；该策略不是操作系统级沙箱。真实调用回归测试：`OMNICODE_CODEBUDDY_WRITE_TEST=1 go test -run ^TestCodeBuddyLiveWorkspaceWrite$ -count=1`，会调用已配置的服务，在临时目录创建和读取一个验证文件。

DSH 使用自身的凭据配置，Omnicode 不向其注入设置里的 API Key，也不为其解析模型配置的密钥环境变量。Omnicode 模型配置中的 API Key 仅供 Pi 使用；Pi 的旧版环境变量配置保留兼容读取。设置里的 API Key 默认遮罩显示，自动保存在当前用户专用的本地 SQLite 设置中（目录 0700、数据库 0600，未加密）；不写入临时模型配置、命令行参数或对话记录。旧版环境变量配置继续作为兼容回退，新配置无需设置环境变量。

DSH 集成依据 [内置命令运行时](https://github.com/deepseek-ai/deepseek-harness/blob/master/apps/desktop/README.zh.md#bundled-command-runtime)。headless 默认禁用 credentials、authorization 和 deepseek-account；Omnicode 的临时覆盖仅启用这三个服务，并从 `$DSH_HOME/profiles/desktop/cordis.patch.yml` 读取 DeepSeek 官方或账号模型选择。未找到桌面配置时保留 headless 默认模型。不会复制 Key、覆盖 DSH 全局配置或启动 Desktop 独占 profile。临时 patch 随任务结束清理。

AI 文件修改默认使用“修改前审批”（设置 → AI 引擎 → 文件修改权限）。引擎在临时项目副本中生成变更，macOS 系统沙箱限制写入范围，原项目不可写；仅允许临时运行目录及所选 CLI 自身的状态目录写入。每 700 ms 检查工作区变更，显示文件、首个修改行及原生左右差异预览，任务结束后可逐文件批准、全部批准或拒绝。批准前校验原文件是否被修改、路径是否被替换为符号链接，遇到冲突不会覆盖。未处理的审批会阻止切换项目、新建对话与正常关闭窗口。

批准 AI 对已有文件的修改或删除时，Omnicode 会先将原内容和文件权限保存到本地 SQLite。打开文件后点击路径栏的历史图标，在独立对比窗口中切换版本，左侧显示当前磁盘内容，右侧显示备份内容；核对差异后可恢复。已删除文件可从文件树右键项目根目录的“AI 修改备份…”找回。备份按文件完整路径保留多个版本。

“完全控制”直接在原项目运行，不再等待 Omnicode 文件审批，仍显示观察到的文件变更与差异。引擎自身的权限规则继续生效。观察范围是任务期间的工作区变化，包含这期间用户或其他程序的修改，不将其误认为精确的工具调用事件。两种模式均略过 .git、node_modules、vendor、.idea、bin、artifacts、.venv；原始版本保存在本机临时目录，按文件流式复制与校验，审批副本会重建指向已复制内容的项目内符号链接，跳过外部、失效或目标未复制的链接。审批只追踪普通文件的变化；遇到其他特殊文件会拒绝启动。空目录变更不在审批范围内。审批副本中的构建命令可能因依赖目录未复制而不可用。当前仅 macOS 支持隔离审批，不支持的平台明确拒绝该模式，不降级为直接写入。

内置 Pi 固定为 0.81.1，Node 固定为 24.15.0。`make pi-runtime` 根据 `packaging/pi/package-lock.json` 和 Node 官方 SHA-256 准备运行时，`make bundle` 自动执行此步骤并将运行时、依赖与许可证放入应用 Resources/pi。成品无需全局 Pi、npm 或 Node，也不在首次运行时下载引擎。打包对原生依赖签名，并在挂载后的 DMG 内以最小 PATH 验证 Pi 启动。安装版始终使用包内运行时，忽略旧设置遗留的外部 Pi 路径。

Pi 和 CodeBuddy CLI 使用 JSON 事件流（CodeBuddy 需支持 `--output-format stream-json --include-partial-messages`）：回复文本增量立即渲染，工具开始/完成/失败单独展示简洁进度，思考阶段显示状态。消息结束事件仅作兜底，不重复追加已流式显示的文本；JSON 模式的模型错误也会显示为任务失败。工具参数中的命令正文与原始工具输出不自动展示。

HTML/HTM 文件右键选择“预览”，在应用内打开使用系统 WebView 的网页预览窗口，支持本地 CSS、JavaScript、图片及项目根目录相对路径。刷新图标先保存已编辑的项目文件，再绕过缓存刷新页面。预览仅监听 127.0.0.1，关闭窗口即停止服务；不提供目录列表、隐藏文件或指向项目外的符号链接。页面不获得 Golet 原生桥接权限。此预览为静态文件服务，不替代 Vite、Next.js 等开发服务器。

搜索（侧边栏搜索图标或 ⌘P）同时匹配文件路径和 UTF-8 文本内容，按不区分大小写的字面文本搜索，展示匹配片段、文件路径和行号，点击内容结果会定位并选中对应文本。优先搜索已打开编辑器中的当前内容。内容搜索后台执行并防抖，切换查询会取消旧任务；跳过二进制、超过 8 MiB 的文件和文件索引忽略的依赖/构建目录。内容结果最多 100 项，扫描预算为 128 MiB / 5 秒，达到限制会提示缩小范围。

### 原生代码智能提示（Build 33）

编辑器使用 Go 实现的 LSP 客户端和 Golet 原生补全／悬浮／诊断控件，不需要模型 Key。
安装包内置 gopls v0.24.0（Go 官方）、typescript-language-server 6.0.3 + TypeScript 6.0.3，以及 vscode-langservers-extracted 4.10.0（HTML/CSS/JSON）。后两者复用应用内置 Node，不需要用户安装 Node/npm。Go 项目仍需要本机 Go SDK；启动时自动补充 Homebrew、/usr/local/go/bin 等路径。

- Go、JS/TS（含 JSX/TSX）、HTML、CSS/SCSS/Less：输入时自动补全，Ctrl+Space 手动唤起，方向键选择，Tab/Enter 接受，Esc 关闭。
- Go、JavaScript 和 TypeScript（含 JSX / TSX）文件中，右键方法名选择“查看引用”，编辑器下方按文件和行列出使用位置，点击结果即可跳转。
- 支持语言服务提供的悬浮类型／文档、F12 或 Cmd+点击跳转定义、错误下划线；JSON 支持语法诊断，候选取决于文档上下文。
- 未保存缓冲区同步到语言服务；后台执行、取消旧请求、UTF-16/字节偏移转换，切换项目及退出时释放进程。
- 标准数字占位片段展开后定位到第一个输入位置；暂不支持多占位符 Tab 跳转、片段变量／转换、自动 import、多文件重构或代码修复。需要附加编辑的补全项不会展示，避免插入不完整代码。
- 仅启用上述已内置的语言，其他语言继续使用基础编辑功能。语言服务首次索引项目时可能需要片刻。

开发构建：`make lsp-runtime` 准备锁定版本的服务；`make bundle` 自动包含运行时。
真实集成验证：`OMNICODE_LSP_TEST=1 go test -run 'Test(LSP|BundledLanguage|NativeLSP|NativeSnippet)' -v .`。

### 原生 Git 管理（Build 36）

poprsi 的 Git 管理功能已迁移为 Golet 原生控件，沿用 Omnicode 主题、图标工具条和隐藏标题栏。只管理当前打开的仓库根目录；不保留 poprsi 的多项目登记列表。克隆后直接打开新项目。

- 变更：工作区／暂存区差异、整文件／全部／区块／逐行暂存与取消暂存，逐行丢弃，点击多选、Shift 连选及文本拖选；文件打开、复制路径、Finder、移到废纸篓。
- 提交：提交说明、提交后推送、AI 生成说明、超过 10 MiB 的暂存文件提醒。AI 结果先填入输入框，由用户确认提交。
- 历史：最近 100 条提交拓扑图，工作区节点，提交搜索、补丁预览，本地／远端分支与标签历史。
- 分支：新建、切换、跟踪远端、合并、变基、比较、重命名、安全删除、推送指定分支。
- 同步：抓取、推送、远端分支查询，快进／合并／变基拉取及合并选项；继续／中止未完成操作，输出进度与停止。
- 贮藏：创建、预览、应用、应用并移除、删除。配置：提交作者、远端 URL、浏览器打开远端。
- 忽略：精确文件、扩展名、父目录、自定义规则；项目、本地和全局范围，写入前预览。
- AI：独立原生审查窗口、范围选择、补充要求、流式输出、停止；内置 Open CodeReview 的规则审查运行时，模型分析使用当前配置的 AI 引擎。

AI 审查与提交说明读取固定的过滤快照，跳过敏感、二进制、过大和冲突文件；macOS 沙箱禁止读取和修改原项目。其他 Git 操作使用系统 Git 与已有凭据，不自动联网；修改工作区时暂停编辑与自动保存，并与 AI 写入互斥。丢弃、删除和历史变更操作保留确认。停止网络操作后应刷新确认状态。

边界与 poprsi 一致：标签作为历史入口，不提供标签创建／删除；无交互式变基编辑器或三方冲突编辑器。文本差异上限 2 MiB，逐行操作最多 5000 行，特殊文件使用整文件操作或终端。完整对应关系见 [迁移清单](docs/git-migration.md)。

验证：`go test ./...`、`go vet ./...`；Git 回归测试使用临时仓库和本地裸远端。`OMNICODE_GIT_OCR_TEST=1 go test -run TestGitBundledOCRRuntime .` 验证内置 OCR，无需调用模型。安装包验证包括应用签名、图标、内置运行时和 Applications 快捷方式。

### HTTP 请求

左侧地球图标打开当前项目的 `requests.http`。已有的 `.http` 和 `.rest` 文件也可直接从文件树打开。用 `### 名称` 分隔请求，在文件开头定义 `@baseUrl = http://localhost:8080`，通过 `{{baseUrl}}` 在 URL、请求头或正文中引用。请求头与正文之间留一个空行。

把光标放在目标请求内，点三角图标发送。也可选中接口地址或请求块，右键选择「run」。加号追加请求，复制图标生成可在终端复用的 cURL 命令。请求文件使用编辑器的自动保存和 Git 功能。响应只保留在当前标签页，显示状态码、耗时、响应头及最多 1 MiB 的正文。请求可取消，超时为 30 秒，重定向会显示原始 3xx 响应。
