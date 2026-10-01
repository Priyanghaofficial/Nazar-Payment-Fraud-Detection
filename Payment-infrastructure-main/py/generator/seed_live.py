#!/usr/bin/env python3
"""Live seeder: replay generated events against a running Nazar instance.

    python seed_live.py --file ../../data/generated/demo_scenarios.jsonl \
        --url http://localhost:8080 --delay-ms 300

POSTs each event to POST {url}/v1/decide, spaced out by --delay-ms, and prints the resulting
`action` as it goes -- this is what drives the live monitor screen during a demo.

By default, timestamps are rewritten to "now" at send time (--rewrite-timestamps, on by
default): the generated files carry timestamps from whenever they were generated, but a live
demo reads better when events look like they're happening in real time. Pass
--no-rewrite-timestamps to send the file's own event_ts_ms/accepted_at_ms verbatim (e.g. for
testing point-in-time behaviour).

Connection errors are retried a couple of times, then the event is skipped -- one bad request
never aborts the whole run.
"""
from __future__ import annotations

import argparse
import json
import sys
import time
import urllib.error
import urllib.request
from typing import Optional


def post_event(url: str, event: dict, timeout_s: float) -> Optional[dict]:
    body = json.dumps(event).encode("utf-8")
    req = urllib.request.Request(
        url, data=body, method="POST",
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=timeout_s) as resp:
        return json.loads(resp.read().decode("utf-8"))


def send_with_retry(url: str, event: dict, retries: int, timeout_s: float) -> Optional[dict]:
    last_err = None
    for attempt in range(retries + 1):
        try:
            return post_event(url, event, timeout_s)
        except urllib.error.HTTPError as e:
            # server responded but with an error status -- surface the body, don't retry
            try:
                detail = e.read().decode("utf-8")
            except Exception:
                detail = str(e)
            print(f"  HTTP {e.code}: {detail}")
            return None
        except (urllib.error.URLError, TimeoutError, ConnectionError) as e:
            last_err = e
            if attempt < retries:
                time.sleep(0.5 * (attempt + 1))
                continue
    print(f"  connection failed after {retries + 1} attempt(s): {last_err}")
    return None


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--file", type=str, default="../../data/generated/demo_scenarios.jsonl")
    ap.add_argument("--url", type=str, default="http://localhost:8080")
    ap.add_argument("--delay-ms", type=int, default=350, help="Delay between events, milliseconds.")
    ap.add_argument("--limit", type=int, default=0, help="Only send the first N events (0 = all).")
    ap.add_argument("--retries", type=int, default=2)
    ap.add_argument("--timeout-s", type=float, default=5.0)
    ap.add_argument("--rewrite-timestamps", dest="rewrite_timestamps", action="store_true", default=True)
    ap.add_argument("--no-rewrite-timestamps", dest="rewrite_timestamps", action="store_false")
    args = ap.parse_args()

    endpoint = args.url.rstrip("/") + "/v1/decide"

    events = []
    with open(args.file) as f:
        for line in f:
            line = line.strip()
            if line:
                events.append(json.loads(line))
    if args.limit:
        events = events[: args.limit]

    print(f"Seeding {len(events)} event(s) from {args.file} -> {endpoint} "
          f"(delay={args.delay_ms}ms, rewrite_timestamps={args.rewrite_timestamps})")

    ok = 0
    failed = 0
    action_counts: dict = {}
    for i, ev in enumerate(events, 1):
        if args.rewrite_timestamps:
            now = int(time.time() * 1000)
            ev = dict(ev)
            ev["accepted_at_ms"] = now
            ev["event_ts_ms"] = now

        result = send_with_retry(endpoint, ev, args.retries, args.timeout_s)
        if result is None:
            failed += 1
            print(f"[{i}/{len(events)}] {ev.get('end_to_end_id')} -> SKIPPED (no response)")
        else:
            ok += 1
            decision = result.get("decision", {})
            action = decision.get("action", "?")
            action_counts[action] = action_counts.get(action, 0) + 1
            replayed = result.get("replayed", False)
            print(f"[{i}/{len(events)}] {ev.get('end_to_end_id')} -> {action}"
                  f"{' (replayed)' if replayed else ''}")

        if i < len(events) and args.delay_ms > 0:
            time.sleep(args.delay_ms / 1000.0)

    print(f"\nDone: {ok} ok, {failed} failed/skipped.")
    if action_counts:
        print("Action distribution:", action_counts)
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
