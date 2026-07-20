CREATE TABLE operations (
    id TEXT PRIMARY KEY CHECK (length(trim(id)) > 0),
    profile_id TEXT REFERENCES profiles(id) ON DELETE SET NULL,
    kind TEXT NOT NULL CHECK (length(trim(kind)) > 0),
    state TEXT NOT NULL CHECK (state IN ('pending', 'running', 'succeeded', 'failed', 'rolling_back', 'rolled_back')),
    phase TEXT NOT NULL CHECK (length(trim(phase)) > 0),
    attempt INTEGER NOT NULL CHECK (attempt >= 0),
    recovery TEXT NOT NULL CHECK (json_valid(recovery)),
    error_code TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX operations_state_updated ON operations(state, updated_at, id);

CREATE TABLE settings (
    profile_id TEXT REFERENCES profiles(id) ON DELETE CASCADE,
    key TEXT NOT NULL CHECK (length(trim(key)) > 0),
    value TEXT NOT NULL CHECK (json_valid(value)),
    updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX settings_global_key ON settings(key) WHERE profile_id IS NULL;
CREATE UNIQUE INDEX settings_profile_key ON settings(profile_id, key) WHERE profile_id IS NOT NULL;
CREATE INDEX settings_profile ON settings(profile_id, key);
