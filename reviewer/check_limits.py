#!/usr/bin/env python3

import json
import os
import sys
import urllib.error
import urllib.request

API_URL = "https://openrouter.ai/api/v1/key"


def format_amount(value: object) -> str:
    if value is None:
        return "not set"
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return f"${value:g}"
    return str(value)


def format_status(payload: object) -> str:
    if not isinstance(payload, dict):
        raise ValueError("OpenRouter returned an unexpected response (missing data)")
    data = payload.get("data")
    if not isinstance(data, dict):
        raise ValueError("OpenRouter returned an unexpected response (missing data)")

    lines = [
        "OpenRouter API key status",
        f"  Label: {data.get('label') or 'not set'}",
        f"  Usage: {format_amount(data.get('usage'))}",
        f"  Limit: {format_amount(data.get('limit'))}",
        f"  Remaining: {format_amount(data.get('limit_remaining'))}",
    ]

    if "is_free_tier" in data:
        lines.append(f"  Free tier: {'yes' if data['is_free_tier'] else 'no'}")

    rate_limit = data.get("rate_limit")
    if isinstance(rate_limit, dict):
        requests = rate_limit.get("requests")
        interval = rate_limit.get("interval")
        if requests is not None and interval is not None:
            lines.append(f"  Rate limit: {requests} requests per {interval}")

    return "\n".join(lines)


def main() -> int:
    api_key = os.environ.get("OPENROUTER_API_KEY")
    if not api_key:
        print("OPENROUTER_API_KEY is not set", file=sys.stderr)
        return 1

    request = urllib.request.Request(
        API_URL,
        headers={"Authorization": f"Bearer {api_key}"},
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            payload = json.loads(response.read())
    except urllib.error.HTTPError as exc:
        print(f"OpenRouter returned HTTP {exc.code}", file=sys.stderr)
        return 1
    except urllib.error.URLError as exc:
        print(f"Could not connect to OpenRouter: {exc.reason}", file=sys.stderr)
        return 1
    except json.JSONDecodeError:
        print("OpenRouter returned invalid JSON", file=sys.stderr)
        return 1

    try:
        print(format_status(payload))
    except ValueError as exc:
        print(str(exc), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
