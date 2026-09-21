# 代码创作实验室

供校园局域网课堂使用的 AI 趣味编程平台。学生可以和 AI 编程助手讨论创意、应用完整 HTML 代码提案、在独立 Runner 中预览，把作品发布到本班广场，并用班级评分积分支持同学作品。教师可以管理班级、名单、AI 额度、作品评分和审计记录。

## 技术结构

- 前端：React、Vite、CodeMirror 6、Phosphor Icons
- 后端：Go、Gin、GORM
- 数据库：MySQL 8
- 模型接口：OpenAI Chat Completions 兼容协议，支持 SSE 和普通 JSON 响应
- 主平台：默认 `8080`
- Runner：默认 `8081`

Runner 与主平台使用不同 Origin。学生代码只在 `sandbox="allow-scripts"` iframe 中运行，并附带 CSP、权限策略、短期运行令牌和服务端静态检查。
作品封面截取所需的固定版本组件会随前端构建到 `dist/runner-assets`，运行时由 Runner 本地提供，不依赖学生浏览器访问公网 CDN。

## 本地启动

要求：Go 1.26+、Node.js 20+、MySQL 8+。

1. 确认 MySQL 已启动。默认开发连接为 `root:1234@127.0.0.1`，服务首次启动时会自动创建 `class_code_lab` 数据库。

2. 构建前端：

```bash
cd frontend
npm ci
npm run build
```

3. 配置并启动后端：

```bash
cd backend
cp .env.example .env
go run ./cmd/class-code-lab
```

服务启动时会自动读取当前目录的 `.env`，从项目根目录启动时也会读取 `backend/.env`。如果系统已经设置同名环境变量，系统环境变量优先。

如果 `8080` 已被其他教学系统使用，可以临时改为：

```bash
APP_ADDRESS=:18080 go run ./cmd/class-code-lab
```

注意：学生浏览器默认从当前主机的 `8081` 端口打开 Runner，因此校园服务器防火墙需要同时允许主平台端口和 `8081`。

4. 打开 `http://服务器地址:8080`。

首次教师账号：

- 登录名：`teacher`
- 初始密码：`123456`
- 首次登录后必须立即改密

## 模型配置

最低需要设置：

```env
AI_BASE_URL=https://api.openai.com/v1
AI_API_KEY=服务端密钥
AI_MODEL=模型名称
AI_TIMEOUT_SECONDS=600
AI_MAX_OUTPUT_TOKENS=393216
AI_MODIFICATION_MAX_TOKENS=393216
AI_REASONING_EFFORT=low
AI_HISTORY_MESSAGES=8
AI_HISTORY_CHARS=16000
```

平台会请求 `{AI_BASE_URL}/chat/completions`，使用 Bearer 鉴权、`stream: true` 和 `stream_options.include_usage: true`。兼容供应商忽略流式参数并返回普通 JSON 的情况。

使用 DeepSeek 时，平台会发送 `max_tokens` 和 `reasoning_effort`。所有学生作品生成、增量修改、复杂调试和全量重试均使用 `low` 思考强度；输出上限设为 DeepSeek Chat Completions 当前允许的最大值 393216 Token，请求超时为 600 秒。这是最大上限而非强制生成长度，模型正常完成时仍会提前停止。

历史对话会受条数和字符预算双重限制，历史 AI 回复里的完整 HTML 代码块不会重复发送给模型。

未设置 `AI_API_KEY` 或 `AI_MODEL` 时，系统仍可正常启动。学生可以手动粘贴、编辑、预览和发布作品，AI 页面会明确显示“模型服务未配置”。

## 课堂使用顺序

1. 教师创建班级。
2. 在学生管理中下载模板并导入 Excel，或添加单个学生。
3. 上课前开启班级登录，按需开启 AI、发布功能，并设置每名学生的作品评分积分。
4. 学生选择班级和姓名，用初始密码登录并强制改密。
5. 学生生成或粘贴代码，保存、预览并发布；发布后浏览器会自动截取运行首帧作为广场封面。
6. 学生在本班广场体验作品、分配评分积分，并实时查看排行榜和冠亚季军。
7. 教师在课堂总览查看进度，在作品管理中查看排名、预览、撤下、锁定或加入跨班精选。
8. 下课后关闭班级登录；如需暂停评分，将班级评分积分设为 `0`，已有榜单会保留。

同班重名时，请在 Excel 中填写不同的“登录名”。界面仍显示真实姓名。

## 导入演示数据

需要快速查看教师总览、学生状态、作品广场和 AI 用量时，可执行：

```bash
cd backend
go run ./cmd/seed-demo
```

命令会同步 `演示班 A`、`演示班 B` 中的模拟账号，不会删除其他正式班级。每个姓名分别创建一个 `-学生` 账号和对应的 `-老师` 账号，登录名与界面姓名相同。

- 所有模拟账号初始密码：`123456`
- 所有模拟账号首次登录后必须修改密码
- 每个模拟教师都可以管理两个演示班
- 命令不会创建作品、历史版本、AI 对话、消息、用量或审计记录

重复执行命令会清理并重新生成这两个演示班的数据。

## 验证命令

```bash
cd backend
go test ./...
go build ./...

cd ../frontend
npm ci
npm run build
```

健康检查：

```bash
curl http://127.0.0.1:8080/api/health
curl http://127.0.0.1:8081/health
```

## 安全边界

- 学生班级身份只从服务端会话读取，不接受前端传入的班级条件。
- 登录使用数据库会话和 HttpOnly Cookie，不使用 localStorage 保存认证令牌。
- 草稿与公开版本分开保存，继续编辑不会改变已发布快照。
- 作品封面是发布时生成的静态图片；广场和精选列表不会运行学生代码，截取失败时显示默认图标。
- 普通作品只允许本班访问；跨班精选只公开已发布快照。
- 代码检查会阻止网络请求、表单、外部跳转、弹窗、嵌套网页、Worker、动态字符串执行和明显无限循环。
- 外部资源只允许带明确版本号的 jsDelivr 与 cdnjs URL。
- 浏览器无法数学意义上证明任意 JavaScript 安全。教师仍应通过锁定和撤下功能处理异常作品。

## 当前范围

已完成代码和基础工程验证。本项目暂未包含校园服务器部署、域名、HTTPS、真实模型付费测试和 60 人真实并发压测。上线前请使用两个测试班再进行一次局域网课堂演练。
