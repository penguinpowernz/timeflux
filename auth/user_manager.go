package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const (
	usersTableName        = "_timeflux_users"
	userPermissionsTable  = "_timeflux_user_permissions"
	bcryptCost            = 12
	generatedPasswordLen  = 20
)

// User represents a Timeflux user
type User struct {
	Username     string
	PasswordHash string
}

// Permission represents a user's access to a database or measurement
type Permission struct {
	Username    string
	Database    string
	Measurement string // empty string means all measurements
	CanRead     bool
	CanWrite    bool
}

// UserManager manages user authentication and authorization
type UserManager struct {
	pool *pgxpool.Pool
}

// NewUserManager creates a new UserManager
func NewUserManager(pool *pgxpool.Pool) *UserManager {
	return &UserManager{pool: pool}
}

// InitializeSchema creates the users and permissions tables if they don't exist
func (um *UserManager) InitializeSchema(ctx context.Context) error {
	// Create users table
	createUsersTable := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			username TEXT PRIMARY KEY,
			password_hash TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)
	`, pgx.Identifier{usersTableName}.Sanitize())

	if _, err := um.pool.Exec(ctx, createUsersTable); err != nil {
		return fmt.Errorf("failed to create users table: %w", err)
	}

	// Create permissions table
	createPermissionsTable := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			username TEXT NOT NULL,
			database TEXT NOT NULL,
			measurement TEXT NOT NULL DEFAULT '',
			can_read BOOLEAN NOT NULL DEFAULT false,
			can_write BOOLEAN NOT NULL DEFAULT false,
			PRIMARY KEY (username, database, measurement),
			FOREIGN KEY (username) REFERENCES %s(username) ON DELETE CASCADE
		)
	`, pgx.Identifier{userPermissionsTable}.Sanitize(), pgx.Identifier{usersTableName}.Sanitize())

	if _, err := um.pool.Exec(ctx, createPermissionsTable); err != nil {
		return fmt.Errorf("failed to create permissions table: %w", err)
	}

	log.Printf("User management schema initialized")
	return nil
}

// AddUser creates a new user with the given username and password
// If password is empty, generates a secure random password and returns it
func (um *UserManager) AddUser(ctx context.Context, username, password string) (string, error) {
	if username == "" {
		return "", fmt.Errorf("username cannot be empty")
	}

	generatedPassword := ""
	if password == "" {
		var err error
		password, err = generatePassword(generatedPasswordLen)
		if err != nil {
			return "", fmt.Errorf("failed to generate password: %w", err)
		}
		generatedPassword = password
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}

	// Insert user
	query := fmt.Sprintf(`
		INSERT INTO %s (username, password_hash)
		VALUES ($1, $2)
	`, pgx.Identifier{usersTableName}.Sanitize())

	if _, err := um.pool.Exec(ctx, query, username, string(hash)); err != nil {
		return "", fmt.Errorf("failed to create user: %w", err)
	}

	log.Printf("User created: %s", username)
	return generatedPassword, nil
}

// DeleteUser removes a user and all their permissions
func (um *UserManager) DeleteUser(ctx context.Context, username string) error {
	query := fmt.Sprintf(`
		DELETE FROM %s WHERE username = $1
	`, pgx.Identifier{usersTableName}.Sanitize())

	result, err := um.pool.Exec(ctx, query, username)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("user not found: %s", username)
	}

	log.Printf("User deleted: %s", username)
	return nil
}

// ResetPassword resets a user's password
// If newPassword is empty, generates a secure random password and returns it
func (um *UserManager) ResetPassword(ctx context.Context, username, newPassword string) (string, error) {
	generatedPassword := ""
	if newPassword == "" {
		var err error
		newPassword, err = generatePassword(generatedPasswordLen)
		if err != nil {
			return "", fmt.Errorf("failed to generate password: %w", err)
		}
		generatedPassword = newPassword
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}

	// Update user
	query := fmt.Sprintf(`
		UPDATE %s SET password_hash = $1, updated_at = NOW()
		WHERE username = $2
	`, pgx.Identifier{usersTableName}.Sanitize())

	result, err := um.pool.Exec(ctx, query, string(hash), username)
	if err != nil {
		return "", fmt.Errorf("failed to reset password: %w", err)
	}

	if result.RowsAffected() == 0 {
		return "", fmt.Errorf("user not found: %s", username)
	}

	log.Printf("Password reset for user: %s", username)
	return generatedPassword, nil
}

// GrantPermission grants a user access to a database or measurement
func (um *UserManager) GrantPermission(ctx context.Context, username, database, measurement string, canRead, canWrite bool) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (username, database, measurement, can_read, can_write)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (username, database, measurement)
		DO UPDATE SET can_read = $4, can_write = $5
	`, pgx.Identifier{userPermissionsTable}.Sanitize())

	if _, err := um.pool.Exec(ctx, query, username, database, measurement, canRead, canWrite); err != nil {
		return fmt.Errorf("failed to grant permission: %w", err)
	}

	perm := "none"
	if canRead && canWrite {
		perm = "read/write"
	} else if canRead {
		perm = "read"
	} else if canWrite {
		perm = "write"
	}

	target := fmt.Sprintf("%s.%s", database, measurement)
	if measurement == "" {
		target = database + ".*"
	}

	log.Printf("Permission granted: %s -> %s (%s)", username, target, perm)
	return nil
}

// RevokePermission removes a user's access to a database or measurement
func (um *UserManager) RevokePermission(ctx context.Context, username, database, measurement string) error {
	query := fmt.Sprintf(`
		DELETE FROM %s
		WHERE username = $1 AND database = $2 AND measurement = $3
	`, pgx.Identifier{userPermissionsTable}.Sanitize())

	result, err := um.pool.Exec(ctx, query, username, database, measurement)
	if err != nil {
		return fmt.Errorf("failed to revoke permission: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("permission not found")
	}

	target := fmt.Sprintf("%s.%s", database, measurement)
	if measurement == "" {
		target = database + ".*"
	}

	log.Printf("Permission revoked: %s -> %s", username, target)
	return nil
}

// Authenticate checks if the provided credentials are valid
// Uses constant-time comparison to prevent timing attacks for username enumeration
func (um *UserManager) Authenticate(ctx context.Context, username, password string) (bool, error) {
	query := fmt.Sprintf(`
		SELECT password_hash FROM %s WHERE username = $1
	`, pgx.Identifier{usersTableName}.Sanitize())

	var hash string
	err := um.pool.QueryRow(ctx, query, username).Scan(&hash)

	// Use a dummy hash if user doesn't exist to prevent timing attacks
	// This ensures bcrypt is always called, making timing consistent
	dummyHash := "$2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewY5GyYKKDq0.1im" // bcrypt hash of "dummy"

	if err != nil {
		if err == pgx.ErrNoRows {
			// User doesn't exist - still perform bcrypt to prevent timing attack
			bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
			return false, nil
		}
		return false, fmt.Errorf("failed to query user: %w", err)
	}

	// Compare password (always performed regardless of user existence)
	err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil, nil
}

// CheckPermission checks if a user has the requested permission for a database/measurement
// Supports wildcard permissions: checks in order of specificity
// 1. database.measurement (most specific)
// 2. database.* (all measurements in database)
// 3. *.measurement (specific measurement across all databases)
// 4. *.* (all databases, all measurements - least specific)
func (um *UserManager) CheckPermission(ctx context.Context, username, database, measurement string, needWrite bool) (bool, error) {
	// Query for permissions in order of specificity
	// Most specific permission wins
	query := fmt.Sprintf(`
		SELECT can_read, can_write FROM %s
		WHERE username = $1
		  AND (database = $2 OR database = '*')
		  AND (measurement = $3 OR measurement = '')
		ORDER BY
		  CASE WHEN database = '*' THEN 1 ELSE 0 END,
		  CASE WHEN measurement = '' THEN 1 ELSE 0 END
		LIMIT 1
	`, pgx.Identifier{userPermissionsTable}.Sanitize())

	var canRead, canWrite bool
	err := um.pool.QueryRow(ctx, query, username, database, measurement).Scan(&canRead, &canWrite)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("failed to query permission: %w", err)
	}

	if needWrite {
		return canWrite, nil
	}
	return canRead, nil
}

// ListUsers returns all users
func (um *UserManager) ListUsers(ctx context.Context) ([]string, error) {
	query := fmt.Sprintf(`
		SELECT username FROM %s ORDER BY username
	`, pgx.Identifier{usersTableName}.Sanitize())

	rows, err := um.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []string
	for rows.Next() {
		var username string
		if err := rows.Scan(&username); err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, username)
	}

	return users, rows.Err()
}

// ListUserPermissions returns all permissions for a user
func (um *UserManager) ListUserPermissions(ctx context.Context, username string) ([]Permission, error) {
	query := fmt.Sprintf(`
		SELECT database, measurement, can_read, can_write FROM %s
		WHERE username = $1
		ORDER BY database, measurement
	`, pgx.Identifier{userPermissionsTable}.Sanitize())

	rows, err := um.pool.Query(ctx, query, username)
	if err != nil {
		return nil, fmt.Errorf("failed to list permissions: %w", err)
	}
	defer rows.Close()

	var perms []Permission
	for rows.Next() {
		var p Permission
		p.Username = username
		if err := rows.Scan(&p.Database, &p.Measurement, &p.CanRead, &p.CanWrite); err != nil {
			return nil, fmt.Errorf("failed to scan permission: %w", err)
		}
		perms = append(perms, p)
	}

	return perms, rows.Err()
}

// IsAuthTableQuery checks if a query is trying to access auth tables
// This prevents users from querying the auth tables via the HTTP interface
func IsAuthTableQuery(query string) bool {
	lowerQuery := strings.ToLower(query)
	return strings.Contains(lowerQuery, usersTableName) ||
		strings.Contains(lowerQuery, userPermissionsTable)
}

// generatePassword generates a secure random password
func generatePassword(length int) (string, error) {
	// Generate random bytes
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	// Encode to base64 and trim to desired length
	password := base64.URLEncoding.EncodeToString(bytes)
	if len(password) > length {
		password = password[:length]
	}

	return password, nil
}
