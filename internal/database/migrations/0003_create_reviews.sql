CREATE TABLE reviews (
    id             BIGSERIAL PRIMARY KEY,
    game_id        BIGINT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    reviewer_uuid  UUID NOT NULL,
    rating         SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 10),
    comment        TEXT CHECK (char_length(comment) <= 800),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_reviews_game_reviewer UNIQUE (game_id, reviewer_uuid)
);

CREATE INDEX idx_reviews_game_id ON reviews (game_id);
CREATE INDEX idx_reviews_created_at ON reviews (created_at DESC);
