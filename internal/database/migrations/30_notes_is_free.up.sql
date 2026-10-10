-- A note can be free on its own, like a video: users previewing the course can open it even when its lesson is paid.
ALTER TABLE notes ADD COLUMN is_free BOOLEAN NOT NULL DEFAULT false;
