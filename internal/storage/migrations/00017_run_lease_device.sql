-- +goose Up
-- The hub device name of the process that holds a run's lease, so a task list
-- shows on which device an agent works. Empty for a device without a hub and
-- for leases written before this column.
ALTER TABLE run_leases ADD COLUMN owner_device TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE run_leases DROP COLUMN owner_device;
