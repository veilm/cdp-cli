# cdp-cli

A tiny Chrome DevTools Protocol helper for poking at live tabs (DOM snapshots, styles/rect metrics, screenshots, console streaming, etc.).

```
git clone https://github.com/veilm/cdp-cli
./cdp-cli/install.sh # will prompt for sudo. requires Go only
cdp --help
```

Use `cdp --help` (or `cdp <command> --help`) for switches and examples. Highlights:

- `cdp eval manager --file script.js --pretty` (or `--stdin`) runs multi-line scripts without shell gymnastics.
- `cdp rect manager ".selector"` prints a DOMRect snapshot.
- `cdp tabs --plain` quickly shows the currently discoverable tabs when you're picking one to connect to.
- Set `CDP_PRETTY=1` in your shell to make pretty JSON the default for eval output.

## other similar projects
- `https://github.com/myers/cdp-cli`: looks pretty good judging from the README.
would probably be fine for my use case tbh but I just personally prefer only
using software that I've watched codex make. and I dislike node
