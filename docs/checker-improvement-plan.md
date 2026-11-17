# frpp check 与数据库迁移改进规划

> 整理日期：2026-11-15 ｜ 状态：**阶段 0~3 全部实施完成（2026-11-15）**
> 范围：`internal/checker` 排查框架 + `models/migrate.go` 迁移机制的配套改进
> 原则：默认只读不被破坏；新增破坏性能力一律 `--destructive` 门控；biz/services 业务代码零改动
>
> **确认方式**：回复"按推荐方案实施"即可全部按本文档执行；或指出需要调整的条目与决策点。每个阶段完成后停下来汇报，再进入下一阶段。

## 决策点速览（不指定时按推荐方案执行）

| # | 决策点 | 推荐方案 | 备选 |
|---|---|---|---|
| A | 组级 skip 死逻辑（registry.go） | **删除**死代码，保持框架简单 | 实现组级前置条件跳过 |
| B | detectRole 判定依据 | 按 `.env` 中 `DB_TYPE`/`DB_DSN` 是否被**显式**设置（区别于默认值）判定 master | 加 `--role` 手工指定 |
| C | 报告敏感信息处理 | 渲染层统一 redact（DSN、URL 中的凭证、密码字段），`--out` 权限收紧为 0600 | 仅文档提示不实现 |
| D | CI 多数据库矩阵 | GitHub Actions 新增 workflow（mysql/postgres service 容器跑迁移测试） | Drone pipeline |
| E | 邮件告警发送方 | checker 子命令内直发（`--notify-mail`，复用现有邮件工具） | master 定时任务内置巡检 |

---

## 一、背景与现状

- 排查工具现状：10 组 65 项 Checker，text/json/markdown 三种报告，`--fail-on` 接 CI 门禁；已知限制记录在 docs/checker.md 第五节/第七节
- 迁移机制现状（2026-11-14 落地）：AutoMigrate 在前、版本化迁移在后（`models/migrate.go`），版本表 `schema_migrations`，当前仅迁移 001；`data/db_migration` 检查项可对账
- 本次梳理新发现的问题：框架零测试、组级 skip 死逻辑、warn 不显示修复建议、报告缺版本与参数回显、迁移失败后无独立排障通道等（详见各阶段条目）

## 二、总体节奏

| 阶段 | 主题 | 条目数 | 工作量估算 | 依赖 |
|---|---|---|---|---|
| 阶段 0 | 迁移安全闭环 | 3 | 半天 | 无 |
| 阶段 1 | 框架质量加固 | 5 | 1 天 | 无 |
| 阶段 2 | 检查项扩充 | 7 | 1 天 | 阶段 1（复用测试基建） |
| 阶段 3 | 便利功能 | 6 | 1~2 天 | 阶段 0/1 |
| 阶段 4 | 大型扩展 | 5 | 另行立项 | 明确不做，仅备忘 |

每阶段统一验证：`go build ./...`、`go test ./internal/checker/... ./models/...`、交叉编译（linux/amd64、linux/arm64、darwin/arm64、windows/amd64）、对临时 sqlite 库手动跑一轮 `frpp check`。

---

## 三、阶段明细

### 阶段 0：迁移安全闭环（最高优先，改动小、价值直接）

#### 0.1 `frpp migrate` 独立子命令 ★核心

**动机**：迁移挂在 master 启动链上（`models/db.go` Init → `Fatalf`），一旦某步失败 master 起不来，且旁路看不到原因——独立命令是迁移中断后的唯一排障通道，也是"先迁移后重启"的运维入口。

**内容**：
- `frpp migrate status` — 只读：显示库内当前版本、待执行迁移清单（版本号+名称）、备份文件清单
- `frpp migrate up` — 执行全部待应用迁移，逐步输出日志；失败时给出精确到步骤的报错与恢复建议
- `frpp migrate verify` — 校验 schema_migrations 记录完整性（版本连续性、无脏数据）并输出与 `db_migration` 检查项一致的结论

**改动**：
- `cmd/frpp/shared/migrate.go`（新增，cobra 子命令，复用 `NewCheckCmd` 同款配置注入）
- `models/migrate.go`（新增导出）：`AppliedVersion(db)`、`PendingSteps(db)`、`RegisteredMigrations()`、`Migrate(db)`（现 `migrate` 升级为导出，`Init` 内调用同步改名）
- `docs/checker.md` 与 contribute.md 补充用法

**验收**：对旧库 `status` 显示版本 0 + 待执行 001；`up` 后版本 1 且与 master 启动效果一致；对已最新库 `up` 为 no-op；master 完全不启动时可独立执行。

#### 0.2 sqlite 迁移前自动备份

**内容**：runner 在应用**首个**待迁移前，将 sqlite 库文件复制为 `<db>.bak-migration-v<目标版本>`；保留最近 5 份（超出滚动清理）。`:memory:` 跳过；mysql/postgres 仅输出"请先自行备份"日志提示。

**改动**：`models/migrate.go`（runner 内新增 `backupBeforeMigrate`；从 `db.Dialector` 类型断言 `*sqlite.Dialector` 取 DSN，剥 `file:` 前缀与 query 得到文件路径）。

**验收**：迁移测试中断言备份文件生成且内容可打开；重复升级不覆盖旧备份。

#### 0.3 warn 级别显示修复建议

**内容**：text/markdown 渲染中 FixHint 的显示条件从 `Status == Fail` 放宽为 `Fail || Warn`。

**改动**：`internal/checker/output.go` 两处条件。

**验收**：`db_migration` 落后（warn）时终端与 markdown 均出现"重启 master 应用迁移"提示。

---

### 阶段 1：框架质量加固

#### 1.1 checker 单元测试（零 → 有）

**内容**（`internal/checker/*_test.go`，全部不触网不触真实库）：
- `util_db_test.go`：`sqliteReadOnlyDSN` table-driven（裸路径/带 pragma/file: 前缀/`:memory:` 四类输入——**昨天修复的 `?...?...` bug 正是缺这类测试**）
- `runner_test.go`：并发执行、单项超时降级为 fail、panic 防护、结果排序稳定性、`--fail-on` 退出码门控
- `output_test.go`：text/json/markdown 三格式渲染快照、warn 显示 FixHint（承接 0.3）
- `registry_test.go`：分组注册完整性、`AllCheckerNames` 与 `GroupNames` 一致

#### 1.2 组级 skip 死逻辑处置（决策点 A，推荐：删除）

**改动**：`registry.go` 返回值去掉 `skipped`、`runner.go` 删除合并循环；若选"实现"则需定义组级前置条件接口（工作量大 3 倍，暂不推荐）。

#### 1.3 detectRole 修正（决策点 B）

**改动**：`runner.go:detectRole`——用 `os.Getenv("DB_TYPE")`/`os.Getenv("DB_DSN")` 是否显式设置判定（cleanenv 默认值不入环境变量），客户端机器不再误判 master。

#### 1.4 报告版本与参数回显

**改动**：`RunReport` 增加 `schema_version`（常量 1，字段变更时递增）与 `opts` 回显（format/out/fail-on/destructive/node/concurrency，**不含凭证**）；json/text/markdown 头部同步。

#### 1.5 敏感信息 redact 与报告文件权限（决策点 C）

**改动**：`output.go` 渲染层对消息做统一 redact（DSN 连接串、URL userinfo、`password/secret` 字样的值）；`--out` 写文件权限 0600；报告头部回显已脱敏。

#### 1.6 CI 多数据库矩阵（决策点 D）

**内容**：新增 workflow，mysql:8 与 postgres:16 service 容器下运行 `go test ./models/...`（迁移测试），并跑一轮 `frpp migrate verify` 冒烟。覆盖迁移方言正确性（MySQL DDL 隐式提交、pg 事务行为）与 checker 远端 DSN 打开路径。

**验收**：CI 三个库全绿；sqlite 既有测试不受影响。

---

### 阶段 2：检查项扩充（7 个新 Checker，全部默认只读）

| # | 检查项 | 组 | 级别 | 数据来源与判定 | 平台 |
|---|---|---|---|---|---|
| 2.1 | `data/cert_expiry` | data | major（<30 天 warn，<7 天 fail） | `certs` 表 + master TLS 证书解析 NotAfter | 全平台 |
| 2.2 | `master/node_version_skew` | master | minor | 节点心跳/clientsstatus 中的版本与 master 构建版本对账；若心跳未携带版本则降级为 OTA status.json 对账并标注⚠️ | 全平台 |
| 2.3 | `env/disk_watermark` | env | major（剩余 <1GB 或 <10%） | data 目录 statfs；sqlite 库文件与 `-wal/-shm` 体积、WAL 占比 | unix（windows Skip） |
| 2.4 | `env/file_permissions` | env | major | `.env`、data.db 权限 ≠0600 报告（仅 unix） | unix |
| 2.5 | `auth/default_credentials` | auth | critical | **前置核实**：若 master 存在出厂默认管理员密码逻辑则检测其未修改；无此逻辑则不做此条 | 全平台 |
| 2.6 | `data/quota_usage` | data | minor | `proxy_config.quota_limit` 与当期流量对比，出"接近配额"清单 | 全平台 |
| 2.7 | `data/backup_freshness` | data | warn（>7 天无备份） | 按 0.2 的备份命名约定扫描 `*.bak-migration-*` mtime；无备份机制的其他库给出建议 | 全平台 |

**通用要求**：每项实现 Checker 四方法 + `buildXxxGroup` 注册 + docs/checker.md 矩阵 + 一条单测（数据构造用 glebarez 内存库）；涉及平台能力沿用 wgdevice 的 build-tag 降级模式。

**实施说明（2026-11-15）**：
- 2.5 `auth/default_credentials` **未实施**——调研确认 master 无出厂默认管理员密码逻辑（用户经注册流程自设密码），按预案跳过；
- 2.7 `backup_freshness` 级别由 warn 调整为 minor：迁移备份是事件型（迁移时生成）而非周期型，"超过 7 天无备份"会误报稳定部署；实际语义改为"已应用迁移但无备份文件"时 warn；
- 总计 65 → **72** 项（6 个新检查项 + 并行交付的 `duplicate_proxy_stats`），检查项纯函数测试见 group_data_test.go；
- 顺带落地同站安全补强：master 会话 Cookie 显式下发 `SameSite=Lax`（middleware/jwt.go、biz/master/auth/logout.go），auth 组断言扩展 SameSite 检测。

**验收**：`frpp check --list` 计数 +7；每项至少一次真实 pass 与一次人为构造 fail/warn 的截图记录。

---

### 阶段 3：便利功能

#### 3.1 单项过滤 `--only` / `--skip`

`frpp check all --skip perf`、`frpp check data --only db_migration`。实现于 `check.go` 参数解析 + `CollectCheckers` 过滤（支持 `group/checker` 与 `group` 两种粒度）。

#### 3.2 邮件告警 `--notify-mail`（决策点 E）

**内容**：check 运行结束后，若存在 fail/warn 且配置了 `--notify-mail`（值=收件地址），从 DB 只读读取 `SMTPSettings`，复用现有邮件发送工具（HTML 模板）发送**增量**失败摘要。为尊重"旁路只读"，发送走 checker 进程自身而非 master。配套：无 SMTP 配置时 Skip 并提示；相同失败内容 24h 内不重复发送（本地状态文件 `--history-dir` 控制）。

#### 3.3 报告历史与 `check diff`

**内容**：`check diff old.json new.json` 输出新增失败/已修复项；`--history-dir`（默认 `./check-history/`）按日期滚动保留最近 30 份报告。**不写库**——checker 保持只读原则，历史全靠文件。`frpp check all` 不带 `--out` 时若 history-dir 存在，终端尾部自动附上一行"与上次相比：+2 新增失败 / 1 已修复"。

#### 3.4 `check report merge`

**内容**：`frpp check report merge a.json b.json ... --out overview.json`——多节点报告聚合为总览（按节点/角色分列，汇总计数），支持 markdown 输出。docs/checker.md 第七节已预留此扩展点。

#### 3.5 `--format prometheus`

**内容**：输出 `frpp_check_result{group,checker,severity} 0|1` 形式的指标文本，适配 node_exporter textfile collector（`--out /var/lib/node_exporter/collector/frpp.prom`）与 master `/metrics` 的接入习惯。

#### 3.6 `--fix` 一键修复

**内容**：仅对显式可自动修复项生效（首批：孤儿记录清理、sort_index 归一化、废弃环境变量清单生成）。两段式：`--fix --dry-run` 列出将要执行的操作；确认后 `--fix --destructive` 实际执行并逐项回写"已修复 N 项"。未列出的检查项永远只报告不动作。

---

### 阶段 4：大型扩展（本轮明确不做，备忘）

**不实施的安全后果**：1~4 项不做不产生安全问题（不新增攻击面，仅损失观测能力，其中 wintun/perf 不做反而是更安全的状态）；仅第 5 项存在安全覆盖缺口——租户隔离缺乏自动化验证，兜底方案：手动用两个真实租户账号互访对方资源 ID 验证一次（确认返回 403/404 而非数据），无需立项即可完成。

每项都有明确的安全前置条件，落地时必须先满足：

| 条目 | 前置条件 | 安全前置（硬性） |
|---|---|---|
| `--check-internal` 进程内检查模式 | master 侧注入接口设计 | 触发入口仅 `AuthAdmin`（middleware/auth.go:62）；报告必须过 redact 管线（阶段 1.5 先落地）——进程内可见 connect_secret/JWT 材料/全租户拓扑，越权访问即高危泄露；进程内禁用 perf/destructive 子集防自我 DoS |
| webui 健康检查页 | 依赖 `--check-internal` | 同上 AuthAdmin；webui 通道永久禁用 destructive/perf；触发端点加互斥锁限频（同时仅一个 check 实例）；报告不落盘，仅随管理员会话返回 |
| wg windows/wintun 设备探测 | wintun 驱动集成验证 | **本地提权面**：wintun.dll 必须显式绝对路径加载并校验完整性（防 DLL search-order 劫持——frpp 以 SYSTEM 服务运行时被劫持即 LPE）；低权限上下文直接 Skip |
| perf 大规模节点注册压测 | 独立 staging 环境 | **自我 DoS 面**：生产库（存在真实租户数据）强制拒绝执行，要求 staging 确认环境变量；测试数据用独立前缀（`checker-perf-`）+ 硬编码速率上限，保证可清理 |
| 跨租户越权 destructive 用例 | 第二租户测试账号构造 | 风险最低且为安全正向项；测试账号用固定用户名（`checker-probe`）+ 幂等清理（每次先删同名再建、结束断言已删除，失败时报告标记残留）；随机强密码且绝不写入报告 |

**顺带发现的现存缺口（与阶段 4 无关，建议并入阶段 2）**：全仓未设置会话 Cookie 的 SameSite 属性（biz/middleware/utils 零处匹配）。现代浏览器默认按 Lax 处理可挡跨站 POST，但建议：① 阶段 2 实施时给 auth 组的 Set-Cookie 断言检查项扩展 SameSite 检测；② 后端登录处补 `SameSite=Lax`（或 Strict）设置，一行改动。

---

## 四、风险与回归防护

1. **只读原则**：阶段 2/3 所有新能力默认只读；写操作仅存在于 0.2（备份文件）、3.6（`--destructive` 门控）、迁移 up（master 启动链内已有）
2. **兼容性**：`RunReport` 新增字段均为可选（`omitempty`），旧消费方不受影响；`schema_version` 从 1 起
3. **迁移子命令与 master 启动链共用同一 runner**（`models.Migrate`），不会出现两条执行逻辑漂移
4. **每阶段收尾**：全套验证命令 + docs/checker.md、contribute.md 同步更新 + 记忆文件更新
5. **git**：不主动提交，改动全部留在工作区由你审阅

## 五、验收清单（实施完成后逐项勾选）

- [x] 0.1 `frpp migrate status/up/verify` 三命令可用，中断恢复路径演练通过
- [x] 0.2 迁移前 sqlite 自动备份生成且滚动保留 ≤5 份
- [x] 0.3 warn 项显示 FixHint
- [x] 1.1 checker 单测 ≥4 个文件，`go test ./internal/checker/...` 全绿
- [x] 1.2~1.5 死逻辑/角色判定/报告版本/redact 完成
- [x] 1.6 CI mysql/pg 矩阵全绿（workflow 已交付；GitHub 首跑验证见 .github/workflows/db-migration-check.yml）
- [x] 2.1~2.7 七个新 checker 上线（2.5 经调研按预案跳过，实际新增 6 项 + SameSite 补强，`--list` 计数 65→72）
- [x] 3.1~3.6 六项便利功能可用（--only/--skip、--notify-mail、diff+history、report merge、prometheus、--fix）
- [x] docs（checker.md、contribute.md、本文件状态改"已实施"）同步
