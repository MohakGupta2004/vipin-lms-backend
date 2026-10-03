-- Fails if a deleted course and a live course share a slug; rename one of them first.
DROP INDEX courses_slug_active_idx;
ALTER TABLE courses ADD CONSTRAINT courses_slug_key UNIQUE (slug);
