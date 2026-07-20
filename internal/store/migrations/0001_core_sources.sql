CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY CHECK (version > 0),
    name TEXT NOT NULL,
    checksum TEXT NOT NULL,
    applied_at TEXT NOT NULL
);

CREATE TABLE profiles (
    id TEXT PRIMARY KEY CHECK (length(trim(id)) > 0),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    mode TEXT NOT NULL CHECK (mode IN ('managed', 'external', 'legacy')),
    core_type TEXT NOT NULL CHECK (core_type IN ('mihomo')),
    config_path TEXT,
    active INTEGER NOT NULL DEFAULT 0 CHECK (active IN (0, 1)),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (
        (mode = 'managed' AND config_path IS NULL) OR
        (mode IN ('external', 'legacy') AND config_path IS NOT NULL AND length(trim(config_path)) > 0)
    )
);

CREATE UNIQUE INDEX profiles_one_active ON profiles(active) WHERE active = 1;
CREATE INDEX profiles_name ON profiles(name, id);

CREATE TABLE subscriptions (
    id TEXT PRIMARY KEY CHECK (length(trim(id)) > 0),
    profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    url TEXT NOT NULL CHECK (length(trim(url)) > 0),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    etag TEXT,
    last_modified TEXT,
    last_attempt_at TEXT,
    last_success_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX subscriptions_profile ON subscriptions(profile_id, name, id);

CREATE TABLE nodes (
    id TEXT PRIMARY KEY CHECK (length(trim(id)) > 0),
    subscription_id TEXT NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    remote_key TEXT NOT NULL CHECK (length(trim(remote_key)) > 0),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    protocol TEXT NOT NULL CHECK (length(trim(protocol)) > 0),
    spec TEXT NOT NULL CHECK (json_valid(spec)),
    position INTEGER NOT NULL CHECK (position >= 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(subscription_id, remote_key)
);

CREATE INDEX nodes_subscription_position ON nodes(subscription_id, position, id);
