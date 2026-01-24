• Here’s what I ran into using cdp on MiniMax:

  - cdp eval doesn’t support --wait even though the help suggests it; async code can’t be
    awaited, so any await/Promise work in eval fails.
  - cdp type generates untrusted input events in React-controlled inputs. In this UI that
    means the value changes but the app state doesn’t, so buttons stay disabled. It’s the
    biggest blocker.
  - cdp network-log seems to miss some in‑page requests (I could only see them after user
    actions + “Proceed” dialog, and sometimes only history/list calls showed). It doesn’t
    capture all fetch/xhr in this app.
  - cdp eval sometimes returns "object" instead of JSON when the JS result is a DOM element
    or something not serializable; clearer error or auto‑stringify option would help.
  - cdp tabs open doesn’t accept flags like --activate; if you try it, it errors. Could
    document or accept --activate consistently.

  Feature requests that would help:

  1. Trusted input mode for cdp type (dispatch InputEvent with isTrusted or simulate native
     input). Without this, React apps often ignore input.
  2. cdp eval --await that resolves Promises before returning.
  3. cdp network-log --include-all / devtools “log all requests” equivalent; right now it
     seems to miss some fetches.
  4. cdp eval --json or --stringify option to avoid "object" returns and make outputs
     deterministic.
  5. cdp click --force to bypass cursor-not-allowed states (for popover confirmations).

  If you want, I can draft a short repro script for the cdp type trusted-input issue; that’s
  the most actionable bug report.


