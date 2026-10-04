-- +goose Up
ALTER TABLE context_snapshot_assembly_metadata
    ADD COLUMN default_durable_budget_items INTEGER NOT NULL DEFAULT 12
        CHECK (default_durable_budget_items > 0);
ALTER TABLE context_snapshot_assembly_metadata
    ADD COLUMN durable_budget_items INTEGER NOT NULL DEFAULT 12
        CHECK (durable_budget_items > 0);
ALTER TABLE context_snapshot_assembly_metadata
    ADD COLUMN durable_item_budget_reason TEXT NOT NULL DEFAULT 'default'
        CHECK (durable_item_budget_reason IN ('default', 'explicit', 'required_always', 'legacy_all_context'));
ALTER TABLE context_snapshot_assembly_metadata
    ADD COLUMN required_always_items INTEGER NOT NULL DEFAULT 0
        CHECK (required_always_items >= 0);

-- +goose Down
ALTER TABLE context_snapshot_assembly_metadata DROP COLUMN required_always_items;
ALTER TABLE context_snapshot_assembly_metadata DROP COLUMN durable_item_budget_reason;
ALTER TABLE context_snapshot_assembly_metadata DROP COLUMN durable_budget_items;
ALTER TABLE context_snapshot_assembly_metadata DROP COLUMN default_durable_budget_items;
