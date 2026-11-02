package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"slices"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// prepExpiredSQL matches attemptExpiredSQL: answers in flight at the deadline
	// get the same grace window as exams.
	prepExpiredSQL = `deadline_at + interval '30 seconds' < now()`
	// An untimed sitting left open this long is closed, so its answers still count.
	prepAbandonedSQL = `deadline_at IS NULL AND started_at < now() - interval '1 day'`
	// Practice weighs less than timed work because the student sees answers as they go.
	prepPracticeWeight = 0.5
	prepTimedWeight    = 1.0
	// A wrong answer returns for review after this long.
	prepReviewDelay = `1 day`

	defaultDrillQuestions = 10
	maxDrillQuestions     = 30
	defaultPaperQuestions = 50
	maxPaperQuestions     = 100
	defaultPaperMinutes   = 120
	maxPaperMinutes       = 240
)

func (a *API) registerPrepPracticeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/student/prep", a.student(a.studentPrepHub))
	mux.HandleFunc("POST /api/v1/student/prep/onboarding", a.student(a.completePrepOnboarding))
	mux.HandleFunc("POST /api/v1/student/prep/sessions", a.student(a.startPrepSession))
	mux.HandleFunc("GET /api/v1/student/prep/sessions/{sessionID}", a.student(a.getPrepSession))
	mux.HandleFunc("PUT /api/v1/student/prep/sessions/{sessionID}/answers/{questionID}", a.student(a.savePrepAnswer))
	mux.HandleFunc("POST /api/v1/student/prep/sessions/{sessionID}/finish", a.student(a.finishPrepSessionHandler))
	mux.HandleFunc("POST /api/v1/student/prep/mocks", a.student(a.startPrepMock))
	mux.HandleFunc("POST /api/v1/student/prep/mocks/{mockID}/papers", a.student(a.startPrepMockPaper))
}

// prepStudent resolves the signed-in learner, writing the error when there is none.
func (a *API) prepStudent(w http.ResponseWriter, r *http.Request, id auth.Identity) (string, bool) {
	studentID, err := a.learnerStudent(r, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 403, "STUDENT_PROFILE_UNAVAILABLE", "No active student profile is linked to this account.", nil)
		return "", false
	}
	if err != nil {
		a.serverError(w, r, err)
		return "", false
	}
	return studentID, true
}

// selectPrepQuestions picks a sitting's questions. It returns fewer than `limit`
// when the bank runs out. Exam-only questions never appear in practice.
func selectPrepQuestions(ctx context.Context, tx pgx.Tx, schoolID, studentID, mode, paper string, skillID *string, limit int) ([]string, error) {
	order := `(h.question_id IS NULL) DESC,COALESCE(h.is_correct,false) ASC,random()`
	extra := ``
	switch mode {
	case "weak":
		order = `(h.is_correct IS TRUE AND h.answered_at > now() - interval '7 days') ASC,COALESCE((SELECT min(m.estimated_mastery) FROM mastery m JOIN question_skill_tags t ON t.skill_id=m.skill_id WHERE t.question_id=q.id),50) ASC,random()`
	case "review":
		order = `h.answered_at ASC`
		extra = ` AND h.is_correct IS FALSE AND h.answered_at < now() - interval '` + prepReviewDelay + `'`
	case "timed":
		order = `random()`
	}
	rows, err := tx.Query(ctx, `
		WITH history AS (
			SELECT DISTINCT ON (i.question_id) i.question_id,i.is_correct,i.answered_at
			FROM prep_session_items i JOIN prep_sessions ps ON ps.id=i.session_id
			WHERE ps.student_id=$3 AND i.answered_at IS NOT NULL
			ORDER BY i.question_id,i.answered_at DESC
		), mastery AS (
			SELECT DISTINCT ON (skill_id) skill_id,estimated_mastery FROM mastery_snapshots WHERE student_id=$3 ORDER BY skill_id,calculated_at DESC
		)
		SELECT q.id::text FROM questions q LEFT JOIN history h ON h.question_id=q.id
		WHERE q.school_id=$1 AND q.status='published' AND q.question_type='single_choice' AND NOT q.exam_only
		AND EXISTS(SELECT 1 FROM question_skill_tags t JOIN curriculum_nodes n ON n.id=t.skill_id JOIN subjects s ON s.id=n.subject_id
		           WHERE t.question_id=q.id AND s.national_paper=$2 AND ($4::uuid IS NULL OR t.skill_id=$4))`+extra+`
		ORDER BY `+order+` LIMIT $5`, schoolID, paper, studentID, skillID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	questionIDs := []string{}
	for rows.Next() {
		var questionID string
		if err = rows.Scan(&questionID); err != nil {
			return nil, err
		}
		questionIDs = append(questionIDs, questionID)
	}
	return questionIDs, rows.Err()
}

// createPrepSession stores a sitting and gives each question its own option order.
func createPrepSession(ctx context.Context, tx pgx.Tx, schoolID, studentID, mode, paper string, mockID *string, questionIDs []string, minutes int) (string, error) {
	var deadline *time.Time
	if mode == "timed" {
		value := time.Now().Add(time.Duration(minutes) * time.Minute)
		deadline = &value
	}
	var sessionID string
	err := tx.QueryRow(ctx, `INSERT INTO prep_sessions(school_id,student_id,mock_id,mode,paper,deadline_at,question_count) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id::text`, schoolID, studentID, mockID, mode, paper, deadline, len(questionIDs)).Scan(&sessionID)
	if err != nil {
		return "", err
	}
	for index, questionID := range questionIDs {
		order := slices.Clone(prepOptionIDs)
		var stored []byte
		if err = tx.QueryRow(ctx, `SELECT options FROM questions WHERE id=$1`, questionID).Scan(&stored); err != nil {
			return "", err
		}
		var options []struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(stored, &options) == nil && len(options) > 0 {
			order = order[:0]
			for _, option := range options {
				order = append(order, option.ID)
			}
		}
		rand.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
		if _, err = tx.Exec(ctx, `INSERT INTO prep_session_items(session_id,school_id,question_id,position,option_order) VALUES($1,$2,$3,$4,$5)`, sessionID, schoolID, questionID, index+1, order); err != nil {
			return "", err
		}
	}
	return sessionID, nil
}

func (a *API) startPrepSession(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID, ok := a.prepStudent(w, r, id)
	if !ok {
		return
	}
	var req struct {
		Mode            string  `json:"mode"`
		Paper           string  `json:"paper"`
		SkillID         *string `json:"skill_id"`
		QuestionCount   int     `json:"question_count"`
		DurationMinutes int     `json:"duration_minutes"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	if !slices.Contains([]string{"drill", "weak", "review", "timed"}, req.Mode) || !validPaper(req.Paper) || req.SkillID != nil && !uuidPattern.MatchString(*req.SkillID) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a practice type and a paper.", nil)
		return
	}
	limit, most := defaultDrillQuestions, maxDrillQuestions
	if req.Mode == "timed" {
		limit, most = defaultPaperQuestions, maxPaperQuestions
	}
	if req.QuestionCount != 0 {
		limit = req.QuestionCount
	}
	minutes := req.DurationMinutes
	if minutes == 0 {
		minutes = defaultPaperMinutes
	}
	if limit < 1 || limit > most || minutes < 5 || minutes > maxPaperMinutes {
		writeError(w, 400, "VALIDATION_ERROR", "That number of questions or minutes is out of range.", nil)
		return
	}
	if err := a.closeOpenPractice(r.Context(), id.SchoolID, studentID); err != nil {
		a.serverError(w, r, err)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if req.Mode == "timed" {
		var active string
		err = tx.QueryRow(r.Context(), `SELECT id::text FROM prep_sessions WHERE student_id=$1 AND school_id=$2 AND status='in_progress' AND mode='timed' LIMIT 1`, studentID, id.SchoolID).Scan(&active)
		if err == nil {
			writeError(w, 409, "SESSION_ACTIVE", "Finish your timed paper before starting another.", nil)
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			a.serverError(w, r, err)
			return
		}
	}
	questionIDs, err := selectPrepQuestions(r.Context(), tx, id.SchoolID, studentID, req.Mode, req.Paper, req.SkillID, limit)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if len(questionIDs) == 0 {
		if req.Mode == "review" {
			writeError(w, 409, "NOTHING_TO_REVIEW", "No questions are due for review in this paper yet.", nil)
		} else {
			writeError(w, 409, "NO_QUESTIONS", "This paper has no approved practice questions yet. Ask your teacher.", nil)
		}
		return
	}
	sessionID, err := createPrepSession(r.Context(), tx, id.SchoolID, studentID, req.Mode, req.Paper, nil, questionIDs, minutes)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]any{"id": sessionID, "question_count": len(questionIDs)}})
}

// closeOpenPractice finishes the student's untimed sittings when they start
// another, so their answers count. Timed papers keep running.
func (a *API) closeOpenPractice(ctx context.Context, schoolID, studentID string) error {
	rows, err := a.db.Query(ctx, `SELECT id::text FROM prep_sessions WHERE student_id=$1 AND school_id=$2 AND status='in_progress' AND mode<>'timed'`, studentID, schoolID)
	if err != nil {
		return err
	}
	open := []string{}
	for rows.Next() {
		var sessionID string
		if err = rows.Scan(&sessionID); err != nil {
			rows.Close()
			return err
		}
		open = append(open, sessionID)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, sessionID := range open {
		if err = finishPrepSessionByID(ctx, a.db, schoolID, sessionID); err != nil {
			return err
		}
	}
	return nil
}

// finishPrepSessionTx grades a sitting and records its evidence. Unanswered
// questions on a timed paper count as wrong, as they do in the exam; skipped
// practice questions simply do not count.
func finishPrepSessionTx(ctx context.Context, tx pgx.Tx, schoolID, sessionID string) error {
	var status, mode, studentID string
	var mockID *string
	err := tx.QueryRow(ctx, `SELECT status,mode,student_id::text,mock_id::text FROM prep_sessions WHERE id=$1 AND school_id=$2 FOR UPDATE`, sessionID, schoolID).Scan(&status, &mode, &studentID, &mockID)
	if err != nil {
		return err
	}
	if status != "in_progress" {
		return nil
	}
	weight := prepPracticeWeight
	if mode == "timed" {
		weight = prepTimedWeight
		if _, err = tx.Exec(ctx, `UPDATE prep_session_items i SET is_correct=(i.answer IS NOT NULL AND q.answer_key->>0=i.answer),answered_at=COALESCE(i.answered_at,now())
			FROM questions q WHERE q.id=i.question_id AND i.session_id=$1`, sessionID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE prep_sessions SET status='finished',finished_at=now(),correct_count=(SELECT count(*) FROM prep_session_items WHERE session_id=$1 AND is_correct) WHERE id=$1`, sessionID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO skill_evidence(school_id,student_id,skill_id,prep_session_id,question_id,correctness,evidence_weight)
		SELECT ps.school_id,ps.student_id,t.skill_id,ps.id,i.question_id,CASE WHEN i.is_correct THEN 1 ELSE 0 END,t.weight*$2::numeric
		FROM prep_sessions ps JOIN prep_session_items i ON i.session_id=ps.id JOIN question_skill_tags t ON t.question_id=i.question_id
		WHERE ps.id=$1 AND i.is_correct IS NOT NULL
		ON CONFLICT (prep_session_id,question_id,skill_id) DO NOTHING`, sessionID, weight)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if err = recalculateMastery(ctx, tx, schoolID, studentID, ""); err != nil {
			return err
		}
	}
	if mockID != nil {
		_, err = tx.Exec(ctx, `UPDATE prep_mocks m SET status='finished',finished_at=now() WHERE m.id=$1 AND m.status='in_progress'
			AND (SELECT count(*) FROM prep_sessions ps WHERE ps.mock_id=m.id AND ps.status='finished')=6`, *mockID)
	}
	return err
}

func finishPrepSessionByID(ctx context.Context, db *pgxpool.Pool, schoolID, sessionID string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = finishPrepSessionTx(ctx, tx, schoolID, sessionID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// finalizeExpiredPrepSessions closes timed papers past their deadline and
// practice left open, so a closed tab still counts. Returns how many it closed.
func finalizeExpiredPrepSessions(ctx context.Context, db *pgxpool.Pool) (int, error) {
	rows, err := db.Query(ctx, `SELECT id::text,school_id::text FROM prep_sessions WHERE status='in_progress' AND (`+prepExpiredSQL+` OR `+prepAbandonedSQL+`) ORDER BY started_at LIMIT 200`)
	if err != nil {
		return 0, err
	}
	type target struct{ sessionID, schoolID string }
	targets := []target{}
	for rows.Next() {
		var item target
		if err = rows.Scan(&item.sessionID, &item.schoolID); err != nil {
			rows.Close()
			return 0, err
		}
		targets = append(targets, item)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	var firstErr error
	for _, item := range targets {
		if err := finishPrepSessionByID(ctx, db, item.schoolID, item.sessionID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return len(targets), firstErr
}

type prepOption struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type prepItem struct {
	QuestionID  string       `json:"question_id"`
	Position    int          `json:"position"`
	Topic       string       `json:"topic"`
	Prompt      string       `json:"prompt"`
	Options     []prepOption `json:"options"`
	Answer      *string      `json:"answer"`
	Flagged     bool         `json:"flagged"`
	IsCorrect   *bool        `json:"is_correct,omitempty"`
	Correct     string       `json:"correct_option,omitempty"`
	Explanation string       `json:"explanation,omitempty"`
}

// prepSessionView reads a sitting for its student. Answers and explanations
// appear only once they may be seen: after each answer in practice, and after
// finishing on a timed paper. Nothing about an unanswered question is revealed.
func (a *API) prepSessionView(ctx context.Context, schoolID, studentID, sessionID string) (map[string]any, error) {
	var mode, paper, status, startedAt string
	var mockID, deadlineAt *string
	var correctCount, questionCount int
	var seconds *int
	err := a.db.QueryRow(ctx, `SELECT mode,paper,status,started_at::text,mock_id::text,deadline_at::text,correct_count,question_count,CASE WHEN deadline_at IS NULL THEN NULL ELSE GREATEST(0,ceil(extract(epoch FROM deadline_at-now())))::int END
		FROM prep_sessions WHERE id=$1 AND school_id=$2 AND student_id=$3`, sessionID, schoolID, studentID).Scan(&mode, &paper, &status, &startedAt, &mockID, &deadlineAt, &correctCount, &questionCount, &seconds)
	if err != nil {
		return nil, err
	}
	rows, err := a.db.Query(ctx, `SELECT i.question_id::text,i.position,COALESCE((SELECT n.title FROM question_skill_tags t JOIN curriculum_nodes n ON n.id=t.skill_id WHERE t.question_id=i.question_id LIMIT 1),''),
		q.prompt->>'text',q.options,i.option_order,i.answer,i.flagged,i.is_correct,q.answer_key->>0,COALESCE(q.explanation->>'text','')
		FROM prep_session_items i JOIN questions q ON q.id=i.question_id WHERE i.session_id=$1 ORDER BY i.position`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []prepItem{}
	answered := 0
	for rows.Next() {
		var x prepItem
		var stored []byte
		var order []string
		var key, explanation string
		if err = rows.Scan(&x.QuestionID, &x.Position, &x.Topic, &x.Prompt, &stored, &order, &x.Answer, &x.Flagged, &x.IsCorrect, &key, &explanation); err != nil {
			return nil, err
		}
		var options []prepOption
		if err = json.Unmarshal(stored, &options); err != nil {
			return nil, err
		}
		x.Options = make([]prepOption, 0, len(options))
		for _, optionID := range order {
			for _, option := range options {
				if option.ID == optionID {
					x.Options = append(x.Options, option)
				}
			}
		}
		if x.Answer != nil {
			answered++
		}
		if status == "finished" && (mode == "timed" || x.Answer != nil) || mode != "timed" && x.Answer != nil {
			x.Correct, x.Explanation = key, explanation
		} else {
			x.IsCorrect = nil
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	view := map[string]any{"id": sessionID, "mode": mode, "paper": paper, "status": status, "started_at": startedAt, "mock_id": mockID, "deadline_at": deadlineAt, "seconds_left": seconds, "question_count": questionCount, "answered_count": answered, "items": items}
	if status == "finished" {
		view["correct_count"] = correctCount
		view["score_out_of_100"] = paperScore(correctCount, questionCount, mode, answered)
	}
	return view, nil
}

// paperScore scales a result to the exam's 100 marks. Skipped practice questions
// do not count against the student.
func paperScore(correct, questions int, mode string, answered int) float64 {
	total := questions
	if mode != "timed" {
		total = answered
	}
	if total == 0 {
		return 0
	}
	return float64(correct) / float64(total) * 100
}

func (a *API) getPrepSession(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID, ok := a.prepStudent(w, r, id)
	if !ok {
		return
	}
	sessionID := r.PathValue("sessionID")
	var expired bool
	err := a.db.QueryRow(r.Context(), `SELECT status='in_progress' AND COALESCE(`+prepExpiredSQL+`,false) FROM prep_sessions WHERE id=$1 AND school_id=$2 AND student_id=$3`, sessionID, id.SchoolID, studentID).Scan(&expired)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if expired {
		if err = finishPrepSessionByID(r.Context(), a.db, id.SchoolID, sessionID); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	view, err := a.prepSessionView(r.Context(), id.SchoolID, studentID, sessionID)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": view})
}

func (a *API) savePrepAnswer(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID, ok := a.prepStudent(w, r, id)
	if !ok {
		return
	}
	var req struct {
		Answer  *string `json:"answer"`
		Flagged bool    `json:"flagged"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	sessionID, questionID := r.PathValue("sessionID"), r.PathValue("questionID")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var mode, status string
	var expired bool
	err = tx.QueryRow(r.Context(), `SELECT mode,status,COALESCE(`+prepExpiredSQL+`,false) FROM prep_sessions WHERE id=$1 AND school_id=$2 AND student_id=$3 FOR UPDATE`, sessionID, id.SchoolID, studentID).Scan(&mode, &status, &expired)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if status != "in_progress" || expired {
		writeError(w, 409, "SESSION_FINISHED", "This practice has finished.", nil)
		return
	}
	var existing *string
	var order []string
	var key, explanation string
	var isCorrect *bool
	err = tx.QueryRow(r.Context(), `SELECT i.answer,i.is_correct,i.option_order,q.answer_key->>0,COALESCE(q.explanation->>'text','') FROM prep_session_items i JOIN questions q ON q.id=i.question_id WHERE i.session_id=$1 AND i.question_id=$2`, sessionID, questionID).Scan(&existing, &isCorrect, &order, &key, &explanation)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if req.Answer != nil && !slices.Contains(order, *req.Answer) {
		writeError(w, 400, "INVALID_ANSWER", "Choose one of the question's options.", nil)
		return
	}
	data := map[string]any{"question_id": questionID}
	if mode == "timed" {
		// A timed answer can change until the paper finishes.
		_, err = tx.Exec(r.Context(), `UPDATE prep_session_items SET answer=$3,flagged=$4,answered_at=CASE WHEN $3::text IS NULL THEN NULL ELSE now() END WHERE session_id=$1 AND question_id=$2`, sessionID, questionID, req.Answer, req.Flagged)
	} else if existing != nil || req.Answer == nil {
		// In practice the first answer stands, because the student has seen the explanation.
		_, err = tx.Exec(r.Context(), `UPDATE prep_session_items SET flagged=$3 WHERE session_id=$1 AND question_id=$2`, sessionID, questionID, req.Flagged)
	} else {
		correct := *req.Answer == key
		isCorrect, existing = &correct, req.Answer
		_, err = tx.Exec(r.Context(), `UPDATE prep_session_items SET answer=$3,is_correct=$4,flagged=$5,answered_at=now() WHERE session_id=$1 AND question_id=$2`, sessionID, questionID, *req.Answer, correct, req.Flagged)
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if mode != "timed" && existing != nil {
		data["answer"], data["is_correct"], data["correct_option"], data["explanation"] = *existing, isCorrect, key, explanation
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": data})
}

func (a *API) finishPrepSessionHandler(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID, ok := a.prepStudent(w, r, id)
	if !ok {
		return
	}
	sessionID := r.PathValue("sessionID")
	var owned bool
	if err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM prep_sessions WHERE id=$1 AND school_id=$2 AND student_id=$3)`, sessionID, id.SchoolID, studentID).Scan(&owned); err != nil {
		a.serverError(w, r, err)
		return
	}
	if !owned {
		writeError(w, 404, "NOT_FOUND", "Practice not found.", nil)
		return
	}
	if err := finishPrepSessionByID(r.Context(), a.db, id.SchoolID, sessionID); err != nil {
		a.serverError(w, r, err)
		return
	}
	view, err := a.prepSessionView(r.Context(), id.SchoolID, studentID, sessionID)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": view})
}

func (a *API) completePrepOnboarding(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID, ok := a.prepStudent(w, r, id)
	if !ok {
		return
	}
	if _, err := a.db.Exec(r.Context(), `UPDATE students SET prep_onboarded_at=COALESCE(prep_onboarded_at,now()) WHERE id=$1 AND school_id=$2`, studentID, id.SchoolID); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]bool{"onboarded": true}})
}

// startPrepMock opens a full mock exam for the student's stream, or returns the
// one already open.
func (a *API) startPrepMock(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID, ok := a.prepStudent(w, r, id)
	if !ok {
		return
	}
	var stream *string
	if err := a.db.QueryRow(r.Context(), `SELECT stream FROM students WHERE id=$1 AND school_id=$2`, studentID, id.SchoolID).Scan(&stream); err != nil {
		a.serverError(w, r, err)
		return
	}
	if stream == nil {
		writeError(w, 409, "STREAM_NOT_SET", "Your school has not set your stream yet. Ask your teacher.", nil)
		return
	}
	var mockID string
	err := a.db.QueryRow(r.Context(), `INSERT INTO prep_mocks(school_id,student_id,stream) VALUES($1,$2,$3)
		ON CONFLICT (student_id) WHERE status='in_progress' DO UPDATE SET stream=prep_mocks.stream RETURNING id::text`, id.SchoolID, studentID, *stream).Scan(&mockID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]string{"id": mockID}})
}

// startPrepMockPaper starts, or returns, one timed paper of a full mock.
func (a *API) startPrepMockPaper(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID, ok := a.prepStudent(w, r, id)
	if !ok {
		return
	}
	var req struct {
		Paper string `json:"paper"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	mockID := r.PathValue("mockID")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var stream, status string
	err = tx.QueryRow(r.Context(), `SELECT stream,status FROM prep_mocks WHERE id=$1 AND school_id=$2 AND student_id=$3 FOR UPDATE`, mockID, id.SchoolID, studentID).Scan(&stream, &status)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if !slices.Contains(streamPapers(stream), req.Paper) {
		writeError(w, 400, "VALIDATION_ERROR", "This paper is not part of your stream's mock exam.", nil)
		return
	}
	var existing, existingStatus string
	err = tx.QueryRow(r.Context(), `SELECT id::text,status FROM prep_sessions WHERE mock_id=$1 AND paper=$2`, mockID, req.Paper).Scan(&existing, &existingStatus)
	if err == nil {
		if existingStatus != "in_progress" {
			writeError(w, 409, "PAPER_DONE", "You have already taken this paper.", nil)
			return
		}
		writeJSON(w, 200, map[string]any{"data": map[string]string{"id": existing}})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		a.serverError(w, r, err)
		return
	}
	if status != "in_progress" {
		writeError(w, 409, "PAPER_DONE", "This mock exam has finished.", nil)
		return
	}
	var active string
	err = tx.QueryRow(r.Context(), `SELECT id::text FROM prep_sessions WHERE student_id=$1 AND school_id=$2 AND status='in_progress' AND mode='timed' LIMIT 1`, studentID, id.SchoolID).Scan(&active)
	if err == nil {
		writeError(w, 409, "SESSION_ACTIVE", "Finish your timed paper before starting another.", nil)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		a.serverError(w, r, err)
		return
	}
	questionIDs, err := selectPrepQuestions(r.Context(), tx, id.SchoolID, studentID, "timed", req.Paper, nil, maxPaperQuestions)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if len(questionIDs) == 0 {
		writeError(w, 409, "NO_QUESTIONS", "This paper has no approved practice questions yet. Ask your teacher.", nil)
		return
	}
	sessionID, err := createPrepSession(r.Context(), tx, id.SchoolID, studentID, "timed", req.Paper, &mockID, questionIDs, defaultPaperMinutes)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]string{"id": sessionID}})
}

// studentPrepHub is everything the student's exam prep home page needs.
func (a *API) studentPrepHub(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID, ok := a.prepStudent(w, r, id)
	if !ok {
		return
	}
	ctx := r.Context()
	var stream *string
	var onboarded bool
	if err := a.db.QueryRow(ctx, `SELECT stream,prep_onboarded_at IS NOT NULL FROM students WHERE id=$1 AND school_id=$2`, studentID, id.SchoolID).Scan(&stream, &onboarded); err != nil {
		a.serverError(w, r, err)
		return
	}
	bank, err := a.db.Query(ctx, `SELECT s.national_paper,count(DISTINCT q.id)::int,
		count(DISTINCT q.id) FILTER (WHERE h.is_correct IS FALSE AND h.answered_at < now() - interval '`+prepReviewDelay+`')::int
		FROM questions q JOIN question_skill_tags t ON t.question_id=q.id JOIN curriculum_nodes n ON n.id=t.skill_id JOIN subjects s ON s.id=n.subject_id
		LEFT JOIN LATERAL (SELECT i.is_correct,i.answered_at FROM prep_session_items i JOIN prep_sessions ps ON ps.id=i.session_id WHERE i.question_id=q.id AND ps.student_id=$2 AND i.answered_at IS NOT NULL ORDER BY i.answered_at DESC LIMIT 1) h ON TRUE
		WHERE q.school_id=$1 AND q.status='published' AND q.question_type='single_choice' AND NOT q.exam_only AND s.national_paper IS NOT NULL
		GROUP BY s.national_paper`, id.SchoolID, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer bank.Close()
	type paperInfo struct {
		Paper     string `json:"paper"`
		Available int    `json:"available"`
		DueReview int    `json:"due_review"`
	}
	found := map[string]paperInfo{}
	for bank.Next() {
		var x paperInfo
		if err = bank.Scan(&x.Paper, &x.Available, &x.DueReview); err != nil {
			a.serverError(w, r, err)
			return
		}
		found[x.Paper] = x
	}
	if err = bank.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	bank.Close()
	papers := []paperInfo{}
	streamName := ""
	if stream != nil {
		streamName = *stream
	}
	allPapers := streamPapers(streamName)
	if stream == nil {
		allPapers = slices.Concat(commonPapers, naturalPapers, socialPapers)
	}
	for _, paper := range allPapers {
		x := found[paper]
		x.Paper = paper
		papers = append(papers, x)
	}
	readiness, err := a.studentReadiness(ctx, id.SchoolID, studentID, streamName)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	active := map[string]any{}
	var activeID, activeMode, activePaper string
	err = a.db.QueryRow(ctx, `SELECT id::text,mode,paper FROM prep_sessions WHERE student_id=$1 AND school_id=$2 AND status='in_progress' ORDER BY (mode='timed') DESC,started_at DESC LIMIT 1`, studentID, id.SchoolID).Scan(&activeID, &activeMode, &activePaper)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		a.serverError(w, r, err)
		return
	}
	if err == nil {
		active = map[string]any{"id": activeID, "mode": activeMode, "paper": activePaper}
	}
	mocks, err := a.studentMocks(ctx, id.SchoolID, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"stream": stream, "onboarded": onboarded, "papers": papers, "readiness": readiness, "active_session": active, "mocks": mocks}})
}

// studentMocks lists recent full mocks with each paper's state and the total
// out of 600 once all six are finished.
func (a *API) studentMocks(ctx context.Context, schoolID, studentID string) ([]map[string]any, error) {
	rows, err := a.db.Query(ctx, `SELECT id::text,stream,status,started_at::text FROM prep_mocks WHERE student_id=$1 AND school_id=$2 ORDER BY started_at DESC LIMIT 5`, studentID, schoolID)
	if err != nil {
		return nil, err
	}
	type mock struct{ id, stream, status, started string }
	found := []mock{}
	for rows.Next() {
		var x mock
		if err = rows.Scan(&x.id, &x.stream, &x.status, &x.started); err != nil {
			rows.Close()
			return nil, err
		}
		found = append(found, x)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	result := []map[string]any{}
	for _, x := range found {
		sessions, err := a.db.Query(ctx, `SELECT paper,id::text,status,correct_count,question_count FROM prep_sessions WHERE mock_id=$1`, x.id)
		if err != nil {
			return nil, err
		}
		type sitting struct {
			id, status string
			score      float64
		}
		byPaper := map[string]sitting{}
		for sessions.Next() {
			var paper string
			var item sitting
			var correct, total int
			if err = sessions.Scan(&paper, &item.id, &item.status, &correct, &total); err != nil {
				sessions.Close()
				return nil, err
			}
			item.score = paperScore(correct, total, "timed", 0)
			byPaper[paper] = item
		}
		sessions.Close()
		if err = sessions.Err(); err != nil {
			return nil, err
		}
		papers := []map[string]any{}
		total := 0.0
		for _, paper := range streamPapers(x.stream) {
			entry := map[string]any{"paper": paper, "status": "not_started"}
			if item, ok := byPaper[paper]; ok {
				entry["session_id"], entry["status"] = item.id, item.status
				if item.status == "finished" {
					entry["score"] = item.score
					total += item.score
				}
			}
			papers = append(papers, entry)
		}
		entry := map[string]any{"id": x.id, "stream": x.stream, "status": x.status, "started_at": x.started, "papers": papers}
		if x.status == "finished" {
			entry["total"] = total
		}
		result = append(result, entry)
	}
	return result, nil
}
