# cdp-cli

A tiny Chrome DevTools Protocol helper for poking at live tabs (DOM snapshots, styles/rect metrics, screenshots, console streaming, etc.).

```
git clone https://github.com/veilm/cdp-cli
./cdp-cli/install.sh # will prompt for sudo. requires Go only
cdp --help
```

Prerequisite: run a Chromium-based browser with its CDP port exposed.

- Chromium example: `chromium --remote-debugging-port=9992`
- Brave example: `brave --remote-debugging-port=9992`
- Other Chromium-based browsers use the same flag pattern. Firefox-based browsers aren't supported
- QtWebEngine-based browsers like qutebrowser are supported; see their docs for how to enable CDP

Use `cdp --help` (or `cdp <command> --help`) for switches and examples. Highlights:

- `cdp eval --session manager --file script.js --pretty` (or `--stdin`) runs multi-line scripts without shell gymnastics.
- `cdp eval --session manager --json --wait "({ready: document.readyState})"` can wait for load and JSON-serialize values.
- Tip: if your JS starts with an object literal, wrap it like `({a: 1})` so it isn't parsed as a block.
- Tip: `click`, `type`, and `hover` also accept inline `:has-text(...)` at the end of the selector (e.g. `.btn:has-text(Submit)`), which maps to `--has-text`.
- `cdp rect --session manager ".selector"` prints a DOMRect snapshot.
- `cdp tabs list --plain` quickly shows the currently discoverable tabs when you're picking one to connect to.
- `cdp connect --session manager --tab 3 --port 9222` binds a session by tab index or pattern.
- `cdp connect --session manager --new --port 9222` opens a new tab and connects it immediately.
- `cdp tabs open https://example.com` spawns a fresh tab (foreground by default, pass `--activate=false` for background).
- `cdp tabs switch 3` (or a target id/pattern) activates a tab directly from the CLI.
- `cdp wait --session manager --selector ".compose"` and `cdp wait-visible --session manager ".compose"` pause until the page is ready.
- Basic UI automation examples:
- `cdp click --session manager ".btn"`
- `cdp hover --session manager ".card"`
- `cdp drag --session manager ".piece" ".slot"`
- `cdp gesture --session manager "canvas" "0.1,0.5 0.9,0.5"` (draw, swipe, slide, trace)
- `cdp key --session manager "Ctrl+s"`
- `cdp type --session manager ".input" "hello"`
- `cdp scroll --session manager 800 --element ".scroll-pane"`
- `cdp upload --session manager "input[type=file]" ./file.txt`
- `cdp upload` supports multiple files and can `--wait` for the selector to exist (with `--poll` and `--timeout`).
- `cdp network-log --session manager --dir /tmp/network --url '.*\\.json'` mirrors every Fetch response into timestamped folders so you can `tail -F` or `jq` through the saved request/response artifacts without extra tooling.
- `cdp keep-alive --session manager` toggles focus/lifecycle emulation and foregrounds the tab so throttled UI pieces start rendering again.
- Set `CDP_PRETTY=1` in your shell to make pretty JSON the default for eval output.
- Set `CDP_PORT=9310` (or whatever you need) to change the default DevTools port used by commands that talk to the browser.
- Set `CDP_SESSION_NAME=manager` to make `--session` optional for commands that operate on a saved session.

## WebNav Helpers (Injected JS API)

Most UI automation commands (`click`, `hover`, `drag`, `gesture`, `key`, `type`, `scroll`) now call a small helper API injected into the page. This lets you reuse the same logic from your own `cdp eval` scripts without copy-pasting long snippets.

- Auto-injection: the first time you run one of those commands, the helpers are injected automatically.
- Manual injection: `cdp inject --session <name>` (use `--force` to re-inject).

### Helper Surface

The injection defines a global object and convenience aliases:

- `window.WebNav` (namespace)
- `window.WebNavClick`, `window.WebNavHover`, `window.WebNavDrag`, `window.WebNavGesture`, `window.WebNavKey`, `window.WebNavTypePrepare`, `window.WebNavTypeFallback`, `window.WebNavScroll`, `window.WebNavFocus`

Each helper accepts either an `HTMLElement` or a CSS selector string (or string array for `click`/`hover`/`type`).

### Examples

```
cdp eval --session manager "window.WebNavClick('#save-button')"
cdp eval --session manager "window.WebNavClick(window.myElement)"
cdp eval --session manager "window.WebNavTypePrepare('#title', '', '', 'Hello', false)"
cdp eval --session manager "window.WebNavScroll(200, 0, '#scroll-pane', true)"
```

Notes:
- `WebNavClick(target, hasTextSpec, attValueSpec, count)` returns `{submitForm, selector}`.
- `WebNavClick` accepts NodeList/HTMLCollection/iterables. By default it clicks the first element; pass `opts={all:true}` to click all (e.g. `WebNavClick(document.querySelectorAll('button'), '', '', 1, {all:true})`).
- `WebNavHover(target, hasTextSpec, attValueSpec)` returns `{x, y, selector}`.
- `WebNavDrag(fromTarget, toTarget, fromIndex, toIndex, delayMs)` performs a drag/drop.
- `WebNavGesture(target, points, delayMs)` performs pointer down/move/up along `[[x,y], ...]` relative to the element.
- `WebNavKey({key, code, ctrlKey, shiftKey, altKey, metaKey})` dispatches keydown/keyup.
- `WebNavTypePrepare(target, hasTextSpec, attValueSpec, inputText, append)` prepares selection/value and returns a state object. If `handled` is false, you can follow with `Input.insertText` (what `cdp type` does).
- `WebNavTypeFallback(target, hasTextSpec, attValueSpec, inputText, append)` is a last-resort textContent setter.
- `WebNavScroll(yPx, xPx, elementTarget, emit)` scrolls window or element and returns `{scrollTop, scrollLeft}`.
- `WebNavElements.hasText(text, opts)` filters an element collection by text content. It is available on `NodeList` as `hasText` when WebNav is injected.

Example:

```
cdp eval --session manager "WebNavClick(document.querySelectorAll('button').hasText('Press me'))"
```
