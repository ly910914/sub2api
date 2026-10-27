"""Runtime smoke tests: no Prism login, browser download or upstream calls."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import urllib.error
import urllib.request


class ManagedRuntimeTest(unittest.TestCase):
    def test_loopback_auth_and_parent_eof(self):
        with tempfile.TemporaryDirectory() as directory:
            process = subprocess.Popen(
                [sys.executable, "-u", str(Path(__file__).with_name("managed.py"))],
                stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                text=True, encoding="utf-8",
                env={**os.environ, "PRISM_HOST": "0.0.0.0", "PRISM_ALLOW_MODEL_FALLBACK": "1"},
            )
            try:
                config = {"profile": str(Path(directory) / "profile"), "cookie": "fake", "key": "test-private-key"}
                process.stdin.write(json.dumps(config) + "\n")
                process.stdin.flush()
                port = json.loads(process.stdout.readline())["port"]
                opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
                def post(body, key):
                    request = urllib.request.Request(
                        f"http://127.0.0.1:{port}/v1/responses",
                        data=json.dumps(body).encode(),
                        headers={"Authorization": f"Bearer {key}", "Content-Type": "application/json"},
                    )
                    with self.assertRaises(urllib.error.HTTPError) as caught:
                        opener.open(request, timeout=10)
                    return caught.exception.code
                self.assertEqual(401, post({"input": "hello"}, "wrong-key"))
                self.assertEqual(400, post({}, config["key"]))
                # Both invalid requests must be rejected before worker startup.
                self.assertFalse((Path(directory) / "profile").exists())
                process.stdin.close()
                self.assertEqual(0, process.wait(timeout=10))
                self.assertEqual("", process.stdout.read())
                self.assertEqual("", process.stderr.read())
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait()
                process.stdout.close()
                process.stderr.close()


if __name__ == "__main__":
    unittest.main()
