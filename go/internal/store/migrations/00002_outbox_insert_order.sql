-- +goose Up
-- Publish order: rows are numbered when inserted, which Emit does after it holds the
-- subject's seq row lock, so a subject's events are published in seq order whatever
-- clock the emitters used.
ALTER TABLE town_outbox ADD COLUMN insert_order bigint GENERATED ALWAYS AS IDENTITY;
CREATE INDEX town_outbox_pending_order_idx ON town_outbox (insert_order) WHERE status = 'Pending';

-- +goose Down
DROP INDEX town_outbox_pending_order_idx;
ALTER TABLE town_outbox DROP COLUMN insert_order;
