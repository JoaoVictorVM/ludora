CREATE TABLE IF NOT EXISTS schema_migrations (
    version     VARCHAR(14) PRIMARY KEY,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
