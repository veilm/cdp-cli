# cdp-cli

A tiny Chrome DevTools Protocol helper for poking at live tabs (DOM snapshots, styles/rect metrics, screenshots, console streaming, etc.).

```
git clone https://github.com/veilm/cdp-cli
./cdp-cli/install.sh # will prompt for sudo. requires Go only
cdp --help
```

Use `cdp --help` (or `cdp <command> --help`) for switches and examples. Highlights:

- `cdp eval manager --file script.js --pretty` (or `--stdin`) runs multi-line scripts without shell gymnastics.
- `cdp eval manager --json --wait "({ready: document.readyState})"` can wait for load and JSON-serialize values.
- `cdp rect manager ".selector"` prints a DOMRect snapshot.
- `cdp tabs list --plain` quickly shows the currently discoverable tabs when you're picking one to connect to.
- `cdp connect manager --tab 3 --port 9222` binds a session by tab index or pattern.
- `cdp tabs open https://example.com` spawns a fresh tab (foreground by default, pass `--activate=false` for background).
- `cdp tabs switch 3` (or a target id/pattern) activates a tab directly from the CLI.
- `cdp wait manager --selector ".compose"` and `cdp wait-visible manager ".compose"` pause until the page is ready.
- `cdp click manager ".btn"` and `cdp type manager ".input" "hello"` cover basic UI automation.
- `cdp network-log manager --dir /tmp/network --url '.*\\.json'` mirrors every Fetch response into timestamped folders so you can `tail -F` or `jq` through the saved request/response artifacts without extra tooling.
- `cdp keep-alive manager` toggles focus/lifecycle emulation and foregrounds the tab so throttled UI pieces start rendering again.
- Set `CDP_PRETTY=1` in your shell to make pretty JSON the default for eval output.
- Set `CDP_PORT=9310` (or whatever you need) to change the default DevTools port used by commands that talk to the browser.

## other similar projects
- `https://github.com/myers/cdp-cli`: looks pretty good judging from the README.
would probably be fine for my use case tbh but I just personally prefer only
using software that I've watched codex make. and I dislike node
