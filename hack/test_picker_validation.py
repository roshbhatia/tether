import json
import pathlib
import subprocess
import sys
import tempfile
import unittest

PROVIDER = pathlib.Path(__file__).resolve().parents[1] / "extras/wezterm/provider.py"

class ValidationTest(unittest.TestCase):
    def invoke(self, core):
        request = {"version": "provider/v1", "kind": "request", "requestId": "validation-test", "capability": "provider.validate", "input": {}}
        result = subprocess.run([sys.executable, str(PROVIDER), str(core)], input=json.dumps(request), capture_output=True, text=True, check=True)
        frame = json.loads(result.stdout)
        self.assertEqual(frame["requestId"], request["requestId"])
        return frame

    def test_missing_runtime_is_an_error(self):
        with tempfile.TemporaryDirectory() as root:
            self.assertEqual(self.invoke(pathlib.Path(root) / "absent")["status"], "error")

    def test_unusable_runtime_is_an_error(self):
        with tempfile.TemporaryDirectory() as root:
            core = pathlib.Path(root) / "core"
            core.write_text("#!/bin/sh\nexit 23\n")
            core.chmod(0o755)
            self.assertEqual(self.invoke(core)["status"], "error")

    def test_working_runtime_is_valid(self):
        with tempfile.TemporaryDirectory() as root:
            core = pathlib.Path(root) / "core"
            for executable in (core, pathlib.Path(root) / "tsh"):
                executable.write_text("#!/bin/sh\nexit 0\n")
                executable.chmod(0o755)
            self.assertEqual(self.invoke(core)["output"], {"ok": True})

if __name__ == "__main__":
    unittest.main()
