package auth

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/smartystreets/goconvey/convey"
)

func TestAuthenticationMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	Convey("Given authentication middleware", t, func() {
		pool := getTestPool(t)
		if pool == nil {
			SkipSo("Skipping integration tests (set TEST_DATABASE_URL)", true, ShouldBeTrue)
			return
		}
		defer pool.Close()

		ctx := context.Background()
		um := NewUserManager(pool)
		um.InitializeSchema(ctx)

		// Create test user
		username := "testuser"
		password := "testpass123"
		um.AddUser(ctx, username, password)
		defer um.DeleteUser(ctx, username)

		Convey("When authentication is disabled", func() {
			middleware := Middleware(um, false)
			router := gin.New()
			router.Use(middleware)
			router.GET("/test", func(c *gin.Context) {
				c.String(200, "ok")
			})

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			So(w.Code, ShouldEqual, 200)
			So(w.Body.String(), ShouldEqual, "ok")
		})

		Convey("When authentication is enabled", func() {
			middleware := Middleware(um, true)
			router := gin.New()
			router.Use(middleware)
			router.GET("/test", func(c *gin.Context) {
				// Check that username was stored in context
				username, exists := c.Get("username")
				if exists {
					c.String(200, "authenticated:"+username.(string))
				} else {
					c.String(200, "ok")
				}
			})

			Convey("And no credentials provided", func() {
				req := httptest.NewRequest("GET", "/test", nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 401)
				So(w.Body.String(), ShouldContainSubstring, "authentication required")
			})

			Convey("And valid Basic Auth credentials", func() {
				req := httptest.NewRequest("GET", "/test", nil)
				req.SetBasicAuth(username, password)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 200)
				So(w.Body.String(), ShouldContainSubstring, "authenticated:testuser")
			})

			Convey("And invalid Basic Auth credentials", func() {
				req := httptest.NewRequest("GET", "/test", nil)
				req.SetBasicAuth(username, "wrongpassword")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 401)
				So(w.Body.String(), ShouldContainSubstring, "invalid credentials")
			})

			Convey("And credentials in query parameters", func() {
				req := httptest.NewRequest("GET", "/test?u="+username+"&p="+password, nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 200)
				So(w.Body.String(), ShouldContainSubstring, "authenticated:testuser")
			})

			Convey("And invalid credentials in query parameters", func() {
				req := httptest.NewRequest("GET", "/test?u="+username+"&p=wrongpassword", nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 401)
				So(w.Body.String(), ShouldContainSubstring, "invalid credentials")
			})

			Convey("And bearer token (should be rejected)", func() {
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("Authorization", "Bearer some-token")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 401)
				So(w.Body.String(), ShouldContainSubstring, "bearer token authentication not supported")
			})

			Convey("And non-existent user", func() {
				req := httptest.NewRequest("GET", "/test", nil)
				req.SetBasicAuth("nonexistent", "password")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 401)
				So(w.Body.String(), ShouldContainSubstring, "invalid credentials")
			})
		})
	})
}

func TestPermissionMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	Convey("Given permission middleware", t, func() {
		pool := getTestPool(t)
		if pool == nil {
			SkipSo("Skipping integration tests (set TEST_DATABASE_URL)", true, ShouldBeTrue)
			return
		}
		defer pool.Close()

		ctx := context.Background()
		um := NewUserManager(pool)
		um.InitializeSchema(ctx)

		// Create test users
		username1 := "readuser"
		password1 := "pass123"
		um.AddUser(ctx, username1, password1)
		defer um.DeleteUser(ctx, username1)

		username2 := "writeuser"
		password2 := "pass456"
		um.AddUser(ctx, username2, password2)
		defer um.DeleteUser(ctx, username2)

		// Grant permissions
		um.GrantPermission(ctx, username1, "testdb", "", true, false)  // read only
		um.GrantPermission(ctx, username2, "testdb", "", false, true)  // write only
		um.GrantPermission(ctx, username1, "otherdb", "", true, true) // read/write on different db

		Convey("When permission check is disabled", func() {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("username", username1)
				c.Next()
			})
			router.Use(RequirePermission(um, false, false))
			router.GET("/test", func(c *gin.Context) {
				c.String(200, "ok")
			})

			req := httptest.NewRequest("GET", "/test?db=testdb", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			So(w.Code, ShouldEqual, 200)
		})

		Convey("When permission check is enabled", func() {
			Convey("And user has read permission", func() {
				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Set("username", username1)
					c.Next()
				})
				router.Use(RequirePermission(um, true, false)) // require read
				router.GET("/test", func(c *gin.Context) {
					c.String(200, "ok")
				})

				req := httptest.NewRequest("GET", "/test?db=testdb", nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 200)
			})

			Convey("And user has write permission", func() {
				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Set("username", username2)
					c.Next()
				})
				router.Use(RequirePermission(um, true, true)) // require write
				router.POST("/test", func(c *gin.Context) {
					c.String(200, "ok")
				})

				req := httptest.NewRequest("POST", "/test?db=testdb", nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 200)
			})

			Convey("And user lacks read permission", func() {
				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Set("username", username2) // writeuser has no read permission
					c.Next()
				})
				router.Use(RequirePermission(um, true, false)) // require read
				router.GET("/test", func(c *gin.Context) {
					c.String(200, "ok")
				})

				req := httptest.NewRequest("GET", "/test?db=testdb", nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 403)
				So(w.Body.String(), ShouldContainSubstring, "insufficient permissions")
			})

			Convey("And user lacks write permission", func() {
				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Set("username", username1) // readuser has no write permission
					c.Next()
				})
				router.Use(RequirePermission(um, true, true)) // require write
				router.POST("/test", func(c *gin.Context) {
					c.String(200, "ok")
				})

				req := httptest.NewRequest("POST", "/test?db=testdb", nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 403)
				So(w.Body.String(), ShouldContainSubstring, "insufficient permissions")
			})

			Convey("And username not in context", func() {
				router := gin.New()
				router.Use(RequirePermission(um, true, false))
				router.GET("/test", func(c *gin.Context) {
					c.String(200, "ok")
				})

				req := httptest.NewRequest("GET", "/test?db=testdb", nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 401)
				So(w.Body.String(), ShouldContainSubstring, "authentication required")
			})

			Convey("And database parameter is missing", func() {
				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Set("username", username1)
					c.Next()
				})
				router.Use(RequirePermission(um, true, false))
				router.GET("/test", func(c *gin.Context) {
					c.String(200, "ok")
				})

				req := httptest.NewRequest("GET", "/test", nil) // no db parameter
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 400)
				So(w.Body.String(), ShouldContainSubstring, "database parameter required")
			})

			Convey("And user has permission on different database", func() {
				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Set("username", username1)
					c.Next()
				})
				router.Use(RequirePermission(um, true, true)) // require write
				router.POST("/test", func(c *gin.Context) {
					c.String(200, "ok")
				})

				// User has write permission on otherdb, not testdb
				req := httptest.NewRequest("POST", "/test?db=testdb", nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 403)

				// Should succeed on otherdb
				req2 := httptest.NewRequest("POST", "/test?db=otherdb", nil)
				w2 := httptest.NewRecorder()
				router.ServeHTTP(w2, req2)

				So(w2.Code, ShouldEqual, 200)
			})
		})
	})
}

func TestMiddlewareIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	Convey("Given combined auth and permission middleware", t, func() {
		pool := getTestPool(t)
		if pool == nil {
			SkipSo("Skipping integration tests (set TEST_DATABASE_URL)", true, ShouldBeTrue)
			return
		}
		defer pool.Close()

		ctx := context.Background()
		um := NewUserManager(pool)
		um.InitializeSchema(ctx)

		// Create test user with permissions
		username := "fulluser"
		password := "fullpass123"
		um.AddUser(ctx, username, password)
		defer um.DeleteUser(ctx, username)

		um.GrantPermission(ctx, username, "mydb", "", true, true) // read/write

		Convey("When both middlewares are applied", func() {
			router := gin.New()
			router.Use(Middleware(um, true))
			router.Use(RequirePermission(um, true, true))
			router.POST("/write", func(c *gin.Context) {
				c.String(200, "write successful")
			})

			Convey("And valid credentials with permission", func() {
				req := httptest.NewRequest("POST", "/write?db=mydb", nil)
				req.SetBasicAuth(username, password)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 200)
				So(w.Body.String(), ShouldEqual, "write successful")
			})

			Convey("And valid credentials without permission", func() {
				req := httptest.NewRequest("POST", "/write?db=unauthorized_db", nil)
				req.SetBasicAuth(username, password)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 403)
				So(w.Body.String(), ShouldContainSubstring, "insufficient permissions")
			})

			Convey("And invalid credentials", func() {
				req := httptest.NewRequest("POST", "/write?db=mydb", nil)
				req.SetBasicAuth(username, "wrongpassword")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 401)
				So(w.Body.String(), ShouldContainSubstring, "invalid credentials")
			})

			Convey("And no credentials", func() {
				req := httptest.NewRequest("POST", "/write?db=mydb", nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 401)
				So(w.Body.String(), ShouldContainSubstring, "authentication required")
			})
		})
	})
}

func TestMiddlewareWildcardPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	Convey("Given user with wildcard permissions", t, func() {
		pool := getTestPool(t)
		if pool == nil {
			SkipSo("Skipping integration tests (set TEST_DATABASE_URL)", true, ShouldBeTrue)
			return
		}
		defer pool.Close()

		ctx := context.Background()
		um := NewUserManager(pool)
		um.InitializeSchema(ctx)

		// Create admin user with wildcard permission
		username := "admin"
		password := "adminpass"
		um.AddUser(ctx, username, password)
		defer um.DeleteUser(ctx, username)

		um.GrantPermission(ctx, username, "*", "", true, true) // read/write on all databases

		Convey("When accessing different databases", func() {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("username", username)
				c.Next()
			})
			router.Use(RequirePermission(um, true, true))
			router.POST("/test", func(c *gin.Context) {
				c.String(200, "ok")
			})

			databases := []string{"db1", "db2", "production", "test", "any_database_name"}

			for _, db := range databases {
				req := httptest.NewRequest("POST", "/test?db="+db, nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				So(w.Code, ShouldEqual, 200)
			}
		})
	})
}

func TestMiddlewareConcurrentRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	Convey("Given middleware under concurrent load", t, func() {
		pool := getTestPool(t)
		if pool == nil {
			SkipSo("Skipping integration tests (set TEST_DATABASE_URL)", true, ShouldBeTrue)
			return
		}
		defer pool.Close()

		ctx := context.Background()
		um := NewUserManager(pool)
		um.InitializeSchema(ctx)

		username := "concurrentuser"
		password := "concurrentpass"
		um.AddUser(ctx, username, password)
		defer um.DeleteUser(ctx, username)
		um.GrantPermission(ctx, username, "testdb", "", true, true)

		router := gin.New()
		router.Use(Middleware(um, true))
		router.Use(RequirePermission(um, true, false))
		router.GET("/test", func(c *gin.Context) {
			c.String(200, "ok")
		})

		Convey("When multiple concurrent requests arrive", func() {
			numRequests := 50
			results := make(chan int, numRequests)

			for i := 0; i < numRequests; i++ {
				go func() {
					req := httptest.NewRequest("GET", "/test?db=testdb", nil)
					req.SetBasicAuth(username, password)
					w := httptest.NewRecorder()
					router.ServeHTTP(w, req)
					results <- w.Code
				}()
			}

			// Collect results
			successCount := 0
			for i := 0; i < numRequests; i++ {
				code := <-results
				if code == 200 {
					successCount++
				}
			}

			// All requests should succeed
			So(successCount, ShouldEqual, numRequests)
		})
	})
}

func TestMiddlewareEdgeCases(t *testing.T) {
	gin.SetMode(gin.TestMode)

	Convey("Given middleware with edge cases", t, func() {
		pool := getTestPool(t)
		if pool == nil {
			SkipSo("Skipping integration tests (set TEST_DATABASE_URL)", true, ShouldBeTrue)
			return
		}
		defer pool.Close()

		ctx := context.Background()
		um := NewUserManager(pool)
		um.InitializeSchema(ctx)

		Convey("When database name contains special characters", func() {
			username := "testuser"
			password := "testpass"
			um.AddUser(ctx, username, password)
			defer um.DeleteUser(ctx, username)

			specialDbName := "my-database_123"
			um.GrantPermission(ctx, username, specialDbName, "", true, false)

			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("username", username)
				c.Next()
			})
			router.Use(RequirePermission(um, true, false))
			router.GET("/test", func(c *gin.Context) {
				c.String(200, "ok")
			})

			req := httptest.NewRequest("GET", "/test?db="+specialDbName, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			So(w.Code, ShouldEqual, 200)
		})

		Convey("When username contains special characters", func() {
			username := "user.name@example.com"
			password := "testpass"
			um.AddUser(ctx, username, password)
			defer um.DeleteUser(ctx, username)

			um.GrantPermission(ctx, username, "testdb", "", true, false)

			router := gin.New()
			router.Use(Middleware(um, true))
			router.Use(RequirePermission(um, true, false))
			router.GET("/test", func(c *gin.Context) {
				c.String(200, "ok")
			})

			req := httptest.NewRequest("GET", "/test?db=testdb", nil)
			req.SetBasicAuth(username, password)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			So(w.Code, ShouldEqual, 200)
		})
	})
}

// Helper function
func getTestPool(t *testing.T) *pgxpool.Pool {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		return nil
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	return pool
}
