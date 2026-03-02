# Timeflux Testing Guide

## Overview

Timeflux uses [GoConvey](https://github.com/smartystreets/goconvey) for behavior-driven development (BDD) style testing. Tests are organized by package and cover authentication, authorization, and permission parsing functionality.

## Test Packages

### Auth Package (`auth/`)

**Test File:** `auth/auth_test.go`

Tests credential parsing and authentication table protection:
- ✅ URL parameter authentication (`?u=user&p=pass`)
- ✅ Basic Auth header parsing
- ✅ Token header parsing (`Token user:pass`)
- ✅ Bearer token detection (not supported, properly rejected)
- ✅ Invalid credential format handling
- ✅ Auth table query detection (`_timeflux_users`, `_timeflux_user_permissions`)

**Test File:** `auth/user_manager_test.go`

Tests user management and permission logic:
- ✅ Password generation (secure random 20-char passwords)
- ✅ BCrypt password hashing (cost factor 12)
- ✅ User CRUD operations (create, delete, reset password)
- ✅ Permission granting/revoking
- ✅ Wildcard database permissions (`*:rw`)
- ✅ Wildcard measurement permissions (`*.cpu:r`)
- ✅ Permission specificity resolution
- ⚠️ Integration tests (require TEST_DATABASE_URL)

### User CLI Package (`usercli/`)

**Test File:** `usercli/user_test.go`

Tests command-line permission parsing:
- ✅ Database-level permissions (`mydb:r`, `mydb:w`, `mydb:rw`)
- ✅ Measurement-level permissions (`mydb.cpu:rw`)
- ✅ Wildcard database (`*:rw`, `*.cpu:r`)
- ✅ Wildcard measurement (`mydb.*:r`)
- ✅ Alternative formats (`read`, `write`, `readwrite`, `wr`)
- ✅ Complex names with dashes, underscores, dots
- ✅ Invalid format detection and error handling

## Running Tests

### Run All Tests

```bash
go test ./...
```

### Run Specific Package

```bash
# Auth package
go test ./auth -v

# User CLI package
go test ./usercli -v
```

### Run with Coverage

```bash
go test ./auth ./usercli -cover
```

### Run Integration Tests

Integration tests require a PostgreSQL database:

```bash
export TEST_DATABASE_URL="postgres://user:pass@localhost:5432/testdb"
go test ./auth -v
```

**Note:** Integration tests are automatically skipped if `TEST_DATABASE_URL` is not set.

## Test Results Summary

### Latest Test Run

```
Package: auth
- TestParseCredentials: PASS (30 assertions)
- TestParseToken: PASS (12 assertions)
- TestIsAuthTableQuery: PASS (7 assertions)
- TestGeneratePassword: PASS (8 assertions)
- TestPasswordHashing: PASS (6 assertions)
- TestCheckPermissionLogic: PASS (6 assertions)
- TestUserManagerIntegration: SKIP (requires database)
Total: 69 assertions, 1 skipped

Package: usercli
- TestParsePermission: PASS (76 assertions)
Total: 76 assertions

Overall: 145 assertions, all passing ✅
```

## Writing New Tests

### GoConvey Test Structure

```go
func TestFeature(t *testing.T) {
    Convey("Given [setup context]", t, func() {

        Convey("When [action occurs]", func() {
            // Perform action
            result := doSomething()

            // Assert expectations
            So(result, ShouldEqual, expected)
        })
    })
}
```

### Common Assertions

```go
So(actual, ShouldEqual, expected)
So(actual, ShouldNotEqual, unexpected)
So(actual, ShouldBeNil)
So(actual, ShouldNotBeNil)
So(actual, ShouldBeTrue)
So(actual, ShouldBeFalse)
So(actual, ShouldBeEmpty)
So(actual, ShouldNotBeEmpty)
So(actual, ShouldContain, substring)
So(actual, ShouldContainSubstring, substring)
```

### Testing Guidelines

1. **Use descriptive test names**
   - Given/When/Then structure
   - Clear, specific scenarios

2. **Test edge cases**
   - Empty strings
   - Invalid formats
   - Boundary conditions

3. **Keep tests independent**
   - No shared state between tests
   - Use setup/teardown for database tests

4. **Mock external dependencies**
   - Database interactions in integration tests only
   - Unit tests should be fast (<1ms)

## Continuous Integration

### Pre-commit Checks

Run tests before committing:

```bash
go test ./auth ./usercli -v
go build -o timeflux
```

### CI Pipeline (Recommended)

```yaml
# .github/workflows/test.yml
name: Test
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: timescale/timescaledb:latest-pg15
        env:
          POSTGRES_PASSWORD: postgres
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      - run: go test ./... -v
        env:
          TEST_DATABASE_URL: postgres://postgres:postgres@localhost:5432/postgres
```

## Test Coverage

To view test coverage:

```bash
go test ./auth ./usercli -coverprofile=coverage.out
go tool cover -html=coverage.out
```

**Current Coverage:**
- `auth/auth.go`: ~95% (credential parsing fully covered)
- `auth/user_manager.go`: ~60% (unit tests only, integration tests skipped)
- `usercli/user.go`: ~85% (ParsePermission fully covered)

## Known Issues

### Integration Tests Skip

Integration tests in `auth/user_manager_test.go` are skipped by default. To run:

```bash
# Start TimescaleDB
docker run -d --name timescale \
  -p 5432:5432 \
  -e POSTGRES_PASSWORD=postgres \
  timescale/timescaledb:latest-pg15

# Run tests
export TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/postgres"
go test ./auth -v
```

### Vendor Directory

If tests fail with vendoring errors:

```bash
go mod vendor
go test ./...
```

## Debugging Tests

### Verbose Output

```bash
go test ./auth -v
```

### Run Specific Test

```bash
go test ./auth -run TestParseCredentials -v
```

### Print Test Coverage

```bash
go test ./auth -cover -coverprofile=coverage.out
go tool cover -func=coverage.out
```

## Future Test Improvements

- [ ] Add middleware tests (auth enforcement)
- [ ] Add query handler tests (auth table blocking)
- [ ] Add benchmark tests for password hashing
- [ ] Increase integration test coverage
- [ ] Add mutation testing
- [ ] Add fuzz testing for permission parsing

## Resources

- [GoConvey Documentation](https://github.com/smartystreets/goconvey)
- [Go Testing Package](https://golang.org/pkg/testing/)
- [Table-Driven Tests in Go](https://dave.cheney.net/2019/05/07/prefer-table-driven-tests)
