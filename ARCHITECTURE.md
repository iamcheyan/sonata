# Sonata — Architecture

## 1. 分层与依赖

```
┌───────────────────────────────────────────────┐
│ cmd/sonata                                    │
│  flags / 子命令 / 装配 / 信号处理              │
└───────┬───────────────────────────────┬───────┘
        │                               │
┌───────▼────────┐              ┌───────▼────────┐
│ internal/ui    │              │ internal/mpv   │
│ tcell 页面     │              │ 进程 + IPC     │
└───────┬────────┘              └───────┬────────┘
        │                               │
┌───────▼────────┐              ┌───────▼────────┐
│ internal/player│─────────────▶│ internal/state │
│ 队列/模式/状态机│              │ JSON 持久化    │
└───────┬────────┘              └────────────────┘
        │
┌───────▼────────┐
│ internal/library│  扫描 / 标签 / 索引
└────────────────┘

internal/capabilities：被 ui 与 player 读取（音频输出、存储、后台服务可用性）
```

**硬约束**：

- `library` 与 `state` 不 import `ui`、`mpv`。
- `ui` 不直接调用 `mpv`；一切播放动作经 `player`。
- `player` 不读文件系统，只消费 `library` 的索引与 `state` 的数据。

## 2. 模块职责

### internal/library

- `Scan(root string) (Library, []error)`：递归遍历，按扩展名过滤（mp3/flac/aac/m4a/ogg/opus/wav）。
- 每个文件解析标签；失败时回退：
  - Title ← 文件名（去扩展名、去前导 `NN ` 或 `NN - `）
  - Artist ← 父目录名；若父目录是专辑目录，Artist ← 上两级目录名
  - Album ← 父目录名
  - Track ← 文件名前缀数字
- 输出不可变索引：`Songs []Song`、`Artists []Artist`（含专辑）、`Albums []Album`，并建立 `byID` 映射。
- `Song.ID`：相对路径的稳定哈希（如 FNV-64 → base36），保证重命名目录后仍可尽力匹配（见 STORAGE_LAYOUT §5）。
- 缓存：`cache/library.json`（版本 + 根目录 + mtime/size 指纹），命中则秒开；`sonata scan --refresh` 强制重建。

### internal/state

- 四个文件：`state.json`（播放状态）、`favorites.json`、`history.json`、`config.json`。
- 全部 JSON + 原子写（写 `.tmp` → `fsync` → `rename`）。
- 提供 `Store` 接口，便于测试用内存实现。

### internal/player

- 持有队列 `Queue []SongID`、游标、播放模式（顺序/单曲循环/列表循环/随机）。
- `Next()/Prev()/Toggle()/Seek(delta)`：纯逻辑，返回 `Command`，由 `cmd` 层交给 `mpv`。
- 随机模式使用带种子的洗牌并记录已播放集合，保证一轮内不重复。
- 播放结束事件 → 按模式决定下一首（单曲循环回到同一首）。

### internal/mpv

- `Start(opts)`：若 socket 已存在且可连，则复用（后台播放恢复）；否则启动
  `mpv --idle=yes --no-video --input-ipc-server=<sock> --volume=<v>`。
- `Client`：unix socket + 行分隔 JSON（mpv JSON IPC 协议）。
- 支持命令：`loadfile <path> replace`、`set_property pause`、`seek`、`set_property volume`、`playlist-next/prev`、`get_property time-pos/duration/pause/volume`。
- 事件：`end-file`（reason=eof/stop/error）、`file-loaded`、`property-change`（time-pos 节流 500ms 回调）。
- 失败语义：socket 连不上 → 尝试启动；启动失败 → 返回可读错误（`mpv not found` / `socket timeout`），UI 显示在底部提示栏，不崩溃。

### internal/ui

- tcell `Screen` + 自绘页面（不引入 widget 库，保持可控）。
- 页面栈：`main → songs/artists/albums/favorites/recent → nowplaying → queue`。
- 每帧只重绘脏区域；文本按（内容, 颜色, 样式）缓存到 tcell 的 `Style`，避免重复分配。
- 输入层：`Action`（Confirm/Back/Up/Down/Left/Right/PagePrev/PageNext/PlayPause/QuickAction），映射可配置（`config.json` 的 `input` 段）。
- 长按检测：200ms 内重复按键计数；`Select` 长按 800ms → 锁屏；`Start` 长按 800ms → 熄屏。

### internal/capabilities

- `Audio() bool`（mpv 存在且 `--ao` 可用）、`Storage(path) bool`、`Backlight() bool`（`/sys/class/backlight`）、`Battery() bool`、`Suspend() bool`。
- 供 UI 决定是否显示熄屏/电量；不绑定设备型号。

## 3. 并发模型

| 组件 | 并发方式 |
|---|---|
| 扫描 | 单 goroutine + 可选 worker pool（默认 `runtime.NumCPU()`，上限 4），结果按路径排序保证确定性 |
| mpv IPC | 单读 goroutine（解析事件）+ 单写 goroutine（串行化命令），命令带 request_id 与超时 |
| 状态写盘 | 串行化（mutex）+ 去抖 500ms，退出时强制 flush |
| UI | 主 goroutine 跑 tcell 事件循环；mpv 事件通过 channel 投递，UI 每 100ms 合批处理 |

## 4. 错误处理

- 扫描：单文件失败不中断整体，收集 `[]error` 并在 UI 底部提示 `N files skipped`。
- mpv：命令超时 2s；连续 3 次失败 → 标记 `playerDegraded`，UI 显示 "mpv unavailable"。
- 状态文件损坏：备份为 `.corrupt-<ts>`，以默认值继续（不阻塞启动）。

## 5. 测试策略

| 层 | 方式 |
|---|---|
| library | fixture 目录 + 生成的最小 WAV/无标签文件，断言回退规则与排序 |
| state | 内存 Store + 原子写测试（写后读、损坏恢复） |
| player | 纯逻辑表驱动：四种模式 × 边界（单曲、空队列、末曲） |
| mpv | 命令编码/解析单测；集成测试用 `--ao=null` 启动真实 mpv 并断言 `pause`/`time-pos` |
| ui | `tcell.SimulationScreen` 渲染快照，断言 4 区文本与选中态 |

## 6. 与 heya 的集成（V5 预留）

- Sonata 作为 heya 的一个 Application：`/storage/heya/apps/sonata/app.json` → `exec: /usr/bin/sonata`。
- heya 状态栏可读 `state.json` 的 `title/artist/playing`，不需要链接 Sonata 代码。
- 后台播放保证 launcher 退出/切换不影响音乐（mpv 常驻）。
