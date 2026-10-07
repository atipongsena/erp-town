# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Client for `redis-town`, the town's event bus (spec §4.3) -- separate from Frappe's own Redis."""

import redis

from erp_town.erp_town.doctype.town_settings.town_settings import get_town_settings

SOCKET_TIMEOUT_SECONDS = 2

_clients: dict[str, redis.Redis] = {}


class TownRedisNotConfigured(Exception):
	"""Neither `Town Settings.redis_town_url` nor site config `town_redis_url` is set."""


def get_town_redis(url: str | None = None) -> redis.Redis:
	"""Redis client for `url` (default: the configured `redis_town_url`), cached per URL in this process.

	Responses are bytes (`decode_responses=False`) so payloads round-trip byte-exact; connect and read
	timeouts are 2 s so a dead `redis-town` fails a publish quickly instead of hanging a worker.
	"""
	url = url or get_town_settings().redis_town_url
	if not url:
		raise TownRedisNotConfigured("redis_town_url is not configured (Town Settings or site config)")
	client = _clients.get(url)
	if client is None:
		client = redis.Redis.from_url(
			url,
			decode_responses=False,
			socket_timeout=SOCKET_TIMEOUT_SECONDS,
			socket_connect_timeout=SOCKET_TIMEOUT_SECONDS,
		)
		_clients[url] = client
	return client
