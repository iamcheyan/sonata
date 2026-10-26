# Sonata — Project Plan

Sonata 是**音乐库管理器 + 播放列表管理器 + 播放控制器**，不是播放器。
解码、音频输出、编解码一律交给系统 `mpv`。

体验目标：iPod Classic + Kindle + Pocket Music Device。
第一阶段跑在 Linux 桌面终端；最终作为独立 Application 集成进 heya，运行在 ROCKNIX / Knulli / muOS 的 ARM64 掌机（RG34XX / RG35XX / RG40XX）。

---

## 1. 目标与非目标

### 目标

1. 扫描 `/storage/music`（可配置），递归构建 Library：Songs / Artists / Albums。
2. 读取标签：Title / Artist / Album / Track Number / Duration；缺失时用文件名回退。
3. 浏览：All Songs、Artists、Albums、Favorites、Recently Played、Recently Added。
4. 播放控制：播放/暂停、上一首/下一首、快进/快退、顺序/单曲循环/列表循环/随机。
5. 收藏与播放列表；退出保存状态，再次启动恢复。
6. **后台播放**：Sonata 退出后 mpv 继续播放。
7. 全局状态文件，供 launcher / 系统状态栏 / 其它程序读取。
8. 锁屏与熄屏（防误触、省电），音乐不中断。
9. 单二进制、ARM64 交叉编译、无数据库强依赖、无 Electron、无 Web 技术。

### 非目标

- 不做音频解码、不做音频输出、不做视频播放（交给 mpv）。
- 不做在线流媒体、账号、推荐、同步。
- 不做标签编辑、封面抓取、歌词滚动（V1–V2 不做；后续可作为独立 Provider）。
- 不做鼠标优先的触摸 UI；不做 Spotify/Apple Music 风格。
- V1 不做 SDL UI（终端 UI），但布局从第一天按掌机规范设计。

---

## 2. 阶段划分（Roadmap）

| 版本 | 内容 | 验收 |
|---|---|---|
| **V1** | CLI Library Player：扫描、浏览、播放/暂停/上下曲、四种播放模式、收藏、状态保存/恢复、锁屏与熄屏、后台播放 | Linux 桌面终端可用；`sonata status` 与状态文件一致；mpv 在 Sonata 退出后继续播放 |
| V2 | Playlist Management：命名播放列表、队列编辑、导入导出（JSON/M3U） | 队列可保存、重排、跨会话恢复 |
| V3 | mpv IPC 深化：属性观察、精确 seek、音量、无间隙切歌、播放失败重试 | 切歌无重启 mpv；进度误差 < 1s |
| V4 | Background Music Service：独立 daemon + 客户端，系统级后台播放 | launcher 退出/重启不影响播放；多客户端读同一状态 |
| V5 | Launcher Integration：作为 heya Application，状态栏显示曲目 | 在 heya 中启动 Sonata 并返回；状态栏读取 `state.json` |

V1 只实现本文档中标记 V1 的部分；V2–V5 的协议在 STORAGE_LAYOUT.md 与 MPV_INTEGRATION.md 中预留。

---

## 3. 技术约束

| 项 | 决策 | 理由 |
|---|---|---|
| 语言 | Go（`go 1.21` 指令，兼容 ROCKNIX 自带 Go 1.21） | 单二进制、交叉编译容易、团队已有 Go 栈 |
| UI（V1） | `tcell/v2`（纯 Go，无 CGO） | 终端事件模型完整，`SimulationScreen` 可做确定性渲染测试 |
| 标签解析 | `github.com/dhowden/tag`（纯 Go） | 支持 ID3v1/v2、FLAC、OGG、MP4；无 CGO |
| 播放 | 外部 `mpv`，通过 `--input-ipc-server` 控制 | 解码/输出不归 Sonata |
| 存储 | JSON + 原子写（临时文件 + rename） | 无数据库强依赖；掌机可读可修 |
| 交叉编译 | `CGO_ENABLED=0 GOARCH=arm64` | 无 CGO 依赖，直接交叉编译 |
| 分辨率语义 | 720×480 横屏；终端按 90×30 字符网格设计 | 3.4" 屏可读，未来 SDL 版复用同一布局 |

---

## 4. 架构总览（详见 ARCHITECTURE.md）

```
cmd/sonata
   │
   ├── internal/library   ← 扫描、标签、索引、查询
   ├── internal/state     ← 收藏、历史、播放状态、配置（JSON 原子写）
   ├── internal/player    ← 播放队列与模式（纯逻辑，可测）
   ├── internal/mpv       ← mpv 进程 + IPC 客户端
   ├── internal/ui        ← tcell 界面（4 区布局）
   └── internal/capabilities ← 能力探测（音频、存储、亮度、后台服务）
```

依赖方向：`ui → player → mpv`，`ui → library`，`player → state`；`library`/`state` 不依赖 UI。

---

## 5. 目录结构

```
sonata/
├── cmd/sonata/          # main、子命令
├── internal/
│   ├── library/         # 扫描 + 标签 + 索引
│   ├── state/           # 持久化（favorites/history/playback/config）
│   ├── player/          # 队列、播放模式、状态机
│   ├── mpv/             # 进程与 IPC
│   ├── ui/              # tcell 界面与页面
│   └── capabilities/    # 设备能力探测
├── docs/                # 本组设计文档
├── testdata/            # 测试音频与 fixture
└── Makefile
```

---

## 6. 关键设计决策（摘要）

1. **播放归 mpv，Sonata 只发指令**：V1 起就用 IPC socket，而不是每首歌重启 mpv；避免切歌断音、音量重置、状态丢失。
2. **状态文件是唯一真相**：`state.json` 记录当前曲目、进度、模式、音量、收藏；TUI 与 `sonata status` 都读它。
3. **后台播放**：mpv 以 `--idle=yes` 常驻，Sonata 退出不杀进程；下次启动接管已有 socket。
4. **锁屏是输入围栏，不是安全边界**：只屏蔽输入，不改变系统权限；与 heya 的"接管"语义一致。
5. **终端布局即掌机布局**：90×30 字符网格 ≈ 720×480；未来 SDL 版按同一比例放大。

---

## 7. 验收标准（V1）

- `sonata scan` 在测试目录上输出歌曲/艺术家/专辑计数，缺失标签时用文件名回退。
- `sonata` 打开 TUI：4 区布局、方向键导航、Enter 播放、Space 暂停、L1/R1 切页、长按 Select 锁屏、长按 Start 熄屏。
- 退出 Sonata 后 `sonata status` 显示 `playing`，`mpv` 进程仍在播放。
- 再次启动 Sonata 恢复上次曲目与进度。
- `go test ./...` 覆盖 library/state/player/mpv 命令编码/UI 渲染快照。
- `CGO_ENABLED=0 GOARCH=arm64 go build ./...` 成功。

---

## 8. 交付物

- 代码：V1 可运行二进制 + 单元/集成测试。
- 文档：`PROJECT_PLAN.md`、`ARCHITECTURE.md`、`UI_GUIDELINES.md`、`CLI_WIREFRAMES.md`、`MPV_INTEGRATION.md`、`STORAGE_LAYOUT.md`、`RISKS.md`。
- 自审：文档与代码一致性检查清单（见第 9 节）。

---

## 9. 自审清单（写完文档、进入开发前逐项确认）

- [x] 7 份文档齐全，且每份回答 issue 中对应问题。
- [x] Manifest/存储格式有版本号与降级策略（STORAGE_LAYOUT §4）。
- [x] mpv 方案对比了"直接启动 / IPC / 后台服务 / 队列同步 / 恢复"五项（MPV_INTEGRATION §2）。
- [x] UI 布局、字号、按键语义在 UI_GUIDELINES 与 CLI_WIREFRAMES 中一致。
- [x] 锁屏与熄屏的输入规则明确，且不伪装成安全边界（UI_GUIDELINES §6）。
- [x] 风险清单覆盖 mpv 缺失、socket 竞态、扫描性能、标签缺失、后台进程泄漏（RISKS.md）。
- [x] V1 范围可在一周内实现，且不依赖 V2–V5 的协议。
