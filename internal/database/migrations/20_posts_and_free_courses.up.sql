-- A free course is a course every logged-in student can see posts from,
-- without being enrolled. Think of it as the "free panel".
ALTER TABLE courses ADD COLUMN is_free BOOLEAN NOT NULL DEFAULT false;

-- A post always belongs to an author and a course, and always has text.
ALTER TABLE posts
    ALTER COLUMN post SET NOT NULL,
    ALTER COLUMN user_id SET NOT NULL,
    ALTER COLUMN course_id SET NOT NULL;

ALTER TABLE post_links
    ALTER COLUMN post_id SET NOT NULL,
    ALTER COLUMN link SET NOT NULL;

-- Feed is read newest first per course.
CREATE INDEX posts_course_id_created_at_idx ON posts (course_id, created_at DESC);
