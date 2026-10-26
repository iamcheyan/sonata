# Sonata — Design Self-Review

本文件记录"文档写完之后、进入开发之前"的自审，以及开发中因实测而修正的设计。
每条都对应一个真实问题，不是形式检查。

---

## 1. 自审结论（文档阶段）

| # | 检查项 | 结论 | 处理 |
|---|---|---|---|
| 1 | 7 份文档是否覆盖 issue 的全部问题 | 覆盖 | PROJECT_PLAN §9 逐项勾选 |
| 2 | 播放职责是否清晰 | mpv 独占解码/输出，Sonata 只发指令 | MPV_INTEGRATION §1 明确"不做 A 方案" |
| 3 | 后台播放是否可验证 | 需要"退出后仍在播"的验收项 | PROJECT_PLAN §7 第 3 条；实测通过 |
| 4 | 状态文件是否会被 heya 读取 | 是，字段需冻结 | STORAGE_LAYOUT §2 冻结字段名 + version |
| 5 | 锁屏是否被误当成安全边界 | 有风险 | UI_GUIDELINES §6 明确"输入围栏，不是隔离" |
| 6 | 存储是否引入数据库强依赖 | 否 | STORAGE_LAYOUT §6 对比 4 方案，JSON 为准 |
| 7 | V1 范围是否过大 | 略大（含锁屏/熄屏/后台） | 保留：三者是"手持设备可用"的最低要求，其余推迟到 V2+ |

**结论**：文档可以进入开发；唯一需要提前锁定的是 `state.json` 字段与目录布局，已在 STORAGE_LAYOUT 中冻结。

---

## 2. 开发中因实测而修正的设计

### 2.1 CJK 宽字符被覆盖（严重）

- 现象：`顺序播放` 渲染成 `顺播`，`七里香` 渲染成 `七香`。
- 原因：绘制时每个 rune 只前进 1 列，而 CJK 占 2 列，下一个字符覆盖了右半格。
- 修正：新增 `internal/ui/text.go`，所有绘制/截断/对齐改走 `displayWidth()`（基于 `go-runewidth`）。
- 影响面：列表、状态栏、详情、锁屏居中全部受影响；已全部修正并有测试覆盖。

### 2.2 根页面按 B 会直接退出

- 现象：在主页按 B 退出到 shell，掌机上等于"按错键掉出 launcher"。
- 修正：B 只做 Back，根页面不响应；退出改为显式 `X` / Ctrl-C。

### 2.3 随机播放会立刻重播当前曲

- 现象：切到随机后第一首还是当前曲；一轮结束后也会重复。
- 修正：洗牌序列以当前曲开头，一轮结束重洗后跳过首位。

### 2.4 命令行 flag 放在位置参数之后被忽略

- 现象：`sonata play song.mp3 --data-dir /tmp/x` 使用了默认数据目录。
- 原因：Go `flag` 在第一个非 flag 参数处停止解析。
- 修正：`reorderFlags()` 先收集 flag 再交给 `flag.Parse`，位置参数保持顺序。

### 2.5 音量/pid 没有落到状态文件

- 现象：首次播放后 `state.json` 的 `volume` 为 0，`mpvPid` 缺失。
- 修正：新状态从 `config.volume` 初始化；`Connect` 记录 mpv pid 与 socket 路径。

### 2.6 曲目时长依赖 ffprobe

- 设计：`dhowden/tag` 不提供时长，Sonata 不解码音频。
- 实现：有 `ffprobe` 时读取 `format=duration`，否则时长为 0，UI 显示 `--:--`（不猜）。
- 权衡：宁可显示未知，也不显示错误时长。

---

## 3. 实测证据

| 验证 | 命令 | 结果 |
|---|---|---|
| 单元测试 | `go test ./...` | library / mpv / player / service / state / ui 全绿 |
| 静态检查 | `go vet ./...`、`gofmt -l .` | 无输出 |
| ARM64 交叉编译 | `CGO_ENABLED=0 GOARCH=arm64 go build ./cmd/sonata` | `ELF 64-bit LSB executable, ARM aarch64, statically linked` |
| 真实扫描 | `sonata scan --music /tmp/sonata-fix` | 4 songs / 3 artists / 3 albums / 0 skipped（真实 FLAC/MP3 标签 + ffprobe 时长） |
| 缓存命中 | 再执行一次 scan | `cached: 4 songs…` |
| 真实播放 | `sonata play <album> --ao null` | mpv 进程存活，`state.json` 写入 `playing` |
| 后台播放 | 播放进程退出后 `sonata status` | `playing`，mpv 仍在运行（`--idle=yes` + `setsid`） |
| TUI 渲染 | tmux 90×30 抓屏 | `docs/screenshots/01..04`：主菜单、歌曲列表、正在播放、锁屏 |
| 锁屏围栏 | 锁屏后按方向键/回车 | 选中项与页面栈不变；空格仍可暂停 |
| 解锁手势 | 锁屏下依次按 L1(q) + R1(e) | 解锁成功 |
| 集成测试 | `TestPlaybackIntegration`（真实 mpv，`--ao=null`） | 进度前进、暂停状态、状态文件一致 |

截图目录：`docs/screenshots/`（终端文本抓屏，90×30，对应 720×480 横屏布局）。

---

## 4. 已知未完成（与 V1 范围一致）

- 播放列表（V2）：当前只有临时队列，无命名列表与 M3U 导入导出。
- 封面/歌词/标签编辑：不在范围（非目标）。
- UI 退出后进度不再刷新：状态文件保留最后位置，下次启动 `Resume()` 续播。
- 触摸/鼠标交互：未实现（键盘/手柄优先）。
- SDL/图形界面：V1 不做；布局常量已按 720×480 固化，便于移植。
- 掌机实机（RG34XX）验证：需要 mpv 与 tcell 在 ROCKNIX 上的运行验证，尚未进行。
