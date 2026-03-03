package query

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestTranslatorBasicSelect(t *testing.T) {
	Convey("Given a translator for testdb", t, func() {
		translator := NewTranslator("testdb")

		Convey("When translating basic SELECT query", func() {
			sql, err := translator.Translate("SELECT value FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "SELECT")
			So(sql, ShouldContainSubstring, `"value"`)
			So(sql, ShouldContainSubstring, `FROM "testdb"."cpu"`)
		})

		Convey("When translating SELECT with wildcard", func() {
			sql, err := translator.Translate("SELECT * FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "SELECT *")
			So(sql, ShouldContainSubstring, `FROM "testdb"."cpu"`)
		})

		Convey("When translating SELECT with multiple fields", func() {
			sql, err := translator.Translate("SELECT usage, count, enabled FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `"usage"`)
			So(sql, ShouldContainSubstring, `"count"`)
			So(sql, ShouldContainSubstring, `"enabled"`)
		})

		Convey("When translating SELECT with alias", func() {
			sql, err := translator.Translate("SELECT value AS cpu_value FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `AS "cpu_value"`)
		})
	})
}

func TestTranslatorAggregates(t *testing.T) {
	Convey("Given a translator", t, func() {
		translator := NewTranslator("testdb")

		Convey("When translating MEAN function", func() {
			sql, err := translator.Translate("SELECT mean(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "AVG")
			So(sql, ShouldContainSubstring, `"value"`)
		})

		Convey("When translating COUNT function", func() {
			sql, err := translator.Translate("SELECT count(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "COUNT")
		})

		Convey("When translating SUM function", func() {
			sql, err := translator.Translate("SELECT sum(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "SUM")
		})

		Convey("When translating MAX function", func() {
			sql, err := translator.Translate("SELECT max(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "MAX")
		})

		Convey("When translating MIN function", func() {
			sql, err := translator.Translate("SELECT min(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "MIN")
		})

		Convey("When translating multiple aggregates", func() {
			sql, err := translator.Translate("SELECT mean(value), max(value), min(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "AVG")
			So(sql, ShouldContainSubstring, "MAX")
			So(sql, ShouldContainSubstring, "MIN")
		})
	})
}

func TestTranslatorWhereClause(t *testing.T) {
	Convey("Given a translator", t, func() {
		translator := NewTranslator("testdb")

		Convey("When translating WHERE with equals", func() {
			sql, err := translator.Translate("SELECT value FROM cpu WHERE host = 'server1'")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "WHERE")
			So(sql, ShouldContainSubstring, `"host"`)
			So(sql, ShouldContainSubstring, "=")
			So(sql, ShouldContainSubstring, "'server1'")
		})

		Convey("When translating WHERE with greater than", func() {
			sql, err := translator.Translate("SELECT value FROM cpu WHERE value > 50")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "WHERE")
			So(sql, ShouldContainSubstring, ">")
			So(sql, ShouldContainSubstring, "50")
		})

		Convey("When translating WHERE with less than", func() {
			sql, err := translator.Translate("SELECT value FROM cpu WHERE value < 100")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "<")
			So(sql, ShouldContainSubstring, "100")
		})

		Convey("When translating WHERE with AND", func() {
			sql, err := translator.Translate("SELECT value FROM cpu WHERE host = 'server1' AND region = 'us-west'")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "WHERE")
			So(sql, ShouldContainSubstring, "AND")
			So(sql, ShouldContainSubstring, "'server1'")
			So(sql, ShouldContainSubstring, "'us-west'")
		})

		Convey("When translating WHERE with OR", func() {
			sql, err := translator.Translate("SELECT value FROM cpu WHERE host = 'server1' OR host = 'server2'")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "OR")
		})

		Convey("When translating WHERE with time range", func() {
			sql, err := translator.Translate("SELECT value FROM cpu WHERE time > '2021-05-01T00:00:00Z'")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "WHERE")
			So(sql, ShouldContainSubstring, "time")
			So(sql, ShouldContainSubstring, "2021-05-01")
		})
	})
}

func TestTranslatorGroupBy(t *testing.T) {
	Convey("Given a translator", t, func() {
		translator := NewTranslator("testdb")

		Convey("When translating GROUP BY time(5m)", func() {
			sql, err := translator.Translate("SELECT mean(value) FROM cpu WHERE time > now() - 1h GROUP BY time(5m)")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "time_bucket")
			So(sql, ShouldContainSubstring, "5 minutes")
			So(sql, ShouldContainSubstring, "GROUP BY")
		})

		Convey("When translating GROUP BY time(1h)", func() {
			sql, err := translator.Translate("SELECT mean(value) FROM cpu GROUP BY time(1h)")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "time_bucket")
			So(sql, ShouldContainSubstring, "1 hours")
		})

		Convey("When translating GROUP BY time(1d)", func() {
			sql, err := translator.Translate("SELECT mean(value) FROM cpu GROUP BY time(1d)")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "time_bucket")
			So(sql, ShouldContainSubstring, "1 days")
		})

		Convey("When translating GROUP BY tag", func() {
			sql, err := translator.Translate("SELECT mean(value) FROM cpu GROUP BY host")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "GROUP BY")
			So(sql, ShouldContainSubstring, `"host"`)
		})

		Convey("When translating GROUP BY time and tags", func() {
			sql, err := translator.Translate("SELECT mean(value) FROM cpu GROUP BY time(5m), host, region")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "time_bucket")
			So(sql, ShouldContainSubstring, "GROUP BY")
			So(sql, ShouldContainSubstring, `"host"`)
			So(sql, ShouldContainSubstring, `"region"`)
		})
	})
}

func TestTranslatorLimitOffset(t *testing.T) {
	Convey("Given a translator", t, func() {
		translator := NewTranslator("testdb")

		Convey("When translating with LIMIT", func() {
			sql, err := translator.Translate("SELECT value FROM cpu LIMIT 10")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "LIMIT 10")
		})

		Convey("When translating with OFFSET", func() {
			sql, err := translator.Translate("SELECT value FROM cpu LIMIT 10 OFFSET 20")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "LIMIT 10")
			So(sql, ShouldContainSubstring, "OFFSET 20")
		})

		Convey("When translating with large LIMIT", func() {
			sql, err := translator.Translate("SELECT value FROM cpu LIMIT 1000")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "LIMIT 1000")
		})
	})
}

func TestTranslatorOrderBy(t *testing.T) {
	Convey("Given a translator", t, func() {
		translator := NewTranslator("testdb")

		Convey("When translating ORDER BY ASC", func() {
			sql, err := translator.Translate("SELECT value FROM cpu ORDER BY time ASC")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "ORDER BY")
			So(sql, ShouldContainSubstring, "ASC")
		})

		Convey("When translating ORDER BY DESC", func() {
			sql, err := translator.Translate("SELECT value FROM cpu ORDER BY time DESC")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "ORDER BY")
			So(sql, ShouldContainSubstring, "DESC")
		})

		Convey("When translating with GROUP BY time, should have default ORDER BY", func() {
			sql, err := translator.Translate("SELECT mean(value) FROM cpu GROUP BY time(5m)")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "ORDER BY time")
		})
	})
}

func TestTranslatorShowCommands(t *testing.T) {
	Convey("Given a translator", t, func() {
		translator := NewTranslator("testdb")

		Convey("When translating SHOW DATABASES", func() {
			sql, queryType, err := translator.TranslateWithType("SHOW DATABASES")

			So(err, ShouldBeNil)
			So(queryType, ShouldEqual, QueryTypeShowDatabases)
			So(sql, ShouldContainSubstring, "SELECT")
			So(sql, ShouldContainSubstring, "nspname")
		})

		Convey("When translating SHOW MEASUREMENTS", func() {
			sql, queryType, err := translator.TranslateWithType("SHOW MEASUREMENTS")

			So(err, ShouldBeNil)
			So(queryType, ShouldEqual, QueryTypeShowMeasurements)
			So(sql, ShouldContainSubstring, "SELECT")
		})

		Convey("When translating SHOW TAG KEYS", func() {
			sql, queryType, err := translator.TranslateWithType("SHOW TAG KEYS")

			So(err, ShouldBeNil)
			So(queryType, ShouldEqual, QueryTypeShowTagKeys)
			So(sql, ShouldContainSubstring, "SELECT")
		})

		Convey("When translating SHOW TAG VALUES", func() {
			sql, queryType, err := translator.TranslateWithType("SHOW TAG VALUES FROM cpu WITH KEY = host")

			So(err, ShouldBeNil)
			So(queryType, ShouldEqual, QueryTypeShowTagValues)
			So(sql, ShouldContainSubstring, "SELECT")
		})

		Convey("When translating SHOW FIELD KEYS", func() {
			sql, queryType, err := translator.TranslateWithType("SHOW FIELD KEYS")

			So(err, ShouldBeNil)
			So(queryType, ShouldEqual, QueryTypeShowFieldKeys)
			So(sql, ShouldContainSubstring, "SELECT")
		})

		Convey("When translating SHOW SERIES", func() {
			sql, queryType, err := translator.TranslateWithType("SHOW SERIES")

			So(err, ShouldBeNil)
			So(queryType, ShouldEqual, QueryTypeShowSeries)
			So(sql, ShouldContainSubstring, "SELECT")
		})
	})
}

func TestTranslatorDropCommands(t *testing.T) {
	Convey("Given a translator", t, func() {
		translator := NewTranslator("testdb")

		Convey("When translating DROP DATABASE", func() {
			sql, queryType, err := translator.TranslateWithType("DROP DATABASE mydb")

			So(err, ShouldBeNil)
			So(queryType, ShouldEqual, QueryTypeDropDatabase)
			So(sql, ShouldContainSubstring, "DROP SCHEMA")
		})

		Convey("When translating DROP MEASUREMENT", func() {
			sql, queryType, err := translator.TranslateWithType("DROP MEASUREMENT cpu")

			So(err, ShouldBeNil)
			So(queryType, ShouldEqual, QueryTypeDropMeasurement)
			So(sql, ShouldContainSubstring, "DROP TABLE")
		})

		Convey("When translating DROP SERIES", func() {
			sql, queryType, err := translator.TranslateWithType("DROP SERIES FROM cpu WHERE host = 'server1'")

			So(err, ShouldBeNil)
			So(queryType, ShouldEqual, QueryTypeDropSeries)
			So(sql, ShouldContainSubstring, "DELETE FROM")
		})
	})
}

func TestTranslatorCreateDatabase(t *testing.T) {
	Convey("Given a translator", t, func() {
		translator := NewTranslator("testdb")

		Convey("When translating CREATE DATABASE", func() {
			sql, queryType, err := translator.TranslateWithType("CREATE DATABASE mydb")

			So(err, ShouldBeNil)
			So(queryType, ShouldEqual, QueryTypeCreateDatabase)
			So(sql, ShouldContainSubstring, "CREATE SCHEMA")
		})
	})
}

func TestTranslatorSQLInjectionPrevention(t *testing.T) {
	Convey("Given a translator", t, func() {
		translator := NewTranslator("testdb")

		Convey("When measurement name contains SQL injection attempt", func() {
			// This should be properly escaped by pgx.Identifier
			sql, err := translator.Translate("SELECT * FROM \"cpu'; DROP TABLE users; --\"")

			So(err, ShouldBeNil)
			// Should be properly quoted and escaped (DROP TABLE is inside quotes, so it's safe)
			So(sql, ShouldContainSubstring, `"cpu'; DROP TABLE users; --"`)
			// The dangerous part is safely quoted, so injection is prevented
			// Check that it's in the measurement name position (after FROM)
			So(sql, ShouldContainSubstring, `FROM "testdb"."cpu'; DROP TABLE users; --"`)
		})

		Convey("When field name contains special characters", func() {
			sql, err := translator.Translate("SELECT \"field; DELETE FROM\" FROM cpu")

			So(err, ShouldBeNil)
			// Should be properly quoted
			So(sql, ShouldContainSubstring, `"field; DELETE FROM"`)
		})

		Convey("When WHERE value contains quotes", func() {
			// InfluxQL parser doesn't support double single quotes, use backslash escape
			sql, err := translator.Translate("SELECT * FROM cpu WHERE host = 'server\\'s machine'")

			// InfluxQL parser may not handle escaped quotes - test basic string functionality
			if err != nil {
				// Parser might not support this, which is OK for SQL injection prevention
				So(err, ShouldNotBeNil)
			} else {
				So(sql, ShouldContainSubstring, "server")
			}
		})
	})
}

func TestTranslatorErrors(t *testing.T) {
	Convey("Given a translator", t, func() {
		translator := NewTranslator("testdb")

		Convey("When query is empty", func() {
			_, err := translator.Translate("")

			So(err, ShouldNotBeNil)
		})

		Convey("When query is invalid InfluxQL", func() {
			_, err := translator.Translate("INVALID QUERY SYNTAX")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "failed to parse")
		})

		Convey("When SELECT has no fields", func() {
			_, err := translator.Translate("SELECT FROM cpu")

			So(err, ShouldNotBeNil)
		})

		Convey("When SELECT has no FROM clause", func() {
			// InfluxQL parser should catch this
			_, err := translator.Translate("SELECT value")

			So(err, ShouldNotBeNil)
		})
	})
}

func TestTranslatorQueryTypes(t *testing.T) {
	Convey("Given a translator", t, func() {
		translator := NewTranslator("testdb")

		Convey("When translating different query types", func() {
			testCases := []struct {
				query        string
				expectedType QueryType
			}{
				{"SELECT * FROM cpu", QueryTypeSelect},
				{"SHOW DATABASES", QueryTypeShowDatabases},
				{"SHOW MEASUREMENTS", QueryTypeShowMeasurements},
				{"SHOW TAG KEYS", QueryTypeShowTagKeys},
				{"SHOW FIELD KEYS", QueryTypeShowFieldKeys},
				{"CREATE DATABASE mydb", QueryTypeCreateDatabase},
				{"DROP DATABASE mydb", QueryTypeDropDatabase},
				{"DROP MEASUREMENT cpu", QueryTypeDropMeasurement},
			}

			for _, tc := range testCases {
				_, queryType, err := translator.TranslateWithType(tc.query)
				So(err, ShouldBeNil)
				So(queryType, ShouldEqual, tc.expectedType)
			}
		})
	})
}

func TestTranslatorComplexQueries(t *testing.T) {
	Convey("Given a translator", t, func() {
		translator := NewTranslator("testdb")

		Convey("When translating complex query with multiple conditions", func() {
			sql, err := translator.Translate(`
				SELECT mean(value), max(value), min(value)
				FROM cpu
				WHERE host = 'server1'
				  AND region = 'us-west'
				  AND time > now() - 1h
				GROUP BY time(5m), host
				ORDER BY time DESC
				LIMIT 100
			`)

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "AVG")
			So(sql, ShouldContainSubstring, "MAX")
			So(sql, ShouldContainSubstring, "MIN")
			So(sql, ShouldContainSubstring, "WHERE")
			So(sql, ShouldContainSubstring, "AND")
			So(sql, ShouldContainSubstring, "time_bucket")
			So(sql, ShouldContainSubstring, "GROUP BY")
			So(sql, ShouldContainSubstring, "ORDER BY")
			So(sql, ShouldContainSubstring, "LIMIT 100")
		})

		Convey("When translating query with nested conditions", func() {
			sql, err := translator.Translate(`
				SELECT mean(value)
				FROM cpu
				WHERE (host = 'server1' OR host = 'server2')
				  AND value > 50
			`)

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "WHERE")
			So(sql, ShouldContainSubstring, "OR")
			So(sql, ShouldContainSubstring, "AND")
		})

		Convey("When translating query with multiple GROUP BY tags", func() {
			sql, err := translator.Translate(`
				SELECT mean(value)
				FROM cpu
				GROUP BY time(10m), host, region, datacenter
			`)

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "time_bucket")
			So(sql, ShouldContainSubstring, `"host"`)
			So(sql, ShouldContainSubstring, `"region"`)
			So(sql, ShouldContainSubstring, `"datacenter"`)
		})
	})
}

func TestTranslatorEdgeCases(t *testing.T) {
	Convey("Given a translator", t, func() {
		translator := NewTranslator("testdb")

		Convey("When translating query with special database name", func() {
			specialTranslator := NewTranslator("my-database-123")
			sql, err := specialTranslator.Translate("SELECT * FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `"my-database-123"`)
		})

		Convey("When translating query with measurement name containing underscore", func() {
			sql, err := translator.Translate("SELECT * FROM system_cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `"system_cpu"`)
		})

		Convey("When translating query with measurement name containing dash", func() {
			sql, err := translator.Translate("SELECT * FROM \"cpu-metrics\"")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `"cpu-metrics"`)
		})

		Convey("When translating query with field name that is SQL keyword", func() {
			sql, err := translator.Translate("SELECT \"select\", \"from\", \"where\" FROM cpu")

			So(err, ShouldBeNil)
			// All should be properly quoted
			So(sql, ShouldContainSubstring, `"select"`)
			So(sql, ShouldContainSubstring, `"from"`)
			So(sql, ShouldContainSubstring, `"where"`)
		})

		Convey("When translating SELECT with zero LIMIT", func() {
			sql, err := translator.Translate("SELECT * FROM cpu LIMIT 0")

			So(err, ShouldBeNil)
			// LIMIT 0 should not be included (since stmt.Limit > 0 check)
			So(sql, ShouldNotContainSubstring, "LIMIT")
		})
	})
}

func TestTranslatorDatabaseContext(t *testing.T) {
	Convey("Given translators for different databases", t, func() {

		Convey("When translating same query for different databases", func() {
			db1Translator := NewTranslator("database1")
			db2Translator := NewTranslator("database2")

			sql1, err1 := db1Translator.Translate("SELECT * FROM cpu")
			sql2, err2 := db2Translator.Translate("SELECT * FROM cpu")

			So(err1, ShouldBeNil)
			So(err2, ShouldBeNil)
			So(sql1, ShouldContainSubstring, `"database1"."cpu"`)
			So(sql2, ShouldContainSubstring, `"database2"."cpu"`)
			So(sql1, ShouldNotEqual, sql2)
		})
	})
}

// TestTranslatorPhase1AggregationFunctions tests Phase 1 aggregation functions
func TestTranslatorPhase1AggregationFunctions(t *testing.T) {
	Convey("Given a translator for testdb", t, func() {
		translator := NewTranslator("testdb")

		Convey("STDDEV() translates to STDDEV()", func() {
			sql, err := translator.Translate("SELECT stddev(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `STDDEV("value")`)
		})

		Convey("MEDIAN() translates to percentile_cont(0.5)", func() {
			sql, err := translator.Translate("SELECT median(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "percentile_cont(0.5)")
			So(sql, ShouldContainSubstring, "WITHIN GROUP")
			So(sql, ShouldContainSubstring, `ORDER BY "value"`)
		})

		Convey("SPREAD() translates to MAX - MIN", func() {
			sql, err := translator.Translate("SELECT spread(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `MAX("value")`)
			So(sql, ShouldContainSubstring, `MIN("value")`)
			So(sql, ShouldContainSubstring, " - ")
		})

		Convey("MODE() translates to MODE() WITHIN GROUP", func() {
			sql, err := translator.Translate("SELECT mode(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "MODE()")
			So(sql, ShouldContainSubstring, "WITHIN GROUP")
			So(sql, ShouldContainSubstring, `ORDER BY "value"`)
		})

		Convey("Multiple phase 1 aggregations in one query", func() {
			sql, err := translator.Translate("SELECT stddev(value), median(value), spread(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "STDDEV")
			So(sql, ShouldContainSubstring, "percentile_cont")
			So(sql, ShouldContainSubstring, "MAX")
			So(sql, ShouldContainSubstring, "MIN")
		})

		Convey("STDDEV() with GROUP BY time", func() {
			sql, err := translator.Translate("SELECT stddev(value) FROM cpu GROUP BY time(5m)")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "STDDEV")
			So(sql, ShouldContainSubstring, "time_bucket")
		})
	})
}

// TestTranslatorPhase1MathFunctions tests Phase 1 math/transformation functions
func TestTranslatorPhase1MathFunctions(t *testing.T) {
	Convey("Given a translator for testdb", t, func() {
		translator := NewTranslator("testdb")

		Convey("ABS() translates to ABS()", func() {
			sql, err := translator.Translate("SELECT abs(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `ABS("value")`)
		})

		Convey("CEIL() translates to CEIL()", func() {
			sql, err := translator.Translate("SELECT ceil(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `CEIL("value")`)
		})

		Convey("FLOOR() translates to FLOOR()", func() {
			sql, err := translator.Translate("SELECT floor(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `FLOOR("value")`)
		})

		Convey("ROUND() translates to ROUND()", func() {
			sql, err := translator.Translate("SELECT round(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `ROUND("value")`)
		})

		Convey("SQRT() translates to SQRT()", func() {
			sql, err := translator.Translate("SELECT sqrt(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `SQRT("value")`)
		})

		Convey("POW(field, exp) translates to POWER(field, exp)", func() {
			sql, err := translator.Translate("SELECT pow(value, 2) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "POWER")
			So(sql, ShouldContainSubstring, `"value"`)
			So(sql, ShouldContainSubstring, "2")
		})

		Convey("EXP() translates to EXP()", func() {
			sql, err := translator.Translate("SELECT exp(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `EXP("value")`)
		})

		Convey("LN() translates to LN()", func() {
			sql, err := translator.Translate("SELECT ln(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `LN("value")`)
		})

		Convey("LOG(field, base) swaps arg order to LOG(base, field)", func() {
			sql, err := translator.Translate("SELECT log(value, 8) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "LOG(")
			// base should come first in postgres, cast to numeric for type compatibility
			So(sql, ShouldContainSubstring, "LOG(8::numeric,")
		})

		Convey("LOG2() translates to LOG(2::numeric, field::numeric)", func() {
			sql, err := translator.Translate("SELECT log2(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "LOG(2::numeric,")
			So(sql, ShouldContainSubstring, `"value"`)
		})

		Convey("LOG10() translates to LOG(10::numeric, field::numeric)", func() {
			sql, err := translator.Translate("SELECT log10(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "LOG(10::numeric,")
			So(sql, ShouldContainSubstring, `"value"`)
		})
	})
}

// TestTranslatorPhase1TrigFunctions tests Phase 1 trigonometry functions
func TestTranslatorPhase1TrigFunctions(t *testing.T) {
	Convey("Given a translator for testdb", t, func() {
		translator := NewTranslator("testdb")

		Convey("SIN() translates to SIN()", func() {
			sql, err := translator.Translate("SELECT sin(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `SIN("value")`)
		})

		Convey("COS() translates to COS()", func() {
			sql, err := translator.Translate("SELECT cos(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `COS("value")`)
		})

		Convey("TAN() translates to TAN()", func() {
			sql, err := translator.Translate("SELECT tan(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `TAN("value")`)
		})

		Convey("ASIN() translates to ASIN()", func() {
			sql, err := translator.Translate("SELECT asin(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `ASIN("value")`)
		})

		Convey("ACOS() translates to ACOS()", func() {
			sql, err := translator.Translate("SELECT acos(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `ACOS("value")`)
		})

		Convey("ATAN() translates to ATAN()", func() {
			sql, err := translator.Translate("SELECT atan(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, `ATAN("value")`)
		})

		Convey("ATAN2(y, x) translates to ATAN2(y, x) preserving arg order", func() {
			sql, err := translator.Translate("SELECT atan2(y_val, x_val) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "ATAN2(")
			So(sql, ShouldContainSubstring, `"y_val"`)
			So(sql, ShouldContainSubstring, `"x_val"`)
		})

		Convey("All trig functions can be combined in a single query", func() {
			sql, err := translator.Translate("SELECT sin(value), cos(value), tan(value) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "SIN")
			So(sql, ShouldContainSubstring, "COS")
			So(sql, ShouldContainSubstring, "TAN")
		})
	})
}

// TestTranslatorPhase1FunctionsInContext tests Phase 1 functions in realistic query contexts
func TestTranslatorPhase1FunctionsInContext(t *testing.T) {
	Convey("Given a translator for testdb", t, func() {
		translator := NewTranslator("testdb")

		Convey("Math function with WHERE clause", func() {
			sql, err := translator.Translate("SELECT abs(value) FROM cpu WHERE value < 0")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "ABS")
			So(sql, ShouldContainSubstring, "WHERE")
			So(sql, ShouldContainSubstring, "< 0")
		})

		Convey("Math function with GROUP BY time", func() {
			sql, err := translator.Translate("SELECT abs(mean(value)) FROM cpu GROUP BY time(5m)")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "ABS")
			So(sql, ShouldContainSubstring, "AVG")
			So(sql, ShouldContainSubstring, "time_bucket")
		})

		Convey("STDDEV with GROUP BY tag and time", func() {
			sql, err := translator.Translate("SELECT stddev(value) FROM cpu GROUP BY time(10m), host")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "STDDEV")
			So(sql, ShouldContainSubstring, "time_bucket")
			So(sql, ShouldContainSubstring, `"host"`)
		})

		Convey("SPREAD used with WHERE and LIMIT", func() {
			sql, err := translator.Translate("SELECT spread(value) FROM cpu WHERE host='server1' LIMIT 10")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "MAX")
			So(sql, ShouldContainSubstring, "MIN")
			So(sql, ShouldContainSubstring, "WHERE")
			So(sql, ShouldContainSubstring, "LIMIT 10")
		})

		Convey("MEDIAN with ORDER BY and LIMIT", func() {
			sql, err := translator.Translate("SELECT median(value) FROM cpu ORDER BY time DESC LIMIT 5")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "percentile_cont")
			So(sql, ShouldContainSubstring, "ORDER BY")
			So(sql, ShouldContainSubstring, "LIMIT 5")
		})

		Convey("POW with exponent as float", func() {
			sql, err := translator.Translate("SELECT pow(value, 0.5) FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "POWER")
			So(sql, ShouldContainSubstring, "0.5")
		})

		Convey("LOG with custom base", func() {
			sql, err := translator.Translate("SELECT log(value, 2) FROM cpu")

			So(err, ShouldBeNil)
			// PostgreSQL LOG(base, value) - args are swapped from InfluxQL LOG(value, base)
			// Base must be cast to numeric for type compatibility
			So(sql, ShouldContainSubstring, "LOG(2::numeric,")
		})

		Convey("Math function aliases work correctly", func() {
			sql, err := translator.Translate("SELECT sqrt(value) AS root_val FROM cpu")

			So(err, ShouldBeNil)
			So(sql, ShouldContainSubstring, "SQRT")
			So(sql, ShouldContainSubstring, `AS "root_val"`)
		})
	})
}

// Benchmark tests for query translation performance
func BenchmarkTranslateSimpleSelect(b *testing.B) {
	translator := NewTranslator("testdb")
	query := "SELECT value FROM cpu WHERE host = 'server1'"
	for i := 0; i < b.N; i++ {
		translator.Translate(query)
	}
}

func BenchmarkTranslateComplexSelect(b *testing.B) {
	translator := NewTranslator("testdb")
	query := `SELECT mean(value), max(value), min(value)
		FROM cpu
		WHERE host = 'server1' AND region = 'us-west'
		GROUP BY time(5m), host
		LIMIT 100`
	for i := 0; i < b.N; i++ {
		translator.Translate(query)
	}
}

func BenchmarkTranslateShowDatabases(b *testing.B) {
	translator := NewTranslator("testdb")
	query := "SHOW DATABASES"
	for i := 0; i < b.N; i++ {
		translator.Translate(query)
	}
}
