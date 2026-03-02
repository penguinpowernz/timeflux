 need you to implement the auth now.  The influx code for auth shows how
we can get the user and pass from the request.  However we should ignore
bearer tokens for now.

type credentials struct {
	Method   AuthenticationMethod
	Username string
	Password string
	Token    string
}

func parseToken(token string) (user, pass string, ok bool) {
	if t1, t2, ok := strings.Cut(token, ":"); ok {
		return t1, t2, ok
	}
	return
}

// parseCredentials parses a request and returns the authentication credentials.
// The credentials may be present as URL query params, or as a Basic
// Authentication header.
// As params: http://127.0.0.1/query?u=username&p=password
// As basic auth: http://username:password@127.0.0.1
// As Bearer token in Authorization header: Bearer <JWT_TOKEN_BLOB>
// As Token in Authorization header: Token <username:password>
func parseCredentials(r *http.Request) (*credentials, error) {
	q := r.URL.Query()

	// Check for username and password in URL params.
	if u, p := q.Get("u"), q.Get("p"); u != "" && p != "" {
		return &credentials{
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
				return &credentials{
					Method: BearerAuthentication,
					Token:  strs[1],
				}, nil
			case "Token":
				if u, p, ok := parseToken(strs[1]); ok {
					return &credentials{
						Method:   UserAuthentication,
						Username: u,
						Password: p,
					}, nil
				}
			}
		}

		// Check for basic auth.
		if u, p, ok := r.BasicAuth(); ok {
			return &credentials{
				Method:   UserAuthentication,
				Username: u,
				Password: p,
			}, nil
		}
	}

	return nil, fmt.Errorf("unable to parse authentication credentials")
}

Then we need to add a way to add and delete users.  I want this done via the command.
I want to be able to add username and password, as well as databases and measurements
the have access to, both read and write.  If no password is specified then generate
a safe one and display it at stdout.  There should also be the ability to reset a user
password.  There should be a way to change a users granted measurements and databases
after they are created.

store all these details in postgres. They don't need to be a timescale table. Use best
practice to store the passwords safely so they don't get hacked.  Prevent these table(s)
from ever being access by the HTTP interface, maybe by doing a word search for the 
(hopefully unique) table name or some other method you can come up with.

Be sure to log all user modificatioens in the log file.
