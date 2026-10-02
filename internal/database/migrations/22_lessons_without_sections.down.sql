-- Lessons made without a section cannot satisfy NOT NULL, so they (and their notes) go.
DELETE FROM lessons WHERE section_id IS NULL;

ALTER TABLE lessons ALTER COLUMN section_id SET NOT NULL;
