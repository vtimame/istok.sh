-- +goose Up
-- Ties an executed validation to the code it checked: the commit and the
-- digest of the working tree after the check. Task completion refuses a
-- validation whose tree has changed since. Empty for attested validations and
-- for validations recorded before this migration.
ALTER TABLE validations ADD COLUMN tree_state TEXT NOT NULL DEFAULT '';
ALTER TABLE validations ADD COLUMN tree_commit TEXT NOT NULL DEFAULT '';
ALTER TABLE validations ADD COLUMN tree_digest TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE validations DROP COLUMN tree_digest;
ALTER TABLE validations DROP COLUMN tree_commit;
ALTER TABLE validations DROP COLUMN tree_state;
