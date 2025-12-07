#!/usr/bin/env python3
"""Focus a Hinata Web tab and prep the reply textarea via cdp-cli commands."""

from __future__ import annotations

import argparse
import json
import os
import shlex
import subprocess
import sys
import textwrap
from typing import Any, Dict, List


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "tab_key",
        nargs="?",
        default="[CDP:hw]",
        help="Substring to locate in the tab title (default: %(default)s)",
    )
    parser.add_argument(
        "--host",
        default=os.getenv("CDP_HOST", "127.0.0.1"),
        help="DevTools host (default: %(default)s or $CDP_HOST)",
    )
    parser.add_argument(
        "--port",
        type=int,
        default=env_port_default(),
        help="DevTools port (default: $CDP_PORT or 9222)",
    )
    parser.add_argument(
        "--session",
        default="reply-hnt-web",
        help="Session name to register with cdp connect (default: %(default)s)",
    )
    return parser.parse_args()


def env_port_default() -> int:
    raw = os.getenv("CDP_PORT", "").strip()
    if raw:
        try:
            value = int(raw)
            if value > 0:
                return value
        except ValueError:
            pass
    return 9222


def run_cdp(cmd: List[str], capture: bool = False) -> str:
    kwargs = {"text": True}
    if capture:
        kwargs["capture_output"] = True
    process = subprocess.run(cmd, check=False, **kwargs)
    if process.returncode != 0:
        stderr = process.stderr if capture else ""
        quoted = " ".join(shlex.quote(part) for part in cmd)
        message = f"Command {quoted} failed with exit code {process.returncode}."
        if stderr:
            message = f"{message} {stderr.strip()}"
        raise SystemExit(message)
    if capture:
        assert process.stdout is not None
        return process.stdout
    return ""


def list_tabs(host: str, port: int) -> List[Dict[str, Any]]:
    output = run_cdp(
        [
            "cdp",
            "tabs",
            "list",
            "--host",
            host,
            "--port",
            str(port),
            "--pretty=false",
        ],
        capture=True,
    )
    try:
        return json.loads(output)
    except json.JSONDecodeError as exc:
        raise SystemExit(f"Unable to parse cdp tabs list output as JSON: {exc}") from exc


def find_tab(tabs: List[Dict[str, Any]], key: str) -> Dict[str, Any]:
    needle = key.lower()
    for tab in tabs:
        title = (tab.get("title") or "").lower()
        if needle in title:
            return tab
    available = ", ".join(tab.get("title") or "<untitled>" for tab in tabs)
    raise SystemExit(f"No tab title contains {key!r}. Available titles: {available}")


def switch_tab(tab_id: str, host: str, port: int) -> None:
    run_cdp(
        [
            "cdp",
            "tabs",
            "switch",
            "--host",
            host,
            "--port",
            str(port),
            tab_id,
        ]
    )


def connect_session(session: str, target_url: str, host: str, port: int) -> None:
    run_cdp(
        [
            "cdp",
            "connect",
            session,
            "--host",
            host,
            "--port",
            str(port),
            "--url",
            target_url,
        ]
    )


def focus_reply(session: str) -> None:
    script = textwrap.dedent(
        """
        (() => {
          try {
            window.scrollTo(0, document.documentElement.scrollHeight || document.body.scrollHeight || 0);
          } catch (err) {}
          const el = document.querySelector('textarea#new-message-content');
          if (el) {
            el.focus();
            if (typeof el.setSelectionRange === 'function') {
              const pos = el.value.length;
              el.setSelectionRange(pos, pos);
            }
          }
        })()
        """
    ).strip()
    run_cdp(["cdp", "eval", session, script])


def main() -> None:
    args = parse_args()
    tabs = list_tabs(args.host, args.port)
    if not tabs:
        raise SystemExit("No discoverable tabs. Is Chrome running with --remote-debugging-port?")
    tab = find_tab(tabs, args.tab_key)
    tab_id = tab.get("id")
    if not tab_id:
        raise SystemExit("Selected tab has no ID; cannot continue.")
    switch_tab(tab_id, args.host, args.port)
    target_url = tab.get("url")
    if not target_url:
        raise SystemExit("Selected tab has no URL; cannot connect.")
    connect_session(args.session, target_url, args.host, args.port)
    focus_reply(args.session)
    print("Tab activated and textarea focused.")


if __name__ == "__main__":
    main()
