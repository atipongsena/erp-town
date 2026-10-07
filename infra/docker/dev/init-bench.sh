#!/usr/bin/env bash
# Idempotent bootstrap of the ERP Town dev bench. Runs on every start of the `bench` service
# (compose: `bash /workspace/infra/docker/dev/init-bench.sh && bench start`).
# Every step is guarded by an existence check, so a re-run only does the cheap parts
# (config, editable install of erp_town, migrate).
set -euo pipefail

BENCH_DIR=/home/frappe/frappe-bench
BRANCH=version-16
DEV_SITE=town.localhost
TEST_SITE=test.localhost
SITES=("$DEV_SITE" "$TEST_SITE")
SITE_APPS=(erpnext hrms erp_town)
REDIS_TOWN_URL="${REDIS_TOWN_URL:-redis://redis-town:6379/0}"
: "${DB_ROOT_PASSWORD:?DB_ROOT_PASSWORD is not set (infra/docker/.env)}"
: "${ADMIN_PASSWORD:?ADMIN_PASSWORD is not set (infra/docker/.env)}"

started=$(date +%s)
log() { printf '[init-bench +%4ss] %s\n' "$(($(date +%s) - started))" "$*"; }

# True if $1 is a line of sites/apps.txt.
in_apps_txt() { [ -f "$BENCH_DIR/sites/apps.txt" ] && grep -qxF "$1" "$BENCH_DIR/sites/apps.txt"; }

# Absolute path of a Python 3.14 interpreter (pyenv first, uv-managed as fallback).
find_python314() {
	local v
	v=$(pyenv versions --bare 2>/dev/null | grep -E '^3\.14(\.|$)' | sort -V | tail -n1 || true)
	if [ -n "$v" ]; then
		echo "$(pyenv root)/versions/$v/bin/python3.14"
		return
	fi
	log "Python 3.14 not found in pyenv; installing it with uv" >&2
	uv python install 3.14 >&2
	uv python find 3.14
}

# --- 0. ownership ----------------------------------------------------------------------
# Docker creates the volume mountpoint and the apps/ dir (parent of the erp_town bind
# mount) as root; bench runs as `frappe`.
for d in "$BENCH_DIR" "$BENCH_DIR/apps"; do
	if [ -d "$d" ] && [ "$(stat -c %U "$d")" != frappe ]; then
		log "0/6 chown frappe $d"
		sudo chown frappe:frappe "$d"
	fi
done

# --- 1. bench init -----------------------------------------------------------------------
# The bench dir is the volume mountpoint (cannot be moved/replaced), so init in place with
# --ignore-exist. A half-finished init (no frappe checkout, venv or apps.txt entry) is redone.
cd "$BENCH_DIR"
if [ ! -f apps/frappe/pyproject.toml ] || [ ! -x env/bin/python ] || ! in_apps_txt frappe; then
	py=$(find_python314)
	log "1/6 bench init (frappe $BRANCH, python $("$py" --version 2>&1), node $(node --version))"
	rm -rf apps/frappe env
	# stdin=/dev/null: bench's "rollback?" prompt on failure aborts instead of deleting the
	# bench dir (which contains the bind-mounted app).
	(cd /home/frappe && bench init --ignore-exist --skip-redis-config-generation --skip-assets \
		--frappe-branch "$BRANCH" --python "$py" frappe-bench </dev/null)
else
	log "1/6 bench init: already done ($(env/bin/python --version 2>&1))"
fi
# Redis runs in its own containers, not under honcho.
if grep -qE '^redis_' Procfile; then
	log "1/6 removing redis entries from Procfile"
	sed -i '/^redis_/d' Procfile
fi

# --- 2. common site config ---------------------------------------------------------------
log "2/6 common_site_config (db_host, redis, serve_default_site)"
bench set-config -g db_host mariadb
bench set-config -g redis_cache redis://redis-cache:6379
bench set-config -g redis_queue redis://redis-queue:6379
bench set-config -g redis_socketio redis://redis-queue:6379
bench set-config -g -p serve_default_site True

# --- 3. erpnext + hrms ---------------------------------------------------------------------
# A checkout alone is not enough: if step 1 rebuilt the venv (e.g. its python symlink into the
# image's pyenv went stale), the app must be pip-installed into the new venv again.
for app in erpnext hrms; do
	if [ -f "apps/$app/pyproject.toml" ] && in_apps_txt "$app"; then
		if env/bin/python -c "import $app" 2>/dev/null; then
			log "3/6 get-app $app: already present"
		else
			log "3/6 $app checked out but not importable in env: pip install -e apps/$app"
			bench pip install --quiet -e "apps/$app"
		fi
	else
		log "3/6 get-app $app ($BRANCH)"
		rm -rf "apps/$app"
		bench get-app --skip-assets --branch "$BRANCH" "$app" </dev/null
	fi
done

# --- 4. erp_town (bind mount) ----------------------------------------------------------------
# Always (re)install: cheap with uv, and picks up dependency changes in pyproject.toml.
log "4/6 pip install -e apps/erp_town"
bench pip install --quiet -e apps/erp_town
if ! in_apps_txt erp_town; then
	log "4/6 adding erp_town to sites/apps.txt"
	[ -z "$(tail -c1 sites/apps.txt)" ] || echo >>sites/apps.txt
	echo erp_town >>sites/apps.txt
fi

# Build assets once for all apps (init/get-app ran with --skip-assets); `bench start`'s
# `watch` process keeps them current afterwards.
build_marker=sites/assets/.erp-town-build
if [ ! -f "$build_marker" ] || [ "$(cat "$build_marker")" != "$(cat sites/apps.txt)" ]; then
	log "4/6 bench build"
	bench build
	cp sites/apps.txt "$build_marker"
else
	log "4/6 bench build: assets up to date"
fi

# --- 5. sites ---------------------------------------------------------------------------------
for site in "${SITES[@]}"; do
	if [ -f "sites/$site/site_config.json" ]; then
		log "5/6 new-site $site: exists"
	else
		log "5/6 new-site $site"
		bench new-site "$site" \
			--db-root-password "$DB_ROOT_PASSWORD" \
			--admin-password "$ADMIN_PASSWORD" \
			--mariadb-user-host-login-scope=% </dev/null
	fi
	installed=$(bench --site "$site" list-apps --format json)
	for app in "${SITE_APPS[@]}"; do
		if grep -q "\"$app\"" <<<"$installed"; then
			log "5/6 $site: $app already installed"
		else
			log "5/6 $site: install-app $app"
			bench --site "$site" install-app "$app" </dev/null
		fi
	done
	bench --site "$site" set-config town_redis_url "$REDIS_TOWN_URL"
done
bench --site "$TEST_SITE" set-config -p allow_tests True
bench --site "$DEV_SITE" set-config -p developer_mode 1
bench use "$DEV_SITE"
log "5/6 sites ready: ${SITES[*]} (default: $DEV_SITE)"

# --- 6. migrate (every start) --------------------------------------------------------------
log "6/6 bench --site all migrate"
bench --site all migrate
log "done; starting bench"
