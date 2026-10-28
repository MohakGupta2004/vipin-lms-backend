-- A course can be run again as a new batch. Every batch of a course shares one batch_group_id (the id of
-- the first batch) and gets the next batch_number: IFC is batch 1, its copy "IFC 2.0" is batch 2, and so on.
ALTER TABLE courses
    ADD COLUMN batch_group_id UUID,
    ADD COLUMN batch_number INT NOT NULL DEFAULT 1 CHECK (batch_number >= 1);
UPDATE courses SET batch_group_id = id;
ALTER TABLE courses ALTER COLUMN batch_group_id SET NOT NULL;
CREATE UNIQUE INDEX courses_batch_group_number_idx ON courses (batch_group_id, batch_number) WHERE deleted_at IS NULL;

-- A new course that is not a copy starts its own group. Defaults are filled in before BEFORE triggers run,
-- so NEW.id is already set here.
CREATE FUNCTION courses_default_batch_group()
RETURNS TRIGGER AS $$
BEGIN
    NEW.batch_group_id = COALESCE(NEW.batch_group_id, NEW.id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER courses_set_batch_group
    BEFORE INSERT ON courses
    FOR EACH ROW EXECUTE FUNCTION courses_default_batch_group();

-- private = only students who are already enrolled still see and open the course, until their enrollment
-- ends. Nobody else finds it in the catalog, previews it or gets in through a free course.
ALTER TABLE courses
    ADD COLUMN visibility TEXT NOT NULL DEFAULT 'public' CHECK (visibility IN ('public', 'private'));

-- A new batch reuses the PDFs of the batch it was copied from, so two notes may now point at one file.
ALTER TABLE notes DROP CONSTRAINT notes_object_key_key;
CREATE INDEX notes_object_key_idx ON notes (object_key);
