package auth

import (
	"net/http"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestParseCredentials(t *testing.T) {
	Convey("Given an HTTP request", t, func() {

		Convey("When credentials are in URL parameters", func() {
			req, _ := http.NewRequest("GET", "http://localhost/query?u=alice&p=password123", nil)

			creds, err := ParseCredentials(req)

			So(err, ShouldBeNil)
			So(creds, ShouldNotBeNil)
			So(creds.Method, ShouldEqual, UserAuthentication)
			So(creds.Username, ShouldEqual, "alice")
			So(creds.Password, ShouldEqual, "password123")
			So(creds.Token, ShouldBeEmpty)
		})

		Convey("When credentials are in Basic Auth header", func() {
			req, _ := http.NewRequest("GET", "http://localhost/query", nil)
			req.SetBasicAuth("bob", "secret")

			creds, err := ParseCredentials(req)

			So(err, ShouldBeNil)
			So(creds, ShouldNotBeNil)
			So(creds.Method, ShouldEqual, UserAuthentication)
			So(creds.Username, ShouldEqual, "bob")
			So(creds.Password, ShouldEqual, "secret")
		})

		Convey("When credentials are in Token header", func() {
			req, _ := http.NewRequest("GET", "http://localhost/query", nil)
			req.Header.Set("Authorization", "Token charlie:mypass")

			creds, err := ParseCredentials(req)

			So(err, ShouldBeNil)
			So(creds, ShouldNotBeNil)
			So(creds.Method, ShouldEqual, UserAuthentication)
			So(creds.Username, ShouldEqual, "charlie")
			So(creds.Password, ShouldEqual, "mypass")
		})

		Convey("When Bearer token is provided", func() {
			req, _ := http.NewRequest("GET", "http://localhost/query", nil)
			req.Header.Set("Authorization", "Bearer jwt-token-here")

			creds, err := ParseCredentials(req)

			So(err, ShouldBeNil)
			So(creds, ShouldNotBeNil)
			So(creds.Method, ShouldEqual, BearerAuthentication)
			So(creds.Token, ShouldEqual, "jwt-token-here")
		})

		Convey("When only username is provided in URL", func() {
			req, _ := http.NewRequest("GET", "http://localhost/query?u=alice", nil)

			creds, err := ParseCredentials(req)

			So(err, ShouldNotBeNil)
			So(creds, ShouldBeNil)
			So(err.Error(), ShouldContainSubstring, "unable to parse authentication credentials")
		})

		Convey("When no credentials are provided", func() {
			req, _ := http.NewRequest("GET", "http://localhost/query", nil)

			creds, err := ParseCredentials(req)

			So(err, ShouldNotBeNil)
			So(creds, ShouldBeNil)
			So(err.Error(), ShouldContainSubstring, "unable to parse authentication credentials")
		})

		Convey("When Token header has invalid format", func() {
			req, _ := http.NewRequest("GET", "http://localhost/query", nil)
			req.Header.Set("Authorization", "Token invalid-no-colon")

			creds, err := ParseCredentials(req)

			So(err, ShouldNotBeNil)
			So(creds, ShouldBeNil)
		})

		Convey("When Authorization header has unknown scheme", func() {
			req, _ := http.NewRequest("GET", "http://localhost/query", nil)
			req.Header.Set("Authorization", "Unknown some-value")

			creds, err := ParseCredentials(req)

			So(err, ShouldNotBeNil)
			So(creds, ShouldBeNil)
		})
	})
}

func TestParseToken(t *testing.T) {
	Convey("Given a token string", t, func() {

		Convey("When token is in username:password format", func() {
			user, pass, ok := parseToken("alice:password123")

			So(ok, ShouldBeTrue)
			So(user, ShouldEqual, "alice")
			So(pass, ShouldEqual, "password123")
		})

		Convey("When token has multiple colons", func() {
			user, pass, ok := parseToken("alice:pass:word")

			So(ok, ShouldBeTrue)
			So(user, ShouldEqual, "alice")
			So(pass, ShouldEqual, "pass:word")
		})

		Convey("When token has no colon", func() {
			user, pass, ok := parseToken("alicepassword")

			So(ok, ShouldBeFalse)
			So(user, ShouldBeEmpty)
			So(pass, ShouldBeEmpty)
		})

		Convey("When token is empty", func() {
			user, pass, ok := parseToken("")

			So(ok, ShouldBeFalse)
			So(user, ShouldBeEmpty)
			So(pass, ShouldBeEmpty)
		})
	})
}

func TestIsAuthTableQuery(t *testing.T) {
	Convey("Given a SQL query string", t, func() {

		Convey("When query targets _timeflux_users", func() {
			query := "SELECT * FROM _timeflux_users"

			result := IsAuthTableQuery(query)

			So(result, ShouldBeTrue)
		})

		Convey("When query targets _timeflux_user_permissions", func() {
			query := "SELECT * FROM _timeflux_user_permissions WHERE username = 'alice'"

			result := IsAuthTableQuery(query)

			So(result, ShouldBeTrue)
		})

		Convey("When query mentions auth table in uppercase", func() {
			query := "SELECT * FROM _TIMEFLUX_USERS"

			result := IsAuthTableQuery(query)

			So(result, ShouldBeTrue)
		})

		Convey("When query mentions auth table in mixed case", func() {
			query := "SELECT * FROM _TimeFluX_Users"

			result := IsAuthTableQuery(query)

			So(result, ShouldBeTrue)
		})

		Convey("When query targets regular table", func() {
			query := "SELECT * FROM cpu WHERE time > now() - 1h"

			result := IsAuthTableQuery(query)

			So(result, ShouldBeFalse)
		})

		Convey("When query is SHOW DATABASES", func() {
			query := "SHOW DATABASES"

			result := IsAuthTableQuery(query)

			So(result, ShouldBeFalse)
		})

		Convey("When query has auth table name in comment", func() {
			query := "SELECT * FROM cpu -- not from _timeflux_users"

			result := IsAuthTableQuery(query)

			So(result, ShouldBeTrue) // Still blocks for safety
		})
	})
}
