-- Schema v9 (feature 011, manual devices): devices the owner adds by hand.

-- Owner facts (append-only, replayed by a rebuild like user_device_attrs). add creates the
-- device, mac sets or clears its MAC (value '' clears), remove deletes it.
CREATE TABLE user_manual_devices (
    id           INTEGER PRIMARY KEY,
    identity_key TEXT NOT NULL,
    field        TEXT NOT NULL CHECK (field IN ('add', 'mac', 'remove')),
    value        TEXT NOT NULL,
    at           TEXT NOT NULL
);
CREATE INDEX user_manual_devices_key ON user_manual_devices (identity_key);

-- Projection flag: the device was added by hand.
ALTER TABLE devices ADD COLUMN manual INTEGER NOT NULL DEFAULT 0;
