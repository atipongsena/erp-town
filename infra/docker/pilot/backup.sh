#!/usr/bin/env bash
# ERP Town pilot backup (spec §6.3), baked into the pilot image as /usr/local/bin/erp-town-backup.
#
#   erp-town-backup                  `bench --site $SITE_NAME backup --with-files` plus `pg_dump -Fc` of the
#                                    town database (PGHOST/PGUSER/PGPASSWORD/PGDATABASE), upload of the new set
#                                    to S3-compatible storage, removal of remote sets older than
#                                    BACKUP_RETENTION_DAYS (14). `pnpm erp:pilot:backup` runs this.
#   erp-town-backup --daily          checks the env and the bucket (exits non-zero if either is wrong),
#                                    then the above every day at BACKUP_TIME_UTC (default 19:00 UTC =
#                                    02:00 Asia/Bangkok); command of compose service `backup`
#   erp-town-backup --list           remote backup sets of $SITE_NAME, oldest first
#   erp-town-backup --fetch <set|latest>
#                                    download one set to sites/.restore/<set>/ (restore drill:
#                                    infra/docker/pilot/RESTORE.md); prints the files' common prefix
#
# Remote layout (one "directory" per backup set, named by the bench backup timestamp):
#   s3://$BACKUP_S3_BUCKET/$SITE_NAME/<YYYYmmdd_HHMMSS>/<YYYYmmdd_HHMMSS>-<site_slug>-database.sql.gz
#                                                     .../<...>-files.tar, <...>-private-files.tar,
#                                                     .../<...>-site_config_backup.json,
#                                                     .../<...>-town_pilot.dump (pg_dump -Fc of the Go
#                                                     layer's PostgreSQL database)
set -euo pipefail

: "${SITE_NAME:?SITE_NAME is not set (infra/docker/.env)}"
: "${BACKUP_S3_ENDPOINT:?BACKUP_S3_ENDPOINT is not set (infra/docker/.env)}"
: "${BACKUP_S3_BUCKET:?BACKUP_S3_BUCKET is not set (infra/docker/.env)}"
: "${BACKUP_S3_ACCESS_KEY:?BACKUP_S3_ACCESS_KEY is not set (infra/docker/.env)}"
: "${BACKUP_S3_SECRET_KEY:?BACKUP_S3_SECRET_KEY is not set (infra/docker/.env)}"
: "${PGHOST:?PGHOST is not set (the town PostgreSQL service; compose service backup sets PG*)}"
: "${PGDATABASE:?PGDATABASE is not set (town_pilot)}"
RETENTION_DAYS=${BACKUP_RETENTION_DAYS:-14}
BENCH_DIR=/home/frappe/frappe-bench
REMOTE="s3://$BACKUP_S3_BUCKET/$SITE_NAME"

log() { printf '[backup %s] %s\n' "$(date -u +%FT%TZ)" "$*" >&2; }

export AWS_ACCESS_KEY_ID=$BACKUP_S3_ACCESS_KEY
export AWS_SECRET_ACCESS_KEY=$BACKUP_S3_SECRET_KEY
export AWS_DEFAULT_REGION=${BACKUP_S3_REGION:-us-east-1}
# Path-style addressing (bucket in the path, not the host name) works with SeaweedFS, MinIO and most
# S3-compatible stores; set BACKUP_S3_ADDRESSING_STYLE=virtual or auto for providers that need it.
AWS_CONFIG_FILE=$(mktemp)
export AWS_CONFIG_FILE
trap 'rm -f "$AWS_CONFIG_FILE"' EXIT
printf '[default]\ns3 =\n    addressing_style = %s\n' "${BACKUP_S3_ADDRESSING_STYLE:-path}" >"$AWS_CONFIG_FILE"
s3() { aws --endpoint-url "$BACKUP_S3_ENDPOINT" s3 "$@"; }
# remote set names (YYYYmmdd_HHMMSS) that hold objects, oldest first. Derived from object keys, not
# from "directory" prefixes: some S3-compatible stores (e.g. SeaweedFS) keep an empty prefix after
# its objects were deleted. `aws s3 ls` exits 1 with no output when nothing matches (no sets yet);
# any other failure (bad credentials, missing bucket, unreachable endpoint: exit 255) is fatal.
remote_sets() {
	local out err rc=0
	err=$(mktemp)
	out=$(s3 ls --recursive "$REMOTE/" 2>"$err") || rc=$?
	if [ "$rc" -ne 0 ] && { [ "$rc" -ne 1 ] || [ -s "$err" ] || [ -n "$out" ]; }; then
		cat "$err" >&2
		rm -f "$err"
		log "ERROR: cannot list $REMOTE/ (aws exit $rc)"
		return 1
	fi
	rm -f "$err"
	[ -n "$out" ] || return 0
	awk '{ print $4 }' <<<"$out" | cut -d/ -f2 | { grep -E '^[0-9]{8}_[0-9]{6}$' || true; } | sort -u
}

# Fail fast on a target that cannot work (credentials, bucket, endpoint).
probe_bucket() {
	if ! aws --endpoint-url "$BACKUP_S3_ENDPOINT" s3api head-bucket --bucket "$BACKUP_S3_BUCKET" >/dev/null; then
		log "ERROR: s3://$BACKUP_S3_BUCKET at $BACKUP_S3_ENDPOINT not accessible (check endpoint, bucket and BACKUP_S3_* keys)"
		return 1
	fi
}

if [ "${1:-}" = --daily ]; then
	# validate config and target now, so a misconfigured backup container exits (and shows up as
	# exited/restarting) instead of sleeping until its first nightly run fails
	probe_bucket
	sets=$(remote_sets)
	log "target OK: $REMOTE/ ($(grep -c . <<<"$sets" || true) sets)"
	at=${BACKUP_TIME_UTC:-19:00}
	while :; do
		now=$(date -u +%s)
		next=$(date -u -d "today $at" +%s)
		[ "$next" -gt "$now" ] || next=$(date -u -d "tomorrow $at" +%s)
		log "next run $(date -u -d "@$next" +%FT%TZ)"
		sleep $((next - now))
		# a failed run is logged and retried the next day; the container keeps running
		"$0" || log "FAILED (exit $?)"
	done
fi

cd "$BENCH_DIR"

case "${1:-}" in
--list)
	remote_sets
	exit 0
	;;
--fetch)
	set_name=${2:?usage: erp-town-backup --fetch <YYYYmmdd_HHMMSS|latest>}
	[ "$set_name" != latest ] || set_name=$(remote_sets | tail -n1)
	if [ -z "$set_name" ]; then
		log "ERROR: no backup sets under $REMOTE/"
		exit 1
	fi
	dir="sites/.restore/$set_name"
	log "download $REMOTE/$set_name/ -> $BENCH_DIR/$dir/"
	mkdir -p "$dir"
	s3 cp --only-show-errors --recursive "$REMOTE/$set_name/" "$dir/"
	ls -l "$dir" >&2
	db_file=$(find "$dir" -maxdepth 1 -name '*-database*.sql.gz' | head -n1)
	if [ -z "$db_file" ]; then
		log "ERROR: $REMOTE/$set_name/ has no database file"
		exit 1
	fi
	echo "$BENCH_DIR/${db_file%-database*}"
	exit 0
	;;
"") ;;
*)
	log "unknown argument: $1 (see the header of $0)"
	exit 2
	;;
esac

backup_dir="sites/$SITE_NAME/private/backups"

# --- 1. backup -------------------------------------------------------------------------------
probe_bucket
log "bench --site $SITE_NAME backup --with-files"
marker=$(mktemp)
bench --site "$SITE_NAME" backup --with-files
# the database dump this run wrote (newer than the marker); every file of the set shares its
# "<YYYYmmdd_HHMMSS>-<site_slug>" prefix. Names get "-enc" when System Settings encrypt backups.
db_file=$(find "$backup_dir" -maxdepth 1 -name '*-database*.sql.gz' -newer "$marker" -printf '%T@ %p\n' |
	sort -n | tail -n1 | cut -d' ' -f2-)
rm -f "$marker"
if [ -z "$db_file" ]; then
	log "ERROR: bench backup wrote no database file in $backup_dir"
	exit 1
fi
db_name=$(basename "$db_file")
prefix=${db_name%-database*}
stamp=${prefix%%-*}
mapfile -t set_files < <(find "$backup_dir" -maxdepth 1 -name "$prefix-*" | sort)
for kind in database files private-files site_config_backup; do
	if ! printf '%s\n' "${set_files[@]##*/}" | grep -qE "^$prefix-$kind(-enc)?\.(sql\.gz|tar|tgz|json)$"; then
		log "ERROR: backup set $prefix has no $kind file"
		exit 1
	fi
done

# PostgreSQL (Go town layer): custom-format dump (pg_restore-able, compressed) into the same set.
# Written to a temp name and renamed, so an interrupted dump never leaves a partial .dump behind.
pg_file="$backup_dir/$prefix-$PGDATABASE.dump"
log "pg_dump -Fc $PGDATABASE@$PGHOST"
pg_dump -Fc --no-owner --file "$pg_file.part"
mv "$pg_file.part" "$pg_file"
set_files+=("$pg_file")

# --- 2. upload -------------------------------------------------------------------------------
dest="$REMOTE/$stamp"
for f in "${set_files[@]}"; do
	log "upload $(basename "$f") ($(du -h "$f" | cut -f1)) -> $dest/"
	s3 cp --only-show-errors "$f" "$dest/$(basename "$f")"
done
uploaded=$(s3 ls "$dest/" | wc -l)
if [ "$uploaded" -lt "${#set_files[@]}" ]; then
	log "ERROR: $dest/ lists $uploaded objects, expected ${#set_files[@]}"
	exit 1
fi

# --- 3. remote retention ---------------------------------------------------------------------
# Sets are named by their timestamp, so retention compares names, not object dates.
cutoff=$(date -u -d "-$RETENTION_DAYS days" +%Y%m%d)
remote_sets | while read -r old; do
	if [[ ${old:0:8} < $cutoff ]]; then
		log "retention: remove $REMOTE/$old/ (older than $RETENTION_DAYS days)"
		s3 rm --only-show-errors --recursive "$REMOTE/$old/"
	fi
done

log "done: $dest/ (${#set_files[@]} files)"
