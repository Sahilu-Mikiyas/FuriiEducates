package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
)

// The national exam's papers. Every student sits the common three plus the
// three of their stream, six papers of 100 marks each.
var (
	commonPapers  = []string{"english", "mathematics", "aptitude"}
	naturalPapers = []string{"physics", "chemistry", "biology"}
	socialPapers  = []string{"history", "geography", "economics"}
	paperTitles   = map[string]string{"english": "English", "mathematics": "Mathematics", "aptitude": "Scholastic Aptitude", "physics": "Physics", "chemistry": "Chemistry", "biology": "Biology", "history": "History", "geography": "Geography", "economics": "Economics"}
)

const (
	prepCurriculumName     = "National exam preparation"
	prepCurriculumProvider = "FURII School OS"
	maxPrepImportBytes     = 2 << 20
	maxPrepImportRows      = 500
)

func validPaper(paper string) bool {
	_, ok := paperTitles[paper]
	return ok
}

// streamPapers returns the six papers of a stream, or just the common three
// when the stream is not known yet.
func streamPapers(stream string) []string {
	switch stream {
	case "natural":
		return slices.Concat(commonPapers, naturalPapers)
	case "social":
		return slices.Concat(commonPapers, socialPapers)
	}
	return commonPapers
}

func (a *API) registerPrepRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/prep/bank", a.staffMember(a.prepBank))
	mux.HandleFunc("GET /api/v1/prep/questions", a.staffMember(a.listPrepQuestions))
	mux.HandleFunc("POST /api/v1/prep/questions", a.staffMember(a.createPrepQuestion))
	mux.HandleFunc("POST /api/v1/prep/questions/import", a.staffMember(a.importPrepQuestions))
	mux.HandleFunc("POST /api/v1/prep/questions/approve", a.admin(a.approvePrepQuestions))
	mux.HandleFunc("POST /api/v1/prep/questions/{questionID}/reject", a.admin(a.rejectPrepQuestion))
	mux.HandleFunc("POST /api/v1/prep/mock-papers", a.admin(a.createPrepMockPaper))
	mux.HandleFunc("PUT /api/v1/prep/students/{studentID}/stream", a.admin(a.setStudentStream))
	mux.HandleFunc("PUT /api/v1/prep/classes/{classID}/stream", a.admin(a.setClassStream))
	mux.HandleFunc("GET /api/v1/prep/classes/{classID}/readiness", a.staffMember(a.classReadiness))
	mux.HandleFunc("GET /api/v1/prep/readiness", a.admin(a.schoolReadiness))
	a.registerPrepPracticeRoutes(mux)
	a.registerPrepOfflineRoutes(mux)
}

// prepQuestionInput is one four-option exam question in the simple form used by
// the authoring screen and the CSV import.
type prepQuestionInput struct {
	Paper       string   `json:"paper"`
	Grade       int      `json:"grade"`
	Topic       string   `json:"topic"`
	Prompt      string   `json:"prompt"`
	Options     []string `json:"options"`
	Correct     string   `json:"correct"`
	Explanation string   `json:"explanation"`
	Difficulty  int      `json:"difficulty"`
	Language    string   `json:"language_code"`
	Source      string   `json:"source_name"`
	License     string   `json:"license_notes"`
	ExamOnly    bool     `json:"exam_only"`
}

var prepOptionIDs = []string{"A", "B", "C", "D"}

func (q *prepQuestionInput) normalize() string {
	q.Topic, q.Prompt, q.Explanation = strings.TrimSpace(q.Topic), strings.TrimSpace(q.Prompt), strings.TrimSpace(q.Explanation)
	q.Source, q.License = strings.TrimSpace(q.Source), strings.TrimSpace(q.License)
	q.Correct = strings.ToUpper(strings.TrimSpace(q.Correct))
	q.Language = strings.TrimSpace(q.Language)
	if q.Language == "" {
		q.Language = "en"
	}
	if q.Difficulty == 0 {
		q.Difficulty = 3
	}
	switch {
	case !validPaper(q.Paper):
		return "Choose one of the national exam papers."
	case q.Grade < 9 || q.Grade > 12:
		return "Grade must be from 9 to 12."
	case q.Topic == "" || len(q.Topic) > 200:
		return "Enter a topic of up to 200 characters."
	case q.Prompt == "" || len(q.Prompt) > 5000:
		return "Enter a question of up to 5,000 characters."
	case len(q.Options) != len(prepOptionIDs):
		return "Exam questions need exactly four options."
	case !slices.Contains(prepOptionIDs, q.Correct):
		return "The correct option must be A, B, C or D."
	case len(q.Explanation) > 5000:
		return "Keep the explanation under 5,000 characters."
	case q.Difficulty < 1 || q.Difficulty > 5:
		return "Difficulty must be from 1 to 5."
	case q.Source == "" || q.License == "":
		return "Say where the question came from and what permission covers it."
	}
	seen := map[string]bool{}
	for i, option := range q.Options {
		q.Options[i] = strings.TrimSpace(option)
		if q.Options[i] == "" || seen[q.Options[i]] {
			return "All four options must be filled in and different."
		}
		seen[q.Options[i]] = true
	}
	return ""
}

// ensurePrepSkill finds or creates the skill for a paper, grade and topic in the
// school's own exam preparation curriculum. The lock stops two teachers creating
// the same curriculum or topic at once.
func ensurePrepSkill(ctx context.Context, tx pgx.Tx, schoolID string, q prepQuestionInput) (string, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('prep-curriculum:'||$1::text))`, schoolID); err != nil {
		return "", err
	}
	var curriculumID string
	err := tx.QueryRow(ctx, `SELECT id::text FROM curricula WHERE school_id=$1 AND provider=$2 AND name=$3`, schoolID, prepCurriculumProvider, prepCurriculumName).Scan(&curriculumID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO curricula(school_id,name,provider,country_code,version_label,language_code) VALUES($1,$2,$3,'ET','1','en') RETURNING id::text`, schoolID, prepCurriculumName, prepCurriculumProvider).Scan(&curriculumID)
	}
	if err != nil {
		return "", err
	}
	var subjectID string
	err = tx.QueryRow(ctx, `INSERT INTO subjects(curriculum_id,name,grade_level,national_paper) VALUES($1,$2,$3,$4)
		ON CONFLICT (curriculum_id,name,grade_level) DO UPDATE SET national_paper=EXCLUDED.national_paper RETURNING id::text`,
		curriculumID, paperTitles[q.Paper], fmt.Sprintf("Grade %d", q.Grade), q.Paper).Scan(&subjectID)
	if err != nil {
		return "", err
	}
	var skillID string
	err = tx.QueryRow(ctx, `SELECT id::text FROM curriculum_nodes WHERE subject_id=$1 AND node_type='skill' AND lower(title)=lower($2)`, subjectID, q.Topic).Scan(&skillID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO curriculum_nodes(subject_id,node_type,title) VALUES($1,'skill',$2) RETURNING id::text`, subjectID, q.Topic).Scan(&skillID)
	}
	return skillID, err
}

// insertPrepQuestion saves a draft. Drafts need an administrator's approval
// before students or papers can use them.
func insertPrepQuestion(ctx context.Context, tx pgx.Tx, id auth.Identity, q prepQuestionInput) (string, error) {
	skillID, err := ensurePrepSkill(ctx, tx, id.SchoolID, q)
	if err != nil {
		return "", err
	}
	type option struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	options := make([]option, len(prepOptionIDs))
	for i, optionID := range prepOptionIDs {
		options[i] = option{optionID, q.Options[i]}
	}
	optionsJSON, _ := json.Marshal(options)
	promptJSON, _ := json.Marshal(map[string]string{"text": q.Prompt})
	keyJSON, _ := json.Marshal([]string{q.Correct})
	explanationJSON, _ := json.Marshal(map[string]string{"text": q.Explanation})
	var questionID string
	err = tx.QueryRow(ctx, `INSERT INTO questions(school_id,question_type,prompt,options,answer_key,explanation,difficulty,language_code,source_name,license_notes,exam_only,created_by)
		VALUES($1,'single_choice',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id::text`,
		id.SchoolID, promptJSON, optionsJSON, keyJSON, explanationJSON, q.Difficulty, q.Language, q.Source, q.License, q.ExamOnly, id.UserID).Scan(&questionID)
	if err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO question_skill_tags(question_id,skill_id,weight) VALUES($1,$2,1)`, questionID, skillID); err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'question.saved','question',$3,jsonb_build_object('question_type','single_choice','paper',$4::text))`, id.SchoolID, id.UserID, questionID, q.Paper)
	return questionID, err
}

func (a *API) createPrepQuestion(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var q prepQuestionInput
	if !decode(w, r, &q) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	if message := q.normalize(); message != "" {
		writeError(w, 400, "VALIDATION_ERROR", message, nil)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	questionID, err := insertPrepQuestion(r.Context(), tx, id, q)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]string{"id": questionID, "status": "draft"}})
}

// parsePrepCSV reads the question import format. The header names the columns,
// so their order does not matter. It returns the questions and an error list
// with one entry for each bad row.
func parsePrepCSV(body io.Reader) ([]prepQuestionInput, []map[string]any, string) {
	reader := csv.NewReader(body)
	reader.TrimLeadingSpace = true
	header, err := reader.Read()
	if err != nil {
		return nil, nil, "The file is empty or is not a CSV."
	}
	columns := map[string]int{}
	for i, name := range header {
		columns[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(name, "ï»¿")))] = i
	}
	for _, required := range []string{"paper", "grade", "topic", "question", "option_a", "option_b", "option_c", "option_d", "correct", "source", "license"} {
		if _, ok := columns[required]; !ok {
			return nil, nil, "The file is missing the " + required + " column."
		}
	}
	questions := []prepQuestionInput{}
	rowErrors := []map[string]any{}
	for row := 2; ; row++ {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return questions, rowErrors, ""
		}
		if err != nil {
			rowErrors = append(rowErrors, map[string]any{"row": row, "error": "This row could not be read."})
			continue
		}
		if row-1 > maxPrepImportRows {
			return nil, nil, fmt.Sprintf("Import up to %d questions at a time.", maxPrepImportRows)
		}
		cell := func(name string) string {
			if i, ok := columns[name]; ok && i < len(record) {
				return strings.TrimSpace(record[i])
			}
			return ""
		}
		q := prepQuestionInput{
			Paper: strings.ToLower(cell("paper")), Topic: cell("topic"), Prompt: cell("question"),
			Options: []string{cell("option_a"), cell("option_b"), cell("option_c"), cell("option_d")},
			Correct: cell("correct"), Explanation: cell("explanation"), Language: cell("language"), Source: cell("source"), License: cell("license"),
			ExamOnly: slices.Contains([]string{"yes", "true", "1"}, strings.ToLower(cell("exam_only"))),
		}
		q.Grade, _ = strconv.Atoi(cell("grade"))
		if value := cell("difficulty"); value != "" {
			q.Difficulty, _ = strconv.Atoi(value)
			if q.Difficulty == 0 {
				q.Difficulty = -1
			}
		}
		if message := q.normalize(); message != "" {
			rowErrors = append(rowErrors, map[string]any{"row": row, "error": message})
			continue
		}
		questions = append(questions, q)
	}
}

// importPrepQuestions adds a CSV of questions as drafts. Any bad row stops the
// whole import, so a school never ends up with half a file.
func (a *API) importPrepQuestions(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	questions, rowErrors, message := parsePrepCSV(http.MaxBytesReader(w, r.Body, maxPrepImportBytes))
	if message != "" {
		writeError(w, 400, "VALIDATION_ERROR", message, nil)
		return
	}
	if len(rowErrors) > 0 {
		writeJSON(w, 422, map[string]any{"error": map[string]any{"code": "IMPORT_HAS_ERRORS", "message": "Fix the listed rows and import the file again. Nothing was saved.", "row_errors": rowErrors}})
		return
	}
	if len(questions) == 0 {
		writeError(w, 400, "VALIDATION_ERROR", "The file has no questions.", nil)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	for _, q := range questions {
		if _, err = insertPrepQuestion(r.Context(), tx, id, q); err != nil {
			a.databaseError(w, r, err)
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]int{"imported": len(questions)}})
}

// listPrepQuestions shows exam questions to staff. Administrators review every
// answer; a teacher sees the answers only to the questions they wrote, so a
// teacher cannot read ahead on a colleague's mock paper.
func (a *API) listPrepQuestions(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, err := a.db.Query(r.Context(), `
		SELECT q.id::text,s.national_paper,COALESCE(s.grade_level,''),n.title,q.prompt->>'text',q.options,q.answer_key->>0,COALESCE(q.explanation->>'text',''),
		       COALESCE(q.difficulty,0),q.status,q.exam_only,COALESCE(q.source_name,''),COALESCE(q.license_notes,''),COALESCE(u.display_name,''),q.created_by=$4,q.created_at::text
		FROM questions q
		JOIN question_skill_tags t ON t.question_id=q.id
		JOIN curriculum_nodes n ON n.id=t.skill_id
		JOIN subjects s ON s.id=n.subject_id
		LEFT JOIN users u ON u.id=q.created_by
		WHERE q.school_id=$1 AND s.national_paper IS NOT NULL AND q.status<>'archived' AND ($2='' OR q.status=$2) AND ($3='' OR s.national_paper=$3)
		ORDER BY q.created_at DESC LIMIT 200`, id.SchoolID, r.URL.Query().Get("status"), r.URL.Query().Get("paper"), id.UserID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type item struct {
		ID          string          `json:"id"`
		Paper       string          `json:"paper"`
		Grade       string          `json:"grade"`
		Topic       string          `json:"topic"`
		Prompt      string          `json:"prompt"`
		Options     json.RawMessage `json:"options"`
		Correct     string          `json:"correct,omitempty"`
		Explanation string          `json:"explanation,omitempty"`
		Difficulty  int             `json:"difficulty"`
		Status      string          `json:"status"`
		ExamOnly    bool            `json:"exam_only"`
		Source      string          `json:"source_name"`
		License     string          `json:"license_notes"`
		Author      string          `json:"author"`
		CreatedAt   string          `json:"created_at"`
	}
	isAdmin := hasRole(id.Roles, "school_admin")
	items := []item{}
	for rows.Next() {
		var x item
		var mine bool
		if err = rows.Scan(&x.ID, &x.Paper, &x.Grade, &x.Topic, &x.Prompt, &x.Options, &x.Correct, &x.Explanation, &x.Difficulty, &x.Status, &x.ExamOnly, &x.Source, &x.License, &x.Author, &mine, &x.CreatedAt); err != nil {
			a.serverError(w, r, err)
			return
		}
		if !isAdmin && !mine {
			x.Correct, x.Explanation = "", ""
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

// prepBank counts each paper's questions so staff can see what is missing.
func (a *API) prepBank(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, err := a.db.Query(r.Context(), `
		SELECT paper,count(*) FILTER (WHERE status='published' AND NOT exam_only)::int,count(*) FILTER (WHERE status='published' AND exam_only)::int,count(*) FILTER (WHERE status='draft')::int
		FROM (SELECT DISTINCT q.id,q.status,q.exam_only,s.national_paper AS paper
		      FROM questions q JOIN question_skill_tags t ON t.question_id=q.id JOIN curriculum_nodes n ON n.id=t.skill_id JOIN subjects s ON s.id=n.subject_id
		      WHERE q.school_id=$1 AND s.national_paper IS NOT NULL AND q.question_type='single_choice') bank
		GROUP BY paper`, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type count struct {
		Paper    string `json:"paper"`
		Practice int    `json:"practice"`
		ExamOnly int    `json:"exam_only"`
		Drafts   int    `json:"drafts"`
	}
	found := map[string]count{}
	for rows.Next() {
		var x count
		if err = rows.Scan(&x.Paper, &x.Practice, &x.ExamOnly, &x.Drafts); err != nil {
			a.serverError(w, r, err)
			return
		}
		found[x.Paper] = x
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	items := []count{}
	for _, paper := range slices.Concat(commonPapers, naturalPapers, socialPapers) {
		x := found[paper]
		x.Paper = paper
		items = append(items, x)
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

func (a *API) approvePrepQuestions(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		QuestionIDs []string `json:"question_ids"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	if len(req.QuestionIDs) == 0 || len(req.QuestionIDs) > 200 {
		writeError(w, 400, "VALIDATION_ERROR", "Choose from 1 to 200 questions.", nil)
		return
	}
	for _, questionID := range req.QuestionIDs {
		if !uuidPattern.MatchString(questionID) {
			writeError(w, 400, "VALIDATION_ERROR", "A question identifier is not valid.", nil)
			return
		}
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `UPDATE questions q SET status='published',updated_at=now()
		WHERE q.school_id=$1 AND q.id=ANY($2::uuid[]) AND q.status='draft' AND q.source_name IS NOT NULL AND q.license_notes IS NOT NULL
		AND EXISTS(SELECT 1 FROM question_skill_tags t WHERE t.question_id=q.id)`, id.SchoolID, req.QuestionIDs)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'question.bulk_published','question',$3,jsonb_build_object('count',$4::int))`, id.SchoolID, id.UserID, id.SchoolID, tag.RowsAffected()); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]int64{"approved": tag.RowsAffected()}})
}

func (a *API) rejectPrepQuestion(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	questionID := r.PathValue("questionID")
	tag, err := a.db.Exec(r.Context(), `UPDATE questions SET status='archived',updated_at=now() WHERE id=$1 AND school_id=$2 AND status='draft'`, questionID, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "No draft question was found.", nil)
		return
	}
	if _, err = a.db.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'question.rejected','question',$3,'{}'::jsonb)`, id.SchoolID, id.UserID, questionID); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": questionID, "status": "archived"}})
}

// createPrepMockPaper drafts a school mock paper from the bank on the secure
// exam engine. Exam-only questions are used first because students have not
// seen them in practice. The usual publish, assign and invigilate steps follow.
func (a *API) createPrepMockPaper(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Paper           string `json:"paper"`
		Title           string `json:"title"`
		QuestionCount   int    `json:"question_count"`
		DurationMinutes int    `json:"duration_minutes"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if !validPaper(req.Paper) || req.Title == "" || len(req.Title) > 200 || req.QuestionCount < 5 || req.QuestionCount > 100 || req.DurationMinutes < 5 || req.DurationMinutes > 240 {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a paper and title, from 5 to 100 questions, and from 5 to 240 minutes.", nil)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	rows, err := tx.Query(r.Context(), `SELECT q.id::text,q.exam_only FROM questions q
		WHERE q.school_id=$1 AND q.status='published' AND q.question_type='single_choice' AND `+prepPaperFilterSQL+`
		ORDER BY q.exam_only DESC,random() LIMIT $3`, id.SchoolID, req.Paper, req.QuestionCount)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	questionIDs := []string{}
	examOnly := 0
	for rows.Next() {
		var questionID string
		var only bool
		if err = rows.Scan(&questionID, &only); err != nil {
			rows.Close()
			a.serverError(w, r, err)
			return
		}
		questionIDs = append(questionIDs, questionID)
		if only {
			examOnly++
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	if len(questionIDs) < req.QuestionCount {
		writeError(w, 409, "NOT_ENOUGH_QUESTIONS", fmt.Sprintf("This paper has %d approved questions, and the mock needs %d. Add questions or choose a smaller number.", len(questionIDs), req.QuestionCount), nil)
		return
	}
	var assessmentID string
	err = tx.QueryRow(r.Context(), `INSERT INTO assessments(school_id,title,instructions,duration_minutes,created_by,shuffle_questions,shuffle_options,locked_mode,national_paper) VALUES($1,$2,'',$3,$4,true,true,true,$5) RETURNING id::text`, id.SchoolID, req.Title, req.DurationMinutes, id.UserID, req.Paper).Scan(&assessmentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	for index, questionID := range questionIDs {
		if _, err = tx.Exec(r.Context(), `INSERT INTO assessment_items(assessment_id,school_id,question_id,position,points) VALUES($1,$2,$3,$4,1)`, assessmentID, id.SchoolID, questionID, index+1); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'assessment.created','assessment',$3,jsonb_build_object('national_paper',$4::text,'question_count',$5::int))`, id.SchoolID, id.UserID, assessmentID, req.Paper, len(questionIDs)); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]any{"id": assessmentID, "status": "draft", "question_count": len(questionIDs), "exam_only_count": examOnly}})
}

// prepPaperFilterSQL keeps questions tagged to a skill of the paper in $2.
const prepPaperFilterSQL = `EXISTS(SELECT 1 FROM question_skill_tags t JOIN curriculum_nodes n ON n.id=t.skill_id JOIN subjects s ON s.id=n.subject_id WHERE t.question_id=q.id AND s.national_paper=$2)`

func validStream(stream *string) bool {
	return stream == nil || *stream == "natural" || *stream == "social"
}

func (a *API) setStudentStream(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Stream *string `json:"stream"`
	}
	if !decode(w, r, &req) || !validStream(req.Stream) {
		writeError(w, 400, "VALIDATION_ERROR", "Stream must be natural, social or empty.", nil)
		return
	}
	studentID := r.PathValue("studentID")
	tag, err := a.db.Exec(r.Context(), `UPDATE students SET stream=$3 WHERE id=$1 AND school_id=$2`, studentID, id.SchoolID, req.Stream)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "Student not found.", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"id": studentID, "stream": req.Stream}})
}

func (a *API) setClassStream(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Stream *string `json:"stream"`
	}
	if !decode(w, r, &req) || !validStream(req.Stream) {
		writeError(w, 400, "VALIDATION_ERROR", "Stream must be natural, social or empty.", nil)
		return
	}
	classID := r.PathValue("classID")
	allowed, err := a.canManageClass(r.Context(), id, classID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !allowed {
		writeError(w, 404, "NOT_FOUND", "Class not found.", nil)
		return
	}
	tag, err := a.db.Exec(r.Context(), `UPDATE students s SET stream=$3 FROM enrollments e WHERE e.student_id=s.id AND e.school_id=s.school_id AND e.school_id=$1 AND e.class_id=$2 AND e.status='active'`, id.SchoolID, classID, req.Stream)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"updated": tag.RowsAffected(), "stream": req.Stream}})
}
