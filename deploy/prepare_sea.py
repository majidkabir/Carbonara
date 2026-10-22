"""Download Eurostat SeaRoute's maritime network and convert it to GeoJSON.

Usage: python prepare_sea.py RESOLUTION_KM OUTPUT.geojson

Standard library only (sqlite3 reads the GeoPackage). The network is
EUPL-1.2 licensed and is downloaded on your machine, not shipped with
Sootcase. Files are pinned to a SeaRoute commit and checked by SHA-256.
"""
import hashlib
import json
import os
import sqlite3
import struct
import sys
import tempfile
import urllib.request

COMMIT = "88a2e568a8e0144d1f5a81c3931a7bc2bcce6901"
URL = ("https://raw.githubusercontent.com/eurostat/searoute/" + COMMIT +
       "/modules/core/src/main/resources/marnet/marnet_plus_{res}km.gpkg")
SHA256 = {
    "5": "75ca6cc130b3748f568196a9caf62889e9f096ec358f65223b4095ac070be0e7",
    "10": "738cf880623dee8be3616a3ca46b565cc47a6548003ebd4438fea309dc7fa4f9",
    "20": "8d909ba49d5062e1bdd7b305ee768e3366f1eab2d04b4ac448dfbdf9bf71a915",
    "50": "44a309ffa0fbdc02e00616ed6a9e20ad92c773994b8f4502995cee9ffdf96acf",
    "100": "d0cd431c40efc2aa2cb1c218e0bf0590ef8dd0ba30eee157abe848900b16f3af",
}


def convert(gpkg, out):
    """Write the GeoPackage's LineStrings as a GeoJSON FeatureCollection."""
    db = sqlite3.connect(gpkg)
    table = db.execute("select table_name from gpkg_contents where data_type='features'").fetchone()[0]
    features = []
    for blob, passage in db.execute(f'select geometry, pass from "{table}"'):
        if blob[:2] != b"GP":
            raise ValueError("not a GeoPackage geometry")
        envelope = {0: 0, 1: 32, 2: 48, 3: 48, 4: 64}[(blob[3] >> 1) & 7]
        wkb = blob[8 + envelope:]
        order = "<" if wkb[0] == 1 else ">"
        gtype = struct.unpack(order + "I", wkb[1:5])[0]
        dims = {2: 2, 1002: 3, 2002: 3, 3002: 4}.get(gtype)
        if dims is None:
            raise ValueError(f"unsupported geometry type {gtype}")
        n = struct.unpack(order + "I", wkb[5:9])[0]
        coords = []
        for i in range(n):
            pt = struct.unpack_from(order + "d" * dims, wkb, 9 + i * 8 * dims)
            coords.append([round(pt[0], 7), round(pt[1], 7)])
        features.append({"type": "Feature", "properties": {"pass": passage},
                         "geometry": {"type": "LineString", "coordinates": coords}})
    with open(out, "w") as f:
        json.dump({"type": "FeatureCollection", "features": features}, f, separators=(",", ":"))
    return len(features)


def main():
    if len(sys.argv) != 3 or sys.argv[1] not in SHA256:
        sys.exit(f"usage: prepare_sea.py {{{','.join(SHA256)}}} OUTPUT.geojson")
    res, out = sys.argv[1], sys.argv[2]
    stamp = out + ".source"
    want = f"{COMMIT} {res}km"
    if os.path.exists(out) and os.path.exists(stamp) and open(stamp).read() == want:
        print(f"sea network up to date: {out}")
        return
    os.makedirs(os.path.dirname(os.path.abspath(out)), exist_ok=True)
    url = URL.format(res=res)
    print(f"downloading {url}")
    # Validate the URL to prevent SSRF.
    if not url.startswith("https://raw.githubusercontent.com/eurostat/searoute/"):
        sys.exit(f"Invalid URL: {url}")
    with urllib.request.urlopen(url, timeout=120) as r:
        data = r.read()
    digest = hashlib.sha256(data).hexdigest()
    if digest != SHA256[res]:
        sys.exit(f"checksum mismatch for {url}: got {digest}")
    with tempfile.NamedTemporaryFile(suffix=".gpkg", delete=False) as tmp:
        tmp.write(data)
    try:
        n = convert(tmp.name, out + ".tmp")
    finally:
        os.unlink(tmp.name)
    os.replace(out + ".tmp", out)
    with open(stamp, "w") as f:
        f.write(want)
    print(f"wrote {out}: {n} lines ({res} km network)")


if __name__ == "__main__":
    main()