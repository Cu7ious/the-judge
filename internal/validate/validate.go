package validate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// Spec is a declarative validator from a test case's validators JSON.
type Spec struct {
	Type   string          `json:"type"`
	Value  string          `json:"value,omitempty"`
	Schema json.RawMessage `json:"schema,omitempty"`
}

// Input is what validators inspect.
type Input struct {
	ResponseText string
	TimedOut     bool
	HadError     bool
	ErrorMessage string
	Expected     json.RawMessage
}

// Result is one validator outcome.
type Result struct {
	Type    string
	Passed  bool
	Message string
	Details json.RawMessage
}

// Validator is the extension point for deterministic checks and future LLM-as-judge.
type Validator interface {
	Type() string
	Validate(ctx context.Context, in Input) Result
}

// RunAll parses specs and executes them in order.
func RunAll(ctx context.Context, specsJSON json.RawMessage, in Input) ([]Result, error) {
	var specs []Spec
	if len(specsJSON) == 0 || string(specsJSON) == "null" {
		specs = nil
	} else if err := json.Unmarshal(specsJSON, &specs); err != nil {
		return nil, fmt.Errorf("parse validators: %w", err)
	}

	results := make([]Result, 0, len(specs))
	for _, spec := range specs {
		v, err := Build(spec)
		if err != nil {
			results = append(results, Result{
				Type:    spec.Type,
				Passed:  false,
				Message: err.Error(),
				Details: json.RawMessage(`{}`),
			})
			continue
		}
		results = append(results, v.Validate(ctx, in))
	}
	return results, nil
}

// AllPassed reports whether every result passed.
func AllPassed(results []Result) bool {
	for _, r := range results {
		if !r.Passed {
			return false
		}
	}
	return true
}

// Build constructs a Validator from a Spec.
func Build(spec Spec) (Validator, error) {
	switch spec.Type {
	case "exact", "exact_match":
		return ExactMatch{Expected: spec.Value}, nil
	case "contains":
		return Contains{Substring: spec.Value}, nil
	case "regex":
		re, err := regexp.Compile(spec.Value)
		if err != nil {
			return nil, fmt.Errorf("invalid regex: %w", err)
		}
		return Regex{RE: re, Pattern: spec.Value}, nil
	case "valid_json":
		return ValidJSON{}, nil
	case "json_schema":
		if len(spec.Schema) == 0 {
			return nil, fmt.Errorf("json_schema requires schema")
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource("schema.json", bytes.NewReader(spec.Schema)); err != nil {
			return nil, fmt.Errorf("add schema: %w", err)
		}
		sch, err := compiler.Compile("schema.json")
		if err != nil {
			return nil, fmt.Errorf("compile schema: %w", err)
		}
		return JSONSchema{Schema: sch, Raw: spec.Schema}, nil
	case "no_error":
		return NoError{}, nil
	case "no_timeout":
		return NoTimeout{}, nil
	default:
		return nil, fmt.Errorf("unknown validator type %q", spec.Type)
	}
}

// ExactMatch requires response text == value.
type ExactMatch struct{ Expected string }

func (v ExactMatch) Type() string { return "exact_match" }
func (v ExactMatch) Validate(_ context.Context, in Input) Result {
	ok := in.ResponseText == v.Expected
	msg := "exact match"
	if !ok {
		msg = fmt.Sprintf("expected %q got %q", v.Expected, truncate(in.ResponseText, 120))
	}
	return Result{Type: v.Type(), Passed: ok, Message: msg, Details: json.RawMessage(`{}`)}
}

// Contains requires response text to include substring.
type Contains struct{ Substring string }

func (v Contains) Type() string { return "contains" }
func (v Contains) Validate(_ context.Context, in Input) Result {
	ok := strings.Contains(in.ResponseText, v.Substring)
	msg := "contains substring"
	if !ok {
		msg = fmt.Sprintf("response does not contain %q", v.Substring)
	}
	return Result{Type: v.Type(), Passed: ok, Message: msg, Details: json.RawMessage(`{}`)}
}

// Regex requires response text to match a pattern.
type Regex struct {
	RE      *regexp.Regexp
	Pattern string
}

func (v Regex) Type() string { return "regex" }
func (v Regex) Validate(_ context.Context, in Input) Result {
	ok := v.RE.MatchString(in.ResponseText)
	msg := "regex matched"
	if !ok {
		msg = fmt.Sprintf("regex %q did not match", v.Pattern)
	}
	return Result{Type: v.Type(), Passed: ok, Message: msg, Details: json.RawMessage(`{}`)}
}

// ValidJSON requires response text to be parseable JSON.
type ValidJSON struct{}

func (v ValidJSON) Type() string { return "valid_json" }
func (v ValidJSON) Validate(_ context.Context, in Input) Result {
	var raw any
	err := json.Unmarshal([]byte(in.ResponseText), &raw)
	ok := err == nil
	msg := "valid JSON"
	if !ok {
		msg = fmt.Sprintf("invalid JSON: %v", err)
	}
	return Result{Type: v.Type(), Passed: ok, Message: msg, Details: json.RawMessage(`{}`)}
}

// JSONSchema validates response JSON against a schema document.
type JSONSchema struct {
	Schema *jsonschema.Schema
	Raw    json.RawMessage
}

func (v JSONSchema) Type() string { return "json_schema" }
func (v JSONSchema) Validate(_ context.Context, in Input) Result {
	var doc any
	if err := json.Unmarshal([]byte(in.ResponseText), &doc); err != nil {
		return Result{Type: v.Type(), Passed: false, Message: fmt.Sprintf("not JSON: %v", err), Details: json.RawMessage(`{}`)}
	}
	if err := v.Schema.Validate(doc); err != nil {
		return Result{Type: v.Type(), Passed: false, Message: err.Error(), Details: json.RawMessage(`{}`)}
	}
	return Result{Type: v.Type(), Passed: true, Message: "schema valid", Details: json.RawMessage(`{}`)}
}

// NoError fails when the provider returned an error.
type NoError struct{}

func (v NoError) Type() string { return "no_error" }
func (v NoError) Validate(_ context.Context, in Input) Result {
	ok := !in.HadError
	msg := "no provider error"
	if !ok {
		msg = "provider error: " + in.ErrorMessage
	}
	return Result{Type: v.Type(), Passed: ok, Message: msg, Details: json.RawMessage(`{}`)}
}

// NoTimeout fails when the provider call timed out.
type NoTimeout struct{}

func (v NoTimeout) Type() string { return "no_timeout" }
func (v NoTimeout) Validate(_ context.Context, in Input) Result {
	ok := !in.TimedOut
	msg := "completed within timeout"
	if !ok {
		msg = "timed out"
	}
	return Result{Type: v.Type(), Passed: ok, Message: msg, Details: json.RawMessage(`{}`)}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
