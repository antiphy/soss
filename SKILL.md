# Project Skill — Generalized Architecture & Coding Style

> 由 `github.com/favos-me/meta` 项目提炼出的可复用工程规范。本文档
> **不是项目特定文档**，而是把其中的架构、目录组织、命名习惯、错误处
> 理、日志风格、上下文传递方式抽象成其他 Go 后端项目可直接套用的模板。
> 复制到任何 Go 后端服务的 `skills.md`，按需把占位符替换成实际名称即可。

---

## 0. 占位符约定

文档中以 `<...>` 包裹的均为占位符，复制到新项目时请按规则替换：

- `<module>`      — go.mod 的 module path, 例如 `github.com/<org>/<repo>`
- `<short>`       — 项目短名（用于包名、目录名、日志前缀），例如 `meta`
- `<GroupName>`   — 路由分组, 例如 `account` / `community` / `shop`

---

## 1. 整体架构

单仓库 (mono-repo) 下多应用，按"**传输 → 业务编排 → 数据访问 → 模型**"
分层：

```
┌──────────────────────────────────────────────────────────┐
│                    main.go (Bootstrap)                    │
│   - CLI 子命令: start / init / stop                       │
│   - 初始化 logger / DB / cache / MQ / 服务                │
│   - 拉起 HTTP(S) + gRPC                                  │
│   - 等待 signal 优雅退出                                  │
└──────────────────────────────────────────────────────────┘
              │                                  │
              ▼                                  ▼
   ┌──────────────────────┐         ┌──────────────────────┐
   │ transports/<short>/  │         │      apps/<bin>      │
   │  ├─ web (Echo)       │         │  其他独立二进制       │
   │  │   ├─ controllers  │         │  (proxy / tools …)  │
   │  │   ├─ middleware   │         └──────────────────────┘
   │  │   ├─ viewfuncs    │
   │  │   └─ template     │
   │  └─ grpc             │
   └──────────────────────┘
              │
              ▼
   ┌──────────────────────┐         ┌──────────────────────┐
   │       logic/         │◀────────│   services/          │
   │  业务流程编排          │         │  横切服务              │
   │  ctx.Context 入参     │         │  oss / mq / oauth2  │
   │  鉴权 / 频率 / 转换   │         │  antispam / sms ...  │
   └──────────────────────┘         └──────────────────────┘
              │
              ▼
   ┌──────────────────────┐
   │        dal/          │
   │  ├─ mysql   (GORM)   │ ── implements ──▶  models/interface.*.go
   │  ├─ leveldb (l2 缓存) │
   │  ├─ pogreb  (备选 l2) │
   │  └─ memory  (l1)      │
   └──────────────────────┘
              │
              ▼
   ┌──────────────────────┐
   │       models/        │
   │  interface.*.go      │  —— 数据源接口契约
   │  object.*.go         │  —— 表/聚合根 struct
   │  common.object.*.go  │  —— Context / Params / Response
   │  pb/                 │  —— 生成代码（protoc 输出）
   └──────────────────────┘

   ┌──────────────────────┐     ┌──────────────────────┐
   │       consts/        │     │       errors/        │
   │  全局常量             │     │  CustomError + i18n  │
   │  按子包分组           │     └──────────────────────┘
   └──────────────────────┘
```

**依赖方向（强制单向，不可回环）**：

```
transports → logic → dal → models
                  ↘  services (调用)
                  ↘  errors / consts / utils
```

`models` 不应反向 import `logic`/`dal`/`transports`，保持模型层零依赖。

---

## 2. 目录与文件命名规范

| 路径                            | 作用                                                        |
| ------------------------------- | ----------------------------------------------------------- |
| `main.go`                       | 唯一入口；`init` / `start` 子命令；`//go:embed scripts` 打包 SQL |
| `transports/<short>/web/`       | Echo 路由 + 控制器 + 中间件 + 模板渲染                      |
| `transports/<short>/grpc/`      | gRPC Service 与 Interceptor                                 |
| `transports/<short>/web/controllers/<domain>.go` | 一个领域一个文件（`account.go` / `community.go` …） |
| `logic/<domain>.go`             | 业务流程；无 HTTP/gRPC 类型依赖                              |
| `dal/<backend>/<entity>.go`     | 后端实现按存储分目录（`mysql/`、`leveldb/` …）               |
| `models/interface.<short>.go`   | **数据源接口** —— 后端必须实现                               |
| `models/object.<entity>.go`     | **表/聚合根 struct**，含 `TableName()` 方法                  |
| `models/common.object.go`       | 跨模块公共结构 `Response` / `Params` / `Paging` / `Context` |
| `models/pb/<proto_pkg>/`        | protoc 生成，**禁止手改**                                   |
| `services/<concern>/`           | 横切服务（`oss`、`mq`、`oauth2`、`antispam`、`sms`、`verifier`…） |
| `consts/<group>.go`             | 按主题拆子包/文件：`app/`、`ids/`、`types/`、`secret/`、`paging/`、`service/`、`tablename/` |
| `errors/errors.go` + `errors/consts.go` | `CustomError` + 错误码常量 + 中英文案           |
| `utils/`                        | 纯函数小工具（id 生成、token 工具、平台文件路径 …）          |
| `views/`                        | HTML 模板，按语言拆子目录：`views/zh/`、`views/en/`         |
| `static/`                       | CSS / JS / 字体 / 图片，`http.Static("/static", ...)` 直出 |
| `scripts/*.sql`                 | 初始化 / 迁移 SQL；通过 `//go:embed scripts` 打包           |
| `proto/<proto_pkg>/*.proto`     | 接口定义；通过 protoc 生成 Go + Dart                        |
| `apps/<bin>/`                   | 同仓内其他二进制（proxy / 工具）                             |
| `docs/` + `readme.md` + `server_deployment.md` | 项目文档                              |

---

## 3. 控制器（Controller）规范

> 控制器只是"**请求 → 业务 → 响应**"的薄壳，禁止在此处理业务。

```go
// 文件: transports/<short>/web/controllers/<domain>.go
package controllers

// 1. 命名: <Resource><Action><Method>
//    - 方法后缀 = HTTP Method (GET / POST / PUT / PATCH / DELETE)
//    - 例如 LoginPOST、AccountDetailGET、ShopBuyAssetPOST
func <Resource><Action><Method>(c echo.Context) error {
    ctx := c.Request().Context()                         // 必须从 request 拿 ctx
    // 2. 鉴权前置 (需要登录的页面)
    if _, err := logic.Logic.DenyAnonymous(ctx.Request().Context()); err != nil {
        c.Set(consts.MessageContextKey, err.Error())
        return HintGET(c)                                // 跳提示页 或 JSON
    }
    // 3. 参数绑定 / 校验
    var req models.<Req>
    if err := c.Bind(&req); err != nil {
        return c.JSON(http.StatusOK, (&models.Response{}).DecodeErr(err))
    }
    // 4. 业务全部交给 logic
    data, err := logic.<DoSomething>(ctx, &req)
    if err != nil {
        return c.JSON(http.StatusOK, (&models.Response{}).DecodeErr(err))
    }
    // 5. 渲染或 JSON
    return c.JSON(http.StatusOK, models.Response{Code: 0, Data: data})
}
```

**铁律**

- 签名固定：`func(c echo.Context) error`，没有上下文外参数。
- 入口第一行 `ctx := c.Request().Context()`；**不要**用 `context.Background()`。
- 鉴权统一走 `logic.Logic.DenyAnonymous(ctx.Request().Context())` / `logic.DenyLoggedIn(ctx)`，失败直接渲染提示页或 `JSON(4001,...)`。
- 严禁在控制器里直连数据库 / OSS / 缓存；只允许 `logic.*`。
- 错误统一通过 `models.Response.DecodeErr(err)` 序列化；HTTP 状态码常为 `200`，业务码在 `Response.Code`。

---

## 4. 业务编排（Logic）规范

```go
// 文件: logic/<domain>.go
package logic

func <DoSomething>(ctx context.Context, in *models.<Req>) (*models.<Res>, error) {
    // 1. 从 ctx 还原通用上下文（不要把账号作为参数！）
    mc, ok := ctx.Value(consts.MetaContextKey).(models.Context)
    if !ok {
        return nil, errors.NewError(errors.CodeBadContext)
    }
    // 2. 业务前-校验/计数/配额
    // 3. 调 dal / services
    // 4. 失败日志 + 带 tid
    if err != nil {
        mc.Logger.Error("xxx failed", zap.String("tid", mc.TID), zap.Error(err))
        return nil, err
    }
    // 5. 返回
}
```

**约定**

- 入参头参一定是 `context.Context`，**不再接受 account 作为参数**，登录态全部从 ctx 取。
- 所有函数读到的 `models.Context` 包含：`TID / IP / Lang / Endpoint / Logger / DataSource / AIDataSource / MQ / Account (已登录用户)`。
- 鉴权前置函数：
  - `logic.Logic.DenyAnonymous(ctx.Request().Context())` — 必须登录，否则返回 `CodeLoginRequired`
  - `logic.DenyLoggedIn(ctx)` — 必须未登录，否则返回对应错误
- 业务验证（参数合法性、库存、权限等）放 logic 层，**不要**让控制器重复拷贝。
- 业务失败：`mc.Logger.Error("...", zap.String("tid", mc.TID), zap.Error(err))` 后 `return err`。

---

## 5. 数据访问（Dal）规范

```go
// 文件: dal/mysql/<entity>.go
package mysql

func (s *Storage) Create<X>(x *models.<X>) error {
    err := s.db.Create(x).Error
    if err != nil {
        var myErr *mysql.MySQLError
        if errors.As(err, &myErr) && myErr.Number == 1062 { // 唯一索引冲突
            return errors.NewError(errors.CodeDuplicateEntry)
        }
    }
    return err
}

func (s *Storage) Query<X>(id int64) (models.<X>, error) {
    var x models.<X>
    err := s.db.Model(&x).Where("id = ?", id).First(&x).Error
    if err == gorm.ErrRecordNotFound {
        return x, errors.NewError(errors.CodeRecordNotFound)
    }
    return x, err
}

func (s *Storage) Update<X>(id int64, setter func(*models.<X>)) error {
    var x models.<X>
    setter(&x)                       // 用闭包就地改，调用方不必关心字段集
    return s.db.Model(&x).Where("id = ?", id).Updates(x).Error
}
```

**约定**

- 一个后端一个目录（`mysql/`、`leveldb/`），上层通过 `models` 包的接口注入。
- CRUD 命名统一：`Create<X> / Query<X> / Query<X>List / Update<X> / Delete<X>`。
- 查询列表返回 `([]<X>, Paging, error)`，分页结构统一使用 `models.Paging`。
- `Query` 找不到 → **返回 `errors.CodeRecordNotFound`**，让上层用 `errors.IsErrorCode(err, ...)` 判定。
- 更新用 **setter 闭包**模式，避免业务层自己拼 `map[string]any`。
- 任何错误都包成 `errors.CustomError`，不要把 GORM / SQL 原生错误冒泡到 logic。

---

## 6. 模型（Models）规范

**两层：interface（契约） + object（结构）**

```go
// models/interface.<short>.go —— 描述"数据源必须暴露的能力"
type <Short>DataSource interface {
    Create<X>(x *<X>) error
    Query<X>(id int64) (<X>, error)
    Query<X>List(p Params) ([]<X>, Paging, error)
    Update<X>(id int64, setter func(*<X>)) error
    // ...
}
```

```go
// models/object.<entity>.go —— 描述"一行记录长什么样"
type <X> struct {
    ID       int64                     `json:"id"`
    Name     string                    `json:"name"`
    Group    types.<XGroup>            `json:"group"`   // 强类型枚举
    Status   types.<XStatus>           `json:"status"`
    CreateTS int64                     `json:"create_ts"`
    UpdateTS int64                     `json:"update_ts"`
}
func (*<X>) TableName() string { return tablename.TableName<X> }
```

**约定**

- 所有整型枚举都用 `consts/types/<Name>` 中的具名类型 (typed constants)，不在 struct 里裸用 `int`。
- 时间戳字段：`create_ts` / `update_ts` 用 **毫秒** `int64`，不用 `time.Time`。
- 公共结构在 `models/common.object.go`：`Response{ Code int; Msg string; Data any }`、`Params`、`Paging`。
- `pb/` 是 protoc 生成，禁止手工修改；如需重新生成参考 `readme.md`。

---

## 7. 常量与错误体系

### 7.1 常量集中化

- `consts/<group>.go`：枚举、配置 key、表名。
- `consts/types/<name>.go`：**具名类型 + 常量**，避免魔数：

```go
package types

type AccountGroup int
const (
    AccountGroupNormal AccountGroup = 0
    AccountGroupAdmin  AccountGroup = 1
)
```

- `consts/secret/`：运行时凭据（DB DSN、密钥），与配置代码分离。
- `consts/tablename/`：表名字符串集中，避免散落字面量。
- `consts/ids/`：业务 ID 生成器（雪花 / xid …）。
- 新增任何跨包复用的常量 → 放 `consts/`，**禁止**在控制器 / logic 中写裸字面量。

### 7.2 错误体系（i18n）

```go
// errors/consts.go —— 错误码（尽量贴合 grpc codes，便于 gRPC 复用）
const (
    CodeRecordNotFound    codes.Code = 1002
    CodeLoginRequired     codes.Code = 1003
    CodeInvalidRequest    codes.Code = 1004
    CodePermissionDenied  codes.Code = 1005
    CodeExceedFrequency   codes.Code = 1006
    CodeDuplicateEntry    codes.Code = 1007
    CodeAssetLack         codes.Code = 2001
    CodeInternalServerError codes.Code = 8888
)
```

```go
// errors/errors.go
func NewError(code codes.Code) CustomError                         // 无附加消息
func NewErrorWithMsg(code codes.Code, msg string) CustomError       // 动态上下文
func IsErrorCode(err error, code codes.Code) bool                   // 判定业务码
func (e CustomError) Error() string                                 // 按当前语言返回文案
```

**使用纪律**

- 业务层只 `return errors.NewError(CodeXxx)` / `NewErrorWithMsg`，**不要** `fmt.Errorf("xxx 失败")`。
- 多语种文案放 `errors/consts.go` 的 `...ZH` / `...EN` 常量中。
- HTTP 控制器一律 `res.DecodeErr(err)`；gRPC 拦截器把 `CustomError.Code` 映射回 `status.Error`。

---

## 8. 日志规范（zap）

- 全局 `*zap.Logger`，由 `main.go` 用 `zap.NewProductionConfig()` 构造并写入 `consts.<Short>LogFilePath` / `<Short>ErrLogFilePath`。
- 中间件从请求头读 `TID`，没有就 `utils.NewXID()` 生成后回写响应头。
- 每条日志附上下文字段，常量定义在 `consts/log.go`：

```go
LogFieldNameTraceID  = "tid"
LogFieldNameUID      = "uid"
LogFieldNameRequest  = "req"
LogFieldNameResponse = "res"
LogFieldNameError    = "err"
```

- 业务层记录失败用 `mc.Logger.Error("...", zap.String("tid", mc.TID), zap.Error(err))`。
- 公共上下文（lang / endpoint）放进 `mc`，不在每次调用都传。
- **不**把"正常控制流"用 `Error` 级别打 — `Error` 只用于真正的失败。

---

## 9. 鉴权与上下文

- 身份载体：`models.Context.Account`，从 JWT Cookie / gRPC metadata 解析得到。
- 流程：`request → middleware 注入 ctx → logic 用 `ctx.Value(consts.MetaContextKey)` 取出 → 业务层判定权限`。
- 任何业务函数签名：`func XXX(ctx context.Context, ...)`，**禁止**把 `*Account` 当参数。

```go
// 例: 强制登录
func RequireLogin(ctx context.Context) (models.Context, error) {
    mc, ok := ctx.Value(consts.MetaContextKey).(models.Context)
    if !ok || mc.Account == nil {
        return mc, errors.NewError(errors.CodeLoginRequired)
    }
    return mc, nil
}
```

---

## 10. 传输层（HTTP + gRPC）

### HTTP（Echo）

- 一个项目一个 `NewRouters(...)` 工厂注入所有依赖（DS / MQ / Logger / …）。
- 静态文件：`e.Static("/static", consts.StaticFilePath)`，注意排除需要动态响应的路径（如 `/captcha/*`）。
- 模板渲染：`e.Renderer = newWebRenderer(consts.ViewFilePath)`，多语言按子目录切：`views/<lang>/...`。
- 路由分组：`app.Group("/<group>", [middleware...])`，分组内仅放与该领域相关的路由。
- 全局错误：`app.HTTPErrorHandler = controllers.ErrorHandler`，统一渲染到提示页。

### gRPC

- 一个项目一个 `NewXxxGRPCService()` + `NewLoggingInterceptor(...)`。
- 拦截器：**注册**到 `grpc.UnaryInterceptor(...)`，把 `tid / uid / 鉴权` 塞进 ctx 供 service 复用。
- TLS：生产模式 `credentials.NewServerTLSFromFile(crt, key)`，dev 模式可注释掉。
- 优雅退出：`rpcServer.GracefulStop()` 后再 `frequencyController.Persist()` 等刷盘。

### 双协议共用 logic

- HTTP 控制器与 gRPC Service 都**只调用 `logic.*`**，禁止跨越。
- 把 gRPC 拦截器里的鉴权 / TID 注入与 HTTP 中间件写法对齐，让 logic 层无差别用 ctx。

---

## 11. 数据库迁移与 embed

- 所有 `.sql` 放在 `scripts/`，由 `//go:embed scripts` 打包进二进制。
- `init` 子命令统一负责：
  1. 创建系统服务（Linux：写入 systemd unit）
  2. 创建 bucket / 初始资源
  3. 调用 `dal.NewMigratorRepository(db, scriptsFS).InitRDB()` 执行迁移
- 开发模式下 `getScriptsFS()` 回退到 `os.DirFS("scripts")`，方便热改。
- 章节迁移脚本按文件编号 / 时间顺序命名（推荐 `0001_init.sql`、`0002_xxx.sql` ...）。

---

## 12. 工具与辅助二进制（同仓 `apps/`）

把"非核心服务进程"的 CLI / 工具放在 `apps/<tool>/main.go`：

- `apps/<short>-proxy/`：负载均衡 / 接入代理（与主服务共用 `models` / `consts`）。
- `apps/tools/db/`：DDL 生成、迁移脚本生成。
- `apps/tools/json/`：JSON schema / mock 生成。
- `apps/tools/jwt/`：JWT 调试 / 解码。
- `apps/tools/checker/`：连接性 / 健康检查。

它们通过 `module/<apps>/<name>` 路径 import 主仓库包，避免重复代码。

---

## 13. 日志/快捷检查清单

提交 PR 或合并前过一遍：

- [ ] **分层方向**：`transports → logic → dal/models`，无回环。
- [ ] **控制器签名**：`func(c echo.Context) error`，命名 `Resource+Action+Method`，第一行 `ctx := c.Request().Context()`。
- [ ] **鉴权前置**：受保护路由通过 `logic.Logic.DenyAnonymous(ctx.Request().Context())` 拦截。
- [ ] **业务错误**：使用 `errors.NewError / NewErrorWithMsg`，文案走 i18n 常量。
- [ ] **不可达错误**：把 GORM / 第三方原生错误至少包装一次再返回。
- [ ] **常量入位**：跨包复用值放 `consts/<group>`，具名枚举走 `consts/types/<X>`。
- [ ] **日志字段**：业务失败附 `tid`（必要时附 `uid`/`req`/`err`），不滥用 `Error` 级别。
- [ ] **时间戳**：持久化时间一律毫秒 `int64`。
- [ ] **ctx 一致**：logic / dal 函数接收 `context.Context`，账户从 ctx 读。
- [ ] **生成代码**：`models/pb/**`、`*_grpc.pb.go` 等由 protoc 产出，禁止手改。
- [ ] **静态检查**：至少跑 `staticcheck -checks U1000 ./...` 确认无未使用导出符号。
- [ ] **embed 资源**：`scripts/`、`static/`、`views/` 通过 embed 打包，dev 模式回退到 fs。
- [ ] **优雅退出**：HTTP `Shutdown` + gRPC `GracefulStop` + 缓存刷盘。

---

## 14. 反模式（不要做）

- ❌ 控制器直连 `dal` 或 `services` — 永远走 `logic`。
- ❌ 业务函数再传 `*Account` 参数 — 登录态走 ctx。
- ❌ 把 GORM / SQL / HTTP 原始错误冒泡到 controller — 包成 `CustomError`。
- ❌ 在文件里散落 `1, 2, 3` 这种魔数；即便 `int` 也要先有具名类型。
- ❌ 中间件里同步 `c.JSON()` 与 `return nil` 二次输出。
- ❌ gRPC 与 HTTP 各自一套上下文结构 — 统一为 `models.Context`。
- ❌ 把 SQL 脚本从运行时目录硬编码加载 — 通过 `embed` 注入。
- ❌ 在 `models/pb/**` 里手改 — 改 `.proto` 后重新生成。
- ❌ 在控制器里 `panic` — 用 `zap.Error` + `HTTPErrorHandler`。
- ❌ 把"业务正常但有副作用"的分支用 `Error` 级别打日志。

---

## 15. 落地步骤（迁一个新 Go 后端项目）

1. 在新项目根目录创建上述目录骨架（除 `transports/<short>` 之外都可以原样复用）。
2. 把 `main.go` 模板按"CLI 子命令 + 多服务并行 + signal 退出"模式抄过去。
3. 拷贝 `consts/`、`errors/`、`models/common.object.go`、`utils/` 一整套基础设施。
4. 把 `proto/*.proto` 拷过去并按 `readme.md` 跑 protoc 生成 `models/pb/**`。
5. 按领域逐个实现 `models/object.<x>.go` → `models/interface.*.go` → `dal/mysql/<x>.go` → `logic/<x>.go` → `transports/<short>/web/controllers/<x>.go`。
6. 把上文 §13 清单接入 PR 模板的 checklist。
7. 在 README 末尾保留 `staticcheck -checks U1000 ./...` 一条命令，作为发版前必跑门槛。

> 复制时只改占位符：`<module>`、`<short>`、`<GroupName>`、表名前缀等，
> 其余结构与命名一律保留 —— 这是沉淀一致性的关键。
