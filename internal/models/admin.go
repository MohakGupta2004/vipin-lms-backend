package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PersonEnrollment is one course a student is enrolled in.
type PersonEnrollment struct {
	CourseID       string    `json:"courseId"`
	CourseTitle    string    `json:"courseTitle"`
	InstructorName string    `json:"instructorName"`
	Tier           Tier      `json:"tier"`
	Status         string    `json:"status"`
	EnrolledAt     time.Time `json:"enrolledAt"`
}

// Person is a student account with all of its enrollments.
type Person struct {
	ID        string    `json:"id"`
	FirstName string    `json:"firstName"`
	LastName  string    `json:"lastName"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
	CreatedAt time.Time `json:"createdAt"`
	// LastLoginAt is null when the student signed up but never logged in.
	LastLoginAt   *time.Time         `json:"lastLoginAt"`
	EmailVerified bool               `json:"emailVerified"`
	Enrollments   []PersonEnrollment `json:"enrollments"`
}

// PeoplePage is one page of students.
type PeoplePage struct {
	Total int      `json:"total"`
	Items []Person `json:"items"`
}

// RecentSignup is a short sign-up row for the dashboard list.
type RecentSignup struct {
	ID        string    `json:"id"`
	FirstName string    `json:"firstName"`
	LastName  string    `json:"lastName"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"createdAt"`
}

// RecentLead is a short website lead row for the dashboard list.
type RecentLead struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	CourseSlugs []string  `json:"courseSlugs"`
	CreatedAt   time.Time `json:"createdAt"`
}

// AdminDashboard is the admin home screen: the numbers plus two short lists.
type AdminDashboard struct {
	Days                int            `json:"days"`
	NewSignups          int            `json:"newSignups"`
	NeverLoggedIn       int            `json:"neverLoggedIn"`
	NotEnrolled         int            `json:"notEnrolled"`
	RecentEnrollments   int            `json:"recentEnrollments"`
	ExpiringSoon        int            `json:"expiringSoon"`
	NewVisitors         int            `json:"newVisitors"`
	NewMasterclassSeats int            `json:"newMasterclassSeats"`
	NewFormLeads        int            `json:"newFormLeads"`
	TotalStudents       int            `json:"totalStudents"`
	TotalCourses        int            `json:"totalCourses"`
	PublishedCourses    int            `json:"publishedCourses"`
	RecentSignups       []RecentSignup `json:"recentSignups"`
	RecentLeads         []RecentLead   `json:"recentLeads"`
}

// AdminRepository reads the students, the enrollments and the website numbers the admin pages show.
type AdminRepository struct {
	db *sql.DB
}

func NewAdminRepository(db *sql.DB) *AdminRepository {
	return &AdminRepository{db: db}
}

// peopleWhere is shared by every people view: students that are not deleted.
const peopleWhere = "u.role = 'student' AND u.deleted_at IS NULL"

// peopleFilter is the WHERE part, the ORDER BY part and the arguments of one people view.
type peopleFilter struct {
	where string
	order string
	args  []any
}

// buildPeopleFilter returns the filter of one view. An unknown view gives an empty filter.
func buildPeopleFilter(view string, windowStart time.Time) peopleFilter {
	switch view {
	case "new":
		return peopleFilter{
			where: " AND u.created_at >= $1",
			order: " ORDER BY u.created_at DESC, u.id",
			args:  []any{windowStart},
		}
	case "never_logged_in":
		return peopleFilter{
			where: " AND u.last_login_at IS NULL",
			order: " ORDER BY u.created_at DESC, u.id",
		}
	case "not_enrolled":
		return peopleFilter{
			where: " AND u.last_login_at IS NOT NULL AND NOT EXISTS (SELECT 1 FROM enrollments e WHERE e.user_id = u.id)",
			order: " ORDER BY u.last_login_at DESC, u.id",
		}
	case "recently_enrolled":
		return peopleFilter{
			where: " AND EXISTS (SELECT 1 FROM enrollments e WHERE e.user_id = u.id AND e.enrolled_at >= $1)",
			order: " ORDER BY (SELECT MAX(e.enrolled_at) FROM enrollments e WHERE e.user_id = u.id AND e.enrolled_at >= $1) DESC, u.id",
			args:  []any{windowStart},
		}
	}
	return peopleFilter{}
}

// ListPeople returns one page of students for one admin view. Every person carries all of its
// enrollments, newest first.
func (r *AdminRepository) ListPeople(ctx context.Context, view string, days, limit, offset int, now time.Time) (*PeoplePage, error) {
	windowStart := now.AddDate(0, 0, -days)
	filter := buildPeopleFilter(view, windowStart)
	if filter.where == "" {
		return nil, fmt.Errorf("unknown people view %q", view)
	}

	page := &PeoplePage{}

	countQuery := "SELECT COUNT(*) FROM users u WHERE " + peopleWhere + filter.where
	if err := r.db.QueryRowContext(ctx, countQuery, filter.args...).Scan(&page.Total); err != nil {
		return nil, err
	}

	limitParam := len(filter.args) + 1
	offsetParam := len(filter.args) + 2
	itemsQuery := fmt.Sprintf(`SELECT u.id, u.first_name, u.last_name, u.email, COALESCE(u.phone, ''),
			u.created_at, u.last_login_at, u.email_verified_at IS NOT NULL
		FROM users u WHERE %s%s%s LIMIT $%d OFFSET $%d`,
		peopleWhere, filter.where, filter.order, limitParam, offsetParam)
	itemsArgs := append([]any{}, filter.args...)
	itemsArgs = append(itemsArgs, limit, offset)

	rows, err := r.db.QueryContext(ctx, itemsQuery, itemsArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	page.Items = []Person{}
	userIDs := []string{}
	for rows.Next() {
		var person Person
		var lastLoginAt sql.NullTime
		if err := rows.Scan(&person.ID, &person.FirstName, &person.LastName, &person.Email, &person.Phone,
			&person.CreatedAt, &lastLoginAt, &person.EmailVerified); err != nil {
			return nil, err
		}
		if lastLoginAt.Valid {
			t := lastLoginAt.Time
			person.LastLoginAt = &t
		}
		person.Enrollments = []PersonEnrollment{}
		page.Items = append(page.Items, person)
		userIDs = append(userIDs, person.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	enrollments, err := r.listEnrollments(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	for i := range page.Items {
		personEnrollments := enrollments[page.Items[i].ID]
		if personEnrollments != nil {
			page.Items[i].Enrollments = personEnrollments
		}
	}

	return page, nil
}

// listEnrollments returns the enrollments of the given students, newest first, grouped by student id.
func (r *AdminRepository) listEnrollments(ctx context.Context, userIDs []string) (map[string][]PersonEnrollment, error) {
	result := map[string][]PersonEnrollment{}
	if len(userIDs) == 0 {
		return result, nil
	}

	placeholders := []string{}
	args := []any{}
	for i, userID := range userIDs {
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		args = append(args, userID)
	}

	query := `SELECT e.user_id, e.course_id, c.title,
			COALESCE(i.first_name, '') || ' ' || COALESCE(i.last_name, ''),
			e.tier, e.status, e.enrolled_at
		FROM enrollments e
		JOIN courses c ON c.id = e.course_id
		LEFT JOIN users i ON i.id = c.instructor_id
		WHERE e.user_id IN (` + strings.Join(placeholders, ", ") + `)
		ORDER BY e.enrolled_at DESC`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var userID string
		var enrollment PersonEnrollment
		if err := rows.Scan(&userID, &enrollment.CourseID, &enrollment.CourseTitle, &enrollment.InstructorName,
			&enrollment.Tier, &enrollment.Status, &enrollment.EnrolledAt); err != nil {
			return nil, err
		}
		result[userID] = append(result[userID], enrollment)
	}
	return result, rows.Err()
}

// GetDashboard returns the numbers of the admin home screen for a window of days.
func (r *AdminRepository) GetDashboard(ctx context.Context, days int, now time.Time) (*AdminDashboard, error) {
	windowStart := now.AddDate(0, 0, -days)
	expiryLimit := now.AddDate(0, 0, 7)

	dashboard := &AdminDashboard{Days: days}

	countsQuery := `SELECT
		(SELECT COUNT(*) FROM users u WHERE u.role = 'student' AND u.deleted_at IS NULL AND u.created_at >= $1),
		(SELECT COUNT(*) FROM users u WHERE u.role = 'student' AND u.deleted_at IS NULL AND u.created_at >= $1 AND u.last_login_at IS NULL),
		(SELECT COUNT(*) FROM users u WHERE u.role = 'student' AND u.deleted_at IS NULL AND u.last_login_at IS NOT NULL
			AND NOT EXISTS (SELECT 1 FROM enrollments e WHERE e.user_id = u.id)),
		(SELECT COUNT(*) FROM enrollments e WHERE e.enrolled_at >= $1),
		(SELECT COUNT(*) FROM enrollments e WHERE e.status = 'active' AND e.expires_at IS NOT NULL
			AND e.expires_at >= $2 AND e.expires_at <= $3),
		(SELECT COUNT(*) FROM site_visitors v WHERE v.first_seen_at >= $1),
		(SELECT COUNT(*) FROM site_leads l WHERE l.kind = 'masterclass' AND l.created_at >= $1),
		(SELECT COUNT(*) FROM site_leads l WHERE l.kind <> 'masterclass' AND l.created_at >= $1),
		(SELECT COUNT(*) FROM users u WHERE u.role = 'student' AND u.deleted_at IS NULL),
		(SELECT COUNT(*) FROM courses c WHERE c.deleted_at IS NULL),
		(SELECT COUNT(*) FROM courses c WHERE c.deleted_at IS NULL AND c.status = 'published')`
	err := r.db.QueryRowContext(ctx, countsQuery, windowStart, now, expiryLimit).Scan(
		&dashboard.NewSignups,
		&dashboard.NeverLoggedIn,
		&dashboard.NotEnrolled,
		&dashboard.RecentEnrollments,
		&dashboard.ExpiringSoon,
		&dashboard.NewVisitors,
		&dashboard.NewMasterclassSeats,
		&dashboard.NewFormLeads,
		&dashboard.TotalStudents,
		&dashboard.TotalCourses,
		&dashboard.PublishedCourses,
	)
	if err != nil {
		return nil, err
	}

	signups, err := r.listRecentSignups(ctx, windowStart, 5)
	if err != nil {
		return nil, err
	}
	dashboard.RecentSignups = signups

	leads, err := r.listRecentLeads(ctx, windowStart, 5)
	if err != nil {
		return nil, err
	}
	dashboard.RecentLeads = leads

	return dashboard, nil
}

// listRecentSignups returns the newest students of the window.
func (r *AdminRepository) listRecentSignups(ctx context.Context, windowStart time.Time, limit int) ([]RecentSignup, error) {
	query := `SELECT id, first_name, last_name, email, created_at
		FROM users u
		WHERE u.role = 'student' AND u.deleted_at IS NULL AND u.created_at >= $1
		ORDER BY u.created_at DESC, u.id
		LIMIT $2`

	rows, err := r.db.QueryContext(ctx, query, windowStart, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	signups := []RecentSignup{}
	for rows.Next() {
		var signup RecentSignup
		if err := rows.Scan(&signup.ID, &signup.FirstName, &signup.LastName, &signup.Email, &signup.CreatedAt); err != nil {
			return nil, err
		}
		signups = append(signups, signup)
	}
	return signups, rows.Err()
}

// listRecentLeads returns the newest website leads of the window.
func (r *AdminRepository) listRecentLeads(ctx context.Context, windowStart time.Time, limit int) ([]RecentLead, error) {
	query := `SELECT id, kind, name, email, COALESCE(array_to_json(course_slugs)::text, '[]'), created_at
		FROM site_leads
		WHERE created_at >= $1
		ORDER BY created_at DESC, id
		LIMIT $2`

	rows, err := r.db.QueryContext(ctx, query, windowStart, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	leads := []RecentLead{}
	for rows.Next() {
		var lead RecentLead
		var courseSlugsJSON string
		if err := rows.Scan(&lead.ID, &lead.Kind, &lead.Name, &lead.Email, &courseSlugsJSON, &lead.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(courseSlugsJSON), &lead.CourseSlugs); err != nil {
			return nil, err
		}
		leads = append(leads, lead)
	}
	return leads, rows.Err()
}
