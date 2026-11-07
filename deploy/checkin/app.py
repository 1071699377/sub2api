#!/usr/bin/env python3
"""Sub2API daily check-in sidecar: +50 balance once per Beijing noon day."""

from __future__ import annotations

import json
import logging
import os
import re
from datetime import date, datetime, timedelta, timezone
from decimal import Decimal
from typing import Any, Optional, Tuple
from zoneinfo import ZoneInfo

import asyncpg
import aiohttp
from aiohttp import web

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
log = logging.getLogger("checkin")

CHECKIN_AMOUNT = Decimal(os.getenv("CHECKIN_AMOUNT", "50"))
CHECKIN_TZ = ZoneInfo(os.getenv("CHECKIN_TZ", "Asia/Shanghai"))
CHECKIN_RESET_HOUR = int(os.getenv("CHECKIN_RESET_HOUR", "12"))
SUB2API_BASE = os.getenv("SUB2API_BASE", "http://sub2api:8080").rstrip("/")
ADMIN_API_KEY = os.getenv("ADMIN_API_KEY", "").strip()
DATABASE_URL = os.getenv(
    "DATABASE_URL",
    "postgresql://sub2api:sub2api@postgres:5432/sub2api",
)
STATIC_DIR = os.getenv("STATIC_DIR", "/app/static")


def period_key_now(now: Optional[datetime] = None) -> date:
    """Day key for the open check-in window (resets at Beijing CHECKIN_RESET_HOUR)."""
    now = now or datetime.now(tz=CHECKIN_TZ)
    if now.tzinfo is None:
        now = now.replace(tzinfo=CHECKIN_TZ)
    else:
        now = now.astimezone(CHECKIN_TZ)
    boundary = now.replace(hour=CHECKIN_RESET_HOUR, minute=0, second=0, microsecond=0)
    if now < boundary:
        return (boundary - timedelta(days=1)).date()
    return boundary.date()


def next_reset_at(now: Optional[datetime] = None) -> datetime:
    now = now or datetime.now(tz=CHECKIN_TZ)
    if now.tzinfo is None:
        now = now.replace(tzinfo=CHECKIN_TZ)
    else:
        now = now.astimezone(CHECKIN_TZ)
    boundary = now.replace(hour=CHECKIN_RESET_HOUR, minute=0, second=0, microsecond=0)
    if now < boundary:
        return boundary
    return boundary + timedelta(days=1)


def extract_bearer(request: web.Request) -> Optional[str]:
    auth = request.headers.get("Authorization") or request.headers.get("authorization") or ""
    m = re.match(r"(?i)^Bearer\s+(\S+)$", auth.strip())
    if m:
        return m.group(1)
    # allow token query for simple debugging only when explicitly enabled
    if os.getenv("ALLOW_TOKEN_QUERY", "").lower() in ("1", "true", "yes"):
        q = request.rel_url.query.get("token")
        if q:
            return q
    return None


async def auth_me(session: aiohttp.ClientSession, token: str) -> dict[str, Any]:
    url = f"{SUB2API_BASE}/api/v1/auth/me"
    headers = {"Authorization": f"Bearer {token}"}
    async with session.get(url, headers=headers, timeout=aiohttp.ClientTimeout(total=15)) as resp:
        body = await resp.json(content_type=None)
        if resp.status != 200 or body.get("code") != 0:
            raise web.HTTPUnauthorized(text=json.dumps({"code": 401, "message": "未登录或登录已过期"}), content_type="application/json")
        data = body.get("data") or {}
        if not data.get("id"):
            raise web.HTTPUnauthorized(text=json.dumps({"code": 401, "message": "无效用户"}), content_type="application/json")
        return data


async def admin_add_balance(session: aiohttp.ClientSession, user_id: int, amount: Decimal, period: date) -> dict[str, Any]:
    if not ADMIN_API_KEY:
        raise web.HTTPInternalServerError(text=json.dumps({"code": 500, "message": "ADMIN_API_KEY 未配置"}), content_type="application/json")
    url = f"{SUB2API_BASE}/api/v1/admin/users/{user_id}/balance"
    headers = {
        "x-api-key": ADMIN_API_KEY,
        "Idempotency-Key": f"checkin-{user_id}-{period.isoformat()}",
        "Content-Type": "application/json",
    }
    payload = {
        "balance": float(amount),
        "operation": "add",
        "notes": f"daily checkin {period.isoformat()} +{amount}",
    }
    async with session.post(url, headers=headers, json=payload, timeout=aiohttp.ClientTimeout(total=20)) as resp:
        body = await resp.json(content_type=None)
        if resp.status != 200 or body.get("code") != 0:
            log.error("admin balance failed user=%s status=%s body=%s", user_id, resp.status, body)
            raise web.HTTPBadGateway(
                text=json.dumps({"code": 502, "message": body.get("message") or "加余额失败"}),
                content_type="application/json",
            )
        return body.get("data") or {}


def ok(data: Any) -> web.Response:
    return web.json_response({"code": 0, "message": "success", "data": data})


def err(status: int, message: str, code: Optional[int] = None) -> web.Response:
    return web.json_response({"code": code or status, "message": message}, status=status)


async def handle_status(request: web.Request) -> web.Response:
    token = extract_bearer(request)
    if not token:
        return err(401, "请先登录")
    pool: asyncpg.Pool = request.app["pool"]
    session: aiohttp.ClientSession = request.app["http"]
    user = await auth_me(session, token)
    user_id = int(user["id"])
    pk = period_key_now()
    row = await pool.fetchrow(
        "SELECT id, amount, created_at FROM user_daily_checkins WHERE user_id=$1 AND period_key=$2",
        user_id,
        pk,
    )
    checked = row is not None
    return ok(
        {
            "user_id": user_id,
            "email": user.get("email"),
            "balance": float(user.get("balance") or 0),
            "period_key": pk.isoformat(),
            "amount": float(CHECKIN_AMOUNT),
            "can_checkin": not checked,
            "checked_in": checked,
            "checked_in_at": row["created_at"].isoformat() if row else None,
            "next_reset_at": next_reset_at().isoformat(),
            "timezone": str(CHECKIN_TZ),
            "reset_hour": CHECKIN_RESET_HOUR,
        }
    )


async def handle_checkin(request: web.Request) -> web.Response:
    token = extract_bearer(request)
    if not token:
        return err(401, "请先登录")
    pool: asyncpg.Pool = request.app["pool"]
    session: aiohttp.ClientSession = request.app["http"]
    user = await auth_me(session, token)
    user_id = int(user["id"])
    pk = period_key_now()

    # insert first (unique constraint = claim lock), then credit; rollback insert if credit fails
    try:
        row = await pool.fetchrow(
            """
            INSERT INTO user_daily_checkins (user_id, period_key, amount)
            VALUES ($1, $2, $3)
            RETURNING id, amount, created_at
            """,
            user_id,
            pk,
            CHECKIN_AMOUNT,
        )
    except asyncpg.UniqueViolationError:
        existing = await pool.fetchrow(
            "SELECT id, amount, created_at FROM user_daily_checkins WHERE user_id=$1 AND period_key=$2",
            user_id,
            pk,
        )
        return err(409, "本轮已签到，请下个北京时间 12:00 后再来", code=409)

    try:
        updated = await admin_add_balance(session, user_id, CHECKIN_AMOUNT, pk)
    except Exception:
        await pool.execute("DELETE FROM user_daily_checkins WHERE id=$1", row["id"])
        raise

    return ok(
        {
            "user_id": user_id,
            "period_key": pk.isoformat(),
            "amount": float(CHECKIN_AMOUNT),
            "checked_in_at": row["created_at"].isoformat(),
            "balance": float(updated.get("balance") if updated.get("balance") is not None else (float(user.get("balance") or 0) + float(CHECKIN_AMOUNT))),
            "next_reset_at": next_reset_at().isoformat(),
        }
    )


async def handle_health(_request: web.Request) -> web.Response:
    return web.json_response({"status": "ok"})


async def handle_checkin_index(_request: web.Request) -> web.Response:
    """Serve SPA entry for /checkin and /checkin/ (nginx uses index.html; aiohttp does not)."""
    index = os.path.join(STATIC_DIR, "index.html")
    if not os.path.isfile(index):
        raise web.HTTPNotFound(text="checkin ui missing")
    return web.FileResponse(index)


async def on_startup(app: web.Application) -> None:
    app["pool"] = await asyncpg.create_pool(DATABASE_URL, min_size=1, max_size=8)
    app["http"] = aiohttp.ClientSession()
    log.info(
        "checkin started amount=%s tz=%s reset_hour=%s sub2api=%s",
        CHECKIN_AMOUNT,
        CHECKIN_TZ,
        CHECKIN_RESET_HOUR,
        SUB2API_BASE,
    )


async def on_cleanup(app: web.Application) -> None:
    await app["http"].close()
    await app["pool"].close()


def create_app() -> web.Application:
    app = web.Application()
    app.on_startup.append(on_startup)
    app.on_cleanup.append(on_cleanup)
    app.router.add_get("/health", handle_health)
    app.router.add_get("/api/checkin/status", handle_status)
    app.router.add_post("/api/checkin", handle_checkin)
    # also under /api/checkin/ for nginx prefix
    app.router.add_get("/api/checkin/status/", handle_status)
    app.router.add_post("/api/checkin/", handle_checkin)
    app.router.add_get("/checkin", handle_checkin_index)
    app.router.add_get("/checkin/", handle_checkin_index)
    if os.path.isdir(STATIC_DIR):
        app.router.add_static("/checkin/", STATIC_DIR, show_index=False)
    return app


def main() -> None:
    host = os.getenv("HOST", "0.0.0.0")
    port = int(os.getenv("PORT", "18100"))
    web.run_app(create_app(), host=host, port=port)


if __name__ == "__main__":
    main()
