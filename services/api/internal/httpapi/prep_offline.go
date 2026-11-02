package httpapi

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"slices"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
)

const (
	defaultPackQuestions = 30
	maxPackQuestions     = 100
	// An offline sitting older than this is treated as if it happened at this
	// limit, so a wrong device clock cannot reach far into the past.
	maxOfflineAge = 60 * 24 * time.Hour
)

func (a *API) registerPrepOfflineRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/student/prep/packs", a.student(a.downloadPrepPack))
	mux.HandleFunc("POST /api/v1/student/prep/offline-sittings", a.student(a.uploadOfflineSitting))
}

// downloadPrepPack gives the device questions to practise without a connection.
// The answers come with it, because the device must mark them by itself. That is
// why packs hold practice questions only and never exam-only questions: anything
// sent here should be treated as readable by the student.
func (a *API) downloadPrepPack(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID, ok := a.prepStudent(w, r, id)
	if !ok {
		return
	}
	var req struct {
		Paper         string `json:"paper"`
		QuestionCount int    `json:"question_count"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	if req.QuestionCount == 0 {
		req.QuestionCount = defaultPackQuestions
	}
	if !validPaper(req.Paper) || req.QuestionCount < 1 || req.QuestionCount > maxPackQuestions {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a paper and up to 100 questions.", nil)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	questionIDs, err := selectPrepQuestions(r.Context(), tx, id.SchoolID, studentID, "drill", req.Paper, nil, req.QuestionCount)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if len(questionIDs) == 0 {
		writeError(w, 409, "NO_QUESTIONS", "This paper has no approved practice questions yet. Ask your teacher.", nil)
		return
	}
	rows, err := tx.Query(r.Context(), `SELECT q.id::text,COALESCE((SELECT n.title FROM question_skill_tags t JOIN curriculum_nodes n ON n.id=t.skill_id WHERE t.question_id=q.id LIMIT 1),''),
		q.prompt->>'text',q.options,q.answer_key->>0,COALESCE(q.explanation->>'text','')
		FROM questions q WHERE q.id=ANY($1::uuid[]) AND q.school_id=$2`, questionIDs, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type packItem struct {
		QuestionID  string       `json:"question_id"`
		Topic       string       `json:"topic"`
		Prompt      string       `json:"prompt"`
		Options     []prepOption `json:"options"`
		Correct     string       `json:"correct_option"`
		Explanation string       `json:"explanation"`
	}
	items := []packItem{}
	for rows.Next() {
		var x packItem
		var stored []byte
		if err = rows.Scan(&x.QuestionID, &x.Topic, &x.Prompt, &stored, &x.Correct, &x.Explanation); err != nil {
			a.serverError(w, r, err)
			return
		}
		if err = json.Unmarshal(stored, &x.Options); err != nil {
			a.serverError(w, r, err)
			return
		}
		rand.Shuffle(len(x.Options), func(i, j int) { x.Options[i], x.Options[j] = x.Options[j], x.Options[i] })
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	rand.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
	writeJSON(w, 201, map[string]any{"data": map[string]any{"paper": req.Paper, "items": items}})
}

// clampOfflineTime keeps a device's timestamp between the oldest time allowed
// and now. Device clocks are often wrong, and readiness uses these for recency.
func clampOfflineTime(value, now time.Time) time.Time {
	if value.IsZero() || value.After(now) {
		return now
	}
	if oldest := now.Add(-maxOfflineAge); value.Before(oldest) {
		return oldest
	}
	return value
}

// uploadOfflineSitting records practice done on the device. The server marks the
// answers itself and ignores anything the device concluded. Offline work counts
// at the practice weight even when timed, since no server watched the clock.
func (a *API) uploadOfflineSitting(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID, ok := a.prepStudent(w, r, id)
	if !ok {
		return
	}
	var req struct {
		ClientID    string    `json:"client_id"`
		Paper       string    `json:"paper"`
		Mode        string    `json:"mode"`
		StartedAt   time.Time `json:"started_at"`
		QuestionIDs []string  `json:"question_ids"`
		Answers     []struct {
			QuestionID string    `json:"question_id"`
			Answer     *string   `json:"answer"`
			Flagged    bool      `json:"flagged"`
			AnsweredAt time.Time `json:"answered_at"`
		} `json:"answers"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	if !uuidPattern.MatchString(req.ClientID) || !validPaper(req.Paper) || (req.Mode != "drill" && req.Mode != "timed") || len(req.QuestionIDs) == 0 || len(req.QuestionIDs) > maxPackQuestions {
		writeError(w, 400, "VALIDATION_ERROR", "This practice could not be read.", nil)
		return
	}
	for _, questionID := range req.QuestionIDs {
		if !uuidPattern.MatchString(questionID) {
			writeError(w, 400, "VALIDATION_ERROR", "This practice could not be read.", nil)
			return
		}
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	now := time.Now()
	var sessionID string
	err = tx.QueryRow(r.Context(), `INSERT INTO prep_sessions(school_id,student_id,mode,paper,started_at,question_count,client_id,offline)
		VALUES($1,$2,$3,$4,$5,$6,$7,true) ON CONFLICT (student_id,client_id) WHERE client_id IS NOT NULL DO NOTHING RETURNING id::text`,
		id.SchoolID, studentID, req.Mode, req.Paper, clampOfflineTime(req.StartedAt, now), len(req.QuestionIDs), req.ClientID).Scan(&sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Already uploaded: a retry after the reply was lost. Report the same result.
		if err = tx.QueryRow(r.Context(), `SELECT id::text FROM prep_sessions WHERE student_id=$1 AND client_id=$2`, studentID, req.ClientID).Scan(&sessionID); err != nil {
			a.serverError(w, r, err)
			return
		}
		a.writeOfflineResult(w, r, id.SchoolID, studentID, sessionID, 200)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	// Only practice questions from this school and paper can be recorded, so a
	// changed upload cannot touch another school's questions or exam-only ones.
	rows, err := tx.Query(r.Context(), `SELECT q.id::text,q.options,q.answer_key->>0 FROM questions q
		WHERE q.id=ANY($3::uuid[]) AND q.school_id=$1 AND q.status<>'draft' AND q.question_type='single_choice' AND NOT q.exam_only AND `+prepPaperFilterSQL, id.SchoolID, req.Paper, req.QuestionIDs)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	type known struct {
		order []string
		key   string
	}
	questions := map[string]known{}
	for rows.Next() {
		var questionID, key string
		var stored []byte
		if err = rows.Scan(&questionID, &stored, &key); err != nil {
			rows.Close()
			a.serverError(w, r, err)
			return
		}
		var options []prepOption
		if err = json.Unmarshal(stored, &options); err != nil {
			rows.Close()
			a.serverError(w, r, err)
			return
		}
		item := known{key: key}
		for _, option := range options {
			item.order = append(item.order, option.ID)
		}
		questions[questionID] = item
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	if len(questions) != len(slicesCompactSorted(req.QuestionIDs)) {
		writeError(w, 400, "VALIDATION_ERROR", "This practice includes a question that is not available.", nil)
		return
	}
	for index, questionID := range req.QuestionIDs {
		if _, err = tx.Exec(r.Context(), `INSERT INTO prep_session_items(session_id,school_id,question_id,position,option_order) VALUES($1,$2,$3,$4,$5)`, sessionID, id.SchoolID, questionID, index+1, questions[questionID].order); err != nil {
			a.databaseError(w, r, err)
			return
		}
	}
	for _, answer := range req.Answers {
		item, found := questions[answer.QuestionID]
		if !found || !slices.Contains(req.QuestionIDs, answer.QuestionID) {
			continue
		}
		given := answer.Answer
		if given != nil && !slices.Contains(item.order, *given) {
			given = nil
		}
		var correct *bool
		var answeredAt *time.Time
		if given != nil {
			value := *given == item.key
			correct = &value
			at := clampOfflineTime(answer.AnsweredAt, now)
			answeredAt = &at
		}
		if _, err = tx.Exec(r.Context(), `UPDATE prep_session_items SET answer=$3,is_correct=$4,flagged=$5,answered_at=$6 WHERE session_id=$1 AND question_id=$2`, sessionID, answer.QuestionID, given, correct, answer.Flagged, answeredAt); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	if err = finishPrepSessionTx(r.Context(), tx, id.SchoolID, sessionID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.writeOfflineResult(w, r, id.SchoolID, studentID, sessionID, 201)
}

func slicesCompactSorted(values []string) []string {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return slices.Compact(sorted)
}

func (a *API) writeOfflineResult(w http.ResponseWriter, r *http.Request, schoolID, studentID, sessionID string, status int) {
	var correct, questions, answered int
	var mode string
	err := a.db.QueryRow(r.Context(), `SELECT ps.mode,ps.correct_count,ps.question_count,(SELECT count(*)::int FROM prep_session_items i WHERE i.session_id=ps.id AND i.answer IS NOT NULL) FROM prep_sessions ps WHERE ps.id=$1 AND ps.school_id=$2 AND ps.student_id=$3`, sessionID, schoolID, studentID).Scan(&mode, &correct, &questions, &answered)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, status, map[string]any{"data": map[string]any{"id": sessionID, "correct_count": correct, "question_count": questions, "answered_count": answered, "score_out_of_100": paperScore(correct, questions, mode, answered)}})
}
