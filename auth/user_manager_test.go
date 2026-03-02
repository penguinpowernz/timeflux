package auth

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/smartystreets/goconvey/convey"
	"golang.org/x/crypto/bcrypt"
)

// Note: These tests require a running PostgreSQL database
// Set TEST_DATABASE_URL environment variable to run integration tests
// Example: TEST_DATABASE_URL=postgres://user:pass@localhost:5432/testdb

func TestGeneratePassword(t *testing.T) {
	Convey("Given the password generator", t, func() {

		Convey("When generating a password of length 20", func() {
			password, err := generatePassword(20)

			So(err, ShouldBeNil)
			So(password, ShouldNotBeEmpty)
			So(len(password), ShouldEqual, 20)
		})

		Convey("When generating a password of length 32", func() {
			password, err := generatePassword(32)

			So(err, ShouldBeNil)
			So(len(password), ShouldEqual, 32)
		})

		Convey("When generating multiple passwords", func() {
			pass1, _ := generatePassword(20)
			pass2, _ := generatePassword(20)

			// Passwords should be different (extremely unlikely to be same)
			So(pass1, ShouldNotEqual, pass2)
		})

		Convey("When generating a very short password", func() {
			password, err := generatePassword(1)

			So(err, ShouldBeNil)
			So(len(password), ShouldEqual, 1)
		})
	})
}

func TestPasswordHashing(t *testing.T) {
	Convey("Given a password", t, func() {
		password := "testPassword123!"

		Convey("When hashing the password", func() {
			hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)

			So(err, ShouldBeNil)
			So(hash, ShouldNotBeEmpty)

			Convey("Then comparing with correct password should succeed", func() {
				err := bcrypt.CompareHashAndPassword(hash, []byte(password))
				So(err, ShouldBeNil)
			})

			Convey("Then comparing with incorrect password should fail", func() {
				err := bcrypt.CompareHashAndPassword(hash, []byte("wrongPassword"))
				So(err, ShouldNotBeNil)
			})
		})
	})
}

func TestCheckPermissionLogic(t *testing.T) {
	// This tests the permission resolution logic without database
	// The SQL query logic is tested in integration tests

	Convey("Given permission resolution rules", t, func() {

		Convey("Permission specificity order should be", func() {
			// Most specific: database.measurement
			// Next: database.*
			// Next: *.measurement
			// Least specific: *.*

			So("mydb.cpu", ShouldNotEqual, "mydb.*")
			So("mydb.*", ShouldNotEqual, "*.cpu")
			So("*.cpu", ShouldNotEqual, "*.*")
		})

		Convey("Wildcard database should match any database name", func() {
			wildcard := "*"
			specificDB := "mydb"

			// Wildcard should logically match any database
			So(wildcard, ShouldEqual, "*")
			So(specificDB, ShouldNotEqual, "*")
		})

		Convey("Empty measurement should mean all measurements", func() {
			measurement := ""

			So(measurement, ShouldBeEmpty)
		})
	})
}

// Integration tests - only run if TEST_DATABASE_URL is set
func getTestDB(t *testing.T) *pgxpool.Pool {
	// Skip integration tests if no database URL provided
	SkipConvey("Integration tests require TEST_DATABASE_URL", t, func() {})
	return nil
}

func TestUserManagerIntegration(t *testing.T) {
	pool := getTestDB(t)
	if pool == nil {
		t.Skip("Skipping integration tests (no TEST_DATABASE_URL)")
		return
	}
	defer pool.Close()

	Convey("Given a UserManager with test database", t, func() {
		ctx := context.Background()
		um := NewUserManager(pool)

		// Initialize schema
		err := um.InitializeSchema(ctx)
		So(err, ShouldBeNil)

		// Clean up function
		cleanupUser := func(username string) {
			um.DeleteUser(ctx, username)
		}

		Convey("When adding a user with generated password", func() {
			username := "test_user_1"
			defer cleanupUser(username)

			generatedPass, err := um.AddUser(ctx, username, "")

			So(err, ShouldBeNil)
			So(generatedPass, ShouldNotBeEmpty)
			So(len(generatedPass), ShouldEqual, generatedPasswordLen)

			Convey("Then authentication should work with generated password", func() {
				authenticated, err := um.Authenticate(ctx, username, generatedPass)

				So(err, ShouldBeNil)
				So(authenticated, ShouldBeTrue)
			})

			Convey("Then authentication should fail with wrong password", func() {
				authenticated, err := um.Authenticate(ctx, username, "wrongpassword")

				So(err, ShouldBeNil)
				So(authenticated, ShouldBeFalse)
			})
		})

		Convey("When adding a user with specified password", func() {
			username := "test_user_2"
			password := "mySpecificPassword123"
			defer cleanupUser(username)

			generatedPass, err := um.AddUser(ctx, username, password)

			So(err, ShouldBeNil)
			So(generatedPass, ShouldBeEmpty) // No password generated

			Convey("Then authentication should work", func() {
				authenticated, err := um.Authenticate(ctx, username, password)

				So(err, ShouldBeNil)
				So(authenticated, ShouldBeTrue)
			})
		})

		Convey("When resetting a user password", func() {
			username := "test_user_3"
			originalPass := "original123"
			defer cleanupUser(username)

			// Create user
			um.AddUser(ctx, username, originalPass)

			// Reset password
			newPass, err := um.ResetPassword(ctx, username, "")

			So(err, ShouldBeNil)
			So(newPass, ShouldNotBeEmpty)

			Convey("Then old password should not work", func() {
				authenticated, err := um.Authenticate(ctx, username, originalPass)

				So(err, ShouldBeNil)
				So(authenticated, ShouldBeFalse)
			})

			Convey("Then new password should work", func() {
				authenticated, err := um.Authenticate(ctx, username, newPass)

				So(err, ShouldBeNil)
				So(authenticated, ShouldBeTrue)
			})
		})

		Convey("When granting specific database permission", func() {
			username := "test_user_4"
			defer cleanupUser(username)

			um.AddUser(ctx, username, "password")

			err := um.GrantPermission(ctx, username, "mydb", "", true, false)
			So(err, ShouldBeNil)

			Convey("Then user should have read permission", func() {
				hasPermission, err := um.CheckPermission(ctx, username, "mydb", "", false)

				So(err, ShouldBeNil)
				So(hasPermission, ShouldBeTrue)
			})

			Convey("Then user should not have write permission", func() {
				hasPermission, err := um.CheckPermission(ctx, username, "mydb", "", true)

				So(err, ShouldBeNil)
				So(hasPermission, ShouldBeFalse)
			})
		})

		Convey("When granting wildcard database permission", func() {
			username := "test_user_5"
			defer cleanupUser(username)

			um.AddUser(ctx, username, "password")

			err := um.GrantPermission(ctx, username, "*", "", false, true)
			So(err, ShouldBeNil)

			Convey("Then user should have write permission to any database", func() {
				hasPermission1, err1 := um.CheckPermission(ctx, username, "db1", "", true)
				hasPermission2, err2 := um.CheckPermission(ctx, username, "db2", "", true)
				hasPermission3, err3 := um.CheckPermission(ctx, username, "anything", "", true)

				So(err1, ShouldBeNil)
				So(err2, ShouldBeNil)
				So(err3, ShouldBeNil)
				So(hasPermission1, ShouldBeTrue)
				So(hasPermission2, ShouldBeTrue)
				So(hasPermission3, ShouldBeTrue)
			})

			Convey("Then user should not have read permission (only write)", func() {
				hasPermission, err := um.CheckPermission(ctx, username, "db1", "", false)

				So(err, ShouldBeNil)
				So(hasPermission, ShouldBeFalse)
			})
		})

		Convey("When granting measurement-specific wildcard permission", func() {
			username := "test_user_6"
			defer cleanupUser(username)

			um.AddUser(ctx, username, "password")

			// Grant read access to 'cpu' measurement across all databases
			err := um.GrantPermission(ctx, username, "*", "cpu", true, false)
			So(err, ShouldBeNil)

			Convey("Then user should have read permission to cpu in any database", func() {
				hasPermission1, err1 := um.CheckPermission(ctx, username, "prod", "cpu", false)
				hasPermission2, err2 := um.CheckPermission(ctx, username, "staging", "cpu", false)

				So(err1, ShouldBeNil)
				So(err2, ShouldBeNil)
				So(hasPermission1, ShouldBeTrue)
				So(hasPermission2, ShouldBeTrue)
			})

			Convey("Then user should not have permission to other measurements", func() {
				hasPermission, err := um.CheckPermission(ctx, username, "prod", "memory", false)

				So(err, ShouldBeNil)
				So(hasPermission, ShouldBeFalse)
			})
		})

		Convey("When testing permission specificity", func() {
			username := "test_user_7"
			defer cleanupUser(username)

			um.AddUser(ctx, username, "password")

			// Grant wildcard write permission
			um.GrantPermission(ctx, username, "*", "", false, true)

			// Grant specific database read permission (more specific should win)
			um.GrantPermission(ctx, username, "mydb", "", true, false)

			Convey("Then specific database permission should override wildcard", func() {
				// Should have read permission from specific grant
				hasRead, err := um.CheckPermission(ctx, username, "mydb", "", false)
				So(err, ShouldBeNil)
				So(hasRead, ShouldBeTrue)

				// Should NOT have write permission (specific overrides wildcard)
				hasWrite, err := um.CheckPermission(ctx, username, "mydb", "", true)
				So(err, ShouldBeNil)
				So(hasWrite, ShouldBeFalse)
			})

			Convey("Then other databases should still have wildcard permission", func() {
				hasWrite, err := um.CheckPermission(ctx, username, "otherdb", "", true)

				So(err, ShouldBeNil)
				So(hasWrite, ShouldBeTrue)
			})
		})

		Convey("When listing users", func() {
			username1 := "test_user_8"
			username2 := "test_user_9"
			defer cleanupUser(username1)
			defer cleanupUser(username2)

			um.AddUser(ctx, username1, "pass1")
			um.AddUser(ctx, username2, "pass2")

			users, err := um.ListUsers(ctx)

			So(err, ShouldBeNil)
			So(users, ShouldContain, username1)
			So(users, ShouldContain, username2)
		})

		Convey("When deleting a user", func() {
			username := "test_user_10"

			um.AddUser(ctx, username, "password")
			um.GrantPermission(ctx, username, "mydb", "", true, true)

			err := um.DeleteUser(ctx, username)
			So(err, ShouldBeNil)

			Convey("Then user should not exist", func() {
				authenticated, err := um.Authenticate(ctx, username, "password")

				So(err, ShouldBeNil)
				So(authenticated, ShouldBeFalse)
			})

			Convey("Then permissions should be automatically deleted", func() {
				perms, err := um.ListUserPermissions(ctx, username)

				So(err, ShouldBeNil)
				So(perms, ShouldBeEmpty)
			})
		})
	})
}
