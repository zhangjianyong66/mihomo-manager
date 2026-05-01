# Mihomo Manager (Interactive CLI)

`mm` is now a fully interactive Mihomo manager built with Go.

## Install

```bash
make install
```

## Usage

```bash
mm
```

Default behavior opens an interactive terminal UI.

## Keybindings

- `↑/↓`: move selection
- `Enter`: confirm/enter
- `Esc`: go back
- `q`: quit (from main menu)

## Features

- Service management: status/start/stop/restart/reload/test/logs
- Node management: current/switch/speed test (low concurrency)/switch fastest
- Subscription: save URL/show URL/update from saved URL
- Whitelist: add/remove/list direct domains
- Config: backup/restore/edit/apply route preset (CN direct + others via `GLOBAL`)

## Notes

- Legacy non-interactive commands are no longer supported.
- YAML format/order/comments may change after config writes.
- Speed testing uses low concurrency (`5`) for stability.
