package auth

import (
	"fmt"
	"net/http"
	"strings"
)

// AuthenticationMethod represents the method used to authenticate
type AuthenticationMethod int

const (
	UserAuthentication AuthenticationMethod = iota
	BearerAuthentication
)

// Credentials holds authentication credentials from a request
type Credentials struct {
	Method   AuthenticationMethod
	Username string
	Password string
	Token    string
}

// parseToken extracts username and password from a "username:password" token
func parseToken(token string) (user, pass string, ok bool) {
	if t1, t2, ok := strings.Cut(token, ":"); ok {
		return t1, t2, ok
	}
	return
}

// ParseCredentials parses a request and returns the authentication credentials.
// The credentials may be present as URL query params, or as a Basic
// Authentication header.
// As params: http://127.0.0.1/query?u=username&p=password
// As basic auth: http://username:password@127.0.0.1
// As Bearer token in Authorization header: Bearer <JWT_TOKEN_BLOB>
// As Token in Authorization header: Token <username:password>
func ParseCredentials(r *http.Request) (*Credentials, error) {
	q := r.URL.Query()

	// Check for username and password in URL params.
	if u, p := q.Get("u"), q.Get("p"); u != "" && p != "" {
		return &Credentials{
			Method:   UserAuthentication,
			Username: u,
			Password: p,
		}, nil
	}

	// Check for the HTTP Authorization header.
	if s := r.Header.Get("Authorization"); s != "" {
		// Check for Bearer token.
		strs := strings.Split(s, " ")
		if len(strs) == 2 {
			switch strs[0] {
			case "Bearer":
				return &Credentials{
					Method: BearerAuthentication,
					Token:  strs[1],
				}, nil
			case "Token":
				if u, p, ok := parseToken(strs[1]); ok {
					return &Credentials{
						Method:   UserAuthentication,
						Username: u,
						Password: p,
					}, nil
				}
			}
		}

		// Check for basic auth.
		if u, p, ok := r.BasicAuth(); ok {
			return &Credentials{
				Method:   UserAuthentication,
				Username: u,
				Password: p,
			}, nil
		}
	}

	return nil, fmt.Errorf("unable to parse authentication credentials")
}
