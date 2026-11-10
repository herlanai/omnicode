# 原生数据库管理

点击左侧工具条中 HTTP 请求图标上方的数据库图标。页面使用 Golet 原生 UI、Forma 表单和按钮、原生 SQL 编辑器与可横向滚动/调整列宽的结果表格。

## 连接

- MySQL：填写主机、端口、用户名、密码和数据库名称；可启用 TLS 证书验证。使用纯 Go `go-sql-driver/mysql`，不依赖 MySQL 命令行。
- SQLite：选择已有文件，或新建 SQLite 文件。使用已随应用打包的 Golet SQLite 插件，通过 Go API 调用，不依赖 sqlite3 命令行。
- Cloudflare D1：填写 Account ID、Database ID 和 API Token。使用 [Cloudflare 官方 raw SQL API](https://developers.cloudflare.com/api/resources/d1/subresources/database/methods/raw/)，无需 Wrangler；Token 需要目标账户的 D1 Read 权限，修改操作需要 D1 Edit 权限。

连接名称、参数、密码/API Token 和最近 20 条 SQL 历史保存在本机 Omnicode 的权限为 0600 的配置数据库中；凭据不是加密存储。密码输入框隐藏内容，连接错误会移除凭据。

## 使用

1. 新建连接，测试并保存。点击已保存的连接，加载表与视图。
2. 搜索表名并点击浏览，每页 100 行。点击“表结构”或右键表名查看字段、类型、默认值和主键信息。
3. 在 SQL 编辑器输入语句，点击运行或按 ⌘ Enter。选中一段 SQL 时只执行选区。
4. 查询与修改共用运行按钮和 ⌘ Enter；普通查询直接执行，可能修改数据的 SQL 会展示目标连接和 SQL，二次确认后执行。支持通过 SQL 建表以及增删改数据。
5. 可取消执行、查看 SQL 历史、右键复制单元格，以及导出当前显示结果为 CSV。

每次只执行一条 SQL，连接按操作打开和关闭，不支持跨次运行的事务。查询最多显示 1000 行，D1 响应最多接收 16 MB；操作超时为 30 秒。CSV 导出当前查询结果或当前浏览页，不是全库导出。SQLite/MySQL 查询模式使用只读连接/事务，D1 查询模式校验语句；D1 读权限应同时在 Token 上限制。

## 验证

数据库测试覆盖真实临时 SQLite 文件的读写、结构、行数限制和取消；D1 使用模拟 HTTP transport 检查路径、鉴权、参数、错误和大整数精度；MySQL 检查驱动配置、TLS 和禁用多语句。原生界面测试覆盖打开页面、连接持久化、表浏览、D1 表单和返回文件浏览器。未使用真实 MySQL 服务或 Cloudflare 账户验证。
