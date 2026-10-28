DROP INDEX notes_object_key_idx;
ALTER TABLE notes ADD CONSTRAINT notes_object_key_key UNIQUE (object_key);

ALTER TABLE courses DROP COLUMN visibility;

DROP TRIGGER courses_set_batch_group ON courses;
DROP FUNCTION courses_default_batch_group();
DROP INDEX courses_batch_group_number_idx;
ALTER TABLE courses DROP COLUMN batch_number, DROP COLUMN batch_group_id;
