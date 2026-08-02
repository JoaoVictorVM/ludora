CREATE TABLE games (
    id               BIGSERIAL PRIMARY KEY,
    external_id      TEXT NOT NULL,
    external_source  TEXT NOT NULL DEFAULT 'rawg',
    name             TEXT NOT NULL,
    cover_url        TEXT,
    released_at      DATE,
    developer        TEXT,
    description      TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_games_external UNIQUE (external_source, external_id)
);

CREATE INDEX idx_games_name ON games (name);
