-- Rows with a file would lose their file reference, so they go too.
DELETE FROM notes WHERE object_key IS NOT NULL;

ALTER TABLE notes
    DROP CONSTRAINT notes_file_columns_check,
    DROP COLUMN size_bytes,
    DROP COLUMN object_key,
    DROP COLUMN file_name;
