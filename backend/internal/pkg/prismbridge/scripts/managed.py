"""sub2api-owned lifecycle adapter for the vendored Prism Bridge.

One process/profile per account. Configuration (including cookies) arrives on
stdin, never command-line arguments. EOF shuts down the HTTP server/browser.
"""
import json
import os
import sys
import threading
import urllib.parse


def main():
    config = json.loads(sys.stdin.readline())
    # Ignore ambient Prism settings: account isolation and model selection must
    # not depend on a previous standalone bridge invocation.
    for key in list(os.environ):
        if key.startswith("PRISM_"):
            del os.environ[key]
    os.environ.update({
        "PRISM_PROFILE_DIR": config["profile"],
        "PRISM_CALLER_OWNED_TOOLS": "true",
        "PRISM_BRIDGE_API_KEY": config["key"],
        "PRISM_FORWARD_CLIENT_INSTRUCTIONS": "1",
        "PRISM_COMPACT_MAX_PARTS": "0",
        "PRISM_ALLOW_MODEL_FALLBACK": "0",
    })
    import bridge
    from pathlib import Path
    bridge.RELAY_STATE_FILE = Path(config["profile"]).parent / "relay-sessions.json"

    # Browser startup errors stay local, and arbitrary local callers cannot
    # trigger a browser by probing the original unauthenticated /health route.
    original_get = bridge.Handler.do_GET
    def authenticated_get(self):
        if not self._reject_auth():
            original_get(self)
    bridge.Handler.do_GET = authenticated_get

    bridge.load_cookie = lambda: config["cookie"]
    # A gateway must never interpret client prompts as paths on its own host.
    bridge.collect_local_image_refs = lambda *args: []
    bridge._local_image_path = lambda ref: None
    bridge.extract_file_action_from_text = lambda *args, **kwargs: None
    original_resolve_image = bridge.resolve_image_bytes
    bridge.resolve_image_bytes = lambda ref, timeout=15: (
        original_resolve_image(ref, timeout) if ref.startswith("data:") else None
    )

    # Apply the account proxy to the entire browser, including Sentinel.
    original_playwright = bridge.sync_playwright
    class ManagedPlaywright:
        def __enter__(self):
            self.owner = original_playwright()
            pw = self.owner.__enter__()
            original_launch = pw.chromium.launch_persistent_context
            def launch(**kwargs):
                if config.get("browser"):
                    kwargs.pop("channel", None)
                    kwargs["executable_path"] = config["browser"]
                if config.get("proxy"):
                    parsed = urllib.parse.urlsplit(config["proxy"])
                    host = parsed.hostname
                    if ":" in host:
                        host = "[" + host + "]"
                    scheme = "socks5" if parsed.scheme == "socks5h" else parsed.scheme
                    proxy = {"server": f"{scheme}://{host}:{parsed.port}"}
                    if parsed.username:
                        proxy["username"] = urllib.parse.unquote(parsed.username)
                    if parsed.password:
                        proxy["password"] = urllib.parse.unquote(parsed.password)
                    kwargs["proxy"] = proxy
                return original_launch(**kwargs)
            pw.chromium.launch_persistent_context = launch
            return pw
        def __exit__(self, *args):
            return self.owner.__exit__(*args)
    bridge.sync_playwright = ManagedPlaywright

    # Reserve an OS-assigned loopback port before announcing readiness. Browser
    # initialization is lazy and serialized by the Go account lease.
    server = bridge.ThreadingHTTPServer(("127.0.0.1", 0), bridge.Handler)
    server.daemon_threads = True
    print(json.dumps({"port": server.server_port}), flush=True)
    # Upstream diagnostic text can contain request content. Do not put it on
    # stdout (our control protocol) or in sub2api's logs.
    sys.stdout = open(os.devnull, "w")
    sys.stderr = open(os.devnull, "w")
    bridge.load_relay_state()
    def watch_parent():
        sys.stdin.read()
        server.shutdown()
    threading.Thread(target=watch_parent, daemon=True).start()
    try:
        server.serve_forever(poll_interval=0.2)
    finally:
        server.server_close()
        if bridge.WORKER:
            bridge.WORKER.q.put(None)
            for thread in threading.enumerate():
                if thread.name == "prism-pw":
                    thread.join(timeout=3)


if __name__ == "__main__":
    main()
