#!/usr/bin/env python3
"""Portable guards for the Microsoft Store package; makeappx runs only on Windows."""
from pathlib import Path
import struct
import tempfile
import unittest
import xml.etree.ElementTree as ElementTree
import zlib

from package_msix import (VALIDATION_IDENTITY, asset_plan, check_identity, msix_version, png, render_icon,
                          render_manifest, write_assets)

STORE_IDENTITY = {
    "identityName": "12345Example.LedgeSync",
    "publisher": "CN=01234567-89AB-CDEF-0123-456789ABCDEF",
    "publisherDisplayName": "Example Publisher",
}
NS = {"m": "http://schemas.microsoft.com/appx/manifest/foundation/windows10",
      "uap": "http://schemas.microsoft.com/appx/manifest/uap/windows10"}


def png_size(data: bytes) -> tuple[int, int]:
    if data[:8] != b"\x89PNG\r\n\x1a\n" or data[12:16] != b"IHDR":
        raise AssertionError("not a PNG")
    return struct.unpack(">II", data[16:24])


def pixels(data: bytes) -> tuple[int, int, bytes]:
    width, height = png_size(data)
    start = data.index(b"IDAT") + 4
    length = struct.unpack(">I", data[start - 8:start - 4])[0]
    return width, height, zlib.decompress(data[start:start + length])


class StorePackageGuards(unittest.TestCase):
    def test_versions_follow_store_rules_and_always_increase(self):
        self.assertEqual(msix_version("0.1.0-alpha.8"), "1.1.8.0")
        sequence = ["0.1.0-alpha.8", "0.1.0-alpha.9", "0.1.0-beta.1", "0.1.0-rc.1", "0.1.0", "0.1.1-alpha.1", "0.2.0-alpha.1", "1.0.0"]
        mapped = [tuple(int(part) for part in msix_version(v).split(".")) for v in sequence]
        self.assertEqual(mapped, sorted(mapped))
        self.assertEqual(len(set(mapped)), len(mapped))
        for value in mapped:
            self.assertGreaterEqual(value[0], 1)
            self.assertEqual(value[3], 0)
        for invalid in ("0.1", "0.1.0-alpha.0", "0.1.0-alpha.300", "0.1.0-dev.1", "0.1.65", "0.1.66-alpha.1", "65535.0.0"):
            with self.assertRaises(ValueError, msg=invalid):
                msix_version(invalid)

    def test_identity_must_look_like_partner_center_values(self):
        self.assertEqual(check_identity(STORE_IDENTITY), STORE_IDENTITY)
        check_identity(VALIDATION_IDENTITY)
        for key, value in (("identityName", "bad name"), ("identityName", "x"), ("publisher", "O=Missing CN"),
                           ("publisher", "CN=line\nbreak"), ("publisherDisplayName", " "), ("publisherDisplayName", "a\tb")):
            with self.assertRaises(ValueError, msg=(key, value)):
                check_identity({**STORE_IDENTITY, key: value})

    def test_manifest_values_are_escaped_and_assets_exist(self):
        identity = {**STORE_IDENTITY, "publisherDisplayName": 'Tom & "Jerry" <Co>'}
        for arch in ("x64", "arm64"):
            root = ElementTree.fromstring(render_manifest(identity, "1.1.8.0", arch).encode())
            node = root.find("m:Identity", NS)
            self.assertEqual((node.get("Name"), node.get("Publisher"), node.get("Version"), node.get("ProcessorArchitecture")),
                             (identity["identityName"], identity["publisher"], "1.1.8.0", arch))
            self.assertEqual(root.find("m:Properties/m:PublisherDisplayName", NS).text, identity["publisherDisplayName"])
            application = root.find("m:Applications/m:Application", NS)
            self.assertEqual((application.get("Executable"), application.get("EntryPoint")), ("LedgeSync.exe", "Windows.FullTrustApplication"))
            references = [root.find("m:Properties/m:Logo", NS).text]
            visual = application.find("uap:VisualElements", NS)
            references += [visual.get("Square150x150Logo"), visual.get("Square44x44Logo"), visual.find("uap:DefaultTile", NS).get("Wide310x150Logo")]
            plan = asset_plan()
            for reference in references:
                folder, name = reference.split("\\")
                self.assertEqual(folder, "Assets")
                self.assertTrue(any(asset.startswith(name.removesuffix(".png") + ".scale-100") for asset in plan), reference)
        with self.assertRaises(ValueError):
            render_manifest(STORE_IDENTITY, "1.1.8.0", "x86")

    def test_icon_is_centered_and_transparent_outside(self):
        width, height, raw = pixels(png(310, 150, render_icon(100)))
        self.assertEqual((width, height), (310, 150))
        self.assertEqual(len(raw), height * (1 + 4 * width))

        def pixel(x, y):
            offset = y * (1 + 4 * width) + 1 + 4 * x
            return raw[offset:offset + 4]
        self.assertEqual(pixel(5, 75)[3], 0)
        self.assertEqual(pixel(0, 0)[3], 0)
        self.assertEqual(pixel(155 - 30, 75 - 30)[3], 255)
        self.assertEqual(tuple(pixel(155 - 30, 75 - 30)[:3]), (18, 89, 237))
        with self.assertRaises(ValueError):
            png(10, 10, render_icon(16))

    def test_every_asset_has_its_planned_size(self):
        with tempfile.TemporaryDirectory(prefix="ledgesync-msix-assets-") as temporary:
            directory = Path(temporary) / "Assets"
            names = write_assets(directory)
            self.assertEqual(sorted(path.name for path in directory.iterdir()), names)
            for name, (width, height, _icon) in asset_plan().items():
                self.assertEqual(png_size((directory / name).read_bytes()), (width, height), name)


if __name__ == "__main__":
    unittest.main()
