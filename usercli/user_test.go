package usercli

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestParsePermission(t *testing.T) {
	Convey("Given a permission string", t, func() {

		Convey("When parsing specific database with read permission", func() {
			database, measurement, canRead, canWrite, err := ParsePermission("mydb:r")

			So(err, ShouldBeNil)
			So(database, ShouldEqual, "mydb")
			So(measurement, ShouldBeEmpty)
			So(canRead, ShouldBeTrue)
			So(canWrite, ShouldBeFalse)
		})

		Convey("When parsing specific database with write permission", func() {
			database, measurement, canRead, canWrite, err := ParsePermission("mydb:w")

			So(err, ShouldBeNil)
			So(database, ShouldEqual, "mydb")
			So(measurement, ShouldBeEmpty)
			So(canRead, ShouldBeFalse)
			So(canWrite, ShouldBeTrue)
		})

		Convey("When parsing specific database with read/write permission", func() {
			database, measurement, canRead, canWrite, err := ParsePermission("mydb:rw")

			So(err, ShouldBeNil)
			So(database, ShouldEqual, "mydb")
			So(measurement, ShouldBeEmpty)
			So(canRead, ShouldBeTrue)
			So(canWrite, ShouldBeTrue)
		})

		Convey("When parsing specific database and measurement", func() {
			database, measurement, canRead, canWrite, err := ParsePermission("mydb.cpu:rw")

			So(err, ShouldBeNil)
			So(database, ShouldEqual, "mydb")
			So(measurement, ShouldEqual, "cpu")
			So(canRead, ShouldBeTrue)
			So(canWrite, ShouldBeTrue)
		})

		Convey("When parsing database with wildcard measurement", func() {
			database, measurement, canRead, canWrite, err := ParsePermission("mydb.*:r")

			So(err, ShouldBeNil)
			So(database, ShouldEqual, "mydb")
			So(measurement, ShouldBeEmpty) // * converted to empty string
			So(canRead, ShouldBeTrue)
			So(canWrite, ShouldBeFalse)
		})

		Convey("When parsing wildcard database (all databases)", func() {
			database, measurement, canRead, canWrite, err := ParsePermission("*:rw")

			So(err, ShouldBeNil)
			So(database, ShouldEqual, "*")
			So(measurement, ShouldBeEmpty)
			So(canRead, ShouldBeTrue)
			So(canWrite, ShouldBeTrue)
		})

		Convey("When parsing wildcard database with specific measurement", func() {
			database, measurement, canRead, canWrite, err := ParsePermission("*.cpu:r")

			So(err, ShouldBeNil)
			So(database, ShouldEqual, "*")
			So(measurement, ShouldEqual, "cpu")
			So(canRead, ShouldBeTrue)
			So(canWrite, ShouldBeFalse)
		})

		Convey("When parsing wildcard database and wildcard measurement", func() {
			database, measurement, canRead, canWrite, err := ParsePermission("*.*:w")

			So(err, ShouldBeNil)
			So(database, ShouldEqual, "*")
			So(measurement, ShouldBeEmpty) // * converted to empty string
			So(canRead, ShouldBeFalse)
			So(canWrite, ShouldBeTrue)
		})

		Convey("When parsing 'wr' as alternative to 'rw'", func() {
			_, _, canRead, canWrite, err := ParsePermission("mydb:wr")

			So(err, ShouldBeNil)
			So(canRead, ShouldBeTrue)
			So(canWrite, ShouldBeTrue)
		})

		Convey("When parsing 'read' as verbose alternative", func() {
			_, _, canRead, canWrite, err := ParsePermission("mydb:read")

			So(err, ShouldBeNil)
			So(canRead, ShouldBeTrue)
			So(canWrite, ShouldBeFalse)
		})

		Convey("When parsing 'write' as verbose alternative", func() {
			_, _, canRead, canWrite, err := ParsePermission("mydb:write")

			So(err, ShouldBeNil)
			So(canRead, ShouldBeFalse)
			So(canWrite, ShouldBeTrue)
		})

		Convey("When parsing 'readwrite' as verbose alternative", func() {
			_, _, canRead, canWrite, err := ParsePermission("mydb:readwrite")

			So(err, ShouldBeNil)
			So(canRead, ShouldBeTrue)
			So(canWrite, ShouldBeTrue)
		})

		Convey("When missing colon separator", func() {
			_, _, _, _, err := ParsePermission("mydb")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "invalid permission format")
		})

		Convey("When missing access mode", func() {
			_, _, _, _, err := ParsePermission("mydb:")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "invalid access mode")
		})

		Convey("When using invalid access mode", func() {
			_, _, _, _, err := ParsePermission("mydb:x")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "invalid access mode")
		})

		Convey("When database is empty", func() {
			_, _, _, _, err := ParsePermission(":rw")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "database cannot be empty")
		})

		Convey("When permission string is empty", func() {
			_, _, _, _, err := ParsePermission("")

			So(err, ShouldNotBeNil)
		})

		Convey("When permission has too many colons", func() {
			_, _, _, _, err := ParsePermission("mydb:cpu:rw")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "invalid permission format")
		})

		Convey("When parsing complex database name", func() {
			database, measurement, canRead, _, err := ParsePermission("my-db_123:r")

			So(err, ShouldBeNil)
			So(database, ShouldEqual, "my-db_123")
			So(measurement, ShouldBeEmpty)
			So(canRead, ShouldBeTrue)
		})

		Convey("When parsing complex measurement name", func() {
			database, measurement, _, canWrite, err := ParsePermission("mydb.cpu_usage_percent:w")

			So(err, ShouldBeNil)
			So(database, ShouldEqual, "mydb")
			So(measurement, ShouldEqual, "cpu_usage_percent")
			So(canWrite, ShouldBeTrue)
		})

		Convey("When parsing measurement with dots", func() {
			database, measurement, canRead, canWrite, err := ParsePermission("mydb.system.cpu.usage:rw")

			So(err, ShouldBeNil)
			So(database, ShouldEqual, "mydb")
			So(measurement, ShouldEqual, "system.cpu.usage") // Everything after first dot
			So(canRead, ShouldBeTrue)
			So(canWrite, ShouldBeTrue)
		})
	})
}
