package write

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestParseLineProtocol(t *testing.T) {
	Convey("Given valid line protocol", t, func() {

		Convey("When parsing basic point with float", func() {
			point, err := ParseLineProtocol("cpu value=42.5 1620000000000000000")

			So(err, ShouldBeNil)
			So(point, ShouldNotBeNil)
			So(point.Measurement, ShouldEqual, "cpu")
			So(point.Fields["value"], ShouldEqual, 42.5)
			So(point.Timestamp, ShouldEqual, time.Unix(0, 1620000000000000000))
		})

		Convey("When parsing integer field", func() {
			point, err := ParseLineProtocol("cpu value=42i 1620000000000000000")

			So(err, ShouldBeNil)
			So(point.Fields["value"], ShouldEqual, int64(42))
		})

		Convey("When parsing negative integer", func() {
			point, err := ParseLineProtocol("cpu value=-42i 1620000000000000000")

			So(err, ShouldBeNil)
			So(point.Fields["value"], ShouldEqual, int64(-42))
		})

		Convey("When parsing string field", func() {
			point, err := ParseLineProtocol(`cpu value="hello world" 1620000000000000000`)

			So(err, ShouldBeNil)
			So(point.Fields["value"], ShouldEqual, "hello world")
		})

		Convey("When parsing boolean fields", func() {
			testCases := []struct {
				input    string
				expected bool
			}{
				{`cpu value=t 1620000000000000000`, true},
				{`cpu value=T 1620000000000000000`, true},
				{`cpu value=true 1620000000000000000`, true},
				{`cpu value=True 1620000000000000000`, true},
				{`cpu value=TRUE 1620000000000000000`, true},
				{`cpu value=f 1620000000000000000`, false},
				{`cpu value=F 1620000000000000000`, false},
				{`cpu value=false 1620000000000000000`, false},
				{`cpu value=False 1620000000000000000`, false},
				{`cpu value=FALSE 1620000000000000000`, false},
			}

			for _, tc := range testCases {
				point, err := ParseLineProtocol(tc.input)
				So(err, ShouldBeNil)
				So(point.Fields["value"], ShouldEqual, tc.expected)
			}
		})

		Convey("When parsing with tags", func() {
			point, err := ParseLineProtocol("cpu,host=server1,region=us-west value=85.3 1620000000000000000")

			So(err, ShouldBeNil)
			So(point.Measurement, ShouldEqual, "cpu")
			So(point.Tags["host"], ShouldEqual, "server1")
			So(point.Tags["region"], ShouldEqual, "us-west")
			So(point.Fields["value"], ShouldEqual, 85.3)
		})

		Convey("When parsing with multiple fields", func() {
			point, err := ParseLineProtocol("cpu,host=server1 usage=85.3,count=42i,enabled=true 1620000000000000000")

			So(err, ShouldBeNil)
			So(point.Fields["usage"], ShouldEqual, 85.3)
			So(point.Fields["count"], ShouldEqual, int64(42))
			So(point.Fields["enabled"], ShouldEqual, true)
		})

		Convey("When parsing without timestamp", func() {
			before := time.Now()
			point, err := ParseLineProtocol("cpu value=42.5")
			after := time.Now()

			So(err, ShouldBeNil)
			So(point.Timestamp, ShouldHappenOnOrBetween, before, after)
		})

		Convey("When parsing with escaped characters in tags", func() {
			point, err := ParseLineProtocol(`cpu,tag\ name=value\ here value=42`)

			So(err, ShouldBeNil)
			So(point.Tags["tag name"], ShouldEqual, "value here")
		})

		Convey("When parsing with escaped comma in tag", func() {
			point, err := ParseLineProtocol(`cpu,host=server\,1 value=42`)

			So(err, ShouldBeNil)
			So(point.Tags["host"], ShouldEqual, "server,1")
		})

		Convey("When parsing with escaped equals in tag", func() {
			point, err := ParseLineProtocol(`cpu,tag=val\=ue value=42`)

			So(err, ShouldBeNil)
			So(point.Tags["tag"], ShouldEqual, "val=ue")
		})

		Convey("When parsing string field with escaped quotes", func() {
			point, err := ParseLineProtocol(`cpu value="hello \"world\"" 1620000000000000000`)

			So(err, ShouldBeNil)
			So(point.Fields["value"], ShouldEqual, `hello "world"`)
		})

		Convey("When parsing string field with escaped backslash", func() {
			point, err := ParseLineProtocol(`cpu value="path\\to\\file" 1620000000000000000`)

			So(err, ShouldBeNil)
			So(point.Fields["value"], ShouldEqual, `path\to\file`)
		})

		Convey("When parsing with space in measurement", func() {
			point, err := ParseLineProtocol(`my\ measurement value=42`)

			So(err, ShouldBeNil)
			So(point.Measurement, ShouldEqual, "my measurement")
		})

		Convey("When parsing empty line", func() {
			point, err := ParseLineProtocol("")

			So(err, ShouldBeNil)
			So(point, ShouldBeNil)
		})

		Convey("When parsing comment line", func() {
			point, err := ParseLineProtocol("# this is a comment")

			So(err, ShouldBeNil)
			So(point, ShouldBeNil)
		})

		Convey("When parsing scientific notation float", func() {
			point, err := ParseLineProtocol("cpu value=1.23e10")

			So(err, ShouldBeNil)
			So(point.Fields["value"], ShouldEqual, 1.23e10)
		})

		Convey("When parsing negative float", func() {
			point, err := ParseLineProtocol("cpu value=-42.5")

			So(err, ShouldBeNil)
			So(point.Fields["value"], ShouldEqual, -42.5)
		})

		Convey("When parsing zero values", func() {
			point, err := ParseLineProtocol("cpu int_val=0i,float_val=0.0,bool_val=f")

			So(err, ShouldBeNil)
			So(point.Fields["int_val"], ShouldEqual, int64(0))
			So(point.Fields["float_val"], ShouldEqual, 0.0)
			So(point.Fields["bool_val"], ShouldEqual, false)
		})
	})

	Convey("Given malformed line protocol", t, func() {

		Convey("When missing fields", func() {
			_, err := ParseLineProtocol("cpu 1620000000000000000")

			So(err, ShouldNotBeNil)
			// Parser treats timestamp as field segment, then fails to parse it
			So(err.Error(), ShouldContainSubstring, "invalid field format")
		})

		Convey("When only measurement provided", func() {
			_, err := ParseLineProtocol("cpu")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "expected at least 2 parts")
		})

		Convey("When field has no equals sign", func() {
			_, err := ParseLineProtocol("cpu value42")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "invalid field format")
		})

		Convey("When tag has no equals sign", func() {
			_, err := ParseLineProtocol("cpu,hostserver1 value=42")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "invalid tag format")
		})

		Convey("When integer suffix but invalid number", func() {
			_, err := ParseLineProtocol("cpu value=abci")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "invalid integer")
		})

		Convey("When invalid float", func() {
			_, err := ParseLineProtocol("cpu value=abc")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "invalid number")
		})

		Convey("When invalid timestamp", func() {
			_, err := ParseLineProtocol("cpu value=42 notanumber")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "invalid timestamp")
		})

		Convey("When string field missing closing quote", func() {
			_, err := ParseLineProtocol(`cpu value="unclosed`)

			So(err, ShouldNotBeNil)
		})

		Convey("When measurement starts with comma", func() {
			point, err := ParseLineProtocol(",host=server1 value=42")

			// Parser splits by comma: empty first segment is skipped, so "host=server1" becomes measurement
			So(err, ShouldBeNil)
			So(point.Measurement, ShouldEqual, "host=server1")
		})
	})

	Convey("Given edge cases", t, func() {

		Convey("When parsing very large integer", func() {
			point, err := ParseLineProtocol("cpu value=9223372036854775807i")

			So(err, ShouldBeNil)
			So(point.Fields["value"], ShouldEqual, int64(9223372036854775807))
		})

		Convey("When parsing very small integer", func() {
			point, err := ParseLineProtocol("cpu value=-9223372036854775808i")

			So(err, ShouldBeNil)
			So(point.Fields["value"], ShouldEqual, int64(-9223372036854775808))
		})

		Convey("When parsing empty string field", func() {
			point, err := ParseLineProtocol(`cpu value=""`)

			So(err, ShouldBeNil)
			So(point.Fields["value"], ShouldEqual, "")
		})

		Convey("When parsing field with comma in quoted string", func() {
			point, err := ParseLineProtocol(`cpu msg="hello, world"`)

			So(err, ShouldBeNil)
			So(point.Fields["msg"], ShouldEqual, "hello, world")
		})

		Convey("When parsing multiple spaces before timestamp", func() {
			point, err := ParseLineProtocol("cpu value=42    1620000000000000000")

			So(err, ShouldBeNil)
			So(point.Timestamp, ShouldEqual, time.Unix(0, 1620000000000000000))
		})

		Convey("When parsing with many tags", func() {
			point, err := ParseLineProtocol("cpu,tag1=a,tag2=b,tag3=c,tag4=d,tag5=e value=42")

			So(err, ShouldBeNil)
			So(len(point.Tags), ShouldEqual, 5)
			So(point.Tags["tag1"], ShouldEqual, "a")
			So(point.Tags["tag5"], ShouldEqual, "e")
		})

		Convey("When parsing with many fields", func() {
			point, err := ParseLineProtocol("cpu f1=1,f2=2,f3=3,f4=4,f5=5")

			So(err, ShouldBeNil)
			So(len(point.Fields), ShouldEqual, 5)
		})
	})
}

func TestParseBatch(t *testing.T) {
	Convey("Given multiple lines of line protocol", t, func() {

		Convey("When parsing valid batch", func() {
			data := `cpu,host=server1 value=85.3 1620000000000000000
cpu,host=server2 value=90.1 1620000000000000001
mem,host=server1 used=1024i 1620000000000000002`

			points, err := ParseBatch(data)

			So(err, ShouldBeNil)
			So(len(points), ShouldEqual, 3)
			So(points[0].Measurement, ShouldEqual, "cpu")
			So(points[0].Tags["host"], ShouldEqual, "server1")
			So(points[1].Tags["host"], ShouldEqual, "server2")
			So(points[2].Measurement, ShouldEqual, "mem")
		})

		Convey("When parsing batch with empty lines", func() {
			data := `cpu value=85.3

mem value=1024i`

			points, err := ParseBatch(data)

			So(err, ShouldBeNil)
			So(len(points), ShouldEqual, 2)
		})

		Convey("When parsing batch with comments", func() {
			data := `# First point
cpu value=85.3
# Second point
mem value=1024i`

			points, err := ParseBatch(data)

			So(err, ShouldBeNil)
			So(len(points), ShouldEqual, 2)
		})

		Convey("When parsing batch with error", func() {
			data := `cpu value=85.3
invalid line
mem value=1024i`

			_, err := ParseBatch(data)

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "line 2")
		})

		Convey("When parsing empty batch", func() {
			points, err := ParseBatch("")

			So(err, ShouldBeNil)
			So(len(points), ShouldEqual, 0)
		})
	})
}

func TestGetFieldType(t *testing.T) {
	Convey("Given field values", t, func() {

		Convey("When getting type for int64", func() {
			fieldType := GetFieldType(int64(42))
			So(fieldType, ShouldEqual, FieldTypeInt)
		})

		Convey("When getting type for float64", func() {
			fieldType := GetFieldType(42.5)
			So(fieldType, ShouldEqual, FieldTypeFloat)
		})

		Convey("When getting type for string", func() {
			fieldType := GetFieldType("hello")
			So(fieldType, ShouldEqual, FieldTypeString)
		})

		Convey("When getting type for bool", func() {
			fieldType := GetFieldType(true)
			So(fieldType, ShouldEqual, FieldTypeBool)
		})

		Convey("When getting type for unknown type", func() {
			fieldType := GetFieldType(struct{}{})
			So(fieldType, ShouldEqual, FieldTypeString)
		})
	})
}

func TestSplitLineProtocol(t *testing.T) {
	Convey("Given line protocol strings", t, func() {

		Convey("When splitting simple line", func() {
			parts := splitLineProtocol("cpu value=42 1620000000")
			So(len(parts), ShouldEqual, 3)
			So(parts[0], ShouldEqual, "cpu")
			So(parts[1], ShouldEqual, "value=42")
			So(parts[2], ShouldEqual, "1620000000")
		})

		Convey("When splitting with escaped space", func() {
			parts := splitLineProtocol(`my\ measurement value=42`)
			So(len(parts), ShouldEqual, 2)
			So(parts[0], ShouldEqual, `my\ measurement`)
		})

		Convey("When splitting with quoted field", func() {
			parts := splitLineProtocol(`cpu msg="hello world"`)
			So(len(parts), ShouldEqual, 2)
			So(parts[1], ShouldEqual, `msg="hello world"`)
		})

		Convey("When splitting with multiple spaces", func() {
			parts := splitLineProtocol("cpu   value=42   1620000000")
			So(len(parts), ShouldEqual, 3)
		})
	})
}

func TestUnescapeFunctions(t *testing.T) {
	Convey("Given escaped strings", t, func() {

		Convey("When unescaping key with space", func() {
			result := unescapeKey(`my\ key`)
			So(result, ShouldEqual, "my key")
		})

		Convey("When unescaping key with comma", func() {
			result := unescapeKey(`my\,key`)
			So(result, ShouldEqual, "my,key")
		})

		Convey("When unescaping key with equals", func() {
			result := unescapeKey(`my\=key`)
			So(result, ShouldEqual, "my=key")
		})

		Convey("When unescaping value with all special chars", func() {
			result := unescapeValue(`val\ ue\,with\=all\"chars`)
			So(result, ShouldEqual, `val ue,with=all"chars`)
		})

		Convey("When unescaping plain string", func() {
			result := unescapeKey("plain")
			So(result, ShouldEqual, "plain")
		})
	})
}

// Benchmark tests for performance-critical parsing
func BenchmarkParseLineProtocol(b *testing.B) {
	line := "cpu,host=server1,region=us-west value=85.3,count=42i,enabled=true 1620000000000000000"
	for i := 0; i < b.N; i++ {
		ParseLineProtocol(line)
	}
}

func BenchmarkParseLineProtocolSimple(b *testing.B) {
	line := "cpu value=42.5 1620000000000000000"
	for i := 0; i < b.N; i++ {
		ParseLineProtocol(line)
	}
}

func BenchmarkParseBatch(b *testing.B) {
	data := `cpu,host=server1 value=85.3 1620000000000000000
cpu,host=server2 value=90.1 1620000000000000001
mem,host=server1 used=1024i 1620000000000000002
mem,host=server2 used=2048i 1620000000000000003
disk,host=server1 free=50000i 1620000000000000004`

	for i := 0; i < b.N; i++ {
		ParseBatch(data)
	}
}
