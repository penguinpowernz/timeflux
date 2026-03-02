package usercli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/penguinpowernz/timeflux/auth"
	"github.com/penguinpowernz/timeflux/config"
)

// UserCommand handles user management CLI commands
type UserCommand struct {
	cfg         *config.Config
	userManager *auth.UserManager
}

// NewUserCommand creates a new UserCommand
func NewUserCommand(cfg *config.Config) *UserCommand {
	return &UserCommand{cfg: cfg}
}

// connect establishes database connection for user operations
func (uc *UserCommand) connect(ctx context.Context) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, uc.cfg.Database.ConnectionString())
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	uc.userManager = auth.NewUserManager(pool)

	// Initialize schema
	if err := uc.userManager.InitializeSchema(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to initialize user schema: %w", err)
	}

	return pool, nil
}

// AddUser adds a new user
func (uc *UserCommand) AddUser(username, password string) error {
	ctx := context.Background()
	pool, err := uc.connect(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	generatedPassword, err := uc.userManager.AddUser(ctx, username, password)
	if err != nil {
		return err
	}

	if generatedPassword != "" {
		fmt.Printf("User '%s' created with generated password: %s\n", username, generatedPassword)
		fmt.Println("Please save this password securely - it will not be displayed again.")
	} else {
		fmt.Printf("User '%s' created successfully.\n", username)
	}

	return nil
}

// DeleteUser deletes a user
func (uc *UserCommand) DeleteUser(username string) error {
	ctx := context.Background()
	pool, err := uc.connect(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := uc.userManager.DeleteUser(ctx, username); err != nil {
		return err
	}

	fmt.Printf("User '%s' deleted successfully.\n", username)
	return nil
}

// ResetPassword resets a user's password
func (uc *UserCommand) ResetPassword(username, newPassword string) error {
	ctx := context.Background()
	pool, err := uc.connect(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	generatedPassword, err := uc.userManager.ResetPassword(ctx, username, newPassword)
	if err != nil {
		return err
	}

	if generatedPassword != "" {
		fmt.Printf("Password reset for user '%s': %s\n", username, generatedPassword)
		fmt.Println("Please save this password securely - it will not be displayed again.")
	} else {
		fmt.Printf("Password reset for user '%s' successfully.\n", username)
	}

	return nil
}

// GrantPermission grants a user permission to a database or measurement
func (uc *UserCommand) GrantPermission(username, database, measurement string, canRead, canWrite bool) error {
	ctx := context.Background()
	pool, err := uc.connect(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := uc.userManager.GrantPermission(ctx, username, database, measurement, canRead, canWrite); err != nil {
		return err
	}

	perm := "none"
	if canRead && canWrite {
		perm = "read/write"
	} else if canRead {
		perm = "read"
	} else if canWrite {
		perm = "write"
	}

	// Format target for display
	var target string
	if database == "*" && measurement == "" {
		target = "*.*"
	} else if database == "*" {
		target = fmt.Sprintf("*.%s", measurement)
	} else if measurement == "" {
		target = database + ".*"
	} else {
		target = fmt.Sprintf("%s.%s", database, measurement)
	}

	fmt.Printf("Permission granted: %s -> %s (%s)\n", username, target, perm)
	return nil
}

// RevokePermission revokes a user's permission
func (uc *UserCommand) RevokePermission(username, database, measurement string) error {
	ctx := context.Background()
	pool, err := uc.connect(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := uc.userManager.RevokePermission(ctx, username, database, measurement); err != nil {
		return err
	}

	// Format target for display
	var target string
	if database == "*" && measurement == "" {
		target = "*.*"
	} else if database == "*" {
		target = fmt.Sprintf("*.%s", measurement)
	} else if measurement == "" {
		target = database + ".*"
	} else {
		target = fmt.Sprintf("%s.%s", database, measurement)
	}

	fmt.Printf("Permission revoked: %s -> %s\n", username, target)
	return nil
}

// ListUsers lists all users
func (uc *UserCommand) ListUsers() error {
	ctx := context.Background()
	pool, err := uc.connect(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	users, err := uc.userManager.ListUsers(ctx)
	if err != nil {
		return err
	}

	if len(users) == 0 {
		fmt.Println("No users found.")
		return nil
	}

	fmt.Println("Users:")
	for _, user := range users {
		fmt.Printf("  - %s\n", user)
	}

	return nil
}

// ShowUser shows details about a user including their permissions
func (uc *UserCommand) ShowUser(username string) error {
	ctx := context.Background()
	pool, err := uc.connect(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	perms, err := uc.userManager.ListUserPermissions(ctx, username)
	if err != nil {
		return err
	}

	fmt.Printf("User: %s\n", username)
	if len(perms) == 0 {
		fmt.Println("  No permissions granted.")
		return nil
	}

	fmt.Println("Permissions:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  Database\tMeasurement\tRead\tWrite")
	fmt.Fprintln(w, "  --------\t-----------\t----\t-----")

	for _, p := range perms {
		measurement := p.Measurement
		if measurement == "" {
			measurement = "*"
		}
		fmt.Fprintf(w, "  %s\t%s\t%v\t%v\n", p.Database, measurement, p.CanRead, p.CanWrite)
	}
	w.Flush()

	return nil
}

// ParsePermission parses a permission string like "database.measurement:rw" or "database:r"
// Supports wildcard: "*:rw" for all databases, "*.cpu:r" for cpu measurement across all databases
func ParsePermission(perm string) (database, measurement string, canRead, canWrite bool, err error) {
	// Split by :
	parts := strings.Split(perm, ":")
	if len(parts) != 2 {
		return "", "", false, false, fmt.Errorf("invalid permission format, expected 'database[.measurement]:r|w|rw' or '*[.measurement]:r|w|rw'")
	}

	target := parts[0]
	access := parts[1]

	// Parse target (database.measurement or database or *.measurement or *)
	if strings.Contains(target, ".") {
		targetParts := strings.SplitN(target, ".", 2)
		database = targetParts[0]
		measurement = targetParts[1]
		if measurement == "*" {
			measurement = ""
		}
	} else {
		database = target
		measurement = ""
	}

	// Parse access
	access = strings.ToLower(access)
	switch access {
	case "r", "read":
		canRead = true
	case "w", "write":
		canWrite = true
	case "rw", "wr", "readwrite", "read-write":
		canRead = true
		canWrite = true
	default:
		return "", "", false, false, fmt.Errorf("invalid access mode, expected 'r', 'w', or 'rw'")
	}

	if database == "" {
		return "", "", false, false, fmt.Errorf("database cannot be empty")
	}

	// Wildcard is valid
	// database can be "*" for all databases
	// measurement can be "" for all measurements (already handled by * -> "")

	return database, measurement, canRead, canWrite, nil
}
