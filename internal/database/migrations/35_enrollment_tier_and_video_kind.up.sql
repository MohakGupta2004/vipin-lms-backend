-- The tier a student bought for a course. Existing enrollments keep full access (ultra).
ALTER TABLE enrollments ADD COLUMN tier TEXT NOT NULL DEFAULT 'ultra'
    CHECK (tier IN ('plus', 'premium', 'ultra'));
ALTER TABLE enrollments ALTER COLUMN tier DROP DEFAULT; -- new rows must pick one

-- Offline recordings and Meet (live class) recordings are uploaded the same way but shown apart.
ALTER TABLE videos ADD COLUMN kind TEXT NOT NULL DEFAULT 'offline'
    CHECK (kind IN ('offline', 'meet'));
