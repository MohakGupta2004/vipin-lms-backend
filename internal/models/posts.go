package models

import (
	"context"
	"database/sql"
	"time"
)

// Post is one instructor post shown in a student's feed.
type Post struct {
	ID          string    `json:"id"`
	CourseID    string    `json:"courseId"`
	CourseTitle string    `json:"courseTitle"`
	AuthorID    string    `json:"authorId"`
	AuthorName  string    `json:"authorName"`
	Content     string    `json:"content"`
	Links       []string  `json:"links"`
	CreatedAt   time.Time `json:"createdAt"`
}

type PostRepository struct {
	db *sql.DB
}

func NewPostRepository(db *sql.DB) *PostRepository {
	return &PostRepository{
		db: db,
	}
}

// GetCourseTitleForInstructor returns the course title only if the course
// exists and is taught by this instructor. It returns sql.ErrNoRows otherwise.
func (r *PostRepository) GetCourseTitleForInstructor(ctx context.Context, courseID, instructorID string) (string, error) {
	query := `SELECT title FROM courses
		WHERE id = $1 AND instructor_id = $2 AND deleted_at IS NULL`

	var title string
	err := r.db.QueryRowContext(ctx, query, courseID, instructorID).Scan(&title)
	return title, err
}

// CreatePost saves the post and all its links together.
// If any insert fails, nothing is saved (transaction rollback).
func (r *PostRepository) CreatePost(ctx context.Context, authorID, courseID, content string, links []string) (*Post, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	// Rollback does nothing if Commit already succeeded.
	defer tx.Rollback()

	post := &Post{
		CourseID: courseID,
		AuthorID: authorID,
		Content:  content,
		Links:    links,
	}

	query := "INSERT INTO posts (post, user_id, course_id) VALUES ($1, $2, $3) RETURNING id, created_at"
	err = tx.QueryRowContext(ctx, query, content, authorID, courseID).Scan(&post.ID, &post.CreatedAt)
	if err != nil {
		return nil, err
	}

	for _, link := range links {
		_, err = tx.ExecContext(ctx, "INSERT INTO post_links (post_id, link) VALUES ($1, $2)", post.ID, link)
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return post, nil
}

// ListFeed returns the newest posts this user is allowed to see:
//   - posts in free courses (everyone),
//   - posts in courses the user is enrolled in (enrollment still valid),
//   - posts in courses the user teaches (so instructors see their own posts).
func (r *PostRepository) ListFeed(ctx context.Context, userID string, limit, offset int) ([]Post, error) {
	query := `
		SELECT p.id, p.course_id, c.title, p.user_id, u.first_name || ' ' || u.last_name, p.post, p.created_at
		FROM posts p
		JOIN courses c ON c.id = p.course_id
		JOIN users u ON u.id = p.user_id
		WHERE c.deleted_at IS NULL
		  AND (
		        c.instructor_id = $1
		        OR (
		            c.status = 'published'
		            AND (
		                c.is_free
		                OR EXISTS (
		                    SELECT 1 FROM enrollments e
		                    WHERE e.course_id = c.id
		                      AND e.user_id = $1
		                      AND e.status IN ('active', 'completed')
		                      AND (e.expires_at IS NULL OR e.expires_at > now())
		                )
		            )
		        )
		  )
		ORDER BY p.created_at DESC, p.id DESC
		LIMIT $2 OFFSET $3`

	rows, err := r.db.QueryContext(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	posts := []Post{}
	postIDs := []string{}
	for rows.Next() {
		var p Post
		err := rows.Scan(&p.ID, &p.CourseID, &p.CourseTitle, &p.AuthorID, &p.AuthorName, &p.Content, &p.CreatedAt)
		if err != nil {
			return nil, err
		}
		p.Links = []string{}
		posts = append(posts, p)
		postIDs = append(postIDs, p.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(posts) == 0 {
		return posts, nil
	}

	// Load links for all posts in one query instead of one query per post.
	links, err := r.getLinksByPostIDs(ctx, postIDs)
	if err != nil {
		return nil, err
	}
	for i := range posts {
		if postLinks, ok := links[posts[i].ID]; ok {
			posts[i].Links = postLinks
		}
	}
	return posts, nil
}

// getLinksByPostIDs returns a map of post ID -> links of that post.
func (r *PostRepository) getLinksByPostIDs(ctx context.Context, postIDs []string) (map[string][]string, error) {
	query := `SELECT post_id, link FROM post_links
		WHERE post_id = ANY($1::uuid[])
		ORDER BY created_at, id`

	rows, err := r.db.QueryContext(ctx, query, postIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	links := map[string][]string{}
	for rows.Next() {
		var postID, link string
		if err := rows.Scan(&postID, &link); err != nil {
			return nil, err
		}
		links[postID] = append(links[postID], link)
	}
	return links, rows.Err()
}

// DeletePost deletes a post only if it was written by authorID.
// It returns false when no such post exists for this author.
// Links are removed automatically (ON DELETE CASCADE).
func (r *PostRepository) DeletePost(ctx context.Context, postID, authorID string) (bool, error) {
	result, err := r.db.ExecContext(ctx, "DELETE FROM posts WHERE id = $1 AND user_id = $2", postID, authorID)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
