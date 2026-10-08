"""Decky backend for the E-Ink Faceplate plugin.

This file only supervises the bundled ``monoinkd`` binary and forwards the
panel's requests to it over a user-only Unix socket. It uses the Python
standard library exclusively, so it has no dependencies to break.
"""

import asyncio
import base64
import http.client
import json
import os
import shutil
import socket

import decky

BUNDLED_BINARY = os.path.join(decky.DECKY_PLUGIN_DIR, "bin", "monoinkd")
LOG_FILE = os.path.join(decky.DECKY_PLUGIN_LOG_DIR, "monoinkd.log")
LOG_MAX_BYTES = 2 * 1024 * 1024


def _runtime_dir() -> str:
    # Must match paths.RuntimeDir() in the Go code so `monoinkd push` from a
    # normal shell finds the same provider socket.
    run = f"/run/user/{os.getuid()}"
    if os.path.isdir(run):
        return os.path.join(run, "monoink")
    return os.path.join(decky.DECKY_PLUGIN_RUNTIME_DIR, "run")


RUNTIME_DIR = _runtime_dir()
CONTROL_SOCKET = os.path.join(RUNTIME_DIR, "control.sock")
PROVIDER_SOCKET = os.path.join(RUNTIME_DIR, "providers.sock")


def _binary() -> str:
    """Path to an executable monoinkd.

    Decky unpacks plugins as root and may drop the executable bit; since we
    run as the user and can't chmod there, fall back to a private copy.
    """
    if os.access(BUNDLED_BINARY, os.X_OK):
        return BUNDLED_BINARY
    copy = os.path.join(decky.DECKY_PLUGIN_RUNTIME_DIR, "bin", "monoinkd")
    src = os.stat(BUNDLED_BINARY)
    try:
        dst = os.stat(copy)
        fresh = dst.st_size == src.st_size and dst.st_mtime >= src.st_mtime
    except OSError:
        fresh = False
    if not fresh:
        os.makedirs(os.path.dirname(copy), exist_ok=True)
        tmp = copy + ".tmp"
        shutil.copyfile(BUNDLED_BINARY, tmp)
        os.chmod(tmp, 0o755)
        os.replace(tmp, copy)
    return copy


class _UnixHTTPConnection(http.client.HTTPConnection):
    def __init__(self, path: str, timeout: float):
        super().__init__("monoink", timeout=timeout)
        self._path = path

    def connect(self):
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        sock.settimeout(self.timeout)
        sock.connect(self._path)
        self.sock = sock


def _request(method: str, path: str, body, timeout: float):
    conn = _UnixHTTPConnection(CONTROL_SOCKET, timeout)
    try:
        payload = None if body is None else json.dumps(body)
        headers = {"Content-Type": "application/json"} if payload else {}
        conn.request(method, path, body=payload, headers=headers)
        resp = conn.getresponse()
        data = resp.read()
        return resp.status, resp.getheader("Content-Type", ""), data
    finally:
        conn.close()


class Plugin:
    _proc = None
    _supervisor = None
    _stopping = False

    async def _main(self):
        self._stopping = False
        self._supervisor = asyncio.get_event_loop().create_task(self._supervise())

    async def _unload(self):
        self._stopping = True
        if self._supervisor:
            self._supervisor.cancel()
        await self._stop_process()

    async def _uninstall(self):
        # Nothing to clean up outside Decky's own plugin and settings dirs.
        pass

    async def _supervise(self):
        delay = 1.0
        while not self._stopping:
            try:
                binary = _binary()
            except OSError as exc:
                decky.logger.error("monoinkd binary unavailable (%s): %s", BUNDLED_BINARY, exc)
                return
            self._rotate_log()
            env = dict(os.environ)
            # The log includes the display's Bluetooth address: keep it private.
            fd = os.open(LOG_FILE, os.O_WRONLY | os.O_APPEND | os.O_CREAT, 0o600)
            os.fchmod(fd, 0o600)  # also tighten logs created by older versions
            with os.fdopen(fd, "ab") as log:
                self._proc = await asyncio.create_subprocess_exec(
                    binary, "serve",
                    "-settings", decky.DECKY_PLUGIN_SETTINGS_DIR,
                    "-data", decky.DECKY_PLUGIN_RUNTIME_DIR,
                    "-control", CONTROL_SOCKET,
                    "-providers", PROVIDER_SOCKET,
                    stdout=log, stderr=log, env=env,
                )
            decky.logger.info("monoinkd started (pid %s)", self._proc.pid)
            started = asyncio.get_event_loop().time()
            code = await self._proc.wait()
            self._proc = None
            if self._stopping:
                return
            # Restart with backoff; reset it if the process ran for a while.
            if asyncio.get_event_loop().time() - started > 60:
                delay = 1.0
            decky.logger.warning("monoinkd exited with %s; restarting in %.0fs", code, delay)
            await asyncio.sleep(delay)
            delay = min(delay * 2, 60.0)

    async def _stop_process(self):
        proc = self._proc
        if proc is None or proc.returncode is not None:
            return
        proc.terminate()
        try:
            # monoinkd disconnects from the display before exiting.
            await asyncio.wait_for(proc.wait(), timeout=8)
        except asyncio.TimeoutError:
            proc.kill()
            await proc.wait()
        self._proc = None

    def _rotate_log(self):
        try:
            if os.path.getsize(LOG_FILE) > LOG_MAX_BYTES:
                os.replace(LOG_FILE, LOG_FILE + ".1")
        except OSError:
            pass

    # ---- methods called from the frontend ---------------------------------

    async def api(self, method: str, path: str, body=None):
        """Forward a request to monoinkd's control API (paths under /api/)."""
        if method not in ("GET", "POST", "PATCH") or not path.startswith("/api/"):
            return {"ok": False, "status": 400, "error": "invalid request"}
        timeout = 40.0 if path in ("/api/scan", "/api/weather/search") else 10.0
        try:
            status, _, data = await asyncio.to_thread(_request, method, path, body, timeout)
        except (OSError, http.client.HTTPException) as exc:
            return {"ok": False, "status": 0, "error": f"service not running: {exc}"}
        try:
            parsed = json.loads(data) if data else None
        except ValueError:
            parsed = None
        if status >= 400:
            message = parsed.get("error") if isinstance(parsed, dict) else data.decode(errors="replace")
            return {"ok": False, "status": status, "error": message}
        return {"ok": True, "status": status, "data": parsed}

    async def preview(self, screen: str):
        """Return a PNG preview of a screen as a data URL."""
        if not screen.isalpha():
            return None
        try:
            status, ctype, data = await asyncio.to_thread(
                _request, "GET", f"/api/preview/{screen}", None, 15.0)
        except (OSError, http.client.HTTPException):
            return None
        if status != 200 or ctype != "image/png":
            return None
        return "data:image/png;base64," + base64.b64encode(data).decode()

    async def info(self):
        """Paths the panel shows for transparency and troubleshooting."""
        return {
            "provider_socket": PROVIDER_SOCKET,
            "log_file": LOG_FILE,
            "settings_dir": decky.DECKY_PLUGIN_SETTINGS_DIR,
            "home": decky.DECKY_USER_HOME,
            "running": self._proc is not None and self._proc.returncode is None,
        }
