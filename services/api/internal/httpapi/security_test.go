package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestCurriculumIsIsolatedBetweenSchools(t *testing.T) {
	server := newTestServer(t)
	_, schoolA := seedSchool(t, server)
	_, schoolB := seedSchool(t, server)

	official := map[string]string{"name": "Ethiopian Curriculum", "provider": "Ministry of Education", "version_label": unique("2026"), "language_code": "en"}
	curriculumA := schoolA.create("/curricula", official)
	// Each school owns its own copy; the same version label no longer collides.
	curriculumB := schoolB.create("/curricula", official)
	if curriculumA == curriculumB {
		t.Fatal("schools must get separate curriculum records")
	}
	subjectA := schoolA.create("/curricula/"+curriculumA+"/subjects", map[string]string{"name": "Mathematics", "grade_level": "12"})
	skillA := schoolA.create("/subjects/"+subjectA+"/nodes", map[string]string{"node_type": "skill", "title": "Limits"})
	otherSkillA := schoolA.create("/subjects/"+subjectA+"/nodes", map[string]string{"node_type": "skill", "title": "Derivatives"})
	schoolA.call("POST", "/curriculum-mappings/review", map[string]any{"source_node_id": skillA, "target_node_id": otherSkillA, "mapping_type": "prerequisite"}).expect(http.StatusOK)

	listedByB := schoolB.call("GET", "/curricula", nil).expect(http.StatusOK).list()
	if containsID(listedByB, curriculumA) {
		t.Error("school B can list school A's curriculum")
	}
	if !containsID(listedByB, curriculumB) {
		t.Error("school B cannot list its own curriculum")
	}
	schoolB.call("GET", "/curricula/"+curriculumA+"/subjects", nil).expect(http.StatusNotFound)
	schoolB.call("POST", "/curricula/"+curriculumA+"/subjects", map[string]string{"name": "Injected"}).expect(http.StatusNotFound)
	schoolB.call("GET", "/subjects/"+subjectA+"/nodes", nil).expect(http.StatusNotFound)
	schoolB.call("POST", "/subjects/"+subjectA+"/nodes", map[string]string{"node_type": "skill", "title": "Injected"}).expect(http.StatusNotFound)
	schoolB.call("POST", "/questions", map[string]any{"question_type": "text", "prompt": map[string]string{"text": "Borrowed skill"}, "answer_key": map[string]string{"guide": "x"}, "skill_tags": []map[string]any{{"skill_id": skillA, "weight": 1}}}).expect(http.StatusBadRequest)
	schoolB.call("POST", "/curriculum-mappings/review", map[string]any{"source_node_id": skillA, "target_node_id": otherSkillA, "mapping_type": "related"}).expect(http.StatusNotFound)
	if len(schoolB.call("GET", "/curriculum-mappings", nil).expect(http.StatusOK).list()) != 0 {
		t.Error("school B can list school A's curriculum mappings")
	}
	if len(schoolA.call("GET", "/curriculum-mappings", nil).expect(http.StatusOK).list()) != 1 {
		t.Error("school A should see its own mapping")
	}
}

func TestPlatformCurriculumIsSharedReadOnly(t *testing.T) {
	server := newTestServer(t)
	_, schoolA := seedSchool(t, server)
	_, schoolB := seedSchool(t, server)
	ctx := context.Background()
	var curriculumID, subjectID, skillID string
	if err := testDB.QueryRow(ctx, `INSERT INTO curricula(name,provider,version_label) VALUES('Platform Library','FURII',$1) RETURNING id::text`, unique("v")).Scan(&curriculumID); err != nil {
		t.Fatal(err)
	}
	if err := testDB.QueryRow(ctx, `INSERT INTO subjects(curriculum_id,name) VALUES($1,'Physics') RETURNING id::text`, curriculumID).Scan(&subjectID); err != nil {
		t.Fatal(err)
	}
	if err := testDB.QueryRow(ctx, `INSERT INTO curriculum_nodes(subject_id,node_type,title) VALUES($1,'skill','Motion') RETURNING id::text`, subjectID).Scan(&skillID); err != nil {
		t.Fatal(err)
	}
	for _, school := range []*apiClient{schoolA, schoolB} {
		var found map[string]any
		for _, item := range school.call("GET", "/curricula", nil).expect(http.StatusOK).list() {
			if item["id"] == curriculumID {
				found = item
			}
		}
		if found == nil || found["read_only"] != true || found["owner"] != "platform" {
			t.Fatalf("platform curriculum should be listed read-only, got %v", found)
		}
		school.call("GET", "/curricula/"+curriculumID+"/subjects", nil).expect(http.StatusOK)
		school.call("GET", "/subjects/"+subjectID+"/nodes", nil).expect(http.StatusOK)
	}
	if code := schoolA.call("POST", "/curricula/"+curriculumID+"/subjects", map[string]string{"name": "Chemistry"}).expect(http.StatusForbidden).errorCode(); code != "CURRICULUM_READ_ONLY" {
		t.Fatalf("expected CURRICULUM_READ_ONLY, got %s", code)
	}
	schoolA.call("POST", "/subjects/"+subjectID+"/nodes", map[string]string{"node_type": "skill", "title": "Energy"}).expect(http.StatusForbidden)
	// Schools may tag their own questions with platform skills.
	schoolB.create("/questions", map[string]any{"question_type": "text", "prompt": map[string]string{"text": "Describe motion."}, "answer_key": map[string]string{"guide": "x"}, "skill_tags": []map[string]any{{"skill_id": skillID, "weight": 1}}})
}

func TestCrossSiteRequestsAreRejected(t *testing.T) {
	server := newTestServer(t)
	client := newClient(t, server)
	login := map[string]string{"email": "nobody@example.test", "password": "wrong-password-123"}
	if code := client.callWithHeaders("POST", "/auth/login", login, map[string]string{"Origin": "https://evil.example"}).expect(http.StatusForbidden).errorCode(); code != "ORIGIN_NOT_ALLOWED" {
		t.Fatalf("expected ORIGIN_NOT_ALLOWED, got %s", code)
	}
	client.callWithHeaders("POST", "/auth/login", login, map[string]string{"Sec-Fetch-Site": "cross-site"}).expect(http.StatusForbidden)
	// The API's own origin and non-browser clients reach normal authentication.
	client.callWithHeaders("POST", "/auth/login", login, map[string]string{"Origin": server.URL}).expect(http.StatusUnauthorized)
	client.call("POST", "/auth/login", login).expect(http.StatusUnauthorized)
	// Safe requests are not blocked by fetch metadata alone.
	client.callWithHeaders("GET", "/me", nil, map[string]string{"Sec-Fetch-Site": "cross-site"}).expect(http.StatusUnauthorized)
}

func TestFailedLoginsAreLimitedPerAccount(t *testing.T) {
	limits := testRateLimits()
	limits.loginFailuresPerAccount = limitRule{limit: 3, window: time.Minute}
	server := newTestServerWithLimits(t, limits)
	_, email, _ := seedSchoolAccount(t, server)
	_, otherEmail, _ := seedSchoolAccount(t, server)
	attacker := newClient(t, server)
	for i := 0; i < 3; i++ {
		attacker.call("POST", "/auth/login", map[string]string{"email": email, "password": "wrong-password-123"}).expect(http.StatusUnauthorized)
	}
	// Even the correct password is refused while the account is locked.
	if code := attacker.call("POST", "/auth/login", map[string]string{"email": email, "password": testPassword}).expect(http.StatusTooManyRequests).errorCode(); code != "RATE_LIMITED" {
		t.Fatalf("expected RATE_LIMITED, got %s", code)
	}
	newClient(t, server).login(otherEmail)
}

func TestLoginsAreLimitedPerAddress(t *testing.T) {
	limits := testRateLimits()
	limits.loginPerIP = limitRule{limit: 2, window: time.Minute}
	server := newTestServerWithLimits(t, limits)
	client := newClient(t, server)
	login := map[string]string{"email": "nobody@example.test", "password": "wrong-password-123"}
	client.call("POST", "/auth/login", login).expect(http.StatusUnauthorized)
	client.call("POST", "/auth/login", login).expect(http.StatusUnauthorized)
	client.call("POST", "/auth/login", login).expect(http.StatusTooManyRequests)
}

func TestChangePasswordRevokesOtherSessions(t *testing.T) {
	server := newTestServer(t)
	_, email, current := seedSchoolAccount(t, server)
	other := newClient(t, server)
	other.login(email)

	if code := current.call("POST", "/auth/password", map[string]string{"current_password": "not-my-password", "new_password": "a-brand-new-password"}).expect(http.StatusBadRequest).errorCode(); code != "INVALID_CURRENT_PASSWORD" {
		t.Fatalf("expected INVALID_CURRENT_PASSWORD, got %s", code)
	}
	current.call("POST", "/auth/password", map[string]string{"current_password": testPassword, "new_password": "short"}).expect(http.StatusBadRequest)
	current.call("POST", "/auth/password", map[string]string{"current_password": testPassword, "new_password": "a-brand-new-password"}).expect(http.StatusOK)

	current.call("GET", "/school", nil).expect(http.StatusOK)
	other.call("GET", "/school", nil).expect(http.StatusUnauthorized)
	newClient(t, server).call("POST", "/auth/login", map[string]string{"email": email, "password": testPassword}).expect(http.StatusUnauthorized)
	newClient(t, server).call("POST", "/auth/login", map[string]string{"email": email, "password": "a-brand-new-password"}).expect(http.StatusOK)
}

func teacherEmail(admin *apiClient, teacherID string) string {
	admin.t.Helper()
	for _, member := range admin.call("GET", "/staff", nil).expect(http.StatusOK).list() {
		if member["id"] == teacherID {
			return member["email"].(string)
		}
	}
	admin.t.Fatalf("teacher %s not in staff list", teacherID)
	return ""
}

func TestAdminIssuedPasswordResetForTeacher(t *testing.T) {
	server := newTestServer(t)
	_, admin := seedSchool(t, server)
	_, otherAdmin := seedSchool(t, server)
	teacherID, teacher := inviteTeacher(server, admin)
	email := teacherEmail(admin, teacherID)

	otherAdmin.call("POST", "/staff/"+teacherID+"/password-reset", map[string]any{}).expect(http.StatusNotFound)
	adminID, _ := admin.call("GET", "/me", nil).expect(http.StatusOK).object()["user_id"].(string)
	admin.call("POST", "/staff/"+adminID+"/password-reset", map[string]any{}).expect(http.StatusBadRequest)

	superseded, _ := admin.call("POST", "/staff/"+teacherID+"/password-reset", map[string]any{}).expect(http.StatusCreated).object()["token"].(string)
	issued := admin.call("POST", "/staff/"+teacherID+"/password-reset", map[string]any{}).expect(http.StatusCreated).object()
	token, _ := issued["token"].(string)
	if issued["email"] != email || token == "" || token == superseded {
		t.Fatalf("unexpected reset response: %v", issued)
	}

	anonymous := newClient(t, server)
	anonymous.call("POST", "/auth/password-reset/complete", map[string]string{"token": superseded, "new_password": "teacher-new-password"}).expect(http.StatusBadRequest)
	anonymous.call("POST", "/auth/password-reset/complete", map[string]string{"token": token, "new_password": "short"}).expect(http.StatusBadRequest)
	anonymous.call("POST", "/auth/password-reset/complete", map[string]string{"token": token, "new_password": "teacher-new-password"}).expect(http.StatusOK)
	if code := anonymous.call("POST", "/auth/password-reset/complete", map[string]string{"token": token, "new_password": "another-new-password"}).expect(http.StatusBadRequest).errorCode(); code != "RESET_INVALID" {
		t.Fatalf("a used link must be rejected, got %s", code)
	}

	teacher.call("GET", "/teacher/classes", nil).expect(http.StatusUnauthorized)
	newClient(t, server).call("POST", "/auth/login", map[string]string{"email": email, "password": "teacher-new-password"}).expect(http.StatusOK)
}

func TestExpiredPasswordResetIsRejected(t *testing.T) {
	server := newTestServer(t)
	_, admin := seedSchool(t, server)
	teacherID, _ := inviteTeacher(server, admin)
	token, _ := admin.call("POST", "/staff/"+teacherID+"/password-reset", map[string]any{}).expect(http.StatusCreated).object()["token"].(string)
	if _, err := testDB.Exec(context.Background(), `UPDATE password_reset_tokens SET expires_at=now()-interval '1 minute' WHERE user_id=$1`, teacherID); err != nil {
		t.Fatal(err)
	}
	newClient(t, server).call("POST", "/auth/password-reset/complete", map[string]string{"token": token, "new_password": "teacher-new-password"}).expect(http.StatusBadRequest)
}

func TestAdminIssuedPasswordResetForLearner(t *testing.T) {
	server := newTestServer(t)
	_, admin := seedSchool(t, server)
	classID := createClass(admin)
	studentID, learner := enrollLearner(server, admin, classID)
	noAccount := admin.create("/students", map[string]string{"student_number": unique("S"), "legal_name": "No Account"})

	admin.call("POST", "/students/"+noAccount+"/password-reset", map[string]any{}).expect(http.StatusConflict)
	issued := admin.call("POST", "/students/"+studentID+"/password-reset", map[string]any{}).expect(http.StatusCreated).object()
	newClient(t, server).call("POST", "/auth/password-reset/complete", map[string]string{"token": issued["token"].(string), "new_password": "learner-new-password"}).expect(http.StatusOK)
	learner.call("GET", "/student/assessments", nil).expect(http.StatusUnauthorized)
	newClient(t, server).call("POST", "/auth/login", map[string]string{"email": issued["email"].(string), "password": "learner-new-password"}).expect(http.StatusOK)
}

func TestPlatformOperatorFlagAndAdminRecovery(t *testing.T) {
	server := newTestServer(t)
	_, operatorEmail, operator := seedSchoolAccount(t, server)
	_, lockedOutEmail, lockedOut := seedSchoolAccount(t, server)
	t.Setenv("DEMO_REQUEST_ADMIN_EMAILS", "someone-else@example.test, "+operatorEmail)

	if flag := operator.call("GET", "/me", nil).expect(http.StatusOK).object()["platform_operator"]; flag != true {
		t.Fatalf("operator should be flagged, got %v", flag)
	}
	if flag := lockedOut.call("GET", "/me", nil).expect(http.StatusOK).object()["platform_operator"]; flag != false {
		t.Fatalf("ordinary admin must not be flagged, got %v", flag)
	}
	lockedOut.call("GET", "/platform/demo-requests", nil).expect(http.StatusForbidden)
	lockedOut.call("POST", "/platform/password-resets", map[string]string{"email": operatorEmail}).expect(http.StatusForbidden)

	operator.call("POST", "/platform/password-resets", map[string]string{"email": "missing@example.test"}).expect(http.StatusNotFound)
	token, _ := operator.call("POST", "/platform/password-resets", map[string]string{"email": lockedOutEmail}).expect(http.StatusCreated).object()["token"].(string)
	newClient(t, server).call("POST", "/auth/password-reset/complete", map[string]string{"token": token, "new_password": "recovered-admin-password"}).expect(http.StatusOK)
	newClient(t, server).call("POST", "/auth/login", map[string]string{"email": lockedOutEmail, "password": "recovered-admin-password"}).expect(http.StatusOK)
}

func TestPurgeExpiredRecords(t *testing.T) {
	server := newTestServer(t)
	_, email, admin := seedSchoolAccount(t, server)
	admin.call("GET", "/school", nil).expect(http.StatusOK)
	ctx := context.Background()
	var userID string
	if err := testDB.QueryRow(ctx, `SELECT id::text FROM users WHERE email=$1`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec(ctx, `INSERT INTO sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()-interval '3 days')`, userID, []byte(unique("expired"))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := purgeExpiredRecords(ctx, testDB); err != nil {
		t.Fatal(err)
	}
	var expired int
	if err := testDB.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1 AND expires_at < now()`, userID).Scan(&expired); err != nil {
		t.Fatal(err)
	}
	if expired != 0 {
		t.Fatalf("expired sessions remain: %d", expired)
	}
	admin.call("GET", "/school", nil).expect(http.StatusOK)
}

func TestParseSameSite(t *testing.T) {
	cases := []struct {
		raw     string
		secure  bool
		want    http.SameSite
		wantErr bool
	}{
		{"", true, http.SameSiteNoneMode, false},
		{"", false, http.SameSiteLaxMode, false},
		{"Lax", true, http.SameSiteLaxMode, false},
		{"strict", true, http.SameSiteStrictMode, false},
		{"none", true, http.SameSiteNoneMode, false},
		{"none", false, http.SameSiteLaxMode, true},
		{"sometimes", true, http.SameSiteNoneMode, true},
	}
	for _, c := range cases {
		got, err := parseSameSite(c.raw, c.secure)
		if got != c.want || (err != nil) != c.wantErr {
			t.Errorf("parseSameSite(%q, %v) = %v, %v", c.raw, c.secure, got, err)
		}
	}
}

func TestRateLimiterWindows(t *testing.T) {
	limiter := newRateLimiter()
	rule := limitRule{limit: 2, window: time.Minute}
	start := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	if ok, _ := limiter.hit("k", rule, start); !ok {
		t.Fatal("first hit should pass")
	}
	if blocked, _ := limiter.blocked("k", rule, start); blocked {
		t.Fatal("one of two hits should not block")
	}
	limiter.hit("k", rule, start)
	if ok, retry := limiter.hit("k", rule, start.Add(10*time.Second)); ok || retry != 50*time.Second {
		t.Fatalf("third hit should fail with 50s retry, got %v %v", ok, retry)
	}
	if ok, _ := limiter.hit("k", rule, start.Add(time.Minute)); !ok {
		t.Fatal("a new window should reset the count")
	}
	limiter.reset("k")
	if blocked, _ := limiter.blocked("k", rule, start.Add(time.Minute)); blocked {
		t.Fatal("reset should clear the key")
	}
}

func TestClientIPHonoursTrustedProxyOnly(t *testing.T) {
	request, _ := http.NewRequest("GET", "/", nil)
	request.RemoteAddr = "10.0.0.5:4321"
	request.Header.Set("X-Forwarded-For", "196.188.1.9, 10.0.0.1")
	if ip := (&API{}).clientIP(request); ip != "10.0.0.5" {
		t.Fatalf("untrusted proxy headers must be ignored, got %s", ip)
	}
	if ip := (&API{trustProxy: true}).clientIP(request); ip != "196.188.1.9" {
		t.Fatalf("trusted proxy should use the first forwarded address, got %s", ip)
	}
}
