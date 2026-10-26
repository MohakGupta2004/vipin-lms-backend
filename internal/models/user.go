package models

import (
	"context"
	"database/sql"
	"time"
)

type RegisterUser struct {
	FirstName   string
	LastName    string
	Email       string
	Phone       string
	Password    string
	LastLoginAt time.Time
}

type Role string

const (
	RoleStudent    Role = "student"
	RoleInstructor Role = "instructor"
	RoleAdmin      Role = "admin"
)

type User struct {
	ID        string `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
	// Phone is empty for accounts created before phone numbers were collected.
	Phone       string    `json:"phone"`
	Password    string    `json:"-"`
	Role        Role      `json:"role"`
	LastLoginAt time.Time `json:"lastLoginAt"`
	// EmailVerified is true once the user has confirmed their email with a one-time code.
	EmailVerified bool `json:"emailVerified"`
}

// CanTeach reports whether the user's role may own and manage courses.
// Owning a specific course still requires being its instructor_id.
func (u *User) CanTeach() bool {
	return u.Role == RoleInstructor || u.Role == RoleAdmin
}

// UserSummary is the public view of a user for admin pickers. It never carries password data.
type UserSummary struct {
	ID          string     `json:"id"`
	FirstName   string     `json:"firstName"`
	LastName    string     `json:"lastName"`
	Email       string     `json:"email"`
	Role        Role       `json:"role"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastLoginAt *time.Time `json:"lastLoginAt"`
	// EmailVerified is true once the user has confirmed their email with a one-time code.
	EmailVerified bool `json:"emailVerified"`
}

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{
		db: db,
	}
}

func (r *UserRepository) CreateUser(firstName, lastName, email, phone, password string, ctx context.Context) (*User, error) {
	user := &User{
		FirstName:   firstName,
		LastName:    lastName,
		Email:       email,
		Phone:       phone,
		Password:    password,
		LastLoginAt: time.Now(),
		Role:        RoleStudent,
	}

	query := "INSERT INTO users (first_name, last_name, email, phone, password, role, last_login_at) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id"
	row := r.db.QueryRowContext(ctx, query, user.FirstName, user.LastName, user.Email, user.Phone, user.Password, user.Role, user.LastLoginAt).Scan(&user.ID)

	return user, row
}

func (r *UserRepository) GetUserByEmail(email string, ctx context.Context) (*User, error) {
	query := "SELECT id, first_name, last_name, email, COALESCE(phone, ''), password, role, last_login_at, email_verified_at IS NOT NULL FROM users WHERE lower(email) = lower($1)"
	row := r.db.QueryRowContext(ctx, query, email)

	user := &User{}
	err := row.Scan(&user.ID, &user.FirstName, &user.LastName, &user.Email, &user.Phone, &user.Password, &user.Role, &user.LastLoginAt, &user.EmailVerified)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // User not found
		}
		return nil, err
	}

	return user, nil
}
func (r *UserRepository) GetUserById(id string, ctx context.Context) (*User, error) {
	query := "SELECT id, first_name, last_name, email, COALESCE(phone, ''), password, role, last_login_at, email_verified_at IS NOT NULL FROM users WHERE id = $1"
	row := r.db.QueryRowContext(ctx, query, id)

	user := &User{}
	err := row.Scan(&user.ID, &user.FirstName, &user.LastName, &user.Email, &user.Phone, &user.Password, &user.Role, &user.LastLoginAt, &user.EmailVerified)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // User not found
		}
		return nil, err
	}

	return user, nil
}

func (r *UserRepository) UpdateRefreshToken(userId, refreshToken string, ctx context.Context) (*User, error) {
	query := "UPDATE users SET refresh_token = $1 WHERE id = $2 RETURNING id, first_name, last_name, email, password, role, last_login_at"
	row := r.db.QueryRowContext(ctx, query, refreshToken, userId)

	user := &User{}
	err := row.Scan(&user.ID, &user.FirstName, &user.LastName, &user.Email, &user.Password, &user.Role, &user.LastLoginAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // User not found
		}
		return nil, err
	}

	return user, nil
}

// ListUsers returns active users sorted by name. Empty role means every role. With notEnrolled, only users
// who have never had an enrollment (in any status) are returned, newest sign-up first.
func (r *UserRepository) ListUsers(ctx context.Context, role string, notEnrolled bool, limit, offset int) ([]UserSummary, error) {
	order := "first_name, last_name, id"
	if notEnrolled {
		order = "created_at DESC, id"
	}
	query := `SELECT id, first_name, last_name, email, role, created_at, last_login_at, email_verified_at IS NOT NULL
		FROM users u
		WHERE is_active AND deleted_at IS NULL
		  AND ($1::text IS NULL OR role = $1)
		  AND (NOT $2 OR NOT EXISTS (SELECT 1 FROM enrollments e WHERE e.user_id = u.id))
		ORDER BY ` + order + `
		LIMIT $3 OFFSET $4`

	rows, err := r.db.QueryContext(ctx, query, nullIfEmpty(role), notEnrolled, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []UserSummary{}
	for rows.Next() {
		var u UserSummary
		var lastLoginAt sql.NullTime
		if err := rows.Scan(&u.ID, &u.FirstName, &u.LastName, &u.Email, &u.Role, &u.CreatedAt, &lastLoginAt, &u.EmailVerified); err != nil {
			return nil, err
		}
		if lastLoginAt.Valid {
			t := lastLoginAt.Time
			u.LastLoginAt = &t
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
