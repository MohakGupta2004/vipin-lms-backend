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
	ID          string    `json:"id"`
	FirstName   string    `json:"firstName"`
	LastName    string    `json:"lastName"`
	Email       string    `json:"email"`
	Password    string    `json:"-"`
	Role        Role      `json:"role"`
	LastLoginAt time.Time `json:"lastLoginAt"`
}

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{
		db: db,
	}
}

func (r *UserRepository) CreateUser(firstName, lastName, email, password string, ctx context.Context) (*User, error) {
	user := &User{
		FirstName:   firstName,
		LastName:    lastName,
		Email:       email,
		Password:    password,
		LastLoginAt: time.Now(),
		Role:        RoleStudent,
	}

	query := "INSERT INTO users (first_name, last_name, email, password, role, last_login_at) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id"
	row := r.db.QueryRowContext(ctx, query, user.FirstName, user.LastName, user.Email, user.Password, user.Role, user.LastLoginAt).Scan(&user.ID)

	return user, row
}

func (r *UserRepository) GetUserByEmail(email string, ctx context.Context) (*User, error) {
	query := "SELECT id, first_name, last_name, email, password, role, last_login_at FROM users WHERE email = $1"
	row := r.db.QueryRowContext(ctx, query, email)

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
func (r *UserRepository) GetUserById(id string, ctx context.Context) (*User, error) {
	query := "SELECT id, first_name, last_name, email, password, role, last_login_at FROM users WHERE id = $1"
	row := r.db.QueryRowContext(ctx, query, id)

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
