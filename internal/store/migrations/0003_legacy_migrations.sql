CREATE TABLE legacy_migrations (
    id TEXT PRIMARY KEY CHECK (length(trim(id)) > 0),
    profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE RESTRICT,
    source_dir TEXT NOT NULL CHECK (length(trim(source_dir)) > 0),
    state TEXT NOT NULL CHECK (state IN ('pending', 'succeeded', 'failed', 'rolled_back')),
    error_code TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    completed_at TEXT,
    rolled_back_at TEXT
);

CREATE UNIQUE INDEX legacy_migrations_active_source
ON legacy_migrations(source_dir)
WHERE state != 'rolled_back';

CREATE INDEX legacy_migrations_updated ON legacy_migrations(updated_at, id);

CREATE TABLE legacy_files (
    migration_id TEXT NOT NULL REFERENCES legacy_migrations(id) ON DELETE CASCADE,
    relative_path TEXT NOT NULL CHECK (length(trim(relative_path)) > 0),
    before_exists INTEGER NOT NULL CHECK (before_exists IN (0, 1)),
    before_mode INTEGER NOT NULL CHECK (before_mode >= 0),
    before_size INTEGER NOT NULL CHECK (before_size >= 0),
    before_sha256 TEXT,
    expected_exists INTEGER NOT NULL CHECK (expected_exists IN (0, 1)),
    expected_sha256 TEXT,
    snapshot_path TEXT,
    PRIMARY KEY (migration_id, relative_path),
    CHECK (
        (before_exists = 1 AND length(before_sha256) = 64 AND length(snapshot_path) > 0) OR
        (before_exists = 0 AND before_mode = 0 AND before_size = 0 AND before_sha256 IS NULL AND snapshot_path IS NULL)
    ),
    CHECK (
        (expected_exists = 1 AND length(expected_sha256) = 64) OR
        (expected_exists = 0 AND expected_sha256 IS NULL)
    )
);
