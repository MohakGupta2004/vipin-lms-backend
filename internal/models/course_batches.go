package models

import (
	"context"
	"database/sql"
)

// NewBatch is what the copy of a course is saved with; everything else comes from the course it copies.
type NewBatch struct {
	Title string // "" = the source's title
	Slug  string // "" = the source's slug ending in "-batch-<number>", like "ifc-batch-2"
}

// CopyAsNextBatch starts the next batch of a course: a draft, public copy in the same batch group with the
// next batch number. The copy gets the source's lessons, PDF notes, offline recordings, tests and
// flashcards, in one transaction. Meet (live class) recordings and everything students did (enrollments,
// progress, attempts, reviews, posts) stay with the source. Notes and videos point at the same stored
// files as the source, so nothing is uploaded or transcoded again.
// It returns sql.ErrNoRows when the source course does not exist.
func (r *CourseRepository) CopyAsNextBatch(ctx context.Context, sourceID string, b NewBatch) (*Course, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	// Rollback does nothing if Commit already succeeded.
	defer tx.Rollback()

	// Locking every batch of the group makes two copies made at once number themselves one after the other.
	// Deleted batches still count, so a number is never handed out twice.
	var groupID string
	var nextNumber int
	err = tx.QueryRowContext(ctx, `
		SELECT g.batch_group_id, MAX(g.batch_number) + 1
		FROM (
			SELECT batch_group_id, batch_number FROM courses
			WHERE batch_group_id = (SELECT batch_group_id FROM courses WHERE id = $1 AND deleted_at IS NULL)
			FOR UPDATE
		) g
		GROUP BY g.batch_group_id`, sourceID).Scan(&groupID, &nextNumber)
	if err != nil {
		return nil, err
	}

	var c Course
	err = scanCourse(tx.QueryRowContext(ctx, `
		INSERT INTO courses (exam_id, instructor_id, title, slug, short_description, description, thumbnail_key,
		                     is_free, status, visibility, batch_group_id, batch_number)
		SELECT exam_id, instructor_id,
		       COALESCE(NULLIF($2, ''), title),
		       COALESCE(NULLIF($3, ''), regexp_replace(slug, '-batch-[0-9]+$', '') || '-batch-' || $5::int),
		       short_description, description, thumbnail_key,
		       is_free, 'draft', 'public', $4, $5::int
		FROM courses WHERE id = $1
		RETURNING `+courseColumns, sourceID, b.Title, b.Slug, groupID, nextNumber), &c)
	if err != nil {
		return nil, err
	}

	if err := copyBatchContent(ctx, tx, sourceID, c.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &c, nil
}

// copyBatchContent copies the content of course src into course dst. id_map pairs every copied row's id
// with its copy's id, so children (notes, videos, questions, cards, ...) find their new parent.
// UUIDs never repeat across tables, so one map serves lessons, quizzes, questions and decks.
func copyBatchContent(ctx context.Context, tx *sql.Tx, src, dst string) error {
	srcOnly := []any{src}
	srcAndDst := []any{src, dst}
	steps := []struct {
		query string
		args  []any
	}{
		{`CREATE TEMP TABLE id_map (old_id UUID PRIMARY KEY, new_id UUID NOT NULL DEFAULT gen_random_uuid()) ON COMMIT DROP`, nil},

		// lessons (chapters)
		{`INSERT INTO id_map (old_id) SELECT id FROM lessons WHERE course_id = $1 AND deleted_at IS NULL`, srcOnly},
		{`INSERT INTO lessons (id, course_id, title, lesson_type, content, is_free, is_published, position)
		  SELECT m.new_id, $2, l.title, l.lesson_type, l.content, l.is_free, l.is_published, l.position
		  FROM lessons l JOIN id_map m ON m.old_id = l.id
		  WHERE l.course_id = $1`, srcAndDst},

		// PDF notes the instructor shared (students' own notes are not course material)
		{`INSERT INTO notes (author_id, course_id, lesson_id, title, content, visibility, file_name, object_key, size_bytes, tiers)
		  SELECT n.author_id, $2, m.new_id, n.title, n.content, n.visibility, n.file_name, n.object_key, n.size_bytes, n.tiers
		  FROM notes n JOIN id_map m ON m.old_id = n.lesson_id
		  WHERE n.course_id = $1 AND n.visibility = 'course' AND n.object_key IS NOT NULL AND n.deleted_at IS NULL`, srcAndDst},

		// offline recordings that finished transcoding; Meet recordings belong to the batch that attended them
		{`INSERT INTO videos (uploaded_by, course_id, lesson_id, title, description, kind, tiers,
		                      original_key, hls_prefix, duration_sec, size_bytes, status)
		  SELECT v.uploaded_by, $2, m.new_id, v.title, v.description, v.kind, v.tiers,
		         v.original_key, v.hls_prefix, v.duration_sec, v.size_bytes, v.status
		  FROM videos v JOIN id_map m ON m.old_id = v.lesson_id
		  WHERE v.course_id = $1 AND v.kind = 'offline' AND v.status = 'ready'`, srcAndDst},

		// tests: the quizzes of copied lessons, then their questions, then the options
		{`INSERT INTO id_map (old_id)
		  SELECT q.id FROM quizzes q JOIN id_map m ON m.old_id = q.lesson_id
		  WHERE q.course_id = $1 AND q.deleted_at IS NULL`, srcOnly},
		{`INSERT INTO quizzes (id, course_id, lesson_id, created_by, title, description, time_limit_sec, pass_percent,
		                       status, type, tiers)
		  SELECT qm.new_id, $2, lm.new_id, q.created_by, q.title, q.description, q.time_limit_sec, q.pass_percent,
		         q.status, q.type, q.tiers
		  FROM quizzes q
		  JOIN id_map qm ON qm.old_id = q.id
		  JOIN id_map lm ON lm.old_id = q.lesson_id
		  WHERE q.course_id = $1`, srcAndDst},
		{`INSERT INTO id_map (old_id)
		  SELECT qu.id FROM questions qu JOIN id_map m ON m.old_id = qu.quiz_id`, nil},
		{`INSERT INTO questions (id, quiz_id, question_text, explanation, position)
		  SELECT qum.new_id, qm.new_id, qu.question_text, qu.explanation, qu.position
		  FROM questions qu
		  JOIN id_map qum ON qum.old_id = qu.id
		  JOIN id_map qm ON qm.old_id = qu.quiz_id`, nil},
		{`INSERT INTO question_options (question_id, option_text, is_correct, position)
		  SELECT m.new_id, o.option_text, o.is_correct, o.position
		  FROM question_options o JOIN id_map m ON m.old_id = o.question_id`, nil},

		// flashcards: the decks of copied lessons and the course-wide deck (lesson_id NULL), then their cards
		{`INSERT INTO id_map (old_id)
		  SELECT d.id FROM flashcard_decks d
		  WHERE d.course_id = $1 AND d.deleted_at IS NULL
		    AND (d.lesson_id IS NULL OR d.lesson_id IN (SELECT old_id FROM id_map))`, srcOnly},
		{`INSERT INTO flashcard_decks (id, course_id, lesson_id, created_by, status)
		  SELECT dm.new_id, $2, lm.new_id, d.created_by, d.status
		  FROM flashcard_decks d
		  JOIN id_map dm ON dm.old_id = d.id
		  LEFT JOIN id_map lm ON lm.old_id = d.lesson_id
		  WHERE d.course_id = $1`, srcAndDst},
		{`INSERT INTO flashcards (deck_id, course_id, created_by, front, back, position, tiers)
		  SELECT m.new_id, $2, f.created_by, f.front, f.back, f.position, f.tiers
		  FROM flashcards f JOIN id_map m ON m.old_id = f.deck_id
		  WHERE f.course_id = $1 AND f.deleted_at IS NULL`, srcAndDst},
	}
	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, step.query, step.args...); err != nil {
			return err
		}
	}
	return nil
}
