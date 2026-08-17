---
name: cdp-browser
description: Use whenever work requires a real rendered browser or browser interaction on Delirium, including dynamic or React-based sites, authenticated sites, user-directed web actions, DOM inspection, console or network diagnosis, screenshots, and browser-backed verification.
---

# CDP Browser

Use the `cdp` CLI, which is expected to be in `PATH`, to inspect and control the user's existing Chromium-based browsers through Chrome DevTools Protocol.

Never install or use Playwright, Puppeteer, Selenium, or another competing browser-automation system. If `cdp` lacks a needed feature, prefer a small wrapper around the CLI or a direct CDP WebSocket connection.

## Choose a browser

Probe ports 2102 and 2103 before acting, for example with `cdp tabs list --port PORT --plain`.

- Port 2102 is the user's main Brave browser. Prefer it for anything likely to require an existing login, such as email, Discord, or the user's GitHub account and repositories.
- Port 2103 is the general Chromium browser. Prefer it for public sites and work that does not require a login.
- If uncertain, begin with 2103 and move to 2102 only if authentication is needed.
- If only one port is available, use that one.
- Never launch the browser on port 2102 when it is absent.
- If neither is available and browser work is needed, launch the general browser on 2103 with:

```sh
chromium --user-data-dir=/home/light/sync/config/chromium --profile-directory=Default --remote-debugging-port=2103
```

The environment sets `CDP_PORT=2102`, so commands silently default to the main browser. Pass `--port 2103` explicitly when choosing the general browser.

## Work with a page

Use a distinct, task-specific session name. Typical operations are:

```sh
cdp tabs list --port 2103 --plain
cdp connect --session TASK --port 2103 --url 'example.com'
cdp connect --session TASK --port 2103 --new
cdp read --session TASK 'body'
cdp wait-visible --session TASK '.ready'
cdp click --session TASK 'button:has-text(Continue)'
cdp type --session TASK 'input[name=q]' 'search text'
cdp eval --session TASK --json '({title: document.title, url: location.href})'
cdp screenshot --session TASK --output /tmp/page.png
cdp disconnect --session TASK
```

Run `cdp --help` or `cdp COMMAND --help` for the exact interface. Useful commands also include `hover`, `drag`, `gesture`, `key`, `scroll`, `wheel`, `upload`, `dom`, `styles`, `rect`, `emulate`, `log`, `keep-alive`, and `tabs open|switch|close`.

Use browser interaction when the user asks to interact with a site. Do not substitute private API calls or `curl` for the requested browser action. Browser rendering is also appropriate when static fetching cannot reveal a dynamic page's actual content or state.

## Capture network traffic

Use `network-log` when the important behavior is hidden in page requests, responses, streaming updates, authentication flows, or dynamically loaded data:

```sh
cdp network-log --session TASK --dir /tmp/TASK-network
cdp network-log --session TASK --dir /tmp/TASK-network --url 'api|graphql' --method 'GET|POST' --status '2..' --mime 'json'
```

It is a long-running capture. Leave it running while reproducing the browser behavior, then interrupt it cleanly. It writes timestamped request and response artifacts to the selected directory for inspection with tools such as `rg`, `jq`, and `tail`.
