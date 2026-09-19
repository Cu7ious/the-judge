package validate

import (
	"context"
	"testing"
)

func TestExactMatch(t *testing.T) {
	v := ExactMatch{Expected: "Paris"}
	if !v.Validate(context.Background(), Input{ResponseText: "Paris"}).Passed {
		t.Fatal("expected pass")
	}
	if v.Validate(context.Background(), Input{ResponseText: "London"}).Passed {
		t.Fatal("expected fail")
	}
}

func TestContains(t *testing.T) {
	v := Contains{Substring: "hello"}
	if !v.Validate(context.Background(), Input{ResponseText: "say hello world"}).Passed {
		t.Fatal("expected pass")
	}
	if v.Validate(context.Background(), Input{ResponseText: "hi"}).Passed {
		t.Fatal("expected fail")
	}
}

func TestRegex(t *testing.T) {
	built, err := Build(Spec{Type: "regex", Value: `^\d{3}$`})
	if err != nil {
		t.Fatal(err)
	}
	if !built.Validate(context.Background(), Input{ResponseText: "42"}).Passed == false {
		// 42 is only 2 digits
	}
	if built.Validate(context.Background(), Input{ResponseText: "42"}).Passed {
		t.Fatal("expected fail for 42")
	}
	if !built.Validate(context.Background(), Input{ResponseText: "123"}).Passed {
		t.Fatal("expected pass for 123")
	}
}

func TestValidJSON(t *testing.T) {
	v := ValidJSON{}
	if !v.Validate(context.Background(), Input{ResponseText: `{"a":1}`}).Passed {
		t.Fatal("expected pass")
	}
	if v.Validate(context.Background(), Input{ResponseText: `{nope}`}).Passed {
		t.Fatal("expected fail")
	}
}

func TestJSONSchema(t *testing.T) {
	schema := []byte(`{"type":"object","required":["city"],"properties":{"city":{"type":"string"}}}`)
	v, err := Build(Spec{Type: "json_schema", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	if !v.Validate(context.Background(), Input{ResponseText: `{"city":"Paris"}`}).Passed {
		t.Fatal("expected pass")
	}
	if v.Validate(context.Background(), Input{ResponseText: `{"town":"Paris"}`}).Passed {
		t.Fatal("expected fail")
	}
}

func TestNoErrorNoTimeout(t *testing.T) {
	if !(NoError{}).Validate(context.Background(), Input{}).Passed {
		t.Fatal("no error should pass")
	}
	if (NoError{}).Validate(context.Background(), Input{HadError: true, ErrorMessage: "boom"}).Passed {
		t.Fatal("error should fail")
	}
	if (NoTimeout{}).Validate(context.Background(), Input{TimedOut: true}).Passed {
		t.Fatal("timeout should fail")
	}
}

func TestRunAll(t *testing.T) {
	specs := []byte(`[{"type":"contains","value":"ok"},{"type":"valid_json"}]`)
	results, err := RunAll(context.Background(), specs, Input{ResponseText: `{"status":"ok"}`})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || !AllPassed(results) {
		t.Fatalf("results=%+v", results)
	}
}
