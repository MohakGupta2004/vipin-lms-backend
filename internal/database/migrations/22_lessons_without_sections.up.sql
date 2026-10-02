-- A lesson is a chapter by itself: instructors create lessons straight under a course and
-- attach notes to them. The section grouping stays in the schema but is no longer required.
ALTER TABLE lessons ALTER COLUMN section_id DROP NOT NULL;
