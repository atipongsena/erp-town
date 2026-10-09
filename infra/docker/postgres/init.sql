-- Runs once on a fresh postgres-data volume (/docker-entrypoint-initdb.d); POSTGRES_DB creates `town`.
-- An existing volume never re-runs it: `pnpm erp:pilot:up` creates town_pilot idempotently.
CREATE DATABASE town_pilot;
