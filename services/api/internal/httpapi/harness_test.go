package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/furii/school-os/services/api/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Integration tests run against a real PostgreSQL server. Set
// TEST_DATABASE_URL to a superuser connection string; the harness creates a
// fresh UTF-8 database for the run, applies every migration, and drops the
// database afterwards (set FURII_TEST_KEEP_DB=1 to keep it for debugging, and
// FURII_TEST_LOG=1 to print API server logs).
// Without TEST_DATABASE_URL, database tests are skipped.

const testPassword = "correct-horse-battery-staple"

var testDB *pgxpool.Pool

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	adminURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		return m.Run()
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect to TEST_DATABASE_URL:", err)
		return 1
	}
	defer admin.Close(ctx)

	// Migration 00010 revokes privileges from Supabase's roles, so they must
	// exist on a plain PostgreSQL server too. Roles belong to the whole server
	// and go test runs packages at once, so another package may create one
	// between the check and the CREATE.
	for _, role := range []string{"postgres", "anon", "authenticated"} {
		statement := fmt.Sprintf(`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%s') THEN CREATE ROLE %s NOLOGIN; END IF; EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL; END $$`, role, pgx.Identifier{role}.Sanitize())
		if _, err := admin.Exec(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, "prepare role", role, ":", err)
			return 1
		}
	}

	name := fmt.Sprintf("furii_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name+" ENCODING 'UTF8' LC_COLLATE 'C' LC_CTYPE 'C' TEMPLATE template0"); err != nil {
		fmt.Fprintln(os.Stderr, "create test database:", err)
		return 1
	}
	if os.Getenv("FURII_TEST_KEEP_DB") == "" {
		defer admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	} else {
		fmt.Fprintln(os.Stderr, "keeping test database", name)
	}

	config, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse TEST_DATABASE_URL:", err)
		return 1
	}
	config.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open test database:", err)
		return 1
	}
	defer pool.Close()
	if err := migrations.Up(ctx, pool); err != nil {
		fmt.Fprintln(os.Stderr, "migrate test database:", err)
		return 1
	}
	testDB = pool
	return m.Run()
}

func requireDB(t *testing.T) {
	t.Helper()
	if testDB == nil {
		t.Skip("set TEST_DATABASE_URL to run database integration tests")
	}
}

// testRateLimits keeps limits out of the way of ordinary tests, which sign in
// many times from one address.
func testRateLimits() rateLimits {
	generous := limitRule{limit: 100000, window: time.Minute}
	return rateLimits{loginPerIP: generous, loginFailuresPerAccount: generous, tokenRedemptionsPerIP: generous, demoRequestsPerIP: generous, passwordFailuresPerUser: generous, startCodeFailuresPerUser: generous}
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newTestServerWithLimits(t, testRateLimits())
}

func newTestServerWithLimits(t *testing.T, limits rateLimits) *httptest.Server {
	t.Helper()
	requireDB(t)
	var logOutput io.Writer = io.Discard
	if os.Getenv("FURII_TEST_LOG") != "" {
		logOutput = os.Stderr
	}
	logger := slog.New(slog.NewTextHandler(logOutput, nil))
	service := auth.NewService(testDB, []byte("test-only-session-hash-key-32-bytes!!"), time.Hour)
	server := httptest.NewServer(newAPI(service, testDB, false, time.Hour, logger, limits).routes())
	t.Cleanup(server.Close)
	return server
}

var uniqueCounter atomic.Int64

func unique(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), uniqueCounter.Add(1))
}

func uniqueEmail(prefix string) string { return unique(prefix) + "@example.test" }

// apiClient is one signed-in (or anonymous) browser with its own cookie jar.
type apiClient struct {
	t      *testing.T
	base   string
	client *http.Client
}

func newClient(t *testing.T, server *httptest.Server) *apiClient {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &apiClient{t: t, base: server.URL, client: &http.Client{Jar: jar}}
}

// rawCSV is a request body sent as a CSV file instead of JSON.
type rawCSV string

type apiResponse struct {
	t      *testing.T
	Status int
	Body   []byte
}

func (c *apiClient) call(method, path string, body any) apiResponse {
	c.t.Helper()
	return c.callWithHeaders(method, path, body, nil)
}

func (c *apiClient) callWithHeaders(method, path string, body any, headers map[string]string) apiResponse {
	c.t.Helper()
	var reader io.Reader
	contentType := "application/json"
	if csvBody, ok := body.(rawCSV); ok {
		reader, contentType = bytes.NewReader([]byte(csvBody)), "text/csv"
	} else if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("encode %s %s: %v", method, path, err)
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, c.base+"/api/v1"+path, reader)
	if err != nil {
		c.t.Fatalf("build %s %s: %v", method, path, err)
	}
	if body != nil {
		request.Header.Set("Content-Type", contentType)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := c.client.Do(request)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		c.t.Fatalf("read %s %s: %v", method, path, err)
	}
	return apiResponse{t: c.t, Status: response.StatusCode, Body: raw}
}

func (c *apiClient) login(email string) {
	c.t.Helper()
	c.call("POST", "/auth/login", map[string]string{"email": email, "password": testPassword}).expect(http.StatusOK)
}

// create POSTs a body, expects 201, and returns data.id.
func (c *apiClient) create(path string, body any) string {
	c.t.Helper()
	id, _ := c.call("POST", path, body).expect(http.StatusCreated).object()["id"].(string)
	if id == "" {
		c.t.Fatalf("POST %s returned no id", path)
	}
	return id
}

func (r apiResponse) expect(status int) apiResponse {
	r.t.Helper()
	if r.Status != status {
		r.t.Fatalf("expected HTTP %d, got %d: %s", status, r.Status, r.Body)
	}
	return r
}

func (r apiResponse) envelope() map[string]any {
	r.t.Helper()
	var body map[string]any
	if err := json.Unmarshal(r.Body, &body); err != nil {
		r.t.Fatalf("response is not a JSON object: %v: %s", err, r.Body)
	}
	return body
}

func (r apiResponse) object() map[string]any {
	r.t.Helper()
	data, ok := r.envelope()["data"].(map[string]any)
	if !ok {
		r.t.Fatalf("response data is not an object: %s", r.Body)
	}
	return data
}

func (r apiResponse) list() []map[string]any {
	r.t.Helper()
	raw, ok := r.envelope()["data"].([]any)
	if !ok {
		r.t.Fatalf("response data is not a list: %s", r.Body)
	}
	items := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		object, ok := item.(map[string]any)
		if !ok {
			r.t.Fatalf("list item is not an object: %s", r.Body)
		}
		items = append(items, object)
	}
	return items
}

func (r apiResponse) errorCode() string {
	r.t.Helper()
	failure, _ := r.envelope()["error"].(map[string]any)
	code, _ := failure["code"].(string)
	return code
}

func containsID(items []map[string]any, id string) bool {
	for _, item := range items {
		if item["id"] == id {
			return true
		}
	}
	return false
}

// seedSchool creates a school and its first administrator directly in the
// database, as cmd/bootstrap does, and returns a signed-in admin client.
func seedSchool(t *testing.T, server *httptest.Server) (string, *apiClient) {
	t.Helper()
	schoolID, _, admin := seedSchoolAccount(t, server)
	return schoolID, admin
}

// seedSchoolAccount is seedSchool that also returns the administrator's email.
func seedSchoolAccount(t *testing.T, server *httptest.Server) (string, string, *apiClient) {
	t.Helper()
	ctx := context.Background()
	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	email := uniqueEmail("admin")
	var schoolID, userID string
	if err := testDB.QueryRow(ctx, `INSERT INTO schools(name) VALUES($1) RETURNING id::text`, unique("School")).Scan(&schoolID); err != nil {
		t.Fatal(err)
	}
	if err := testDB.QueryRow(ctx, `INSERT INTO users(school_id,email,password_hash,display_name) VALUES($1,$2,$3,'Test Admin') RETURNING id::text`, schoolID, email, hash).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec(ctx, `INSERT INTO user_roles(user_id,role) VALUES($1,'school_admin')`, userID); err != nil {
		t.Fatal(err)
	}
	admin := newClient(t, server)
	admin.login(email)
	return schoolID, email, admin
}

// createClass creates an academic year and a class in it.
func createClass(admin *apiClient) string {
	admin.t.Helper()
	yearID := admin.create("/academic-years", map[string]string{"name": unique("Year"), "starts_on": "2026-09-01", "ends_on": "2027-07-01"})
	return admin.create("/classes", map[string]string{"name": unique("Section"), "grade_level": "8", "academic_year_id": yearID})
}

// inviteTeacher runs the real invitation flow and returns the teacher's user
// ID and a signed-in client.
func inviteTeacher(server *httptest.Server, admin *apiClient) (string, *apiClient) {
	t := admin.t
	t.Helper()
	email := uniqueEmail("teacher")
	token, _ := admin.call("POST", "/staff/invitations", map[string]string{"email": email}).expect(http.StatusCreated).object()["token"].(string)
	newClient(t, server).call("POST", "/staff/invitations/accept", map[string]string{"token": token, "display_name": "Test Teacher", "password": testPassword}).expect(http.StatusCreated)
	teacher := newClient(t, server)
	teacher.login(email)
	userID, _ := teacher.call("GET", "/me", nil).expect(http.StatusOK).object()["user_id"].(string)
	return userID, teacher
}

// enrollLearner creates a student in a class and activates their learner
// account through the real invitation flow.
func enrollLearner(server *httptest.Server, admin *apiClient, classID string) (string, *apiClient) {
	t := admin.t
	t.Helper()
	studentID := admin.create("/students", map[string]string{"student_number": unique("S"), "legal_name": "Test Learner", "class_id": classID})
	email := uniqueEmail("learner")
	token, _ := admin.call("POST", "/students/"+studentID+"/invitation", map[string]string{"email": email}).expect(http.StatusCreated).object()["token"].(string)
	newClient(t, server).call("POST", "/student-invitations/accept", map[string]string{"token": token, "display_name": "Test Learner", "password": testPassword}).expect(http.StatusCreated)
	learner := newClient(t, server)
	learner.login(email)
	return studentID, learner
}

// createSkill creates a curriculum, subject, and skill node.
func createSkill(admin *apiClient) string {
	admin.t.Helper()
	curriculumID := admin.create("/curricula", map[string]string{"name": "Test Curriculum", "provider": "Test Ministry", "version_label": unique("v"), "language_code": "en"})
	subjectID := admin.create("/curricula/"+curriculumID+"/subjects", map[string]string{"name": "Mathematics", "grade_level": "8"})
	return admin.create("/subjects/"+subjectID+"/nodes", map[string]string{"node_type": "skill", "title": "Add fractions"})
}

// publishQuestion creates a question tagged to skillID and publishes it.
func publishQuestion(admin *apiClient, skillID string, fields map[string]any) string {
	admin.t.Helper()
	body := map[string]any{
		"difficulty":    2,
		"language_code": "en",
		"source_name":   "Test fixture",
		"license_notes": "Test only",
		"skill_tags":    []map[string]any{{"skill_id": skillID, "weight": 1}},
	}
	for key, value := range fields {
		body[key] = value
	}
	id := admin.create("/questions", body)
	admin.call("POST", "/questions/"+id+"/publish", map[string]any{}).expect(http.StatusOK)
	return id
}

// publishAssessment builds, publishes, and assigns an assessment and returns
// the assessment ID and the class assignment ID.
func publishAssessment(admin *apiClient, classID string, questionIDs []string) (string, string) {
	admin.t.Helper()
	assessmentID := admin.create("/assessments", map[string]any{"title": unique("Quiz"), "instructions": "Answer every question.", "duration_minutes": 30, "question_ids": questionIDs})
	admin.call("POST", "/assessments/"+assessmentID+"/publish", map[string]any{}).expect(http.StatusOK)
	assignmentID := admin.create("/assessments/"+assessmentID+"/assignments", map[string]string{"class_id": classID})
	return assessmentID, assignmentID
}
