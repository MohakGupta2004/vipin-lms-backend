-- A deleted course should not keep its slug forever: only courses that are not deleted must have unique slugs.
ALTER TABLE courses DROP CONSTRAINT courses_slug_key;
CREATE UNIQUE INDEX courses_slug_active_idx ON courses (slug) WHERE deleted_at IS NULL;
