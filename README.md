# RestoreWeave

[中文](README.md) | [English](README.en.md)

**面向 NAS 与异构数据的自托管内容保护、搜索与恢复层。**

**少存重复内容，更容易找到，并且能够证明恢复结果。**

> 当前版本：`v0.1.0-prealpha.1` core preview。主要核心流程已经在当前开发配置中实现并测试，但这还不是生产发行版：没有正式安装包、模型自动安装、生产仓库资格或完整多平台发行保证。

## 核心是什么

RestoreWeave 不是网盘文件系统，也不是 OpenList、媒体服务器或备份软件的简单复制。它解决的是一条完整的数据管理链路：

```text
配置存储位置
-> 检查要加入内容库的来源
-> 确认存储计划
-> 保存精确文件内容和可恢复元数据
-> 添加 Note 或 Description
-> 用普通条件和语义搜索找到内容
-> 保存视图并冻结导出清单
-> 校验、导出或恢复原始字节
```

它有三个不会改变的核心原则：

1. **相同字节只需要保存一份。** 文件身份由完整内容的 SHA-256 和长度决定；文件名、路径、相似度、embedding 和模型输出都不能把两个文件认定为同一个文件。
2. **搜索信息可以重建。** 普通索引和 embedding 索引是投影。删除它们会让搜索暂时降级，但不会删除文件、Note、Description 或恢复依据。
3. **恢复不依赖 AI。** 即使模型、向量索引和运行中的 SQLite 不可用，只要内容仓库、认证恢复记录和独立保管的 trust anchor 完整，clean reader 仍可验证并恢复精确字节。

## 现在能做什么

下面按用户能完成的事情列出当前能力。除标为“候选”的部分外，这些能力都有代码和自动化测试；“已实现并测试”不等于“已经生产认证”。

### 配置和状态

- 使用持久化 TOML 配置；兼容读取旧 YAML 配置。
- 配置 catalog、repository、vectors、models 和 publication signing material 的明确路径。
- 相对路径按配置文件所在目录解析，不偷偷使用守护进程当前目录。
- 配置 CLI 分别提供 `rw config init --path <file>`、`rw config validate --path <file>` 和 `rw config show --path <file>`；daemon 使用 `restoreweaved --config <file>`，并支持环境变量覆盖。
- 对配置计算摘要，并把配置身份绑定到计划、快照、Description 和索引/语义 generation。
- 查看 daemon、catalog、repository、索引、计划、任务和 provider 的状态或诊断结果。

### 检查、计划和保护

- 只读遍历本地目录或已经挂载的目录。
- 记录文件名、原始路径、类型、大小、时间、符号链接、硬链接、检测结果，以及当前平台可观察的 xattr/ACL 状态和 sparse indication。
- 识别扫描期间发生变化、无法读取、越界或不稳定的条目，并把它们显示为阻塞项，而不是假装成功。
- 先生成不会写入仓库的保护计划，显示文件数、逻辑大小、预计新增存储量和每个条目的结果；已存在或重复的内容会体现为更少的 `new_bytes`。
- 保存成功后，WebUI 会根据本次经过校验的写入凭据分别显示实际新增内容占用和压缩节省；这不是整个仓库的净节省，无法确认写入结果时会明确显示未测量。
- 只有确认 plan ID 和 plan digest 后才真正写入；重复执行同一计划会重放同一个逻辑结果。
- 默认使用 `STORE_EXACT` 保存精确字节。
- 高级 CLI 支持显式 `LINK_ONLY`、`METADATA_ONLY` 和外部 locator 记录；它们始终显示真实的未保护或不可恢复状态，不会冒充完整保护。
- 派生处理失败不会阻止可读文件被精确保存；结果会明确降级或回退。

### SHA-256 身份与整文件去重

- 流式计算完整文件 SHA-256，并把逻辑长度一起作为精确身份。
- 不同目录、不同名称但字节完全相同的文件复用同一个内容对象。
- 每个原始名称、路径和元数据仍然单独保留，因此去重不会破坏原目录恢复。
- 保护预览会区分逻辑字节和真正需要新增的仓库字节。
- 当前默认是整文件精确去重；chunk dedup 不是核心完成条件，也没有被伪装成现有能力。

### Note、标签、Description 和提取信息

- 为同一个文件保存多个可编辑、带修订号的 Note。
- Note 直接进入普通搜索和语义搜索，不需要复制成另一个隐藏字段。
- 一个内容项可以拥有多个持久标签；WebUI 可创建、复用和移除标签，并从当前工作区已经使用的标签中给出候选。
- 格式和类型作为确定性的系统标签/筛选项展示，不冒充用户标签；未来 AI 归类必须先预览并由用户确认，不能静默覆盖手工标签。
- 默认首页按内容展示；原始目录只是来源证明和恢复投影，可在“按来源路径浏览”中查看，不承担日常组织。
- 保存版本化 Description，保留来源、语言、producer、前一修订和语义分段。
- 保存用户、导入、提取或标记为模型来源的 Description；当前不内置 Description 生成器，也不会在 ingest 时自动生成。
- 内置基础提取器可处理 UTF-8 文本、ID3/FLAC/OGG 音频标签和 EPUB OPF 元数据。
- Processor 结果带 provenance；失败、超时和不可用状态不会取得文件身份或恢复权限。

### 普通、结构化和语义搜索

- 搜索文件名、原路径、后缀、类型、标签、Note、Description 和已提取文本。
- 按 entry type、大小、mtime、SHA-256、duplicate group、`protection_mode`、language 和 suffix 过滤。
- 把 lexical、structured 和 semantic 结果映射回同一个稳定 subject。
- 返回命中的 Description segment 或 Note 内容及其来源，而不只给一个不透明分数。
- 显式提供并校验平台 bundle 后，使用真实的 `BAAI/bge-small-zh-v1.5`、ONNX Runtime 和进程内 zvec 完成本地语义向量生成和查询。
- daemon 启动时执行真实推理探测，重启后可重新打开兼容的 zvec generation。
- 模型、lease 或 generation 不健康时明确报告 semantic unavailable，并继续提供 lexical/structured search、精确保护和恢复。
- 每个 semantic generation 都严格绑定 embedding profile 和配置；不兼容的 generation 会 fail closed，也不会改写旧 Note、Description 或 subject 身份。

### 浏览和精确读取

- 列出原始目录投影，按路径解析 subject，查看文件、目录和符号链接事实。
- 列出一个 subject 可用的精确或派生 representation。
- 通过有范围、过期时间和大小限制的 handle 读取精确内容或字节区间。
- 搜索和物理仓库布局都不会替代原始路径恢复投影。

### 快照、SavedView 和导出

- 列出快照并比较两个快照中的新增、删除、移动、内容、元数据和类型变化。
- 保存动态 `SavedView`，以后重新按同一查询求值。
- 把某次 view 结果冻结为不可变 `ExportManifest`。
- 把冻结清单 materialize 到明确目标目录，并按路径、长度和 SHA-256 逐项验证。
- 重复执行同一 manifest 保持幂等；非空目标或符号链接攻击会被拒绝。
- 这套 view/export 链路已在本地范围端到端测试，但尚未通过正式发行资格。

### 校验、恢复和灾难读取

- 提供 authenticated metadata、sampled content、full bytes、restore drill 和 clean recovery 等校验模式。
- 恢复同样先生成只读计划，确认后只写入新的空目录，并验证最终路径集合、长度和 SHA-256。
- 导出认证 recovery reference、recovery token 和独立 public trust anchor。
- 在没有运行中 SQLite、搜索索引和签名私钥的 clean-install reader 中发现、校验并恢复快照。
- 拒绝被修改、截断、缺失、签名不正确、trust anchor 不匹配或 reader dependency 不兼容的恢复记录和内容。
- 支持仓库搬迁后的只读验证，并有 raw 与 zstd copy-forward 迁移、目标篡改拒绝和保留旧仓库回退的测试证据。
- 跨进程 publication fence 防止两个 daemon 交叉发布；未知结果可根据已认证记录对账。
- daemon 会对同一已签名 processor 计划执行有界自动 retry，并具备 lease、fencing、幂等、重启续跑、unknown-outcome reconciliation 和上限测试；任意用户触发或换路线的通用 reprocessing 仍未开放。

### 接口

- **WebUI：** 查看服务和存储状态、按内容或来源路径浏览、预览/确认存储、搜索、查看路径/SHA/存储状态、维护多个标签和 Note、预览/确认整快照恢复。
- **CLI：** 初始化、诊断、脚本、完整计划/快照/view/export 和紧急恢复入口；不是未来日常使用必须依赖的主界面。
- **MCP：** 本地 stdio 的只读检查、搜索、namespace、representation、annotation 和元数据入口。
- **API：** 当前只有 loopback `GET /api/v1/healthz` 与 typed `POST /api/v1/command`，复用同一 dispatcher，可选 bearer token；它不是可直接暴露公网的完整 REST 平台。

## 一条完整流程

### WebUI

```text
打开 Add content
-> 输入服务器上可访问的目录
-> Preview protection
-> 查看文件数、逻辑大小、新增空间和阻塞项
-> Confirm protection
-> 搜索文件并添加多个 Note
-> 用描述性语句进行 BGE 语义搜索
-> 查看 SHA-256 与保护状态
-> 在 Settings 中配置存储路径、本地 BGE/在线替换 profile、Description、恢复与服务选项
-> 保存时校验并原子更新同一份 TOML；需要时明确提示重启
-> Preview restore
-> 恢复到新的空目录并校验
```

### CLI

```bash
# 初始化配置并启动
go build -tags=purego -o bin/restoreweaved ./server/cmd/restoreweaved
go build -o bin/rw ./client/cmd/rw
bin/rw config init --path ./restoreweave.toml
bin/restoreweaved --config ./restoreweave.toml --socket /tmp/restoreweaved.sock

# 检查目录；命令输出会给出对应的 plan apply 参数
bin/rw --socket /tmp/restoreweaved.sock ingest /path/to/directory
bin/rw --socket /tmp/restoreweaved.sock plan apply <plan-id> \
  --workspace <workspace-id> --digest <plan-digest>

# 搜索和查看快照
bin/rw --socket /tmp/restoreweaved.sock search "灾后需要找回的资料" \
  --workspace <workspace-id>
bin/rw --socket /tmp/restoreweaved.sock snapshot list

# 恢复也先产生计划，再确认执行
bin/rw --socket /tmp/restoreweaved.sock restore <snapshot-ref> /path/to/empty-destination
bin/rw --socket /tmp/restoreweaved.sock plan apply <restore-plan-id> \
  --workspace <workspace-id> --digest <restore-plan-digest>
```

## 数据存在哪里

RestoreWeave 保持较少的物理实体，但它们职责不同：

| 数据 | 默认位置/形态 | 是否可重建 | 用途 |
| --- | --- | --- | --- |
| 配置 | `config.toml` | 不能从内容自动推断 | 选择所有路径与 profile |
| Catalog | 一个 SQLite 文件 | 部分可从认证记录恢复 | subject、路径、事实、Note、Description、计划与状态 |
| 内容仓库 | 配置的 repository 目录 | 不能从索引重建 | 原始精确字节和认证记录 |
| Lexical index | 独立 SQLite FTS generation | 可以 | 普通文本和结构化搜索 |
| Semantic index | zvec generation | 可以 | BGE embedding 搜索 |
| Portable recovery records | repository 中的认证记录 + 显式导出的 recovery reference | 不能用空索引替代 | catalog-free 验证和恢复 |
| Publication signing material | `paths.recovery_records` | 不能重建 | 发布签名私钥和本地 anchor 副本；不是 clean-reader artifact |
| Trust anchor | 独立导出和保管 | 不能从不可信仓库推断 | 验证恢复记录签名 |

逻辑上有 catalog、repository 和 index 三层，不代表必须引入三个数据库服务。当前个人配置只使用本地 SQLite、目录仓库和进程内 zvec，不依赖 Qdrant、Milvus 或 Docker Compose。

## 删除或丢失数据会怎样

| 丢失内容 | 结果 |
| --- | --- |
| 删除 lexical/zvec 索引 | 搜索降级；文件、Note、Description 和恢复能力仍在，可重建索引 |
| BGE/ONNX/zvec bundle 不可用 | 语义搜索不可用；普通搜索、保护、校验和恢复继续工作 |
| SQLite catalog 不可用 | 日常搜索和编辑不可用；保留 repository、导出的 recovery reference 与独立 trust anchor 时，clean reader 仍可验证和恢复 |
| repository payload 丢失 | 恢复记录不能凭空重建原始字节；相应内容无法恢复 |
| catalog 与 portable recovery records 同时丢失 | 仅有无上下文 blob 不足以安全重建原始目录和恢复含义 |
| publication signing material 丢失 | 已导出的 recovery reference 仍可由 clean reader 验证；普通 daemon 不能继续原有签名发布链 |
| 独立 trust anchor 丢失 | 无法按设计认证签名恢复记录；不要只把它放在同一个故障域中 |
| 实验性加密仓库的 key 丢失 | 加密内容不可读；该 profile 当前不是默认配置或生产发行能力 |

## 当前状态

| 能力 | 状态 |
| --- | --- |
| 配置、扫描、计划、SHA-256 身份、整文件去重、精确保护 | 已在当前开发配置实现并测试 |
| Note/Description、lexical/structured search | 已在当前字段范围实现并测试 |
| BGE-small-zh + ONNX + zvec | 在显式 provision 的真实 bundle 上实现并测试；尚未随安装包提供 |
| 签名恢复、clean reader、篡改拒绝、fencing、reconciliation | 已在 admitted development profile 实现并测试 |
| SavedView、ExportManifest、materialize/verify | 本地范围实现并测试；未完成发行资格 |
| React WebUI 与 loopback API | 可用的核心便利界面；尚非远程管理平台 |
| `directory-cas-dev-v1` | 当前生成配置的开发默认；不是 release default |
| `local-zstd-v1` | 可运行候选：整文件压缩、去重、校验、修复和迁移有测试，尚未资格认证 |
| `local-zstd-encrypted-v1` | 实验候选：AES-256-GCM 与外部 KeyProvider 有测试，不是默认配置 |
| GC | 只有 `NON_DESTRUCTIVE_ONLY` reachability 计划，没有删除执行器 |
| `RW-MVP-1` | 尚未完成或 release-qualified |

## 快速启动 WebUI

要求 Go 1.26（或 `go.mod` 声明的版本）以及 Vite 7 支持的 Node.js：

```bash
git clone https://github.com/ailiheizi/restoreweave.git
cd restoreweave

go build -tags=purego -o bin/restoreweaved ./server/cmd/restoreweaved
go build -o bin/rw ./client/cmd/rw
bin/rw config init --path ./restoreweave.toml
```

在生成的配置中启用本地 API：

```toml
[api]
enabled = true
listen = "127.0.0.1:4534"
```

在终端 1 启动 daemon：

```bash
bin/restoreweaved --config ./restoreweave.toml --socket /tmp/restoreweaved.sock
```

在终端 2 启动前端：

```bash
cd web
npm ci
npm run dev
```

打开 `http://127.0.0.1:5173/`。当前 API 只按 loopback convenience adapter 设计；不要把它直接暴露到公网。远程部署仍需要 TLS、身份认证、授权、审计和独立资格验证。

## BGE 模型

个人配置默认选择 `BAAI/bge-small-zh-v1.5`，但模型、ONNX Runtime 和 zvec native bundle 目前不会随仓库自动下载。需要自行准备经过摘要验证的平台 bundle：

```text
<paths.models>/bge-small-zh-v1.5/<goos>-<goarch>/
```

Darwin ARM64 的默认位置示例：

```text
~/.local/share/restoreweave/models/bge-small-zh-v1.5/darwin-arm64/
```

也可以用 `--semantic-bundle` 显式指定。缺少 bundle 时 daemon 会诚实报告 semantic unavailable，不会使用 fixture vector 冒充真实模型。

当前从源码构建 daemon 时必须保留上面的 `-tags=purego`，这样真实 zvec backend 才会编入程序；不带该标签的开发构建只保留明确不可用的占位实现。未来正式安装包应直接包含正确变体，不要求用户理解构建标签。

模型、ONNX Runtime、zvec 和 Go binding 各自的许可证、NOTICE/SBOM 与再分发条件必须随未来安装 bundle 单独保留和资格化；它们不由本仓库的 MIT License 自动覆盖。

## 接下来做什么

近期工作只聚焦把现有核心变成可发行产品：

1. **正式后台任务：** 对现有同计划有界 retry worker 完成发行资格；为用户主动、换路线或通用 reprocessing 另行定义签名 successor contract。
2. **离线安装包：** 打包 daemon、WebUI、ONNX Runtime、BGE model/tokenizer 和 zvec，安装后不依赖首次查询下载。
3. **生产仓库资格：** 用代表性 corpus 比较并选定一个 lossless repository profile，完整验证 encryption、损坏、repair、搬迁、迁移、回滚、clean reader 和实际净节省。
4. **完整日常体验：** 在 WebUI 中补齐可写配置、诊断、SavedView、ExportManifest、备份/升级/恢复引导，并让普通流程不要求用户处理内部 ID。
5. **发行验证：** 在 Linux 和 NAS-like 数据集上完成搜索覆盖率、语义延迟、存储占用、恢复时间、升级/回滚和 clean-install 验收。

更远的可选方向只有在出现真实需求后才进入核心队列：人工审核的源数据迁移与容量释放、更多 extractor/OCR/ASR/CLIP、其他 embedding 或 repository profile、多仓库与分层，以及企业远程管理、RBAC 和 HA。RWKV/Transformer 压缩只能作为未来显式、可逆、可迁移且有回退的研究 profile。

## 明确不做

- 内置 FUSE、SMB、NFS、WebDAV 或 S3 gateway；
- OpenList fork 或把 OpenList 变成核心依赖；
- 内置播放器、阅读器或媒体服务器；
- 自动外部重取、自动删除源文件或破坏性 GC；
- 用 embedding、相似度、文件名或模型输出认定精确身份或授权删除；
- 把 Qdrant、Milvus 或 Docker Compose 变成个人配置依赖；
- 在简单 lossless storage 和完整恢复资格之前启用神经 codec。

## 验证

```bash
go test ./... -count=1
go test -race ./server/internal/store/sqlite ./server/internal/exact \
  ./server/internal/processor ./server/internal/search \
  ./server/controlplane ./server/cmd/restoreweaved -count=1
go vet ./...
go mod verify

cd web
npm ci
npm run build
```

当前仓库包含 400 多个 Go 测试入口，包括真实 daemon/CLI、语义 bundle、索引重建、恢复、篡改、迁移和跨进程场景。测试证明的是当前明确范围，不代表所有平台和生产环境已经获得支持。

## 文档与许可

- [English README](README.en.md)
- [文档地图](docs/README.md)
- [MVP 与 operator contract](docs/requirements/mvp-and-operator-contract.md)
- [内容、去重、搜索、view/export 与 GC 规范](docs/requirements/content-store-views-and-exports.md)
- [核心执行顺序](docs/technical/core-mvp-execution-plan.md)
- [API 与 WebUI 边界](docs/requirements/api-and-webui.md)
- [发行资格要求](docs/requirements/release-qualification-and-traceability.md)
- [变更记录](CHANGELOG.md)
- [MIT License](LICENSE)

RestoreWeave 项目源码采用 [MIT License](LICENSE)。第三方依赖、模型权重、tokenizer、数据集和未来安装 bundle 仍分别遵循各自的许可证与再分发条件。
