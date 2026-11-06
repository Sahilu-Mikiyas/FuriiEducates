package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
)

const (
	quizCurriculumProvider = "FURII class work"
	quizCurriculumName     = "Class quizzes and assignments"
	maxQuizQuestions       = 100
	// An assignment stays open until its due date, within this limit.
	assignmentMaxMinutes = 43200
)

func (a *API) registerQuizRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/quizzes", a.staffMember(a.listQuizzes))
	mux.HandleFunc("POST /api/v1/quizzes", a.staffMember(a.createQuiz))
	mux.HandleFunc("GET /api/v1/quizzes/topics", a.staffMember(a.listQuizTopics))
	mux.HandleFunc("GET /api/v1/quizzes/{quizID}", a.staffMember(a.getQuiz))
	mux.HandleFunc("PUT /api/v1/quizzes/{quizID}", a.staffMember(a.updateQuiz))
	mux.HandleFunc("DELETE /api/v1/quizzes/{quizID}", a.staffMember(a.deleteQuiz))
	mux.HandleFunc("POST /api/v1/quizzes/{quizID}/publish", a.staffMember(a.publishQuiz))
	mux.HandleFunc("PUT /api/v1/quizzes/{quizID}/schedule", a.staffMember(a.rescheduleQuiz))
	mux.HandleFunc("POST /api/v1/quizzes/{quizID}/copy", a.staffMember(a.copyQuiz))
	mux.HandleFunc("GET /api/v1/quizzes/{quizID}/results", a.staffMember(a.quizResults))
	mux.HandleFunc("POST /api/v1/quizzes/{quizID}/send-to-marks", a.staffMember(a.sendQuizToMarks))
}

type quizQuestion struct {
	Type        string   `json:"question_type"`
	Prompt      string   `json:"prompt"`
	Options     []string `json:"options"`
	Correct     int      `json:"correct"`
	Value       *float64 `json:"value"`
	Tolerance   float64  `json:"tolerance"`
	Guide       string   `json:"guide"`
	Topic       string   `json:"topic"`
	Points      float64  `json:"points"`
	Explanation string   `json:"explanation"`
}

type quizInput struct {
	SubjectID    string         `json:"class_subject_id"`
	Title        string         `json:"title"`
	Instructions string         `json:"instructions"`
	Kind         string         `json:"kind"`
	Minutes      int            `json:"duration_minutes"`
	ResourceID   string         `json:"resource_id"`
	ShowAnswers  bool           `json:"show_answers"`
	Shuffle      bool           `json:"shuffle"`
	Questions    []quizQuestion `json:"questions"`
}

// checkQuizQuestion tidies one question and says what is wrong with it, if anything.
func checkQuizQuestion(q *quizQuestion) string {
	q.Prompt, q.Topic, q.Guide, q.Explanation = strings.TrimSpace(q.Prompt), strings.TrimSpace(q.Topic), strings.TrimSpace(q.Guide), strings.TrimSpace(q.Explanation)
	switch {
	case q.Prompt == "" || len(q.Prompt) > 4000:
		return "Write the question."
	case q.Topic == "" || len(q.Topic) > 120:
		return "Name its topic, so results can show what students find hard."
	case q.Points <= 0 || q.Points > 100 || math.IsNaN(q.Points):
		return "Give it from 0.25 to 100 points."
	case len(q.Explanation) > 4000 || len(q.Guide) > 4000:
		return "Keep its explanation under 4,000 characters."
	}
	q.Points = round2(q.Points)
	if q.Points <= 0 {
		return "Give it from 0.25 to 100 points."
	}
	switch q.Type {
	case "single_choice":
		kept := make([]string, 0, len(q.Options))
		for _, option := range q.Options {
			option = strings.TrimSpace(option)
			if option == "" || len(option) > 1000 {
				return "Fill in every answer choice, or remove the blank ones."
			}
			kept = append(kept, option)
		}
		q.Options = kept
		if len(kept) < 2 || len(kept) > 6 {
			return "Give it from 2 to 6 answer choices."
		}
		if q.Correct < 0 || q.Correct >= len(kept) {
			return "Choose the correct answer."
		}
		seen := map[string]bool{}
		for _, option := range kept {
			if seen[strings.ToLower(option)] {
				return "Two answer choices are the same."
			}
			seen[strings.ToLower(option)] = true
		}
	case "numeric":
		if q.Value == nil || math.IsNaN(*q.Value) || math.IsInf(*q.Value, 0) {
			return "Enter the correct number."
		}
		if q.Tolerance < 0 || math.IsNaN(q.Tolerance) || math.IsInf(q.Tolerance, 0) {
			return "The allowed difference cannot be negative."
		}
	case "text":
	default:
		return "Choose a question type."
	}
	return ""
}

// check tidies a quiz and says what is wrong with it, naming the question.
func (in *quizInput) check() string {
	in.Title, in.Instructions = strings.TrimSpace(in.Title), strings.TrimSpace(in.Instructions)
	switch {
	case !uuidPattern.MatchString(in.SubjectID):
		return "Choose a subject."
	case in.Title == "" || len(in.Title) > 200:
		return "Give it a title."
	case len(in.Instructions) > 5000:
		return "Keep the instructions under 5,000 characters."
	case in.ResourceID != "" && !uuidPattern.MatchString(in.ResourceID):
		return "That worksheet could not be read."
	case len(in.Questions) > maxQuizQuestions:
		return fmt.Sprintf("Use at most %d questions.", maxQuizQuestions)
	}
	switch in.Kind {
	case "quiz":
		if in.Minutes < 5 || in.Minutes > 240 {
			return "Give a quiz from 5 to 240 minutes."
		}
	case "assignment":
		in.Minutes = assignmentMaxMinutes
	default:
		return "Choose a quiz or an assignment."
	}
	for i := range in.Questions {
		if message := checkQuizQuestion(&in.Questions[i]); message != "" {
			return fmt.Sprintf("Question %d: %s", i+1, message)
		}
	}
	return ""
}

// ensureQuizSkill finds or adds a topic under the school's class-work curriculum.
// It is apart from exam prep's, so a teacher's questions never enter the national
// exam bank without an administrator's approval.
func ensureQuizSkill(ctx context.Context, tx pgx.Tx, schoolID, subject, grade, topic string) (string, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('quiz-curriculum:'||$1::text))`, schoolID); err != nil {
		return "", err
	}
	var curriculumID string
	err := tx.QueryRow(ctx, `SELECT id::text FROM curricula WHERE school_id=$1 AND provider=$2 AND name=$3`, schoolID, quizCurriculumProvider, quizCurriculumName).Scan(&curriculumID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO curricula(school_id,name,provider,country_code,version_label,language_code) VALUES($1,$2,$3,'ET','1','en') RETURNING id::text`, schoolID, quizCurriculumName, quizCurriculumProvider).Scan(&curriculumID)
	}
	if err != nil {
		return "", err
	}
	var subjectID string
	err = tx.QueryRow(ctx, `INSERT INTO subjects(curriculum_id,name,grade_level) VALUES($1,$2,$3) ON CONFLICT (curriculum_id,name,grade_level) DO UPDATE SET name=EXCLUDED.name RETURNING id::text`, curriculumID, subject, grade).Scan(&subjectID)
	if err != nil {
		return "", err
	}
	var skillID string
	err = tx.QueryRow(ctx, `SELECT id::text FROM curriculum_nodes WHERE subject_id=$1 AND node_type='skill' AND lower(title)=lower($2)`, subjectID, topic).Scan(&skillID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO curriculum_nodes(subject_id,node_type,title) VALUES($1,'skill',$2) RETURNING id::text`, subjectID, topic).Scan(&skillID)
	}
	return skillID, err
}

func quizQuestionJSON(q quizQuestion) (options, key []byte, err error) {
	switch q.Type {
	case "single_choice":
		type option struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}
		list := make([]option, len(q.Options))
		for i, text := range q.Options {
			list[i] = option{ID: string(rune('A' + i)), Text: text}
		}
		if options, err = json.Marshal(list); err != nil {
			return nil, nil, err
		}
		key, err = json.Marshal([]string{list[q.Correct].ID})
	case "numeric":
		key, err = json.Marshal(map[string]float64{"value": *q.Value, "tolerance": q.Tolerance})
	default:
		key, err = json.Marshal(map[string]string{"guide": q.Guide})
	}
	return options, key, err
}

// saveQuizQuestions writes a quiz's questions. They are live as soon as the quiz
// is: the teacher wrote them for their own class.
func saveQuizQuestions(ctx context.Context, tx pgx.Tx, id auth.Identity, assessmentID, subject, grade string, questions []quizQuestion) error {
	for index, q := range questions {
		skillID, err := ensureQuizSkill(ctx, tx, id.SchoolID, subject, grade, q.Topic)
		if err != nil {
			return err
		}
		options, key, err := quizQuestionJSON(q)
		if err != nil {
			return err
		}
		prompt, err := json.Marshal(map[string]string{"text": q.Prompt})
		if err != nil {
			return err
		}
		var explanation any
		if q.Explanation != "" {
			raw, err := json.Marshal(map[string]string{"text": q.Explanation})
			if err != nil {
				return err
			}
			explanation = string(raw)
		}
		var optionText any
		if options != nil {
			optionText = string(options)
		}
		var questionID string
		err = tx.QueryRow(ctx, `INSERT INTO questions(school_id,question_type,prompt,options,answer_key,explanation,language_code,source_name,license_notes,status,created_by)
			VALUES($1,$2,$3::text::jsonb,$4::text::jsonb,$5::text::jsonb,$6::text::jsonb,'en','Written by the class teacher','School-owned','published',$7) RETURNING id::text`,
			id.SchoolID, q.Type, string(prompt), optionText, string(key), explanation, id.UserID).Scan(&questionID)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO question_skill_tags(question_id,skill_id,weight) VALUES($1,$2,1)`, questionID, skillID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO assessment_items(assessment_id,school_id,question_id,position,points) VALUES($1,$2,$3,$4,$5)`, assessmentID, id.SchoolID, questionID, index+1, q.Points); err != nil {
			return err
		}
	}
	return nil
}

// dropQuizQuestions empties a quiz. A question copied into another quiz stays there.
func dropQuizQuestions(ctx context.Context, tx pgx.Tx, assessmentID string) error {
	rows, err := tx.Query(ctx, `DELETE FROM assessment_items WHERE assessment_id=$1 RETURNING question_id::text`, assessmentID)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM questions q WHERE q.id=ANY($1::uuid[]) AND NOT EXISTS(SELECT 1 FROM assessment_items i WHERE i.question_id=q.id)`, ids)
	return err
}

// checkQuizResource makes sure a worksheet is shared with the quiz's class, so
// every student who sees the quiz can open it.
func checkQuizResource(ctx context.Context, tx pgx.Tx, schoolID, classID, resourceID string) (bool, error) {
	if resourceID == "" {
		return true, nil
	}
	var found bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM teaching_resources WHERE id=$1 AND class_id=$2 AND school_id=$3)`, resourceID, classID, schoolID).Scan(&found)
	return found, err
}

func (a *API) createQuiz(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var in quizInput
	if !decode(w, r, &in) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	if message := in.check(); message != "" {
		writeError(w, 400, "VALIDATION_ERROR", message, nil)
		return
	}
	access, ok := a.loadSubjectAccess(w, r, id, in.SubjectID)
	if !ok || !a.requireSubjectEdit(w, access) {
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	found, err := checkQuizResource(r.Context(), tx, id.SchoolID, access.ClassID, in.ResourceID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !found {
		writeError(w, 400, "VALIDATION_ERROR", "That worksheet is not shared with this class.", nil)
		return
	}
	var grade, quizID string
	if err = tx.QueryRow(r.Context(), `SELECT grade_level FROM classes WHERE id=$1`, access.ClassID).Scan(&grade); err != nil {
		a.serverError(w, r, err)
		return
	}
	err = tx.QueryRow(r.Context(), `INSERT INTO assessments(school_id,title,instructions,duration_minutes,created_by,shuffle_questions,shuffle_options,kind,class_subject_id,resource_id,show_answers) VALUES($1,$2,$3,$4,$5,$6,$6,$7,$8,NULLIF($9,'')::uuid,$10) RETURNING id::text`,
		id.SchoolID, in.Title, in.Instructions, in.Minutes, id.UserID, in.Shuffle, in.Kind, in.SubjectID, in.ResourceID, in.ShowAnswers).Scan(&quizID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = saveQuizQuestions(r.Context(), tx, id, quizID, access.Name, grade, in.Questions); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]string{"id": quizID}})
}

type quizRecord struct {
	ID, SubjectID, Subject, Title, Kind, Status string
	Instructions, ResourceID, AssignmentID      string
	ClassID, Grade                              string
	Minutes                                     int
	Attempts                                    int
	Access                                      subjectAccess
}

// loadQuiz finds a teacher's quiz and checks who is asking. Exams set by the
// office are not reached through these routes.
func (a *API) loadQuiz(w http.ResponseWriter, r *http.Request, id auth.Identity, q reportQuerier) (quizRecord, bool) {
	var x quizRecord
	err := q.QueryRow(r.Context(), `SELECT a.id::text,a.class_subject_id::text,a.title,a.kind,a.status,a.instructions,COALESCE(a.resource_id::text,''),COALESCE(aa.id::text,''),a.duration_minutes,
		c.grade_level,(SELECT count(*)::int FROM assessment_attempts at WHERE at.assignment_id=aa.id)
		FROM assessments a JOIN class_subjects cs ON cs.id=a.class_subject_id JOIN classes c ON c.id=cs.class_id LEFT JOIN assessment_assignments aa ON aa.assessment_id=a.id
		WHERE a.id=$1 AND a.school_id=$2 AND a.kind<>'exam'`, r.PathValue("quizID"), id.SchoolID).Scan(&x.ID, &x.SubjectID, &x.Title, &x.Kind, &x.Status, &x.Instructions, &x.ResourceID, &x.AssignmentID, &x.Minutes, &x.Grade, &x.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "NOT_FOUND", "Quiz not found.", nil)
		return x, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return x, false
	}
	access, ok := a.loadSubjectAccess(w, r, id, x.SubjectID)
	if !ok {
		return x, false
	}
	x.Access, x.ClassID, x.Subject = access, access.ClassID, access.Name
	return x, true
}

func (a *API) listQuizTopics(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	subjectID := r.URL.Query().Get("class_subject_id")
	if !uuidPattern.MatchString(subjectID) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a subject.", nil)
		return
	}
	access, ok := a.loadSubjectAccess(w, r, id, subjectID)
	if !ok {
		return
	}
	// Topics already used in this subject and grade, so spelling stays the same.
	rows, err := a.db.Query(r.Context(), `SELECT n.title FROM curriculum_nodes n JOIN subjects s ON s.id=n.subject_id JOIN curricula c ON c.id=s.curriculum_id JOIN classes cl ON cl.id=$4
		WHERE c.school_id=$1 AND c.provider=$2 AND lower(s.name)=lower($3) AND s.grade_level=cl.grade_level AND n.node_type='skill' ORDER BY n.title LIMIT 500`, id.SchoolID, quizCurriculumProvider, access.Name, access.ClassID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	topics, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if topics == nil {
		topics = []string{}
	}
	writeJSON(w, 200, map[string]any{"data": topics})
}

// listQuizzes shows a class's quizzes and assignments, with how far along each is.
func (a *API) listQuizzes(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	classID := r.URL.Query().Get("class_id")
	if !uuidPattern.MatchString(classID) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a class.", nil)
		return
	}
	if !a.requireClass(w, r, id, classID) {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT a.id::text,a.title,a.kind,a.status,cs.name,cs.id::text,a.duration_minutes,
		COALESCE(aa.id::text,''),to_char(aa.opens_at AT TIME ZONE 'UTC',`+isoTimestamp+`),to_char(aa.due_at AT TIME ZONE 'UTC',`+isoTimestamp+`),
		(SELECT count(*)::int FROM assessment_items i WHERE i.assessment_id=a.id),COALESCE((SELECT sum(i.points)::float8 FROM assessment_items i WHERE i.assessment_id=a.id),0),
		(SELECT count(*)::int FROM enrollments e JOIN students s ON s.id=e.student_id WHERE e.class_id=$1 AND e.status='active' AND s.status='active'),
		COALESCE(st.submitted,0),COALESCE(st.waiting,0),st.average,a.mark_item_id IS NOT NULL,
		$3 OR COALESCE(cs.teacher_id=$4,false) OR cs.teacher_id IS NULL
		FROM assessments a JOIN class_subjects cs ON cs.id=a.class_subject_id
		LEFT JOIN assessment_assignments aa ON aa.assessment_id=a.id
		LEFT JOIN LATERAL (SELECT count(*) FILTER (WHERE at.status='submitted')::int submitted,count(*) FILTER (WHERE at.status='submitted' AND NOT at.results_released)::int waiting,
			avg(at.final_score/NULLIF(at.final_max_score,0)*100) FILTER (WHERE at.results_released)::float8 average FROM assessment_attempts at WHERE at.assignment_id=aa.id) st ON TRUE
		WHERE cs.class_id=$1 AND a.school_id=$2 AND a.kind<>'exam' ORDER BY a.created_at DESC LIMIT 200`, classID, id.SchoolID, hasRole(id.Roles, "school_admin"), id.UserID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type row struct {
		ID           string   `json:"id"`
		Title        string   `json:"title"`
		Kind         string   `json:"kind"`
		Status       string   `json:"status"`
		Subject      string   `json:"subject"`
		SubjectID    string   `json:"class_subject_id"`
		Minutes      int      `json:"duration_minutes"`
		AssignmentID string   `json:"assignment_id"`
		OpensAt      *string  `json:"opens_at"`
		DueAt        *string  `json:"due_at"`
		Questions    int      `json:"question_count"`
		Points       float64  `json:"total_points"`
		Students     int      `json:"students"`
		Submitted    int      `json:"submitted"`
		Waiting      int      `json:"waiting_review"`
		Average      *float64 `json:"average_percent"`
		InMarks      bool     `json:"in_marks"`
		CanEdit      bool     `json:"can_edit"`
	}
	items := []row{}
	for rows.Next() {
		var x row
		if err = rows.Scan(&x.ID, &x.Title, &x.Kind, &x.Status, &x.Subject, &x.SubjectID, &x.Minutes, &x.AssignmentID, &x.OpensAt, &x.DueAt, &x.Questions, &x.Points, &x.Students, &x.Submitted, &x.Waiting, &x.Average, &x.InMarks, &x.CanEdit); err != nil {
			a.serverError(w, r, err)
			return
		}
		if x.Average != nil {
			value := round2(*x.Average)
			x.Average = &value
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

// getQuiz returns a quiz with its answers, for its class's teachers to read or edit.
func (a *API) getQuiz(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	quiz, ok := a.loadQuiz(w, r, id, a.db)
	if !ok {
		return
	}
	var shuffle, showAnswers bool
	if err := a.db.QueryRow(r.Context(), `SELECT shuffle_questions,show_answers FROM assessments WHERE id=$1`, quiz.ID).Scan(&shuffle, &showAnswers); err != nil {
		a.serverError(w, r, err)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT q.question_type,q.prompt->>'text',COALESCE(q.options,'[]'::jsonb),q.answer_key,COALESCE(q.explanation->>'text',''),i.points::float8,COALESCE((SELECT n.title FROM question_skill_tags t JOIN curriculum_nodes n ON n.id=t.skill_id WHERE t.question_id=q.id LIMIT 1),'')
		FROM assessment_items i JOIN questions q ON q.id=i.question_id WHERE i.assessment_id=$1 ORDER BY i.position`, quiz.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	questions := []quizQuestion{}
	for rows.Next() {
		var x quizQuestion
		var options, key []byte
		if err = rows.Scan(&x.Type, &x.Prompt, &options, &key, &x.Explanation, &x.Points, &x.Topic); err != nil {
			a.serverError(w, r, err)
			return
		}
		if err = readQuizAnswer(&x, options, key); err != nil {
			a.serverError(w, r, err)
			return
		}
		questions = append(questions, x)
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"id": quiz.ID, "class_subject_id": quiz.SubjectID, "subject": quiz.Subject, "title": quiz.Title, "kind": quiz.Kind, "status": quiz.Status, "instructions": quiz.Instructions,
		"duration_minutes": quiz.Minutes, "resource_id": quiz.ResourceID, "show_answers": showAnswers, "shuffle": shuffle, "attempts": quiz.Attempts, "can_edit": quiz.Access.CanEdit, "questions": questions}})
}

func readQuizAnswer(x *quizQuestion, options, key []byte) error {
	switch x.Type {
	case "single_choice":
		var list []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}
		var correct []string
		if err := json.Unmarshal(options, &list); err != nil {
			return err
		}
		if err := json.Unmarshal(key, &correct); err != nil {
			return err
		}
		for i, option := range list {
			x.Options = append(x.Options, option.Text)
			if len(correct) > 0 && option.ID == correct[0] {
				x.Correct = i
			}
		}
	case "numeric":
		var value struct {
			Value     float64 `json:"value"`
			Tolerance float64 `json:"tolerance"`
		}
		if err := json.Unmarshal(key, &value); err != nil {
			return err
		}
		x.Value, x.Tolerance = &value.Value, value.Tolerance
	default:
		var guide struct {
			Guide string `json:"guide"`
		}
		if err := json.Unmarshal(key, &guide); err != nil {
			return err
		}
		x.Guide = guide.Guide
	}
	return nil
}

func (a *API) updateQuiz(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var in quizInput
	if !decode(w, r, &in) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	if message := in.check(); message != "" {
		writeError(w, 400, "VALIDATION_ERROR", message, nil)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	quiz, ok := a.loadQuiz(w, r, id, tx)
	if !ok || !a.requireSubjectEdit(w, quiz.Access) {
		return
	}
	if quiz.Status != "draft" {
		writeError(w, 409, "QUIZ_PUBLISHED", "A published quiz cannot be changed. Take it back first, if nobody has started it.", nil)
		return
	}
	if in.SubjectID != quiz.SubjectID {
		writeError(w, 400, "VALIDATION_ERROR", "A quiz stays with its subject. Copy it to use it for another.", nil)
		return
	}
	found, err := checkQuizResource(r.Context(), tx, id.SchoolID, quiz.ClassID, in.ResourceID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !found {
		writeError(w, 400, "VALIDATION_ERROR", "That worksheet is not shared with this class.", nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE assessments SET title=$2,instructions=$3,duration_minutes=$4,shuffle_questions=$5,shuffle_options=$5,kind=$6,resource_id=NULLIF($7,'')::uuid,show_answers=$8,updated_at=now() WHERE id=$1`,
		quiz.ID, in.Title, in.Instructions, in.Minutes, in.Shuffle, in.Kind, in.ResourceID, in.ShowAnswers); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = dropQuizQuestions(r.Context(), tx, quiz.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = saveQuizQuestions(r.Context(), tx, id, quiz.ID, quiz.Subject, quiz.Grade, in.Questions); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": quiz.ID}})
}

// deleteQuiz removes a quiz nobody has started. It also serves to take back a
// published one: students stop seeing it at once.
func (a *API) deleteQuiz(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	quiz, ok := a.loadQuiz(w, r, id, tx)
	if !ok || !a.requireSubjectEdit(w, quiz.Access) {
		return
	}
	if quiz.Attempts > 0 {
		writeError(w, 409, "QUIZ_STARTED", "Students have started this, so it cannot be deleted. Their work is kept.", nil)
		return
	}
	if err = dropQuizQuestions(r.Context(), tx, quiz.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM assessments WHERE id=$1`, quiz.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'quiz.deleted','class',$3,jsonb_build_object('title',$4::text))`, id.SchoolID, id.UserID, quiz.ClassID, quiz.Title); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": quiz.ID}})
}

// quizWindow reads and checks when a quiz opens and closes. An assignment needs
// a due date, because that is what ends it.
func quizWindow(kind string, opensAt, dueAt *string) (*time.Time, *time.Time, string) {
	opens, due, message := parseAssignmentWindow(opensAt, dueAt)
	if message != "" {
		return nil, nil, message
	}
	if kind == "assignment" && due == nil {
		return nil, nil, "Choose when the assignment is due."
	}
	if due != nil {
		start := time.Now()
		if opens != nil && opens.After(start) {
			start = *opens
		}
		if !due.After(time.Now()) {
			return nil, nil, "The due time must be in the future."
		}
		if kind == "assignment" && due.Sub(start) > assignmentMaxMinutes*time.Minute {
			return nil, nil, "An assignment can stay open for up to 30 days."
		}
	}
	return opens, due, ""
}

func (a *API) publishQuiz(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		OpensAt *string `json:"opens_at"`
		DueAt   *string `json:"due_at"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	quiz, ok := a.loadQuiz(w, r, id, tx)
	if !ok || !a.requireSubjectEdit(w, quiz.Access) {
		return
	}
	if quiz.Status != "draft" {
		writeError(w, 409, "QUIZ_PUBLISHED", "This has already been published.", nil)
		return
	}
	opens, due, message := quizWindow(quiz.Kind, req.OpensAt, req.DueAt)
	if message != "" {
		writeError(w, 400, "VALIDATION_ERROR", message, nil)
		return
	}
	var count int
	if err = tx.QueryRow(r.Context(), `SELECT count(*)::int FROM assessment_items WHERE assessment_id=$1`, quiz.ID).Scan(&count); err != nil {
		a.serverError(w, r, err)
		return
	}
	if count == 0 {
		writeError(w, 409, "ASSESSMENT_EMPTY", "Add at least one question before publishing.", nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE assessments SET status='published',updated_at=now() WHERE id=$1`, quiz.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	var assignmentID string
	err = tx.QueryRow(r.Context(), `INSERT INTO assessment_assignments(school_id,assessment_id,class_id,assigned_by,opens_at,due_at) VALUES($1,$2,$3,$4,$5,$6) RETURNING id::text`, id.SchoolID, quiz.ID, quiz.ClassID, id.UserID, opens, due).Scan(&assignmentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = a.noticeSchoolwork(r.Context(), tx, id.SchoolID, assignmentID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'quiz.published','class',$3,jsonb_build_object('title',$4::text,'questions',$5::int))`, id.SchoolID, id.UserID, quiz.ClassID, quiz.Title, count); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": quiz.ID, "assignment_id": assignmentID}})
}

// rescheduleQuiz moves a published quiz's due date. Work under way gets the new
// date too, so a student who already started is not left behind by an extension.
func (a *API) rescheduleQuiz(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		DueAt *string `json:"due_at"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	quiz, ok := a.loadQuiz(w, r, id, tx)
	if !ok || !a.requireSubjectEdit(w, quiz.Access) {
		return
	}
	if quiz.AssignmentID == "" {
		writeError(w, 409, "QUIZ_NOT_PUBLISHED", "Publish this first.", nil)
		return
	}
	var opensAt *time.Time
	if err = tx.QueryRow(r.Context(), `SELECT opens_at FROM assessment_assignments WHERE id=$1`, quiz.AssignmentID).Scan(&opensAt); err != nil {
		a.serverError(w, r, err)
		return
	}
	var opens *string
	if opensAt != nil {
		value := opensAt.Format(time.RFC3339)
		opens = &value
	}
	_, due, message := quizWindow(quiz.Kind, opens, req.DueAt)
	if message != "" {
		writeError(w, 400, "VALIDATION_ERROR", message, nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE assessment_assignments SET due_at=$2 WHERE id=$1`, quiz.AssignmentID, due); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE assessment_attempts SET deadline_at=CASE WHEN $2::timestamptz IS NULL THEN started_at+make_interval(mins=>$3) ELSE LEAST(started_at+make_interval(mins=>$3),$2::timestamptz) END WHERE assignment_id=$1 AND status='in_progress'`, quiz.AssignmentID, due, quiz.Minutes); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": quiz.ID}})
}

// copyQuiz makes a draft of a quiz for another class's subject, so a teacher of
// several sections writes it once.
func (a *API) copyQuiz(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		SubjectID string `json:"class_subject_id"`
	}
	if !decode(w, r, &req) || !uuidPattern.MatchString(req.SubjectID) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a class and subject to copy to.", nil)
		return
	}
	quiz, ok := a.loadQuiz(w, r, id, a.db)
	if !ok {
		return
	}
	target, ok := a.loadSubjectAccess(w, r, id, req.SubjectID)
	if !ok || !a.requireSubjectEdit(w, target) {
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	// A worksheet belongs to one class, so the copy starts without it.
	var copyID string
	err = tx.QueryRow(r.Context(), `INSERT INTO assessments(school_id,title,instructions,duration_minutes,created_by,shuffle_questions,shuffle_options,kind,class_subject_id,show_answers)
		SELECT school_id,title,instructions,duration_minutes,$2,shuffle_questions,shuffle_options,kind,$3,show_answers FROM assessments WHERE id=$1 RETURNING id::text`, quiz.ID, id.UserID, req.SubjectID).Scan(&copyID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO assessment_items(assessment_id,school_id,question_id,position,points) SELECT $2,school_id,question_id,position,points FROM assessment_items WHERE assessment_id=$1`, quiz.ID, copyID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]string{"id": copyID}})
}

// quizResults is the class's view of one quiz: who has done it, how each question
// went, and which topics the class finds hard.
func (a *API) quizResults(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	quiz, ok := a.loadQuiz(w, r, id, a.db)
	if !ok {
		return
	}
	if quiz.AssignmentID == "" {
		writeError(w, 409, "QUIZ_NOT_PUBLISHED", "Publish this first.", nil)
		return
	}
	studentRows, err := a.db.Query(r.Context(), `SELECT s.id::text,s.student_number,s.legal_name,COALESCE(at.status,'not_started'),to_char(at.submitted_at AT TIME ZONE 'UTC',`+isoTimestamp+`),
		COALESCE(at.results_released,false),at.final_score::float8,at.final_max_score::float8,at.score::float8,at.max_score::float8
		FROM students s LEFT JOIN assessment_attempts at ON at.student_id=s.id AND at.assignment_id=$2
		WHERE s.school_id=$3 AND (at.id IS NOT NULL OR (s.status='active' AND EXISTS(SELECT 1 FROM enrollments e WHERE e.student_id=s.id AND e.class_id=$1 AND e.status='active')))
		ORDER BY s.legal_name,s.student_number LIMIT 1000`, quiz.ClassID, quiz.AssignmentID, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer studentRows.Close()
	type studentRow struct {
		ID          string   `json:"student_id"`
		Number      string   `json:"student_number"`
		Name        string   `json:"student_name"`
		Status      string   `json:"status"`
		SubmittedAt *string  `json:"submitted_at"`
		Released    bool     `json:"results_released"`
		FinalScore  *float64 `json:"final_score"`
		FinalMax    *float64 `json:"final_max_score"`
		Score       *float64 `json:"auto_score"`
		Max         *float64 `json:"auto_max_score"`
	}
	students := []studentRow{}
	for studentRows.Next() {
		var x studentRow
		if err = studentRows.Scan(&x.ID, &x.Number, &x.Name, &x.Status, &x.SubmittedAt, &x.Released, &x.FinalScore, &x.FinalMax, &x.Score, &x.Max); err != nil {
			a.serverError(w, r, err)
			return
		}
		students = append(students, x)
	}
	if err = studentRows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	studentRows.Close()

	// Work a teacher has not marked yet stays out of the figures.
	questionRows, err := a.db.Query(r.Context(), `SELECT i.position,q.prompt->>'text',q.question_type,COALESCE(n.title,''),i.points::float8,
		count(ans.attempt_id) FILTER (WHERE ans.awarded_points IS NOT NULL)::int,COALESCE(sum(ans.awarded_points)::float8,0)
		FROM assessment_items i JOIN questions q ON q.id=i.question_id
		LEFT JOIN question_skill_tags t ON t.question_id=q.id LEFT JOIN curriculum_nodes n ON n.id=t.skill_id
		LEFT JOIN assessment_attempts at ON at.assignment_id=$2 AND at.status='submitted'
		LEFT JOIN assessment_answers ans ON ans.attempt_id=at.id AND ans.question_id=q.id AND (ans.is_correct IS NOT NULL OR ans.reviewed_at IS NOT NULL)
		WHERE i.assessment_id=$1 GROUP BY i.position,q.prompt,q.question_type,n.title,i.points ORDER BY i.position`, quiz.ID, quiz.AssignmentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer questionRows.Close()
	type questionRow struct {
		Position int      `json:"position"`
		Prompt   string   `json:"prompt"`
		Type     string   `json:"question_type"`
		Topic    string   `json:"topic"`
		Points   float64  `json:"points"`
		Marked   int      `json:"answers_marked"`
		Percent  *float64 `json:"percent"`
	}
	questions := []questionRow{}
	type topicTotals struct{ awarded, possible float64 }
	totals := map[string]*topicTotals{}
	for questionRows.Next() {
		var x questionRow
		var awarded float64
		if err = questionRows.Scan(&x.Position, &x.Prompt, &x.Type, &x.Topic, &x.Points, &x.Marked, &awarded); err != nil {
			a.serverError(w, r, err)
			return
		}
		if x.Marked > 0 {
			percent := round2(awarded / (x.Points * float64(x.Marked)) * 100)
			x.Percent = &percent
			if totals[x.Topic] == nil {
				totals[x.Topic] = &topicTotals{}
			}
			totals[x.Topic].awarded += awarded
			totals[x.Topic].possible += x.Points * float64(x.Marked)
		}
		questions = append(questions, x)
	}
	if err = questionRows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	type topicRow struct {
		Topic   string  `json:"topic"`
		Percent float64 `json:"percent"`
	}
	topics := []topicRow{}
	for topic, total := range totals {
		topics = append(topics, topicRow{Topic: topic, Percent: round2(total.awarded / total.possible * 100)})
	}
	slices.SortFunc(topics, func(p, q topicRow) int {
		switch {
		case p.Percent < q.Percent:
			return -1
		case p.Percent > q.Percent:
			return 1
		}
		return strings.Compare(p.Topic, q.Topic)
	})
	writeJSON(w, 200, map[string]any{"data": map[string]any{"title": quiz.Title, "kind": quiz.Kind, "assignment_id": quiz.AssignmentID, "can_edit": quiz.Access.CanEdit, "students": students, "questions": questions, "topics": topics}})
}

// sendQuizToMarks copies released scores into the subject's marks, as one test
// for a term. Sending again updates the same test. A student whose work is not
// yet marked stays blank, so a missing mark is never mistaken for a zero.
func (a *API) sendQuizToMarks(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		TermID string  `json:"term_id"`
		Weight float64 `json:"weight"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	if !uuidPattern.MatchString(req.TermID) || req.Weight <= 0 || req.Weight > 100 || math.IsNaN(req.Weight) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a term, and a share of the total from 0.01 to 100.", nil)
		return
	}
	req.Weight = round2(req.Weight)
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	quiz, ok := a.loadQuiz(w, r, id, tx)
	if !ok || !a.requireSubjectEdit(w, quiz.Access) {
		return
	}
	if quiz.AssignmentID == "" {
		writeError(w, 409, "QUIZ_NOT_PUBLISHED", "Publish this first.", nil)
		return
	}
	if err = lockClass(r.Context(), tx, quiz.ClassID, "UPDATE"); err != nil {
		a.serverError(w, r, err)
		return
	}
	found, err := a.termInClass(r.Context(), tx, id.SchoolID, req.TermID, quiz.ClassID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !found {
		writeError(w, 400, "VALIDATION_ERROR", "That term is not in this class's academic year.", nil)
		return
	}
	published, err := reportPublished(r.Context(), tx, req.TermID, quiz.ClassID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if published {
		writeReportPublished(w)
		return
	}
	var itemID, itemTerm string
	var itemMissing bool
	err = tx.QueryRow(r.Context(), `SELECT a.mark_item_id::text,i.term_id::text FROM assessments a JOIN mark_items i ON i.id=a.mark_item_id WHERE a.id=$1`, quiz.ID).Scan(&itemID, &itemTerm)
	if errors.Is(err, pgx.ErrNoRows) {
		itemMissing = true
	} else if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !itemMissing && itemTerm != req.TermID {
		writeError(w, 409, "QUIZ_IN_OTHER_TERM", "These scores are already in another term's marks. Remove that test from marks first.", nil)
		return
	}
	message, err := checkItemWeights(r.Context(), tx, quiz.SubjectID, req.TermID, itemID, req.Weight)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if message != "" {
		writeError(w, 400, "WEIGHTS_OVER_100", message, nil)
		return
	}
	var points float64
	if err = tx.QueryRow(r.Context(), `SELECT COALESCE(sum(points)::float8,0) FROM assessment_items WHERE assessment_id=$1`, quiz.ID).Scan(&points); err != nil {
		a.serverError(w, r, err)
		return
	}
	maxScore := min(round2(points), maxMarkScore)
	if itemMissing {
		err = tx.QueryRow(r.Context(), `INSERT INTO mark_items(school_id,class_subject_id,term_id,title,weight,max_score) VALUES($1,$2,$3,$4,$5,$6) RETURNING id::text`, id.SchoolID, quiz.SubjectID, req.TermID, quiz.Title, req.Weight, maxScore).Scan(&itemID)
		if err != nil {
			a.databaseError(w, r, err)
			return
		}
		if _, err = tx.Exec(r.Context(), `UPDATE assessments SET mark_item_id=$2 WHERE id=$1`, quiz.ID, itemID); err != nil {
			a.serverError(w, r, err)
			return
		}
	} else if _, err = tx.Exec(r.Context(), `UPDATE mark_items SET weight=$2 WHERE id=$1`, itemID, req.Weight); err != nil {
		a.serverError(w, r, err)
		return
	}
	tag, err := tx.Exec(r.Context(), `INSERT INTO marks(school_id,item_id,student_id,score,recorded_by)
		SELECT at.school_id,$2,at.student_id,LEAST(ROUND(at.final_score/NULLIF(at.final_max_score,0)*$3::numeric,2),$3::numeric),$4 FROM assessment_attempts at WHERE at.assignment_id=$1 AND at.results_released AND at.final_max_score>0
		ON CONFLICT (item_id,student_id) DO UPDATE SET score=EXCLUDED.score,recorded_by=EXCLUDED.recorded_by,updated_at=now()`, quiz.AssignmentID, itemID, maxScore, id.UserID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var waiting int
	if err = tx.QueryRow(r.Context(), `SELECT count(*)::int FROM assessment_attempts WHERE assignment_id=$1 AND NOT results_released`, quiz.AssignmentID).Scan(&waiting); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'quiz.sent_to_marks','class',$3,jsonb_build_object('title',$4::text,'term_id',$5::text,'sent',$6::int))`, id.SchoolID, id.UserID, quiz.ClassID, quiz.Title, req.TermID, tag.RowsAffected()); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"sent": tag.RowsAffected(), "waiting": waiting, "mark_item_id": itemID}})
}
