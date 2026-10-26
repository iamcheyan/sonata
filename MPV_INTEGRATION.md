# Sonata — MPV Integration

原则：**Sonata 不碰解码与输出**。它只负责"放什么、从哪开始、音量多大、下一首是谁"。

---

## 1. 候选方案评估

| 方案 | 优点 | 缺点 | 结论 |
|---|---|---|---|
| A. 每首歌直接 `mpv <file>` 前台启动 | 实现最简单 | 切歌断音、音量/暂停状态丢失、无法后台、无法 seek 反馈 | 仅用于 `sonata play` 一次性调试 |
| B. 常驻 `mpv --idle` + IPC socket | 切歌无重启、可暂停/seek/音量、退出 Sonata 仍在播、可观测进度 | 需处理 socket 生命周期与并发 | **推荐（V1 起采用）** |
| C. 独立 daemon（Sonata Service）+ 客户端 | 多客户端共享、launcher 可控制 | V1 过度设计；需 IPC 协议与守护进程管理 | V4 |
| D. 队列同步（把整个队列推给 mpv playlist） | mpv 自己管下一首，切歌无空隙 | 播放模式（随机/单曲循环）与 Sonata 队列语义冲突，需双向同步 | 部分采用：只在"播放全部/队列播放"时推 playlist |
| E. 恢复播放（`--start=<time>` 或 IPC seek） | 重启后接着播 | 需要可靠记录进度与文件身份 | V1 采用：`loadfile` + `seek absolute` |

**推荐架构：B + D 的混合 + E。**

- 单曲播放：`loadfile "<path>" replace`，Sonata 自己决定下一首。
- 整张专辑/队列：可选 `loadlist` 推入，但 Sonata 仍以自身队列为准（避免 mpv 自动前进导致状态不同步）。
- 恢复：启动时读 `state.json`，`loadfile` + `seek <pos> absolute+exact`，暂停状态按上次记录。

---

## 2. 进程与 socket 生命周期

```
sonata 启动
  │
  ├─ socket 存在且可连？ ── 是 → 复用（后台播放中），读取 mpv 当前状态
  │                        └─ 否 → 清理陈旧 socket，启动新 mpv
  │
  ├─ mpv --idle=yes --no-video --no-terminal --input-ipc-server=<sock>
  │        --volume=<state.volume> --gapless-audio=yes
  │
  ├─ 等待 socket 就绪（轮询 ≤ 3s，每 50ms）
  │
  └─ 握手：get_property pause / volume / time-pos / duration / path
```

- socket 路径：`$XDG_RUNTIME_DIR/sonata/mpv.sock`，无 `XDG_RUNTIME_DIR` 时用 `/tmp/sonata-<uid>/mpv.sock`。
- 退出 Sonata：**不杀 mpv**（后台播放）；`sonata stop` 才发 `quit`。
- 僵尸 socket：连接失败且无进程占用 → 删除后重启。

## 3. IPC 协议用法

mpv JSON IPC：一行一个 JSON 对象，`\n` 分隔。

请求：

```json
{"command": ["loadfile", "/storage/music/a.flac", "replace"], "request_id": 1}
{"command": ["set_property", "pause", true], "request_id": 2}
{"command": ["seek", 12.5, "absolute+exact"], "request_id": 3}
{"command": ["get_property", "time-pos"], "request_id": 4}
{"command": ["observe_property", 1, "time-pos"]}
{"command": ["set_property", "volume", 70], "request_id": 5}
```

事件：

```json
{"event": "file-loaded"}
{"event": "end-file", "reason": "eof"}
{"event": "property-change", "id": 1, "name": "time-pos", "data": 42.13}
```

实现要点：

- 每条命令带 `request_id`，2s 超时；超时计入失败计数。
- 单读 goroutine 解析事件与响应；单写 goroutine 串行发送（socket 写入不是并发安全的）。
- `time-pos` 观察节流：UI 500ms 采样一次即可，不按事件频率刷新。
- `end-file` 的 `reason` 决定行为：`eof` → 按模式下一首；`stop` → 停止；`error` → 跳过并在 UI 提示。

## 4. 播放模式与队列同步

| 模式 | Sonata 行为 |
|---|---|
| 顺序 | 播完队列停止 |
| 单曲循环 | `end-file(eof)` → 重新 `loadfile` 同一首（或 `seek 0` + unpause） |
| 列表循环 | 播完回到队首 |
| 随机 | 从洗牌序列取下一首；一轮内不重复 |

**不把随机/循环交给 mpv**（`--loop-playlist`/`--shuffle`）：否则 Sonata 的队列与 UI 会与 mpv 实际播放不一致。

## 5. 后台播放

- mpv 以 `--idle=yes` 常驻，父进程退出后继续运行（`setsid` 或 `--no-terminal` 已足够；不依赖 systemd）。
- Sonata 退出时只写状态文件，不发 `quit`。
- 下次启动：连上已有 socket → `get_property path/time-pos/pause` → UI 直接显示"正在播放"，无需重新加载。
- `sonata stop`：`quit` + 删除 socket + 状态标记 `stopped`。

## 6. 音量

- 单一真相：`state.json` 的 `volume`（0–100）。
- 启动 mpv 时 `--volume=<v>`；调整时 `set_property volume`；两者同步写状态文件（去抖 500ms）。
- 不接管系统混音器（掌机音量键可能直连硬件，Sonata 无法拦截）。

## 7. 失败模式

| 场景 | 检测 | 处理 |
|---|---|---|
| mpv 未安装 | `exec.LookPath("mpv")` 失败 | 启动时报错并退出，提示安装命令；TUI 仍可浏览库（只读模式） |
| socket 连接超时 | 3s 内连不上 | 杀进程、清 socket、重试一次；仍失败则进入只读模式 |
| 播放出错 | `end-file` reason=error | 跳过该曲目，UI 底部提示，连续 3 次失败停止自动前进 |
| mpv 崩溃 | 进程退出 + socket 断开 | 标记 degraded，UI 提示；用户按 Space 触发重启 |
| 状态文件与 mpv 不一致 | 启动握手对比 `path` | 以 mpv 为准回写状态（正在播的事实优先） |

## 8. 与 heya 的关系

- heya 只读 `state.json`（title/artist/playing/position），不直接连 mpv socket。
- 若未来需要 heya 控制播放（V5+），再暴露 `sonata ctl play|pause|next` 子命令，由 heya 调用；不共享 socket 细节。
