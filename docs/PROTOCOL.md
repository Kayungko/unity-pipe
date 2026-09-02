# Pipeline HTTP 协议参考（实测版）

本文档描述编辑器内 `com.unity.pipeline` 包（官方 0.6.0-exp.1 / 本仓适配 **0.6.0-2022.1**）暴露的本地 HTTP API。来源为包源码通读（`Runtime/Common/BasePipelineServer.cs`、`Runtime/Models/*.cs`）+ 真实编辑器实测（Unity 2022.3.23f1）。`upipe` 即此协议的 Go 实现。0.5 实测结论在未改动处仍然成立。

⚠️ 注意：编辑器内另有一组 `com.sgame.ai-pipeline.*` 近似名包，那是别的桥，**与本协议无关**。

## 1. 发现：端口文件

每个运行中的 Unity 实例把描述符写到：

```
<project>/Library/Pipeline/.unity-pipeline-port
```

```json
{
  "pid": 57004,
  "port": 7802,
  "projectPath": "E:\\P4\\SGAME_o_jiarongke_6099",
  "projectName": "SGAME_o_jiarongke_6099",
  "unityVersion": "2022.3.23f1",
  "mode": "editor",
  "startedAt": "2026-08-25T06:54:03.9777151Z",
  "lastHeartbeat": "2026-08-25T06:54:03.9777151Z",
  "evalToken": "<base64>"
}
```

字段定义见包源码 `Runtime/Models/InstanceDescriptor.cs`。要点：

- **Editor 实例端口 7800-7849**，随编辑器自动启动；**Runtime 服务器 7900-7949**，仅 dev Player 构建有。
- **每个心跳都会重写此文件；域重载后 port 与 evalToken 可能变化**——客户端绝不缓存，每次请求前重读（upipe 的 `DescriptorSource` 即此契约）。
- 心跳超过 ~30s 未更新视为 stale（编辑器可能假死）。
- 服务只绑 `127.0.0.1`。

## 2. 认证

所有端点要求：

```
Authorization: Bearer <evalToken>
```

token 失效（401/403）通常意味着编辑器重启过——重读端口文件即可；持续 401 则提示重启编辑器。

## 3. 端点

| 端点 | 方法 | 说明 |
|---|---|---|
| `/api/status` | GET | 服务健康（轻量，exec 阻塞时仍可响应） |
| `/api/editor_status` | GET | 编辑器详细状态（内部走 editor_status 命令，与 exec 同一串行门） |
| `/api/commands` | GET | 命令面自发现（见 §4） |
| `/api/exec` | POST | 执行命令（见 §5） |
| `/api/test-status` | GET | 异步测试运行状态 |
| `/api/progress` | GET | 当前命令进度 `{"active": bool, "progress": …}` |
| `/api/job?id=…` | GET | 分离式 job 状态（结果保留 1 小时 / 最近 100 个） |
| `/api/job/cancel` | POST | 取消 job；body `{"id":"<jobId>"}`（上限 4 KiB；upipe 另附兼容字段 `jobId`，服务端忽略） |

请求体上限有 MaxRequestBodyBytes 保护（超限回 413）；未知路径 404。

## 4. GET /api/commands（自发现）

Query 参数（AND 组合过滤）：

- `detail`: `full`（默认，含参数与 JSON schema）/ `compact`（轻量索引：name/description/tags/package）
- `query`: 对 name/description/tags 的不区分大小写子串匹配
- `tag`: 段感知前缀匹配（`assets` 命中 `assets` 与 `assets/import`，不命中 `assetsx`）
- `group_by`: `flat`（默认，返回 `commands` 数组）/ `package` / `tag`（嵌套树）
- `sort`/`order`: `name`（默认）/ `package` × `asc`/`desc`
- `offset`/`limit`: 分页（`total` 为过滤后总数，`count` 为本页数）

Editor 服务器会隐藏 Runtime-only 命令；Runtime 服务器反之。

## 5. POST /api/exec

请求体：

```json
{
  "command": "eval",
  "parameters": { "code": "return 2 + 2;" },
  "timeout": 30000,
  "job": false,
  "verbose": true
}
```

- `parameters`：任意 JSON 对象，服务端按命令 schema 解析
- `timeout`：**毫秒**，正整数。服务端省略时默认 60000ms = 60s——upipe **未设 `--timeout` 时仍发送 10 分钟**（`DefaultRoundTripTimeout`），避免客户端 round-trip 10min、dispatcher 60s 的错位。单位契约为毫秒（服务端 `CommandExecutionRequest.Timeout` 按 `dispatcherTimeoutMs` 直接消费）；客户端 `--timeout` 接受 Go duration（如 `30s`、`2m`），发送前换算为毫秒，同时作为客户端 round-trip deadline（+30s 余量）。**eval 的服务端超时上限 24h（包 0.5.0 起）**
- `job: true`：立即返回 `{"jobId": "…", "state": "queued"}`，后台执行，轮询 `/api/job?id=…` 收割；队列上限 100 并发，超出 429
- `verbose`：官方 0.6 默认 `false`（瘦信封，成功可只剩 `{"success":true,"result":…}`，丢掉 `command`/`executedAt`/`executionTimeMs`）。**upipe 始终发送 `"verbose": true`**，保持完整信封，直到消费者不再依赖这些字段
- `commandLine` / `argv`（0.6 AUTHAPI-68）：与 `command` 三选一。upipe 当前仍走 `command` + `parameters`；错参时服务端可回 400 + `INVALID_COMMAND_ARGS`

响应信封（`BaseResponse` + `CommandExecutionResponse`）：

```json
{
  "success": true,
  "command": "eval",
  "result": { … },
  "message": "",
  "error": "",
  "errorDetails": "",
  "executedAt": "2026-08-25T07:30:05Z",
  "executionTimeMs": 225,
  "status": "",
  "retryable": null
}
```

`success: false` 时错误信息优先级：`errorDetails` > `error` > `message`。

### 关键状态码语义

| HTTP | 场景 | 客户端应对 |
|---|---|---|
| 200 | 正常返回（success 字段区分命令成败） | — |
| 400 | 请求体/参数不合法 | 不重试 |
| 401 / 403 | token 缺失或失效 | 重读端口文件；持续则提示重启编辑器 |
| 413 | 请求体超限 | 缩小参数 |
| 429 | job 队列满 | 等待后重试 |
| 503 | **settling 门**：编辑器启动初期 / busy，带 retryable 信封 | 重试 |

### 串行门与并发

- exec 严格串行（`m_ExecGate`）；`editor_status` / `test-status` 排队等同一道门
- `status` / `progress` / `job` 不进门——**exec 阻塞主线程时它们仍可响应**，轮询应优先用这些

## 6. 域重载行为（客户端最难的部分）

1. recompile 等命令触发 domain reload → **端口文件被重写**（port/token 可能变）
2. 重载窗口内：连接被拒 / 中断，`/api/exec` 可能 503
3. 官方包的 settling 机制：编辑器冷启动首次 idle 前 `/api/status` 扣留 `ready`，该窗口内 exec 对主线程命令回 503 + 结构化信封

客户端契约（upipe 已内建）：

- **每次请求前重读端口文件**（不缓存 descriptor）
- 连接类错误按退避梯重试（upipe：0.2/0.5/1/2/5/10/30s），HTTP 语义错误（4xx/5xx 结构化信封）不盲重试
- 重载状态文件（`Temp/pipeline_recompile_status.json`）在重载前写入，重载后轮询者仍可读到终态

## 7. 轮询型命令的状态值（实测自包源码）

| 命令 | 状态机 |
|---|---|
| `recompile` / `recompile_status` | `idle` → `triggered` → `compiling` → **`completed` / `up_to_date`**（completed 带 `failed` 与 `errors[]`） |
| `run_tests`（`--async_tests`）/ `test_status` | `running` → **`completed` / `error` / `cancelled` / `no_tests`** |
| `audit` / `audit_status` | `idle` → `scanning` → **`completed` / `failed` / `interrupted` / `unavailable`** |
| job | `queued` → `running` → **`completed` / `failed` / `canceled`**（美式拼写，与包 `ToLowerInvariant()` 一致；upipe 同时认英式 `cancelled`，均映射 exit 22） |

## 8. 已知协议陷阱（upipe 已适配，新客户端注意）

1. **双重编码 JSON 字符串**：`recompile_status` / `test_status` 的 C# 实现返回**原始 JSON 字符串**（读状态文件原文），服务端序列化时变成字符串字面量——`result` 是 `"{\"status\":\"up_to_date\"}"` 而非对象。客户端需解包一层（upipe: `protocol.UnwrapJSONString`）。
2. **run_tests 零匹配陷阱**：过滤器无匹配时服务端报 `success: false` 但 `summary.total == 0 && failed == 0`——这不是失败，应特判为"无匹配"。
3. **主线程命令超时**：编辑器未聚焦时主线程不 tick，eval 等命令报 "Main thread operation timed out after 5000ms"——先 `set_autotick --enable true`。
4. **测试失败时结果可能不透明**：`run_tests` 同步模式下失败详情可能被吞——用异步模式 + 轮询，或缩小 filter 重跑。
5. **async 测试终态 body 没有 `success` 字段**：`test_status` 的终态 body 是原始状态文件 JSON（只有 status/duration/summary/results），**顶层无 `success`**——按 `summary.failed > 0` 判定成败，解析 success 标志恒得 false（2026-08-25 实战踩坑，upipe 修复 + 回归测试 TestTestCommandPassAndFail）。
6. **新文件需先 Refresh**：直接写 `.cs` 到 `Assets/` 后 `recompile` 报 `up_to_date`——recompile 只编译已导入的脚本，先用 `eval "AssetDatabase.Refresh()"` 触发导入（或用 `import_asset` 命令）。

## 9. 应急直连（upipe 不可用时）

```powershell
$pf = Get-Content "<project>/Library/Pipeline/.unity-pipeline-port" | ConvertFrom-Json
$h = @{ Authorization = "Bearer $($pf.evalToken)" }
Invoke-RestMethod "http://127.0.0.1:$($pf.port)/api/editor_status" -Headers $h
Invoke-RestMethod "http://127.0.0.1:$($pf.port)/api/exec" -Method Post -Headers $h `
  -ContentType "application/json" `
  -Body (@{ command = "eval"; parameters = @{ code = "return 2 + 2;" } } | ConvertTo-Json)
```

`eval_file` 命令的参数名是 `file`（不是 `filename`）。
