# Sonata × heya

Sonata is exposed to heya as an ordinary v1 Application. No game-specific or
Sonata-specific launcher behavior is required.

## Install

1. Build the standalone binary for the device (`make build-arm64`).
2. Install it at `/usr/bin/sonata` in the device image.
3. Install `apps/sonata/app.json` at `/storage/heya/apps/sonata/app.json`.
4. Ensure `mpv` is installed and the audio backend is available.

The manifest uses heya Application Manifest v1 (`id`, `entrypoint`, `args`,
`workingDirectory`, `permissions`, `features`). `args` are passed as argv
entries directly; no shell is involved. Heya does not enforce the declared
permissions. They describe the app's needs for display and future capability
negotiation only.

## Storage and playback lifetime

- Persistent state: `/storage/heya/data/sonata` (favorites, history, playback state).
- Library: `/storage/music`.
- mpv is launched detached with a user runtime IPC socket. Closing Sonata returns
to heya while audio continues. `sonata stop` explicitly stops mpv. No system
service is needed for this lifecycle.
- Heya may read `data/sonata/state.json` for now-playing display; it should treat
unknown fields as forward-compatible and must not write that file.

## Manual verification on device

- Heya discovers Sonata as one ordinary app (category `media`).
- Confirm launches `/usr/bin/sonata` with the two manifest arguments.
- Back/exit returns to heya; mpv keeps playing.
- Reopening Sonata reconnects to mpv and displays current playback.
- `sonata stop` stops the background player.
