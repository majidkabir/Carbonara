"""Download a file if the URL changed since last time.

Usage: python download.py URL DEST

Writes DEST.url next to DEST; re-runs with the same URL do nothing.
Standard library only.
"""
import os
import shutil
import sys
import urllib.request


def main():
    if len(sys.argv) != 3:
        sys.exit("usage: download.py URL DEST")
    url, dest = sys.argv[1], sys.argv[2]
    stamp = dest + ".url"
    if os.path.exists(dest) and os.path.exists(stamp) and open(stamp).read() == url:
        print(f"{dest} up to date")
        return
    os.makedirs(os.path.dirname(os.path.abspath(dest)), exist_ok=True)
    print(f"downloading {url}")
    tmp = dest + ".part"
    with urllib.request.urlopen(url, timeout=120) as r, open(tmp, "wb") as f:
        shutil.copyfileobj(r, f, length=1 << 20)
    if os.path.getsize(tmp) == 0:
        sys.exit("download was empty")
    os.replace(tmp, dest)
    with open(stamp, "w") as f:
        f.write(url)
    print(f"wrote {dest} ({os.path.getsize(dest) / 1e6:.1f} MB)")


if __name__ == "__main__":
    main()
