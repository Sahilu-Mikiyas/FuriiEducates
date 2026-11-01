package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGradeAnswer(t *testing.T) {
	cases := []struct {
		name            string
		kind, key, ans  string
		correct, manual bool
	}{
		{"single choice correct", "single_choice", `["A"]`, `"A"`, true, false},
		{"single choice as array", "single_choice", `["A"]`, `["A"]`, true, false},
		{"single choice wrong", "single_choice", `["A"]`, `"B"`, false, false},
		{"single choice unanswered", "single_choice", `["A"]`, `null`, false, false},
		{"multiple choice any order", "multiple_choice", `["A","C"]`, `["C","A"]`, true, false},
		{"multiple choice partial", "multiple_choice", `["A","C"]`, `["A"]`, false, false},
		{"multiple choice extra", "multiple_choice", `["A","C"]`, `["A","B","C"]`, false, false},
		{"numeric exact", "numeric", `{"value":12,"tolerance":0}`, `12`, true, false},
		{"numeric string within tolerance", "numeric", `{"value":12,"tolerance":0.5}`, `"12.4"`, true, false},
		{"numeric outside tolerance", "numeric", `{"value":12,"tolerance":0.5}`, `12.6`, false, false},
		{"numeric not a number", "numeric", `{"value":12}`, `"twelve"`, false, false},
		{"numeric unanswered", "numeric", `{"value":0}`, `null`, false, false},
		{"text needs review", "text", `{"guide":"x"}`, `"My answer"`, false, true},
		{"text unanswered", "text", `{"guide":"x"}`, `null`, false, false},
		{"unknown type", "essay", `[]`, `"x"`, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			correct, manual := gradeAnswer(c.kind, []byte(c.key), []byte(c.ans))
			if correct != c.correct || manual != c.manual {
				t.Fatalf("gradeAnswer = (%v, %v), want (%v, %v)", correct, manual, c.correct, c.manual)
			}
		})
	}
}

func TestValidateQuestion(t *testing.T) {
	valid := func() questionPayload {
		return questionPayload{
			Type:    "single_choice",
			Prompt:  json.RawMessage(`{"text":"Pick one"}`),
			Options: json.RawMessage(`[{"id":"A","text":"Yes"},{"id":"B","text":"No"}]`),
			Answer:  json.RawMessage(`["A"]`),
		}
	}
	if msg := validateQuestion(valid()); msg != "" {
		t.Fatalf("valid question rejected: %s", msg)
	}
	five, zero := 5, 0
	cases := map[string]func(*questionPayload){
		"unknown type":       func(q *questionPayload) { q.Type = "essay" },
		"null prompt":        func(q *questionPayload) { q.Prompt = json.RawMessage(`null`) },
		"missing answer key": func(q *questionPayload) { q.Answer = nil },
		"one option":         func(q *questionPayload) { q.Options = json.RawMessage(`[{"id":"A","text":"Yes"}]`) },
		"duplicate option ids": func(q *questionPayload) {
			q.Options = json.RawMessage(`[{"id":"A","text":"Yes"},{"id":"A","text":"No"}]`)
		},
		"key not an option":       func(q *questionPayload) { q.Answer = json.RawMessage(`["C"]`) },
		"single with two answers": func(q *questionPayload) { q.Answer = json.RawMessage(`["A","B"]`) },
		"difficulty too low":      func(q *questionPayload) { q.Difficulty = &zero },
		"numeric with options": func(q *questionPayload) {
			q.Type = "numeric"
			q.Answer = json.RawMessage(`{"value":1}`)
		},
		"numeric without value": func(q *questionPayload) {
			q.Type, q.Options = "numeric", nil
			q.Answer = json.RawMessage(`{"tolerance":1}`)
		},
		"numeric negative tolerance": func(q *questionPayload) {
			q.Type, q.Options = "numeric", nil
			q.Answer = json.RawMessage(`{"value":1,"tolerance":-1}`)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			q := valid()
			mutate(&q)
			if validateQuestion(q) == "" {
				t.Fatal("invalid question was accepted")
			}
		})
	}
	q := valid()
	q.Difficulty = &five
	if msg := validateQuestion(q); msg != "" {
		t.Fatalf("difficulty 5 rejected: %s", msg)
	}
}

func TestParseOptionalCSVNumber(t *testing.T) {
	if value, err := parseOptionalCSVNumber("  "); value != nil || err != nil {
		t.Fatalf("blank should be nil, got %v %v", value, err)
	}
	if value, err := parseOptionalCSVNumber(" 85.5 "); err != nil || *value != 85.5 {
		t.Fatalf("expected 85.5, got %v %v", value, err)
	}
	for _, bad := range []string{"-1", "abc", "NaN", "Inf", "100000"} {
		if _, err := parseOptionalCSVNumber(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestPathIDsValid(t *testing.T) {
	mux := http.NewServeMux()
	var result bool
	mux.HandleFunc("GET /things/{thingID}/parts/{partID}", func(w http.ResponseWriter, r *http.Request) { result = pathIDsValid(r) })
	mux.HandleFunc("GET /plain", func(w http.ResponseWriter, r *http.Request) { result = pathIDsValid(r) })
	const id = "0b6f8f7e-1a52-4f0e-9a51-6d9a1c2b3c01"
	cases := map[string]bool{
		"/things/" + id + "/parts/" + id: true,
		"/things/" + id + "/parts/nope":  false,
		"/things/123/parts/" + id:        false,
		"/plain":                         true,
	}
	for path, want := range cases {
		result = !want
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
		if result != want {
			t.Errorf("pathIDsValid(%s) = %v, want %v", path, result, want)
		}
	}
}
