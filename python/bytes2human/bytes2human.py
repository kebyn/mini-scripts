#!/usr/bin/env python3

import sys


def bytes2human(in_bytes: int) -> str:
    """Convert byte size integer into a human-readable string."""
    suffixes = ("B", "KB", "MB", "GB", "TB", "PB")
    suffix = 0
    while in_bytes > 9999 and suffix < len(suffixes):
        in_bytes = in_bytes >> 10
        suffix += 1

    if suffix >= len(suffixes):
        suffix -= 1
    return f"{in_bytes}{suffixes[suffix]}"


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print(f"Usage: {sys.argv[0]} <bytes>", file=sys.stderr)
        sys.exit(1)
    for arg in sys.argv[1:]:
        print(bytes2human(int(arg)))
