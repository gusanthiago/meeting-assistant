-- +goose Up
-- A failed summary no longer fails the meeting; this holds the reason while the meeting stays ready.
ALTER TABLE meetings ADD COLUMN summary_error text;

-- +goose Down
ALTER TABLE meetings DROP COLUMN summary_error;
