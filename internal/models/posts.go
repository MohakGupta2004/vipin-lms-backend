package models

import (
	"context"
	"database/sql"
)

type PostRepository struct {
	db *sql.DB
}

func NewPostRepository(db *sql.DB) *PostRepository {
	return &PostRepository{
		db: db,
	}
}

func (r *PostRepository) CreatePost(title, content, authorID string, ctx context.Context) error {
	query := "INSERT INTO posts (title, content, author_id) VALUES ($1, $2, $3)"
	_, err := r.db.ExecContext(ctx, query, title, content, authorID)
	return err
}

func (r *PostRepository) DeletePost(postID string, ctx context.Context) error {
	query := "DELETE FROM posts WHERE id = $1"
	_, err := r.db.ExecContext(ctx, query, postID)
	return err
}
