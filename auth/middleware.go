package auth

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// validateDatabaseIdentifier validates that a database name is safe to use as a SQL identifier.
// Allows alphanumeric characters and underscores; must start with a letter or underscore.
func validateDatabaseIdentifier(name string) error {
	if name == "" {
		return fmt.Errorf("identifier cannot be empty")
	}
	if len(name) > 63 {
		return fmt.Errorf("identifier too long (max 63 characters)")
	}
	first := name[0]
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || first == '_') {
		return fmt.Errorf("identifier must start with a letter or underscore")
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return fmt.Errorf("identifier contains invalid character: %c", c)
		}
	}
	return nil
}

// Middleware creates a Gin middleware that enforces authentication
func Middleware(userManager *UserManager, enabled bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		// If authentication is disabled, allow all requests
		if !enabled {
			c.Next()
			return
		}

		// Parse credentials from request
		creds, err := ParseCredentials(c.Request)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "authentication required",
			})
			c.Abort()
			return
		}

		// Skip bearer tokens for now (as requested)
		if creds.Method == BearerAuthentication {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "bearer token authentication not supported",
			})
			c.Abort()
			return
		}

		// Authenticate user
		authenticated, err := userManager.Authenticate(c.Request.Context(), creds.Username, creds.Password)
		if err != nil {
			// Note: err from Authenticate() never contains password data - only DB errors
			log.Printf("Authentication error for user %s: %v", creds.Username, err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "authentication failed",
			})
			c.Abort()
			return
		}

		if !authenticated {
			// Log failed attempts for security monitoring (username only, never password)
			log.Printf("Failed authentication attempt for user: %s", creds.Username)
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "invalid credentials",
			})
			c.Abort()
			return
		}

		// Store username in context for later use
		c.Set("username", creds.Username)
		c.Next()
	}
}

// RequirePermission creates middleware that checks if the user has the required permission
func RequirePermission(userManager *UserManager, enabled bool, needWrite bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		// If authentication is disabled, allow all requests
		if !enabled {
			c.Next()
			return
		}

		// Get username from context (set by auth middleware)
		username, exists := c.Get("username")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "authentication required",
			})
			c.Abort()
			return
		}

		// Get database from query parameter
		database := c.Query("db")
		if database == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "database parameter required",
			})
			c.Abort()
			return
		}

		// Validate database name to prevent injection via query parameter
		if err := validateDatabaseIdentifier(database); err != nil {
			log.Printf("Invalid database parameter from user %s: %v", username, err)
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid database name",
			})
			c.Abort()
			return
		}

		// For now, check database-level permission (measurement is empty)
		// TODO: Extract measurement from query/write data for finer-grained permissions
		hasPermission, err := userManager.CheckPermission(c.Request.Context(), username.(string), database, "", needWrite)
		if err != nil {
			log.Printf("Permission check error for user %s: %v", username, err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "permission check failed",
			})
			c.Abort()
			return
		}

		if !hasPermission {
			action := "read"
			if needWrite {
				action = "write"
			}
			log.Printf("Permission denied: user %s attempted %s on database %s", username, action, database)
			c.JSON(http.StatusForbidden, gin.H{
				"error": "insufficient permissions",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
