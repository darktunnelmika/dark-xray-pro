"""Input-guard tests use synthetic Git fixtures, not a substitute for real core CI."""
import contextlib
import importlib.util
import io
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("overlay", Path(__file__).with_name("apply_vless_sessions.py"))
overlay = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(overlay)

HANDLER = '''package inbound
import (\n\t"strings"\n)
type Handler struct {\n}
func (h *Handler) Close() error {\nreturn nil\n}
func (h *Handler) AddUser() error {\n\treturn h.validator.Add(u)\n}
func (h *Handler) RemoveUser() error {\n\th.RemoveReverse(h.validator.GetByEmail(e))\n\treturn h.validator.Del(e)\n}
func (h *Handler) Process() {\n\tinbound := session.InboundFromContext(ctx)\n}
'''
PROXY = 'package proxy\nfunc copy() {\n\t\t\tw, err := tc.ReadFrom(readerConn)\n}\n'


class OverlayGuards(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name).resolve() / "source"
        self.root.mkdir()
        self.git("init", "-q")
        self.handler = self.root / "proxy/vless/inbound/inbound.go"
        self.handler.parent.mkdir(parents=True)
        self.handler.write_text(HANDLER)
        (self.root / "proxy/proxy.go").write_text(PROXY)
        self.git("add", ".")
        self.commit()

    def git(self, *args):
        return subprocess.check_output(["git", "-c", "core.hooksPath=/dev/null", "-C", str(self.root), *args], stderr=subprocess.DEVNULL)

    def commit(self):
        self.git("-c", "user.name=QA", "-c", "user.email=qa@example.invalid", "commit", "-qm", "synthetic guard fixture")
        self.sha = self.git("rev-parse", "HEAD").decode().strip()

    def execute(self, root=None):
        with patch.object(overlay, "UPSTREAM", self.sha), patch.object(overlay.os, "geteuid", return_value=1000), contextlib.redirect_stdout(io.StringIO()):
            overlay.apply(root or self.root)

    def snapshot(self):
        return {str(p.relative_to(self.root)): p.read_bytes() for p in self.root.rglob("*") if p.is_file() and ".git" not in p.parts}

    def test_clean_fixture_applies_only_expected_files(self):
        self.execute()
        self.assertEqual(len(self.snapshot()), 6)
        self.assertIn("trackUserSession", self.handler.read_text())
        self.assertIn("proCopyMeteredRaw", (self.root / "proxy/proxy.go").read_text())

    def test_repeat_and_dirty_input_never_overwrite(self):
        self.execute()
        before = self.snapshot()
        with self.assertRaises(ValueError):
            self.execute()
        self.assertEqual(before, self.snapshot())

    def test_untracked_file_prevents_any_mutation(self):
        (self.root / "operator-notes").write_text("preserve this")
        before = self.snapshot()
        with self.assertRaises(ValueError):
            self.execute()
        self.assertEqual(before, self.snapshot())

    def test_missing_anchor_validates_before_writing(self):
        self.handler.write_text(HANDLER.replace('"strings"', '"strconv"'))
        self.git("add", ".")
        self.commit()
        before = self.snapshot()
        with self.assertRaises(ValueError):
            self.execute()
        self.assertEqual(before, self.snapshot())

    def test_wrong_revision_root_and_symlink_are_rejected(self):
        before = self.snapshot()
        with patch.object(overlay.os, "geteuid", return_value=1000), self.assertRaises(ValueError):
            overlay.apply(self.root)
        with patch.object(overlay.os, "geteuid", return_value=0), self.assertRaises(ValueError):
            overlay.apply(self.root)
        alias = self.root.parent / "alias"
        alias.symlink_to(self.root, target_is_directory=True)
        with self.assertRaises(ValueError):
            self.execute(alias)
        self.assertEqual(before, self.snapshot())


if __name__ == "__main__":
    unittest.main()
