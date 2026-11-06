#!/usr/bin/env python3

import argparse
import base64
import hashlib
import hmac
import json
import os
import secrets
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
import webbrowser
from collections.abc import Callable, Iterable, Mapping
from dataclasses import dataclass
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import padding, rsa
from cryptography.hazmat.primitives.asymmetric.rsa import RSAPrivateKey

# Built-in scopes from app/models/user_api_key_scope.rb. Plugins may add more.
KNOWN_SCOPES = frozenset(
    {
        "read",
        "write",
        "message_bus",
        "push",
        "one_time_password",
        "notifications",
        "session_info",
        "bookmarks_calendar",
        "user_status",
    }
)
DEFAULT_SCOPES = ("read",)
DEFAULT_SITE = "https://linux.do"
DEFAULT_APPLICATION_NAME = "Sample Discourse App"
SESSION_PATH = "/session/current.json"
# Discourse device auth rejects this scope. Browser and paste flows still allow it.
DEVICE_DISALLOWED_SCOPES = frozenset({"one_time_password"})
POLL_PENDING = "authorization_pending"
POLL_AUTHORIZED = "authorized"
POLL_DENIED = "access_denied"
POLL_EXPIRED = "expired_token"
MIN_POLL_SECONDS = 5
DEFAULT_TIMEOUT_SECONDS = 600
USER_AGENT = "discourse-user-api-key/1.0"


class DiscourseAuthError(Exception):
    """A User API Key request failed."""


@dataclass(frozen=True)
class SiteCapability:
    api_version: str | None
    device_code: bool


@dataclass(frozen=True)
class KeyMaterial:
    private_key: RSAPrivateKey
    public_key_pem: str
    client_id: str
    nonce: str


@dataclass(frozen=True)
class DecryptedPayload:
    key: str
    nonce: str
    push: bool
    api: int | None
    expires_at: str | None


@dataclass(frozen=True)
class SavedKey:
    site: str
    client_id: str
    key: str
    scopes: tuple[str, ...]
    api: int | None
    expires_at: str | None
    username: str | None
    path: str


HttpRequester = Callable[..., tuple[int, Mapping[str, str], bytes]]


def generate_key_material(client_id: str | None = None) -> KeyMaterial:
    private_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    public_key_pem = (
        private_key.public_key()
        .public_bytes(
            encoding=serialization.Encoding.PEM,
            format=serialization.PublicFormat.SubjectPublicKeyInfo,
        )
        .decode("ascii")
    )
    return KeyMaterial(
        private_key=private_key,
        public_key_pem=public_key_pem,
        client_id=client_id or str(uuid.uuid4()),
        nonce=secrets.token_hex(16),
    )


def normalize_site(site: str) -> str:
    value = site.strip().rstrip("/")
    parsed = urllib.parse.urlparse(value)
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        raise DiscourseAuthError(f"站点地址必须是 http 或 https URL: {site}")
    return value


def parse_scopes(value: str | Iterable[str] | None) -> tuple[str, ...]:
    if value is None:
        return DEFAULT_SCOPES
    if isinstance(value, str):
        scopes = tuple(part.strip() for part in value.split(",") if part.strip())
    else:
        scopes = tuple(part.strip() for part in value if part.strip())
    if not scopes:
        raise DiscourseAuthError("至少需要一个 scope")
    if len(scopes) != len(set(scopes)):
        raise DiscourseAuthError("scope 不能重复")
    return scopes


def warn_unknown_scopes(scopes: Iterable[str]) -> list[str]:
    return [scope for scope in scopes if scope not in KNOWN_SCOPES]


def parse_expires(value: str | None) -> int | None:
    if value is None or value == "":
        return None
    text = value.strip().lower()
    if text.isdigit():
        seconds = int(text)
    else:
        if len(text) < 2 or not text[:-1].isdigit():
            raise DiscourseAuthError("过期时间应为秒数，或 30m、12h、7d 这样的时长")
        amount = int(text[:-1])
        multiplier = {"s": 1, "m": 60, "h": 3600, "d": 86400}.get(text[-1])
        if multiplier is None:
            raise DiscourseAuthError("过期时间单位只能是 s、m、h、d")
        seconds = amount * multiplier
    if seconds <= 0:
        raise DiscourseAuthError("过期时间必须大于 0")
    return seconds


def oaep_padding() -> padding.OAEP:
    # Discourse OpenSSL rsa_padding_mode=oaep uses SHA-1 for both the hash and MGF1.
    sha1 = hashes.SHA1()
    return padding.OAEP(mgf=padding.MGF1(algorithm=sha1), algorithm=sha1, label=None)


def decrypt_payload(encoded: str, private_key: RSAPrivateKey, padding_mode: str) -> DecryptedPayload:
    compact = "".join(encoded.split())
    try:
        encrypted = base64.b64decode(compact, validate=True)
    except Exception as exc:
        raise DiscourseAuthError("payload 不是有效的 Base64") from exc
    decrypt_padding: padding.AsymmetricPadding
    if padding_mode == "oaep":
        decrypt_padding = oaep_padding()
    elif padding_mode == "pkcs1":
        decrypt_padding = padding.PKCS1v15()
    else:
        raise DiscourseAuthError("padding 只能是 oaep 或 pkcs1")
    try:
        raw = private_key.decrypt(encrypted, decrypt_padding)
        data = json.loads(raw)
    except Exception as exc:
        raise DiscourseAuthError("payload 解密或 JSON 解析失败") from exc
    if not isinstance(data, dict) or not isinstance(data.get("key"), str) or not isinstance(data.get("nonce"), str):
        raise DiscourseAuthError("payload 缺少 key 或 nonce")
    api = data.get("api")
    expires_at = data.get("expires_at")
    return DecryptedPayload(
        key=data["key"],
        nonce=data["nonce"],
        push=bool(data.get("push", False)),
        api=api if isinstance(api, int) else None,
        expires_at=expires_at if isinstance(expires_at, str) else None,
    )


def require_matching_nonce(payload: DecryptedPayload, expected: str) -> None:
    if not hmac.compare_digest(payload.nonce, expected):
        raise DiscourseAuthError("nonce 不一致，已丢弃返回的密钥")


def build_request_body(
    material: KeyMaterial,
    application_name: str,
    scopes: Iterable[str],
    padding_mode: str,
    expires_in_seconds: int | None,
    push_url: str | None,
) -> dict[str, Any]:
    body: dict[str, Any] = {
        "application_name": application_name,
        "client_id": material.client_id,
        "scopes": ",".join(scopes),
        "public_key": material.public_key_pem,
        "nonce": material.nonce,
        "padding": padding_mode,
    }
    if expires_in_seconds is not None:
        body["expires_in_seconds"] = str(expires_in_seconds)
    if push_url:
        body["push_url"] = push_url
    return body


def _header_map(headers: Mapping[str, str]) -> dict[str, str]:
    return {key.lower(): value for key, value in headers.items()}


def parse_capability(headers: Mapping[str, str]) -> SiteCapability:
    normalized = _header_map(headers)
    version = normalized.get("auth-api-version")
    device = normalized.get("auth-api-device-code", "")
    return SiteCapability(api_version=version, device_code=device.strip().lower() == "true")


def default_request(
    method: str,
    url: str,
    *,
    body: bytes | None = None,
    headers: Mapping[str, str] | None = None,
    timeout: float = 30,
) -> tuple[int, Mapping[str, str], bytes]:
    request = urllib.request.Request(url, data=body, method=method)
    request.add_header("User-Agent", USER_AGENT)
    request.add_header("Accept", "application/json")
    for name, value in (headers or {}).items():
        request.add_header(name, value)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            return response.status, dict(response.headers.items()), response.read()
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")[:500]
        raise DiscourseAuthError(f"{method} {url} 返回 HTTP {exc.code}: {detail}") from exc
    except urllib.error.URLError as exc:
        raise DiscourseAuthError(f"无法连接 {url}: {exc.reason}") from exc


def json_request(
    requester: HttpRequester,
    method: str,
    url: str,
    body: Mapping[str, Any] | None = None,
    headers: Mapping[str, str] | None = None,
) -> tuple[int, Mapping[str, str], Any]:
    encoded = None if body is None else json.dumps(body).encode("utf-8")
    request_headers = dict(headers or {})
    if encoded is not None:
        request_headers["Content-Type"] = "application/json"
    status, response_headers, raw = requester(method, url, body=encoded, headers=request_headers)
    if not raw:
        return status, response_headers, {}
    try:
        return status, response_headers, json.loads(raw)
    except json.JSONDecodeError as exc:
        raise DiscourseAuthError(f"{url} 没有返回 JSON") from exc


def probe_site(site: str, requester: HttpRequester) -> SiteCapability:
    status, headers, _body = requester("HEAD", f"{site}/user-api-key/new")
    if status >= 400:
        raise DiscourseAuthError(f"站点探测失败，HTTP {status}")
    return parse_capability(headers)


def request_device(
    site: str,
    body: Mapping[str, Any],
    requester: HttpRequester,
) -> dict[str, Any]:
    _status, _headers, payload = json_request(
        requester,
        "POST",
        f"{site}/user-api-key/device.json",
        body,
    )
    if not isinstance(payload, dict) or not payload.get("device_code"):
        raise DiscourseAuthError("站点没有返回 device_code")
    return payload


def poll_device(
    site: str,
    device_code: str,
    interval: int,
    timeout_seconds: int,
    requester: HttpRequester,
    sleeper: Callable[[float], None] = time.sleep,
    clock: Callable[[], float] = time.monotonic,
) -> str:
    deadline = clock() + timeout_seconds
    while True:
        if clock() >= deadline:
            raise DiscourseAuthError("等待授权超时")
        _status, _headers, payload = json_request(
            requester,
            "POST",
            f"{site}/user-api-key/device/poll.json",
            {"device_code": device_code},
        )
        if not isinstance(payload, dict):
            raise DiscourseAuthError("轮询响应不是 JSON 对象")
        status = payload.get("status")
        if status == POLL_PENDING:
            sleeper(interval)
            continue
        if status == POLL_AUTHORIZED:
            encrypted = payload.get("payload")
            if not isinstance(encrypted, str) or not encrypted:
                raise DiscourseAuthError("授权响应没有 payload")
            return encrypted
        if status == POLL_DENIED:
            raise DiscourseAuthError("用户拒绝了授权")
        if status == POLL_EXPIRED:
            raise DiscourseAuthError("授权请求已过期或无效")
        raise DiscourseAuthError(f"未知的轮询状态: {status!r}")


def verify_key(
    site: str,
    key: str,
    client_id: str,
    requester: HttpRequester,
) -> str | None:
    status, _headers, raw = requester(
        "GET",
        f"{site}{SESSION_PATH}",
        headers={"User-Api-Key": key, "User-Api-Client-Id": client_id},
    )
    if status != 200:
        raise DiscourseAuthError(f"密钥校验失败，HTTP {status}")
    try:
        payload = json.loads(raw) if raw else {}
    except json.JSONDecodeError as exc:
        raise DiscourseAuthError("密钥校验没有返回 JSON") from exc
    current_user = payload.get("current_user") if isinstance(payload, dict) else None
    username = current_user.get("username") if isinstance(current_user, dict) else None
    if not isinstance(username, str) or not username:
        raise DiscourseAuthError("密钥校验没有返回当前用户")
    return username


def save_key(record: Mapping[str, Any], path: str) -> None:
    parent = os.path.dirname(os.path.abspath(path))
    os.makedirs(parent, exist_ok=True)
    flags = os.O_CREAT | os.O_TRUNC | os.O_WRONLY
    descriptor = os.open(path, flags, 0o600)
    try:
        os.chmod(path, 0o600)
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            json.dump(record, handle, indent=2, ensure_ascii=False)
            handle.write("\n")
            descriptor = -1
    finally:
        if descriptor >= 0:
            os.close(descriptor)


def read_multiline(prompt: str, reader: Callable[[str], str] | None = None) -> str:
    """Read a payload until a blank line. Discourse Base64.encode64 wraps at 60 columns."""
    print(prompt, file=sys.stderr)
    lines: list[str] = []
    while True:
        line = input() if reader is None else reader("")
        if line.strip() == "":
            break
        lines.append(line.strip())
    if not lines:
        raise DiscourseAuthError("没有收到 payload")
    return "".join(lines)


def authorization_url(site: str, body: Mapping[str, Any], auth_redirect: str | None) -> str:
    params = {key: str(value) for key, value in body.items()}
    if auth_redirect:
        params["auth_redirect"] = auth_redirect
    return f"{site}/user-api-key/new?{urllib.parse.urlencode(params)}"


class _RedirectHandler(BaseHTTPRequestHandler):
    result: dict[str, str]

    def do_GET(self) -> None:  # noqa: N802
        parsed = urllib.parse.urlparse(self.path)
        query = urllib.parse.parse_qs(parsed.query)
        payload = query.get("payload", [""])[0]
        self.result["payload"] = payload
        self.result["error"] = query.get("error", [""])[0]
        body = b"Authorization received. You can close this tab.\n"
        self.send_response(200)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt: str, *args: object) -> None:
        return


def wait_for_redirect(timeout_seconds: int) -> tuple[str, str]:
    result: dict[str, str] = {}
    handler = type("BoundRedirectHandler", (_RedirectHandler,), {"result": result})
    server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
    port = server.server_address[1]
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    redirect = f"http://127.0.0.1:{port}/callback"
    try:
        deadline = time.monotonic() + timeout_seconds
        while "payload" not in result and time.monotonic() < deadline:
            time.sleep(0.1)
    finally:
        server.shutdown()
        thread.join(timeout=2)
        server.server_close()
    if "payload" not in result:
        raise DiscourseAuthError("等待浏览器回调超时")
    return redirect, result["payload"]


def run_device_flow(
    site: str,
    body: Mapping[str, Any],
    timeout_seconds: int,
    requester: HttpRequester,
    opener: Callable[[str], bool] = webbrowser.open,
) -> str:
    device = request_device(site, body, requester)
    interval = max(MIN_POLL_SECONDS, int(device.get("interval") or MIN_POLL_SECONDS))
    url = device.get("verification_uri_with_request") or device.get("verification_uri")
    if not isinstance(url, str) or not url:
        raise DiscourseAuthError("站点没有返回授权地址")
    print(f"在浏览器打开: {url}", file=sys.stderr)
    user_code = device.get("user_code")
    if user_code:
        print(f"核对页面上的代码: {user_code}", file=sys.stderr)
    opener(url)
    print("等待授权...", file=sys.stderr)
    return poll_device(site, str(device["device_code"]), interval, timeout_seconds, requester)


def run_browser_flow(
    site: str,
    body: Mapping[str, Any],
    timeout_seconds: int,
    opener: Callable[[str], bool] = webbrowser.open,
) -> str:
    redirect, _unused = wait_for_redirect.__wrapped_start(timeout_seconds) if False else (None, None)
    del redirect, _unused
    result: dict[str, str] = {}
    handler = type("BoundRedirectHandler", (_RedirectHandler,), {"result": result})
    server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
    port = server.server_address[1]
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    redirect = f"http://127.0.0.1:{port}/callback"
    url = authorization_url(site, body, redirect)
    print(f"在浏览器打开并批准授权:\n{url}", file=sys.stderr)
    opener(url)
    try:
        deadline = time.monotonic() + timeout_seconds
        while "payload" not in result and time.monotonic() < deadline:
            time.sleep(0.1)
    finally:
        server.shutdown()
        thread.join(timeout=2)
        server.server_close()
    if not result.get("payload"):
        raise DiscourseAuthError(
            "没有收到浏览器回调。站点可能禁止这个 auth_redirect，请改用 --flow paste"
        )
    return result["payload"]


def run_paste_flow(
    site: str,
    body: Mapping[str, Any],
    opener: Callable[[str], bool] = webbrowser.open,
    reader: Callable[[str], str] | None = None,
) -> str:
    url = authorization_url(site, body, None)
    print("在浏览器打开并批准授权。批准后页面会显示加密 payload。", file=sys.stderr)
    print(url, file=sys.stderr)
    opener(url)
    return read_multiline("把 payload 粘贴到这里，空行结束:", reader)


def resolve_flow(requested: str, capability: SiteCapability) -> str:
    if requested == "auto":
        return "device" if capability.device_code else "browser"
    if requested == "device" and not capability.device_code:
        raise DiscourseAuthError("该站点没有开启 device code，请改用 --flow browser 或 --flow paste")
    return requested


def acquire_key(
    *,
    site: str,
    application_name: str,
    scopes: tuple[str, ...],
    flow: str,
    client_id: str | None,
    padding_mode: str,
    expires_in_seconds: int | None,
    push_url: str | None,
    timeout_seconds: int,
    verify: bool,
    output_path: str,
    requester: HttpRequester = default_request,
    opener: Callable[[str], bool] = webbrowser.open,
    reader: Callable[[str], str] | None = None,
    force: bool = False,
) -> SavedKey:
    site = normalize_site(site)
    if os.path.exists(output_path) and not force:
        raise DiscourseAuthError(f"输出文件已存在: {output_path}。确认覆盖后加 --force")
    unknown = warn_unknown_scopes(scopes)
    if unknown:
        print(f"警告: 这些 scope 不在内置列表中，站点插件可能接受它们: {', '.join(unknown)}", file=sys.stderr)
    if client_id:
        print("复用 client_id 会使该用户此前对这个应用的 User API Key 失效。", file=sys.stderr)
    capability = probe_site(site, requester)
    selected = resolve_flow(flow, capability)
    if selected == "device" and DEVICE_DISALLOWED_SCOPES.intersection(scopes):
        raise DiscourseAuthError("device flow 不能申请 one_time_password，请改用 --flow browser 或 --flow paste")
    if push_url and not ({"push", "notifications"} & set(scopes)):
        raise DiscourseAuthError("push_url 需要 push 或 notifications scope")
    material = generate_key_material(client_id)
    body = build_request_body(
        material,
        application_name,
        scopes,
        padding_mode,
        expires_in_seconds,
        push_url,
    )
    try:
        if selected == "device":
            encoded = run_device_flow(site, body, timeout_seconds, requester, opener)
        elif selected == "browser":
            encoded = run_browser_flow(site, body, timeout_seconds, opener)
        elif selected == "paste":
            encoded = run_paste_flow(site, body, opener, reader)
        else:
            raise DiscourseAuthError(f"未知流程: {selected}")
        payload = decrypt_payload(encoded, material.private_key, padding_mode)
        require_matching_nonce(payload, material.nonce)
    finally:
        # Drop the only reference held by this function. Callers must not retain material.
        del material

    username = None
    if verify and ({"read", "session_info"} & set(scopes)):
        username = verify_key(site, payload.key, body["client_id"], requester)
    elif verify:
        print("当前 scope 不能读取 session，已跳过校验。", file=sys.stderr)

    record = {
        "site": site,
        "client_id": body["client_id"],
        "key": payload.key,
        "scopes": list(scopes),
        "api": payload.api,
        "expires_at": payload.expires_at,
        "username": username,
    }
    save_key(record, output_path)
    return SavedKey(
        site=site,
        client_id=body["client_id"],
        key=payload.key,
        scopes=scopes,
        api=payload.api,
        expires_at=payload.expires_at,
        username=username,
        path=output_path,
    )


def public_summary(saved: SavedKey) -> dict[str, Any]:
    return {
        "site": saved.site,
        "client_id": saved.client_id,
        "scopes": list(saved.scopes),
        "api": saved.api,
        "expires_at": saved.expires_at,
        "username": saved.username,
        "path": saved.path,
        "key_sha256_prefix": hashlib.sha256(saved.key.encode("utf-8")).hexdigest()[:12],
    }


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="生成 Discourse User API Key")
    parser.add_argument("--site", default=DEFAULT_SITE, help=f"Discourse 站点，默认 {DEFAULT_SITE}")
    parser.add_argument("--name", default=DEFAULT_APPLICATION_NAME, help="授权页显示的应用名")
    parser.add_argument("--scopes", default=",".join(DEFAULT_SCOPES), help="逗号分隔的 scope，默认 read")
    parser.add_argument("--output", default="./discourse-user-api-key.json", help="保存密钥的 0600 JSON 文件")
    parser.add_argument("--flow", choices=("auto", "device", "browser", "paste"), default="auto")
    parser.add_argument("--client-id", default=None, help="复用 client_id 会替换该用户已有的同应用密钥")
    parser.add_argument("--padding", choices=("oaep", "pkcs1"), default="oaep")
    parser.add_argument("--expires", default=None, help="可选有效期，例如 7d、12h、30m 或秒数")
    parser.add_argument("--push-url", default=None, help="仅在申请 push 或 notifications 时使用")
    parser.add_argument("--timeout", type=int, default=DEFAULT_TIMEOUT_SECONDS, help="等待授权的秒数")
    parser.add_argument("--no-verify", action="store_true", help="跳过 GET /session/current.json")
    parser.add_argument("--force", action="store_true", help="覆盖已存在的输出文件")
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        scopes = parse_scopes(args.scopes)
        saved = acquire_key(
            site=args.site,
            application_name=args.name,
            scopes=scopes,
            flow=args.flow,
            client_id=args.client_id,
            padding_mode=args.padding,
            expires_in_seconds=parse_expires(args.expires),
            push_url=args.push_url,
            timeout_seconds=args.timeout,
            verify=not args.no_verify,
            output_path=args.output,
            force=args.force,
        )
    except DiscourseAuthError as exc:
        print(f"错误: {exc}", file=sys.stderr)
        return 1
    print(json.dumps(public_summary(saved), indent=2, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
