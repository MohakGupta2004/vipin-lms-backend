package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

const (
	maxPostLength   = 5000 // characters
	maxLinksPerPost = 10
	maxLinkLength   = 2048
)

var (
	// ErrInvalidInput wraps every validation error. Its message is safe to show the client.
	ErrInvalidInput = errors.New("invalid input")
	// ErrForbidden means the user is logged in but not allowed to do this.
	ErrForbidden = errors.New("you are not allowed to do this")
	// ErrPostNotFound means the post does not exist or is not in a course the user owns.
	ErrPostNotFound = errors.New("post not found")

	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

type PostService struct {
	postRepo *models.PostRepository
}

func NewPostService(postRepo *models.PostRepository) *PostService {
	return &PostService{
		postRepo: postRepo,
	}
}

// CreatePost lets the owner of a course (instructor or admin) post to it.
func (s *PostService) CreatePost(ctx context.Context, user *models.User, courseID, content string, links []string) (*models.Post, error) {
	if !user.CanTeach() {
		return nil, ErrForbidden
	}

	content = strings.TrimSpace(content)
	if !uuidPattern.MatchString(courseID) {
		return nil, fmt.Errorf("%w: courseId must be a valid id", ErrInvalidInput)
	}
	if content == "" {
		return nil, fmt.Errorf("%w: post content is required", ErrInvalidInput)
	}
	if utf8.RuneCountInString(content) > maxPostLength {
		return nil, fmt.Errorf("%w: post content must be at most %d characters", ErrInvalidInput, maxPostLength)
	}

	cleanLinks, err := validateLinks(links)
	if err != nil {
		return nil, err
	}

	// Only the course's instructor_id may post to it.
	courseTitle, err := s.postRepo.GetCourseTitleForInstructor(ctx, courseID, user.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrForbidden
	}
	if err != nil {
		return nil, err
	}

	post, err := s.postRepo.CreatePost(ctx, user.ID, courseID, content, cleanLinks)
	if err != nil {
		return nil, err
	}
	post.CourseTitle = courseTitle
	post.AuthorName = user.FirstName + " " + user.LastName
	return post, nil
}

// ListFeed returns the posts the user can see, newest first.
func (s *PostService) ListFeed(ctx context.Context, user *models.User, limit, offset int) ([]models.Post, error) {
	return s.postRepo.ListFeed(ctx, user.ID, limit, offset)
}

// DeletePost lets the owner of a course (instructor or admin) delete any post in it.
func (s *PostService) DeletePost(ctx context.Context, user *models.User, postID string) error {
	if !user.CanTeach() {
		return ErrForbidden
	}
	if !uuidPattern.MatchString(postID) {
		return ErrPostNotFound
	}

	deleted, err := s.postRepo.DeletePost(ctx, postID, user.ID)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrPostNotFound
	}
	return nil
}

// validateLinks trims each link and makes sure it is a real http(s) URL.
// Only http and https are allowed, so links like "javascript:..." can never be saved.
func validateLinks(links []string) ([]string, error) {
	if len(links) > maxLinksPerPost {
		return nil, fmt.Errorf("%w: a post can have at most %d links", ErrInvalidInput, maxLinksPerPost)
	}

	cleanLinks := make([]string, 0, len(links))
	for _, link := range links {
		link = strings.TrimSpace(link)
		if link == "" {
			continue
		}
		if len(link) > maxLinkLength {
			return nil, fmt.Errorf("%w: each link must be at most %d characters", ErrInvalidInput, maxLinkLength)
		}

		u, err := url.ParseRequestURI(link)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("%w: %q is not a valid http or https link", ErrInvalidInput, link)
		}
		cleanLinks = append(cleanLinks, link)
	}
	return cleanLinks, nil
}
