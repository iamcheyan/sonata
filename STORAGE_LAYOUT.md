# Sonata — Storage Layout

目标：无数据库强依赖、可读可修、原子写入、可备份、可 OTA。

---

## 1. 根目录

默认 `$XDG_DATA_HOME/sonata`（桌面）或 `/storage/sonata`（掌机，可用 `--data-dir` 覆盖）。

```
/storage/sonata/
├── config.json          # 用户配置（音乐目录、按键映射、自动熄屏）
├── state.json           # 当前播放状态（唯一真相，供 heya 读取）
├── favorites.json       # 收藏
├── history.json         # 最近播放/最近添加
├── playlists/           # V2：命名播放列表，每个 .json 一个
└── cache/
    ├── library.json     # 扫描缓存（版本 + 指纹 + 索引）
    └── covers/          # 预留：封面缓存（V2+）
```

| 目录/文件 | 职责 | 生命周期 | 备份 | OTA |
|---|---|---|---|---|
| `config.json` | 用户偏好 | 长期，用户可编辑 | ✅ 建议 | 保留（不覆盖） |
| `state.json` | 播放状态 | 高频写，退出/切歌/暂停时 | ⚠️ 可选 | 保留 |
| `favorites.json` | 收藏 | 长期 | ✅ 建议 | 保留 |
| `history.json` | 最近播放/添加 | 长期，可裁剪（保留最近 500 条） | ✅ 建议 | 保留 |
| `playlists/` | 用户播放列表 | 长期 | ✅ 建议 | 保留 |
| `cache/library.json` | 扫描缓存 | 可随时重建 | ❌ 不备份 | 可丢弃 |
| `cache/covers/` | 封面缓存 | 可重建 | ❌ | 可丢弃 |

**备份策略**：备份 `config/state/favorites/history/playlists`，跳过 `cache/`。
**OTA 策略**：升级只替换二进制与默认资源，不触碰用户数据目录；`cache/` 可主动失效（版本不匹配时重建）。

---

## 2. 文件格式（全部 JSON，UTF-8，2 空格缩进）

### config.json

```json
{
  "version": 1,
  "musicDir": "/storage/music",
  "autoScreenOff": "1m",
  "volume": 70,
  "input": {
    "confirm": ["Enter"],
    "back": ["Escape"],
    "pagePrev": ["q"],
    "pageNext": ["e"],
    "playPause": [" "],
    "quickAction": ["Tab"]
  }
}
```

### state.json

```json
{
  "version": 1,
  "updatedAt": "2026-10-26T04:55:12Z",
  "status": "playing",
  "songId": "s_8f31c2",
  "path": "/storage/music/周杰伦/七里香/01 七里香.flac",
  "title": "七里香",
  "artist": "周杰伦",
  "album": "七里香",
  "position": 72.4,
  "duration": 272.0,
  "mode": "list-loop",
  "volume": 70,
  "mpvPid": 41233,
  "socket": "/run/user/1000/sonata/mpv.sock"
}
```

`status`：`playing` / `paused` / `stopped` / `degraded`。

### favorites.json

```json
{ "version": 1, "songs": ["s_8f31c2", "s_1a02de"], "updatedAt": "..." }
```

### history.json

```json
{
  "version": 1,
  "recentlyPlayed": [{"songId": "s_8f31c2", "at": "2026-10-26T04:40:00Z"}],
  "recentlyAdded": [{"songId": "s_1a02de", "at": "2026-10-20T11:02:00Z"}]
}
```

---

## 3. 原子写

所有写入走同一路径：

1. 写 `file.json.tmp`（同目录，保证同一文件系统）；
2. `f.Sync()`；
3. `os.Rename(tmp, file.json)`；
4. 失败时保留旧文件，返回错误。

频率控制：`state.json` 去抖 500ms + 退出强制 flush；`favorites/history` 立即写。

---

## 4. 版本与迁移

每个文件都有 `version` 字段。启动时：

| 情况 | 行为 |
|---|---|
| `version == 当前` | 正常加载 |
| `version < 当前` | 运行迁移函数链（`migrate_1_to_2`…），成功后写回新版本 |
| `version > 当前` | **只读模式**：加载已知字段，不写回（避免旧版本破坏新数据），UI 提示"数据由更新版本创建" |
| 文件缺失 | 使用默认值创建 |
| JSON 损坏 | 备份为 `<name>.corrupt-<ts>`，使用默认值继续 |

---

## 5. 曲目身份（Song ID）

`ID = "s_" + base36(fnv64(相对路径))`

- 相对路径基于 `musicDir`，移动整个音乐库不影响 ID。
- 目录/文件重命名会改变 ID：缓存重建时通过 `(artist, album, track, title, duration)` 做尽力重匹配，命中则迁移收藏与历史。
- 不把绝对路径作为 ID：掌机上挂载点可能变化。

---

## 6. 存储方案对比（issue 要求）

| 方案 | 优点 | 缺点 | 适用 |
|---|---|---|---|
| **JSON 文件** | 可读可修、零依赖、易备份、跨平台 | 大库（>50k 曲）全量读写慢；无事务/索引 | **推荐：V1–V3 的默认** |
| SQLite | 事务、索引、查询快、单文件 | 需 CGO（或纯 Go 驱动）、交叉编译与镜像体积增加、二进制格式不便手改 | V4+ 若库规模超 20k 曲再评估 |
| BoltDB | 纯 Go、单文件、B+ 树、事务 | 二进制格式、生态小、调试不便、仍是"数据库强依赖" | 不推荐（违背 issue 的"无数据库强依赖"） |
| 目录 + 文件名编码元数据 | 无解析成本 | 无法表达状态/历史，重命名即丢数据 | 不采用 |

**推荐**：JSON + 原子写 + 缓存文件 + 版本字段。达到以下任一条件时再引入 SQLite（作为可选后端，不破坏 JSON 路径）：
- 曲目数 > 20,000；
- 冷启动扫描 + 反序列化 > 2s；
- 需要并发多写（V4 daemon）。

---

## 7. 缓存文件格式

```json
{
  "version": 1,
  "root": "/storage/music",
  "generatedAt": "2026-10-26T04:50:00Z",
  "fingerprint": {"files": 128, "bytes": 734003200, "newestMtime": 1761400000},
  "songs": [{"id": "s_8f31c2", "path": "...", "title": "...", "artist": "...", "album": "...", "track": 1, "duration": 272.0}],
  "artists": [{"name": "周杰伦", "albums": ["七里香"], "songCount": 10}],
  "albums": [{"name": "七里香", "artist": "周杰伦", "year": 2004, "songIds": ["s_8f31c2"]}]
}
```

失效条件：`root` 变化、文件数/字节数/最新 mtime 变化、`version` 不匹配、`sonata scan --refresh`。
