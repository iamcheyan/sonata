# Sonata / heya Application Manifest v1 compatibility

Sonata follows heya's manifest contract in `docs/application-manifest-spec.md`.
The integration manifest is `integrations/heya/apps/sonata/app.json`.

| heya v1 field | Sonata value | Effect |
|---|---|---|
| `manifestVersion` | `1` | Stable v1 contract |
| `id` | `sonata` | Stable identity independent of display name |
| `entrypoint` | `/usr/bin/sonata` | Direct argv execution; no shell |
| `args` | `--music /storage/music --data-dir /storage/heya/data/sonata` | Library and persistent state locations |
| `workingDirectory` | `/storage/music` | Default file-browser context |
| `icon` | `nf-md-music` | Uses heya's bundled Nerd Font map |
| `category` | `media` | Ordinary application category |
| `permissions` | `audio`, `storage` | Display/capability declarations only, not sandbox enforcement |
| `features` | `resume-support` | Descriptive; unknown identifiers are ignored |

## Storage contract

- Music library: `/storage/music` (read-only to Sonata).
- Persistent JSON state: `/storage/heya/data/sonata`.
- Rebuildable scan index: `/storage/heya/cache/sonata`.
- mpv log: `/storage/heya/logs/sonata/mpv.log`.
- Heya may read `data/sonata/state.json`, but must not write it. The JSON `version`
  and field names are owned by Sonata; readers should ignore unknown fields.

## Launch and return

Heya launches the binary with the manifest's argv directly. It does not need
Sonata-specific launcher logic. The launcher event loop suspends while Sonata is
running, then resumes when Sonata exits. Sonata detaches mpv with a Unix IPC socket
under `$XDG_RUNTIME_DIR/sonata/`; closing the UI closes only the IPC client, so
mpv keeps playing. Reopening Sonata reconnects to that socket. `sonata stop`
explicitly ends background playback.

This is consistent with Everything is an Application: music and games both use
the standard Application Provider and the same manifest contract.

## Device acceptance checklist

- Heya discovers Sonata with category `media` and icon `nf-md-music`.
- Confirm launches `/usr/bin/sonata` with the two manifest arguments.
- Back/exit returns to heya while mpv continues playing.
- Reopening Sonata shows the current track and progress.
- `sonata stop` stops the background player.
