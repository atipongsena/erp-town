# ERP Town pilot: backup, restore drill and pre-pilot checklist

Spec §6.3: the pilot backs up daily (`bench backup --with-files` → S3-compatible storage outside the
pilot host) and **must pass a restore drill before the pilot starts**. This file is the drill: run it
top to bottom, then tick the checklist at the end.

All commands run from the repo root in Git Bash (Windows) or any POSIX shell. `dc` is shorthand for
the pilot compose call that `scripts/erp.mjs` makes:

```bash
export PILOT_IMAGE=$(docker inspect --format '{{.Config.Image}}' erp-town-backend-1)   # image the pilot runs
dc() { docker compose -f infra/docker/compose.yaml --env-file infra/docker/.env --profile pilot "$@"; }
```

## What a backup is

`pnpm erp:pilot:backup` (and the `backup` service daily at `BACKUP_TIME_UTC`) runs
`/usr/local/bin/erp-town-backup` from `infra/docker/pilot/backup.sh` in the pilot image:

1. `bench --site $SITE_NAME backup --with-files` → `sites/$SITE_NAME/private/backups/`
   `<YYYYmmdd_HHMMSS>-<site_slug>-{database.sql.gz,files.tar,private-files.tar,site_config_backup.json}`
2. `pg_dump -Fc town_pilot` of the Go town layer's PostgreSQL 18 database (service `postgres`, user
   `town`, connection from the `PG*` variables of the `backup` service) →
   `<YYYYmmdd_HHMMSS>-<site_slug>-town_pilot.dump` in the same set
3. uploads that set to `s3://$BACKUP_S3_BUCKET/$SITE_NAME/<YYYYmmdd_HHMMSS>/` (`aws s3 cp --endpoint-url
   $BACKUP_S3_ENDPOINT`, path-style addressing) and checks every object arrived
4. deletes remote sets older than 14 days (`BACKUP_RETENTION_DAYS`)

`site_config_backup.json` holds the site's `encryption_key`: without it, passwords stored in the
database (email accounts, integration secrets) cannot be decrypted after a restore. Treat the bucket
as secret.

## S3 target for a rehearsal (local SeaweedFS)

On the pilot host `BACKUP_S3_*` point at real off-host storage. For a rehearsal on a dev machine,
start a throwaway S3-compatible server on the compose network and point `infra/docker/.env` at it:
`BACKUP_S3_ENDPOINT=http://erp-town-seaweedfs:8333`, `BACKUP_S3_BUCKET=erp-town-backups`, and a
random `BACKUP_S3_ACCESS_KEY` / `BACKUP_S3_SECRET_KEY` (local `.env` only, never committed).

MinIO's public container image is no longer published (Docker Hub `minio/minio` and
`quay.io/minio/minio` are gone since MinIO went source-only), so the drill uses SeaweedFS's official
image. With no S3 config file, `weed` takes its admin credentials from `AWS_ACCESS_KEY_ID` /
`AWS_SECRET_ACCESS_KEY`, so aws-cli authenticates with the `BACKUP_S3_*` keys and wrong keys get
`InvalidAccessKeyId`. backup.sh only uses `aws s3 --endpoint-url`, so the same commands work against
any S3-compatible provider.

```bash
set -a; . infra/docker/.env; set +a        # BACKUP_S3_* into this shell (values stay out of history)
# MSYS_NO_PATHCONV=1: Git Bash would otherwise rewrite -dir=/data into a Windows path
MSYS_NO_PATHCONV=1 docker run -d --name erp-town-seaweedfs --network erp-town_default \
  -e AWS_ACCESS_KEY_ID="$BACKUP_S3_ACCESS_KEY" -e AWS_SECRET_ACCESS_KEY="$BACKUP_S3_SECRET_KEY" \
  chrislusf/seaweedfs:4.48 server -dir=/data -s3
# wait for "Start Seaweed S3 API Server ... at http port 8333" in `docker logs erp-town-seaweedfs`
# bucket, created with the pilot image's aws cli
dc run --rm --no-deps --entrypoint bash backup -c \
  'AWS_ACCESS_KEY_ID=$BACKUP_S3_ACCESS_KEY AWS_SECRET_ACCESS_KEY=$BACKUP_S3_SECRET_KEY \
   aws --endpoint-url "$BACKUP_S3_ENDPOINT" s3 mb "s3://$BACKUP_S3_BUCKET"'
```

Remove it after the drill: `docker rm -f erp-town-seaweedfs` (its data lives in the container).

## Restore drill

The drill restores the latest remote backup of the pilot site into a **fresh site**
(`restore-drill.localhost`) on the same bench and database server, and compares document counts.
It never touches `$SITE_NAME`.

### 1. Data to back up (rehearsal on a dev machine only)

A real pilot has real data. On a rehearsal stack (`SITE_NAME=pilot.localhost`), create some first and
note the Sales Invoice count:

```bash
# seed.run refuses on a site without developer_mode / allow_tests; force=True for a rehearsal only
dc exec backend bench --site pilot.localhost execute erp_town.setup.seed.run --kwargs '{"force": true}'
dc exec backend bench --site pilot.localhost execute erp_town.setup.scenario.run --kwargs '{"days":3}'
dc exec backend bench --site pilot.localhost execute frappe.db.count --args '["Sales Invoice"]'
```

### 2. Back up

```bash
pnpm erp:pilot:backup                                  # exit 0, last line "done: s3://…/<set>/ (4 files)"
dc run --rm --no-deps backup erp-town-backup --list    # the new set is listed last
```

### 3. Download the set

```bash
dc run --rm --no-deps backup erp-town-backup --fetch latest
# prints the common prefix of the downloaded files, e.g.
# /home/frappe/frappe-bench/sites/.restore/20261008_101500/20261008_101500-pilot_localhost
```

The set lands in the `pilot-sites` volume under `sites/.restore/<set>/`, visible to every pilot
container.

### 4. Restore into a fresh site

`create-site` has `DB_ROOT_PASSWORD` and `ADMIN_PASSWORD` in its environment, so the restore runs in a
one-off shell of that service (nothing secret is typed or kept in shell history):

```bash
dc run --rm --no-deps create-site bash
```

In that shell (`P` = the prefix printed by step 3):

```bash
cd /home/frappe/frappe-bench
P=/home/frappe/frappe-bench/sites/.restore/<set>/<set>-pilot_localhost
NEW=restore-drill.localhost

bench new-site "$NEW" --mariadb-user-host-login-scope=% \
  --db-root-password "$DB_ROOT_PASSWORD" --admin-password "$ADMIN_PASSWORD"
# same encryption key as the backed-up site, before its data arrives
bench --site "$NEW" set-config encryption_key "$(jq -r .encryption_key "$P-site_config_backup.json")"
bench --site "$NEW" restore "$P-database.sql.gz" \
  --with-public-files "$P-files.tar" \
  --with-private-files "$P-private-files.tar" \
  --db-root-password "$DB_ROOT_PASSWORD"
# the scheduler flag is site config, not database: the restored site keeps the target's previous
# state (disabled for a fresh new-site). Disable it explicitly anyway: a drill site must never run
# jobs or publish events
bench --site "$NEW" disable-scheduler
bench --site "$NEW" migrate
bench --site "$NEW" list-apps
bench --site "$NEW" execute frappe.db.count --args '["Sales Invoice"]'
exit
```

### 5. Restore the town database

The set also holds `<set>-town_pilot.dump`. Restore it into a **fresh database** on the postgres
service (never over `town_pilot`) and compare the outbox row count. The postgres client in the pilot
image is `postgresql-client-18`; `PGHOST`/`PGUSER`/`PGPASSWORD` of the `backup` service reach the server.

```bash
# before the backup (step 2), note the source count:
dc exec postgres psql -U town -d town_pilot -tAc 'SELECT count(*) FROM town_outbox'
# after step 3, in a one-off backup-service shell: restore into town_restore_drill and count
dc run --rm --no-deps --entrypoint bash backup -c '
  F=$(ls /home/frappe/frappe-bench/sites/.restore/<set>/*-town_pilot.dump)
  createdb town_restore_drill &&
  pg_restore --no-owner -d town_restore_drill "$F" &&
  psql -d town_restore_drill -tAc "SELECT count(*) FROM town_outbox"'
dc exec postgres psql -U town -d town -c 'DROP DATABASE town_restore_drill'   # clean up
```

The two counts must be equal (also compare `consumed_events` if the consumer role ran). On an
existing postgres volume `town_pilot` itself is created by `pnpm erp:pilot:up` (the init script
`infra/docker/postgres/init.sql` only runs on a fresh volume); by hand:
`dc exec postgres psql -U town -d town -c 'CREATE DATABASE town_pilot'`.

### 6. Compare the ERPNext counts

```bash
dc exec backend bench --site pilot.localhost execute frappe.db.count --args '["Sales Invoice"]'
```

The restored count must equal the count of the backed-up site at backup time (step 1; on a live pilot,
compare with the count printed in the backup's time window). Also open
`https://<site>/app/sales-invoice` on the original and check a few documents, attachments (public and
private files) and a print of a Sales Invoice on the restored site if you route a host name to it.

### 7. Clean up

```bash
dc run --rm --no-deps create-site bash -c \
  'cd /home/frappe/frappe-bench && bench drop-site restore-drill.localhost --force --no-backup \
     --db-root-password "$DB_ROOT_PASSWORD" && rm -rf sites/.restore'
```

## Disaster recovery (real restore of the pilot site)

Same commands as the drill, with `NEW=$SITE_NAME` on a fresh host (the town database is restored the
same way with `pg_restore --clean --if-exists --no-owner -d town_pilot` while `town` is stopped):

1. Fresh host, same `infra/docker/.env` (from the password manager), `pnpm erp:pilot:build` of the
   commit that made the backup (or newer), `pnpm erp:pilot:up` → empty `$SITE_NAME`.
2. Stop writers: `dc stop backend town queue-short queue-long scheduler backup`.
3. Steps 3–4 above with `NEW=$SITE_NAME`, skipping `new-site` (the site exists) and
   `disable-scheduler` (the pilot site runs its scheduler). `town_redis_url` and `host_name` are site
   config, not database, so create-site's values survive the restore.
4. `pnpm erp:pilot:up` (re-runs create-site: migrate, scheduler on) and check ping + counts.

Events already in the outbox as `Pending` are published by the sweeper after the restore; consumers
de-duplicate by event `id` (spec §3).

## Pre-pilot checklist

Tick each item (date, initials) before the pilot starts; keep the filled copy with the pilot runbook.

- [ ] `pnpm erp:pilot:build` of the release commit; image tag `erp-town/erpnext:v16-<sha>` (no `-dirty`) recorded
- [ ] `infra/docker/.env` on the pilot host: `SITE_NAME` = public DNS name, `LETSENCRYPT_EMAIL`,
      strong unique `DB_ROOT_PASSWORD` and `PILOT_ADMIN_PASSWORD`, `PILOT_BIND_ADDRESS=0.0.0.0`; file
      mode 600, copy in the password manager; never committed
- [ ] DNS A/AAAA record of `SITE_NAME` points at the host; ports 80 and 443 open; `pnpm erp:pilot:up`
      ends with `pilot is up at https://<SITE_NAME>`; browser shows a valid Let's Encrypt certificate
- [ ] strong unique `TOWN_PG_PASSWORD` (the compose default `town` is dev-only; `pilot:up` refuses it and
      an empty value). `POSTGRES_PASSWORD` only
      applies on a fresh volume: on an existing postgres volume also run
      `dc exec postgres psql -U town -d town -c "ALTER ROLE town PASSWORD '<new>'"`
- [ ] `PILOT_TOWN_JWT_PRIVATE_KEY` set (own value, not the dev key); `town` container up and
      `curl -k https://<SITE_NAME>/town/api/healthz` → `{"status":"ok"}`, `/town/api/me` → 401
      (the distroless image has no healthcheck, so this external check is the probe)
- [ ] Authenticated gateway call through nginx: sign in as Administrator and call `/town/api/me` with
      the session cookie → 200 with `"user":"Administrator"` (catches a `town` → ERPNext site mix-up
      that the unauthenticated checks above cannot see). The password is read, never echoed:

      ```bash
      read -rs PW; jar=$(mktemp)
      curl -sk -c "$jar" -o /dev/null -w '%{http_code}\n' --data-urlencode usr=Administrator \
        --data-urlencode "pwd=$PW" https://<SITE_NAME>/api/method/login    # 200
      curl -sk -b "$jar" -w '\n%{http_code}\n' https://<SITE_NAME>/town/api/me # 200, "user":"Administrator"
      unset PW; rm -f "$jar"
      ```
- [ ] `dc exec backend bench --site <SITE_NAME> list-apps` lists frappe, erpnext, hrms, erp_town
- [ ] Thai company bootstrapped on the pilot site (once; idempotent, no demo data or users — never
      run `erp_town.setup.seed.run` on the pilot): System Settings th / Asia/Bangkok / Thailand / THB,
      base masters, the company with the draft Thai chart and VAT templates, setup marked complete:

      ```bash
      pnpm erp:pilot:bench -- --site <SITE_NAME> execute erp_town.setup.pilot.bootstrap \
        --kwargs '{"company_name": "<ชื่อบริษัทตามทะเบียน>", "abbr": "<CODE-TH>", "tax_id": "<13 digits>"}'
      ```

      then in Desk check Company → tax ID, `th_branch_code` (00000 = สำนักงานใหญ่) and the address;
      the chart and VAT templates are drafts until the accountant signs them off
- [ ] PDF of each Thai print format (ใบยืนยันคำสั่งซื้อ, ใบส่งของ, ใบกำกับภาษี/ใบแจ้งหนี้, ใบลดหนี้ of a
      return invoice, ใบเสร็จรับเงิน) downloaded from the pilot's Desk ("PDF", Chrome generator) renders
      Sarabun: Thai vowels and tone marks correct, no fallback font (`pdffonts <file>.pdf` lists
      Sarabun). The dev bench has no Chromium, so the automated check
      (`test_tax_invoice_pdf_through_chrome_embeds_sarabun`) is skipped there
- [ ] Bucket for backups exists outside the pilot host, versioning or object lock on if the provider
      has it; `BACKUP_S3_*` keys are scoped to that bucket only
- [ ] `pnpm erp:pilot:backup` succeeds (the set includes `*-town_pilot.dump`); the `backup` service log shows the next daily run
- [ ] Re-run this drill against the real pilot bucket before pilot start (steps 2–6 with the
      production `BACKUP_S3_*`, on the pilot host): counts equal, files present
- [ ] Desk check on the restored site: Sales Invoice print (Thai tax invoice) renders
- [ ] Drill site dropped, `sites/.restore` removed
- [ ] Town status (Desk → ERP Town) shows no `Failed`/`Invalid` rows; `town_redis_url` reachable
- [ ] Next restore drill date set (at least monthly during the pilot)
