CREATE TABLE posts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    post TEXT,

    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    course_id UUID REFERENCES courses(id) ON DELETE CASCADE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX posts_user_id_idx ON posts (user_id);
CREATE INDEX posts_course_id_idx ON posts (course_id);

CREATE TRIGGER posts_set_updated_at
    BEFORE UPDATE ON posts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE post_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    post_id UUID REFERENCES posts(id) ON DELETE CASCADE,

    link TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX post_links_post_id_idx ON post_links (post_id);

CREATE TRIGGER post_links_set_updated_at
    BEFORE UPDATE ON post_links
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
