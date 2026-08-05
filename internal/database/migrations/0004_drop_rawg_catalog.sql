-- The provider swap (F08) makes every RAWG-sourced record unreachable: selecting
-- the same title again resolves it under external_source = 'igdb', and the
-- UNIQUE (external_source, external_id) constraint would let both rows coexist
-- as two separate games with two separate review sets. Their reviews go with
-- them through fk_reviews_game's ON DELETE CASCADE.
DELETE FROM games WHERE external_source = 'rawg';
