package usage

import (
	"strings"
	"testing"
)

func TestReadReportsMalformedLineAndContinues(t *testing.T) {
	input := strings.Join([]string{
		`{"request_id":"first","upstream_attempt":1}`,
		`not json`,
		`{"request_id":"last","upstream_attempt":2}`,
	}, "\n")

	result, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 2 || result.Records[0].RequestID != "first" || result.Records[1].RequestID != "last" {
		t.Fatalf("records = %+v", result.Records)
	}
	if len(result.Malformed) != 1 || result.Malformed[0].Line != 2 || result.Malformed[0].Err == nil {
		t.Fatalf("malformed = %+v", result.Malformed)
	}
}

func TestReadHandlesLargeMalformedLineBeforeValidRecord(t *testing.T) {
	input := strings.Repeat("x", 1024*1024) + "\n" + `{"request_id":"last"}`

	result, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Malformed) != 1 || len(result.Records) != 1 || result.Records[0].RequestID != "last" {
		t.Fatalf("result = %+v", result)
	}
}
