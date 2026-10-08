#!/usr/bin/env bash
# One-shot bootstrap of the ERP Town pilot bench: compose service `create-site` (profile pilot), run by
# every `pnpm erp:pilot:up` before backend, workers and scheduler start. Idempotent:
#   1. common_site_config (db/redis hosts, socketio port, scheduler tick), apps.txt of this image
#   2. new-site $SITE_NAME with erpnext, hrms, erp_town (first run only; existing site: install any
#      missing app, then migrate, which is how an upgraded image is rolled out)
#   3. site config: town_redis_url, host_name; scheduler on; default site
# Baked into the image as /usr/local/bin/erp-town-create-site (infra/docker/pilot/Containerfile).
set -euo pipefail

: "${SITE_NAME:?SITE_NAME is not set (infra/docker/.env)}"
: "${DB_ROOT_PASSWORD:?DB_ROOT_PASSWORD is not set (infra/docker/.env)}"
: "${ADMIN_PASSWORD:?PILOT_ADMIN_PASSWORD is not set (infra/docker/.env)}"
DB_HOST=${DB_HOST:-mariadb}
DB_PORT=${DB_PORT:-3306}
REDIS_CACHE_URL=${REDIS_CACHE_URL:?REDIS_CACHE_URL is not set}
REDIS_QUEUE_URL=${REDIS_QUEUE_URL:?REDIS_QUEUE_URL is not set}
TOWN_REDIS_URL=${TOWN_REDIS_URL:?TOWN_REDIS_URL is not set}
SITE_APPS=(erpnext hrms erp_town)
BENCH_DIR=/home/frappe/frappe-bench

started=$(date +%s)
log() { printf '[create-site +%4ss] %s\n' "$(($(date +%s) - started))" "$*"; }
# host:port of a redis://host:port[/db] URL
hostport() { local hp=${1#redis://}; echo "${hp%%/*}"; }

cd "$BENCH_DIR"
wait-for-it -q -t 120 "$DB_HOST:$DB_PORT"
for url in "$REDIS_CACHE_URL" "$REDIS_QUEUE_URL" "$TOWN_REDIS_URL"; do
	wait-for-it -q -t 120 "$(hostport "$url")"
done

# --- 1. bench config -------------------------------------------------------------------------
log "1/3 common_site_config + apps.txt"
cp apps.txt sites/apps.txt
bench set-config -g db_host "$DB_HOST"
bench set-config -gp db_port "$DB_PORT"
bench set-config -g redis_cache "$REDIS_CACHE_URL"
bench set-config -g redis_queue "$REDIS_QUEUE_URL"
bench set-config -g redis_socketio "$REDIS_QUEUE_URL"
bench set-config -gp socketio_port 9000
# Frappe's scheduler wakes every 240 s by default; the town publish sweeper (spec §4.3) is a
# `* * * * *` job, so wake every 60 s.
bench set-config -gp scheduler_tick_interval 60

# --- 2. site ---------------------------------------------------------------------------------
if [ -f "sites/$SITE_NAME/site_config.json" ]; then
	installed=$(bench --site "$SITE_NAME" list-apps --format json)
	for app in "${SITE_APPS[@]}"; do
		if ! grep -q "\"$app\"" <<<"$installed"; then
			log "2/3 $SITE_NAME: install-app $app"
			bench --site "$SITE_NAME" install-app "$app" </dev/null
		fi
	done
	log "2/3 $SITE_NAME exists: migrate"
	bench --site "$SITE_NAME" migrate
else
	log "2/3 new-site $SITE_NAME (${SITE_APPS[*]})"
	install_args=()
	for app in "${SITE_APPS[@]}"; do install_args+=(--install-app "$app"); done
	bench new-site "$SITE_NAME" \
		--mariadb-user-host-login-scope=% \
		--db-root-username root \
		--db-root-password "$DB_ROOT_PASSWORD" \
		--admin-password "$ADMIN_PASSWORD" \
		"${install_args[@]}" </dev/null
fi

# --- 3. site config --------------------------------------------------------------------------
log "3/3 site config, scheduler, default site"
bench --site "$SITE_NAME" set-config town_redis_url "$TOWN_REDIS_URL"
bench --site "$SITE_NAME" set-config host_name "https://$SITE_NAME"
bench --site "$SITE_NAME" enable-scheduler
bench use "$SITE_NAME"
log "done: $SITE_NAME ready ($(bench --site "$SITE_NAME" list-apps | tr '\n' ' '))"
