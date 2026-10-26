# Sonata — UI Redesign (cpr67)

Sonata's interface is now a **single-pane handheld design**: no permanent detail
panel, no statistics dashboard, no dead screen space. Every page fills the panel
and the player is its own centered frame.

## Layout rules

- Top status bar: `♪ Sonata` + current track + playback indicator, right side is
  battery and clock.
- One separator rule under the header.
- Full-width list; selected row is highlighted and prefixed with `›`.
- Bottom hint bar: `A Select   B Back` (plus `Select Favorite` on song pages,
  `L/R Previous/Next` on the player).
- A now-playing strip appears while browsing so playback stays visible without
  a second pane.
- Now playing: centered `♫` icon, title, artist/album, progress bar, mode and
  volume, and transport buttons.

## Navigation

```
Music Library
  ↓
All Songs / Artists / Albums / Favorites / Recently Played
  ↓
Track → Player
```

`A` opens, `B` goes back, `D-Pad` moves, `Select` toggles favorite, `L1/R1`
previous/next, `Start` play/pause. Long-press `Select` locks, long-press `Start`
blanks the screen. `L1 + R1` unlocks.

## Pages

| Frame | Content |
|---|---|
| `01-main` | Main menu with library counts |
| `02-songs` | All songs, full width |
| `03-player` | Centered now playing with progress |
| `04-artists` | Artist list |
| `05-albums` | Album list |
| `06-favorites` | Favorited songs |
| `07-lock` | Lock screen with unlock hint |

## Rendering

Frames are produced by `sonata --dump <dir>` with a `StaticController`, so they are
reproducible on headless machines. PNGs are rendered from the captured terminal
rows; `.txt` files keep the source capture.