import base64
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
import urllib.error
import urllib.request
from unittest.mock import Mock

import login


def browser_fixture_process():
    """Real Chromium and runtime, with Prism replaced by an in-memory fixture."""
    import playwright.sync_api as api
    original = api.sync_playwright
    payload = base64.urlsafe_b64encode(json.dumps({"exp": time.time() + 3600}).encode()).decode().rstrip("=")
    html = '''<form onsubmit="event.preventDefault(); if(document.querySelector('input').value==='test-login') document.cookie='prism_oai_access_token=header.PAYLOAD.sig; path=/; Secure'">
      <input autofocus aria-label="login"><button>Sign in</button></form>'''.replace("PAYLOAD", payload)

    class FixturePlaywright:
        def __enter__(self):
            self.owner = original()
            pw = self.owner.__enter__()
            launch = pw.chromium.launch_persistent_context

            def fixture_launch(**kwargs):
                browser = launch(**kwargs)
                original_route = browser.route

                def fixture_route(pattern, handler):
                    def serve(route):
                        if route.request.url.startswith(login.ORIGIN):
                            route.fulfill(status=200, content_type="text/html", body=html)
                        else:
                            route.abort()
                    original_route(pattern, serve)
                browser.route = fixture_route
                return browser
            pw.chromium.launch_persistent_context = fixture_launch
            return pw

        def __exit__(self, *args):
            return self.owner.__exit__(*args)
    api.sync_playwright = FixturePlaywright
    login.main()


class LoginTest(unittest.TestCase):
    @unittest.skipUnless(os.environ.get("SUB2API_PRISM_TEST_BROWSER"), "set a Chromium path for the browser integration test")
    def test_browser_input_cookie_capture_and_parent_cleanup(self):
        with tempfile.TemporaryDirectory() as profile:
            process = subprocess.Popen([sys.executable, "-B", "-u", str(Path(__file__)), "browser-fixture"],
                                       stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            try:
                process.stdin.write(json.dumps({"key": "test-key", "profile": profile,
                                                "browser": os.environ["SUB2API_PRISM_TEST_BROWSER"]}) + "\n")
                process.stdin.flush()
                port = json.loads(process.stdout.readline())["port"]
                opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

                def request(path, data=None, key="test-key"):
                    req = urllib.request.Request(f"http://127.0.0.1:{port}/{path}",
                                                 data=json.dumps(data or {}).encode(),
                                                 headers={"Authorization": "Bearer " + key})
                    return json.loads(opener.open(req, timeout=15).read())

                with self.assertRaises(urllib.error.HTTPError) as caught:
                    request("state", key="wrong-key")
                self.assertEqual(401, caught.exception.code)

                def wait_for(status):
                    deadline = time.monotonic() + 30
                    while time.monotonic() < deadline:
                        state = request("state")
                        self.assertNotEqual("failed", state["status"])
                        if state["status"] == status:
                            return state
                        time.sleep(0.1)
                    self.fail("browser state timed out")

                state = wait_for("pending")
                self.assertEqual(login.ORIGIN, state["origin"])
                self.assertTrue(base64.b64decode(state["frame"]).startswith(b"\xff\xd8"))
                self.assertTrue(request("action", {"type": "text", "text": "test-login"})["ok"])
                self.assertTrue(request("action", {"type": "key", "key": "Enter"})["ok"])
                state = wait_for("completed")
                self.assertIn("prism_oai_access_token=", state["cookie"])
                self.assertNotIn("frame", state)
                process.stdin.close()
                self.assertEqual(0, process.wait(timeout=10))
                self.assertEqual("", process.stdout.read())
                self.assertEqual("", process.stderr.read())
            finally:
                if process.poll() is None:
                    process.stdin.close()
                    try:
                        process.wait(timeout=10)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait()
                process.stdout.close()
                process.stderr.close()

    def test_expired_or_invalid_cookie_is_not_login(self):
        def cookie(exp):
            payload = base64.urlsafe_b64encode(json.dumps({"exp": exp}).encode()).decode().rstrip("=")
            return [{"name": "prism_oai_access_token", "value": "header." + payload + ".sig"}]
        self.assertEqual("", login.valid_cookie(cookie(time.time() - 1)))
        self.assertEqual("", login.valid_cookie([{"name": "prism_oai_access_token", "value": "bad"}]))
        self.assertIn("prism_oai_access_token=", login.valid_cookie(cookie(time.time() + 3600)))

    def test_navigation_cannot_open_local_or_arbitrary_pages(self):
        for url in ["file:///etc/passwd", "http://127.0.0.1", "https://evilopenai.com", "https://openai.com.evil.test"]:
            self.assertFalse(login.allowed_navigation(url))
        for url in ["https://prism.openai.com/", "https://auth.openai.com/", "https://accounts.google.com/login"]:
            self.assertTrue(login.allowed_navigation(url))

    def test_account_proxy_is_applied_with_credentials_and_ipv6(self):
        self.assertEqual({"server": "http://proxy.example:8080", "username": "user", "password": "pa:ss"},
                         login.proxy_options("http://user:pa%3Ass@proxy.example:8080"))
        self.assertEqual({"server": "socks5://[2001:db8::1]:1080"},
                         login.proxy_options("socks5h://[2001:db8::1]:1080"))
        with self.assertRaises(ValueError):
            login.proxy_options("http://proxy.example")
        with self.assertRaises(ValueError):
            login.proxy_options("http://proxy.example:not-a-port")

    def test_controls_reject_browser_shortcuts_and_out_of_viewport(self):
        page = Mock()
        for action in [{"type": "key", "key": "Control+L"}, {"type": "click", "x": -1, "y": 10},
                       {"type": "click", "x": float('nan'), "y": 1}, {"type": "text", "text": "x" * 4097},
                       {"type": "navigate", "url": "file:///etc/passwd"}]:
            with self.assertRaises(ValueError):
                login.perform(page, action)
        self.assertEqual([], page.mock_calls)
        login.perform(page, {"type": "text", "text": "hello"})
        page.keyboard.insert_text.assert_called_once_with("hello")


if __name__ == '__main__':
    if len(sys.argv) > 1 and sys.argv[1] == "browser-fixture":
        browser_fixture_process()
    else:
        unittest.main()
