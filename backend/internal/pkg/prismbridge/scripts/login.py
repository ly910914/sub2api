"""Private remote login browser. Only the Go manager can access this loopback API.

No cookies, passwords, URLs or browser diagnostics are logged. The parent
owns and removes the browser's temporary profile and process tree.
"""
import base64
import hmac
import json
import math
import os
import queue
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit, unquote

ORIGIN = "https://prism.openai.com"
WIDTH, HEIGHT = 1100, 800


def valid_cookie(cookies):
    # cookies() is scoped to Prism's URL, excluding unrelated identity providers.
    token = next((c["value"] for c in cookies if c["name"] == "prism_oai_access_token"), "")
    try:
        part = token.split(".")[1]
        claims = json.loads(base64.urlsafe_b64decode(part + "=" * (-len(part) % 4)))
        if not isinstance(claims, dict):
            return ""
        expiry = float(claims.get("exp", 0))
        if not math.isfinite(expiry) or expiry <= time.time() + 60:
            return ""
    except (ValueError, IndexError, TypeError):
        return ""
    return "; ".join(f"{c['name']}={c['value']}" for c in cookies)


def allowed_navigation(url):
    parsed = urlsplit(url)
    host = parsed.hostname or ""
    return parsed.scheme == "https" and (
        host == "openai.com" or host.endswith(".openai.com")
        or host.endswith(".auth0.com")
        or host in {"accounts.google.com", "login.microsoftonline.com", "login.live.com",
                    "account.live.com", "appleid.apple.com", "idmsa.apple.com",
                    "challenges.cloudflare.com"}
    )


def proxy_options(raw):
    """Translate a stored proxy URL into Playwright's proxy structure."""
    if not raw:
        return None
    parsed = urlsplit(raw)
    try:
        port = parsed.port
    except ValueError as exc:
        raise ValueError("invalid proxy") from exc
    if parsed.scheme not in {"http", "https", "socks5", "socks5h"} or not parsed.hostname or not port:
        raise ValueError("invalid proxy")
    host = f"[{parsed.hostname}]" if ":" in parsed.hostname else parsed.hostname
    scheme = "socks5" if parsed.scheme == "socks5h" else parsed.scheme
    result = {"server": f"{scheme}://{host}:{port}"}
    if parsed.username:
        result["username"] = unquote(parsed.username)
    if parsed.password:
        result["password"] = unquote(parsed.password)
    return result


def perform(page, action):
    kind = action.get("type")
    if kind == "click":
        x, y = action.get("x"), action.get("y")
        if not all(isinstance(v, (int, float)) and math.isfinite(v) for v in (x, y)):
            raise ValueError("coordinates")
        if not (0 <= x < WIDTH and 0 <= y < HEIGHT):
            raise ValueError("coordinates")
        page.mouse.click(x, y)
    elif kind == "text":
        value = action.get("text")
        if not isinstance(value, str) or not 0 < len(value) <= 4096:
            raise ValueError("text")
        page.keyboard.insert_text(value)
    elif kind == "key":
        key = action.get("key")
        if key not in {"Tab", "Shift+Tab", "Enter", "Backspace", "Delete", "Escape",
                       "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "ControlOrMeta+A"}:
            raise ValueError("key")
        page.keyboard.press(key)
    elif kind == "scroll":
        delta = action.get("delta")
        if not isinstance(delta, (int, float)) or not math.isfinite(delta):
            raise ValueError("scroll")
        page.mouse.wheel(0, max(-800, min(800, delta)))
    else:
        raise ValueError("action")


def main():
    config = json.loads(sys.stdin.readline())
    control_stdout = sys.stdout
    sys.stdout = open(os.devnull, "w")
    sys.stderr = open(os.devnull, "w")
    tasks = queue.Queue(maxsize=8)
    state = {"status": "starting"}
    lock = threading.Lock()
    stop = threading.Event()

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_args):
            pass

        def do_POST(self):
            if not hmac.compare_digest(self.headers.get("Authorization", ""), "Bearer " + config["key"]):
                self.send_error(401)
                return
            try:
                size = int(self.headers.get("Content-Length", "0"))
                if not 0 < size <= 32768:
                    raise ValueError("size")
                data = json.loads(self.rfile.read(size))
                if not isinstance(data, dict):
                    raise ValueError("body")
                if self.path == "/action":
                    result = queue.Queue(maxsize=1)
                    tasks.put_nowait((data, result))
                    reply = result.get(timeout=10)
                elif self.path == "/state":
                    with lock:
                        reply = dict(state)
                else:
                    raise ValueError("path")
                payload = json.dumps(reply).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)
            except (ValueError, queue.Empty, queue.Full):
                self.send_error(400)

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    server.daemon_threads = True
    print(json.dumps({"port": server.server_port}), file=control_stdout, flush=True)
    threading.Thread(target=server.serve_forever, daemon=True).start()

    def watch_parent():
        sys.stdin.read()
        stop.set()
    threading.Thread(target=watch_parent, daemon=True).start()
    display = None
    try:
        # A private virtual display permits headed Chromium in a Docker/server
        # deployment. Windows uses headless Chromium so no desktop is exposed.
        headed = sys.platform.startswith("linux")
        if headed:
            display = subprocess.Popen(
                ["Xvfb", "-displayfd", "1", "-screen", "0", "1100x800x24", "-nolisten", "tcp"],
                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
            )
            os.environ["DISPLAY"] = ":" + display.stdout.readline().decode().strip()
        from playwright.sync_api import sync_playwright
        with sync_playwright() as pw:
            options = dict(user_data_dir=config["profile"], headless=not headed,
                           viewport={"width": WIDTH, "height": HEIGHT}, accept_downloads=False)
            if config.get("browser"):
                options["executable_path"] = config["browser"]
            if config.get("proxy"):
                options["proxy"] = proxy_options(config["proxy"])
            browser = pw.chromium.launch_persistent_context(**options)
            browser.set_default_timeout(4000)
            def route_request(route):
                request = route.request
                if request.is_navigation_request() and not allowed_navigation(request.url):
                    route.abort()
                else:
                    route.continue_()
            browser.route("**/*", route_request)
            page = browser.pages[0]
            page.goto(ORIGIN, wait_until="domcontentloaded", timeout=60000)
            deadline = time.monotonic() + 600
            while not stop.is_set() and time.monotonic() < deadline:
                pages = [p for p in browser.pages if not p.is_closed()]
                if not pages:
                    break
                page = pages[-1]  # OAuth providers may open a new window.
                cookie = valid_cookie(browser.cookies(ORIGIN))
                if cookie:
                    with lock:
                        state.clear()
                        state.update(status="completed", cookie=cookie)
                    browser.close()
                    stop.wait(max(0, deadline - time.monotonic()))
                    return
                try:
                    action, result = tasks.get_nowait()
                    try:
                        perform(page, action)
                        result.put({"ok": True})
                    except Exception:
                        result.put({"ok": False})
                except queue.Empty:
                    pass
                try:
                    frame = base64.b64encode(page.screenshot(type="jpeg", quality=65, timeout=4000)).decode()
                    origin = urlsplit(page.url)
                    with lock:
                        state.update(status="pending", frame=frame,
                                     origin=f"{origin.scheme}://{origin.netloc}")
                except Exception:
                    pass  # Navigations can invalidate one screenshot.
                page.wait_for_timeout(150)
            browser.close()
    except Exception:
        with lock:
            state.clear()
            state.update(status="failed")
        stop.wait(600)
    finally:
        server.shutdown()
        if display:
            display.terminate()


if __name__ == "__main__":
    main()
