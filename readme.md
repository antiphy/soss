# soss — 轻量级本地对象存储

面向网站静态资源、固定图片、用户头像等小型/中型对象的**轻量级、本地化**对象存储服务。
元数据用 SQLite 持久化，对象内容追加写入本地 `data.bin`，热点数据常驻内存。

技术栈：Go 1.27 · Echo v4 · modernc.org/sqlite（纯 Go，无 CGO）· zap · singleflight。

---

## 1. 快速开始

```bash
# 1) 构建
go build -o soss .

# 2) 初始化存储目录 / 元数据 / 数据文件
./soss init -c config.yaml

# 3) 启动
./soss start -c config.yaml

# 开发模式：迁移脚本从 ./scripts 热加载
./soss start -c config.yaml -dev
```

冒烟测试配置：`config.smoke.yaml`（小容量、小缓存、带鉴权 token，监听 `127.0.0.1:18080`）。

```bash
./soss start -c config.smoke.yaml
curl -X POST -H 'Authorization: Bearer admin-token' -H 'Content-Type: application/json' \
     -d '{"name":"static","class":1}' http://127.0.0.1:18080/v1/buckets
curl -X PUT -H 'Authorization: Bearer write-token' -H 'Content-Type: text/css' \
     -H 'X-Filename: app.css' --data-binary 'body{color:red}' \
     http://127.0.0.1:18080/v1/buckets/static/objects/css/app.css
curl -H 'Authorization: Bearer read-token' \
     http://127.0.0.1:18080/v1/buckets/static/objects/css/app.css
```

存储目录布局（design §5）：

```
<storage.root>/
├── oss.db  oss.db-wal  oss.db-shm
├── data.bin
├── lock/writer.lock
├── tmp/
└── recovery/
```

---

## 2. 目录结构与职责

遵循 `SKILL.md` 的分层规范（`transports → logic → dal → models`，依赖单向）：

| 路径 | 职责 |
| --- | --- |
| `main.go` | CLI（`start` / `init` / `version`）、依赖装配、优雅退出、`//go:embed scripts` |
| `transports/soss/web/` | Echo 路由工厂 `NewRouters(Deps)` |
| `transports/soss/web/middleware/` | RequestID（tid）、访问日志/指标、Recover、Bearer 鉴权 |
| `transports/soss/web/controllers/` | 参数绑定 + 调 logic + 写响应（薄壳，不含业务） |
| `logic/` | 业务编排：bucket/object CRUD、版本生成、缓存策略、后台任务 |
| `dal/sqlite/` | 元数据仓储 + 迁移器，实现 `models.SossDataSource` |
| `models/` | 契约（`interface.soss.go`）+ 结构（`object.*.go`）+ 公共外壳 |
| `services/storage/` | `StorageEngine`：record 格式、偏移分配器、扩容、pread/pwrite、fsync |
| `services/cache/` | HOT 全量缓存、LRU 缓存（双上限） |
| `services/recovery/` | 启动恢复：write_cursor 重建、损坏检测、orphan 统计 |
| `services/monitor/` | 指标计数器 + 告警出口（外部渠道留 TODO） |
| `config/` | YAML 配置加载与校验 |
| `consts/` | 常量、日志字段、指标名、具名枚举 `types.BucketClass` |
| `errors/` | `CustomError` + 错误码 ↔ HTTP 状态映射 + 中英文案 |
| `utils/` | 容量解析、object key 校验、分片锁、CRC32C/ETag/SHA256 |
| `scripts/` | SQL 迁移脚本（`0001_init.sql`），embed 打包 |

---

## 3. 核心机制

### 3.1 写入顺序（最重要的一条约束）

```
分配 offset → 写 data.bin → fsync → SQLite BEGIN/INSERT/UPDATE/COMMIT → 更新缓存
```

这条顺序保证崩溃后最多出现"磁盘上的孤儿数据"，**绝不会**出现"元数据存在但数据不存在"。
缓存永远不是事实来源，只有在数据与元数据都持久化之后才更新。

### 3.2 记录格式（design §8）

```
Magic(8) | FormatVersion(2) | HeaderSize(2) | ObjectVersion(8)
PayloadSize(8) | CRC32C(4) | Flags(4) | Reserved(28)   = 64 bytes
payload ...
```

`record_size = 64 + payload_size`。

> **实现说明**：文档 §8 写 `data_offset = payload 起始位置`、§11 又用
> `MAX(data_offset + record_size)` 反推 write_cursor，二者不自洽。
> 本实现选择 **`data_offset = record 起始位置`**，使 §11 的游标公式精确成立，
> 并且恢复扫描时可以直接从 `data_offset` 读到 header 做校验。

### 3.3 版本（design §18）

`version = max(unix_seconds, latest_version + 1)`，同一 object 严格递增且唯一，允许空洞。
版本在 SQLite 事务内再次校正；若被校正，会回填 header 的版本字段（SQLite 始终权威）。

### 3.4 并发模型

- **分配器**：一把互斥锁只保护"分配 offset"这一瞬间，随后立即释放，真正的 `pwrite` 并发执行。
- **同一个 object key**：分片锁串行化，保证版本严格递增。
- **SQLite**：WAL + `synchronous=FULL` + `busy_timeout`，写事务由"独立写连接 + 进程内互斥"串行化（等价于 `BEGIN IMMEDIATE`）。
- **读**：`pread`（`ReadAt`）位置读取，请求之间不共享文件偏移。

### 3.5 缓存

- **HOT（class=1）**：`map[bucket]map[key]*entry`，全量常驻，受 `hot_max_bytes` 约束，超限返回 `507 CACHE_CAPACITY_EXCEEDED`。启动时后台 warm-up，不阻塞服务。
- **NORMAL（class=2）**：LRU，按**字节**为主上限、对象个数为辅上限；对象覆盖写/删除时按对象级索引批量失效。
- **缓存键**：`bucketID/objectKey/version`（文档 §24 允许的写法）。这样读路径可以先查缓存，避免每次 GET 都为拿 `objectID` 落一次库。
- **singleflight**：同一对象缓存未命中时只回源一次，防止头像类热点击穿（design §26）。

### 3.6 容量与恢复

- `data.bin` 初始 1 GiB；剩余 < 500 MiB 时后台 +1 GiB；写路径在进入写入前同步兜底扩容；大对象循环扩容直到满足需求。
- 扩容前检查物理磁盘剩余空间，不足则告警（`TODO` 接入通知渠道）并返回 `507 DISK_FULL`。
- 启动：由 `MAX(data_offset + record_size)` 与 `storage_files.write_offset` 的较大值重建 `write_cursor`（**不用文件大小**），统计 orphan 比例，并校验元数据是否指向数据文件之外；若发现越界记录则进入 **只读安全模式**并告警。
- 删除对象只删元数据 + 缓存，`data.bin` 内容保留为 orphan，v1 不做在线 GC，只监控 orphan ratio（> 40% 告警）。

---

## 4. HTTP API

统一错误体：`{"code":"OBJECT_NOT_FOUND","message":"..."}`

| 方法 | 路径 | 说明 | 权限 |
| --- | --- | --- | --- |
| GET | `/v1/buckets/{bucket}/objects/{key}` | 下载，支持 `?version=` | **公开（匿名）** |
| HEAD | 同上 | 只返回元数据 | **公开（匿名）** |
| PUT | `/v1/buckets/{bucket}/objects/{key}` | 上传（key 可含 `/`） | write header |
| DELETE | 同上 | 删除对象（只删元数据） | write header |
| GET | `/v1/buckets/{bucket}/objects/{key}/versions` | 版本列表 | **公开（匿名）** |
| GET | `/v1/metrics` | 运行态指标 | admin token |
| GET | `/healthz` | 健康检查 | 公开 |

**对象读写权限**

- 读（GET / HEAD）**匿名放行**，方便直接当静态资源用。
- 写（PUT / DELETE）必须在请求头带上 `Authorization: Bearer <write_token>`（admin_token 亦可），
  否则一律 **403 `PERMISSION_DENIED`**。与 `/v1/metrics` 不同，这里**没有开发模式放行**：
  token 没配置就是拒绝一切写入。

**桶的增删改查已迁到 `/admin`**（见第 4.1 节），`/v1/buckets*` 不再存在。

响应头：`ETag`、`X-Object-Version`、`X-Filename`、`Last-Modified`、`Cache-Control`、
`Content-Disposition`、`X-Cache`（`HOT_HIT`/`LRU_HIT`/`LRU_MISS`…）。支持 `If-None-Match → 304`。

错误码（design §69）：`400 INVALID_ARGUMENT`、`413 OBJECT_TOO_LARGE`、
`404 BUCKET_NOT_FOUND / OBJECT_NOT_FOUND / VERSION_NOT_FOUND`、
`409 BUCKET_EXISTS / BUCKET_NOT_EMPTY / VERSION_CONFLICT`、
`507 DISK_FULL / CACHE_CAPACITY_EXCEEDED`、`500 STORAGE_ERROR / METADATA_ERROR`、
`503 STORAGE_CORRUPTED / SERVICE_READ_ONLY`、`401 UNAUTHORIZED`。

> v1 约定：object key 以 `/versions` 结尾时该 GET 走版本列表，因此名为 `xxx/versions` 的对象会被遮蔽。

---

## 4.1 管理后台 `/admin`

浏览器打开 `http://<listen>/admin`，用 pongo2 服务端渲染的桶/对象管理界面（无前端框架依赖）。

### 账号

账号表**硬编码**在 `transports/soss/web/middleware/admin_auth.go` 的 `adminAccounts` 里，
默认账号 `admin`。改密码 = 改那张表重新编译，不落库、不支持注册。

登录成功后签发 HS256 JWT，写入 Cookie `soss_admin`（`HttpOnly` + `SameSite=Lax` + `Path=/admin`），
有效期 `auth.session_hour`（默认 12 小时）。签名密钥取 `auth.jwt_secret`；
**留空则每次启动随机生成**，代价是重启后需要重新登录（宁可这样也不留默认密钥）。

### 页面

| 路径 | 说明 |
| --- | --- |
| `/admin/login` | 登录页 |
| `/admin/logout` | 退出 |
| `/admin/buckets` | 桶列表：创建 / 切换类型 / 删除 |
| `/admin/buckets/{bucket}` | 桶详情：改类型、删桶、上传对象、对象列表（前缀过滤）、版本、删除 |

未登录访问页面 → 302 到登录页；未登录调 API → 401。

### JSON API（供页面 fetch 调用，同样需要会话 Cookie）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/admin/api/buckets` | 桶列表 |
| POST | `/admin/api/buckets` | 创建桶 `{"name":"static","class":1}` |
| GET | `/admin/api/buckets/{bucket}` | 桶详情 |
| PATCH | `/admin/api/buckets/{bucket}` | 修改 class `{"class":2}` |
| DELETE | `/admin/api/buckets/{bucket}` | 删除空桶 |
| GET | `/admin/api/buckets/{bucket}/objects` | 对象列表，`?prefix=` `?limit=` |
| POST | `/admin/api/buckets/{bucket}/objects/{key}` | 上传：multipart（`file` 字段）或裸 body |
| GET | `/admin/api/buckets/{bucket}/objects/{key}` | 对象元信息；`?versions=1` 返回版本列表 |
| DELETE | `/admin/api/buckets/{bucket}/objects/{key}` | 删除对象全部版本 |

对象的"改"= 对同一 key 重新上传，产生新版本（version 递增），即 S3 风格的覆盖写。

### 配置

```yaml
admin:
  enabled: true      # 关掉则整个 /admin 不挂载
  views_dir: views   # pongo2 模板根，相对**进程工作目录**
  static_dir: static # 映射到 /static
```

模板按 SKILL.md §2 放在 `views/zh/admin/`（`base.html` / `login.html` / `buckets.html` / `bucket.html`），
启动时一次性编译；模板目录缺失只记 ERROR，不会拖垮对象存储主服务。

> 这是内网/本地场景的最小实现：**没有 CSRF 防护、没有登录失败限流**。若要暴露到公网，
> 这三件事必须补上。

---

## 5. 监控与告警

`/v1/metrics` 输出：`storage`（容量/游标/剩余/磁盘/orphan/扩容次数）、
`hot`、`lru`、`counters`（请求数、耗时、错误数、缓存命中、put/get 字节数、SQLite busy…）。

告警出口在 `services/monitor/alarm.go`：磁盘不足、数据损坏、缓存容量、SQLite 错误、
存储读错误、orphan 过高。第一版只落 ERROR 日志并计数，**通知渠道为 TODO**
（Slack / Email / PagerDuty / 企业微信 / 钉钉）。

---

## 6. 与 SKILL.md 的偏差（逐条说明）

| 项 | SKILL.md 建议 | 本实现 | 原因 |
| --- | --- | --- | --- |
| 错误码类型 | `grpc codes.Code` | 自定义 `errors.Code` + HTTP 映射表 | 本项目无 gRPC，避免引入无谓依赖；映射关系集中在 `errors/consts.go` |
| 时间戳 | 毫秒 | **秒** | 版本必须使用 Unix 秒（design §18），`created_at` 与之对齐 |
| 响应外壳 | 恒 `{"code":0,"data":...}` + HTTP 200 | 按 design §28~§42 直接返回资源，错误返回对应 HTTP 状态码 | OSS 是 REST 服务，客户端依赖 304/404/507 等状态码语义 |
| 数据源 | MySQL/GORM | SQLite + `database/sql` | design §14 明确要求 SQLite |
| 分层 | `dal/<backend>/` | `dal/sqlite/`，存储引擎/缓存/监控归入 `services/` | 符合 SKILL.md §2 中 `services/<concern>/` 的定位 |
| 登录态 | `ctx` 中的 Account | 无账号体系，改为 Bearer token 三级权限 | OSS 是内部服务，design §90 只要求 API Key |
| 配置目录 | 未定义 | 新增顶层 `config/` | 配置结构体与加载器不适合放进 `consts/` |

未实现的能力（design §3 非目标）：S3 协议、Multipart、跨机复制、加密、ACL/IAM、
Bucket Policy、CDN、Range 下载、在线 GC/Compact。数据文件已通过
`StorageEngine` 接口与 `storage_files` 表预留 `file_id` 抽象，后续可直接演进为多 segment 与在线 Compact。

---

## 7. 备份

必须**同时**备份同一时间点的 `oss.db` 与 `data.bin`（design §92）。
推荐用底层存储的 Snapshot；否则进入维护/只读模式后执行
`PRAGMA wal_checkpoint(TRUNCATE)`（服务停止时已自动执行），再一起备份。

---

## 8. 发版前检查

```bash
go build ./...
go vet ./...
gofmt -l .
staticcheck -checks U1000 ./...   # 可选
```
