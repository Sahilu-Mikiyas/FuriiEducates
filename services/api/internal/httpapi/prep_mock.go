package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
)

func (a *API) registerMockRoundRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/prep/mock-rounds", a.staffMember(a.listMockRounds))
	mux.HandleFunc("POST /api/v1/prep/mock-rounds", a.admin(a.createMockRound))
	mux.HandleFunc("GET /api/v1/prep/mock-rounds/{roundID}/results", a.staffMember(a.mockRoundResults))
	mux.HandleFunc("POST /api/v1/prep/mock-rounds/{roundID}/reschedule", a.admin(a.rescheduleMockPaper))
	mux.HandleFunc("POST /api/v1/prep/mock-rounds/{roundID}/cancel", a.admin(a.cancelMockRound))
}

// mockSlot is one paper's place on the timetable.
type mockSlot struct {
	Paper   string
	Opens   time.Time
	Minutes int
}

// paperStream says which stream sits a paper. Common papers return "", which
// means every student in the class.
func paperStream(paper string) string {
	switch {
	case slices.Contains(naturalPapers, paper):
		return "natural"
	case slices.Contains(socialPapers, paper):
		return "social"
	}
	return ""
}

// mockTimetableProblem checks that a timetable can be sat. It needs all three
// common papers, and each stream's three papers or none of them. No student may
// have two papers at once, counting from opening until the last possible finish.
// Natural and social papers can share a time because no student sits both.
func mockTimetableProblem(slots []mockSlot, startWindow int) string {
	has := func(papers []string) int {
		count := 0
		for _, paper := range papers {
			if slices.ContainsFunc(slots, func(slot mockSlot) bool { return slot.Paper == paper }) {
				count++
			}
		}
		return count
	}
	if has(commonPapers) != len(commonPapers) {
		return "A mock day needs English, Mathematics and Scholastic Aptitude."
	}
	natural, social := has(naturalPapers), has(socialPapers)
	if natural%len(naturalPapers) != 0 || social%len(socialPapers) != 0 || natural+social == 0 {
		return "Add all three natural science papers, all three social science papers, or both."
	}
	for _, papers := range [][]string{streamPapers("natural"), streamPapers("social")} {
		for i, first := range slots {
			for _, second := range slots[i+1:] {
				if !slices.Contains(papers, first.Paper) || !slices.Contains(papers, second.Paper) {
					continue
				}
				firstEnd := first.Opens.Add(time.Duration(startWindow+first.Minutes) * time.Minute)
				secondEnd := second.Opens.Add(time.Duration(startWindow+second.Minutes) * time.Minute)
				if first.Opens.Before(secondEnd) && second.Opens.Before(firstEnd) {
					return fmt.Sprintf("%s and %s overlap. A student could still be in the first when the second opens.", paperTitles[first.Paper], paperTitles[second.Paper])
				}
			}
		}
	}
	return ""
}

func (a *API) createMockRound(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Name              string   `json:"name"`
		ClassIDs          []string `json:"class_ids"`
		QuestionCount     int      `json:"question_count"`
		StartWindow       int      `json:"start_window_minutes"`
		RequiresStartCode bool     `json:"requires_start_code"`
		Papers            []struct {
			Paper   string    `json:"paper"`
			OpensAt time.Time `json:"opens_at"`
			Minutes int       `json:"duration_minutes"`
		} `json:"papers"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 120 || len(req.ClassIDs) == 0 || req.QuestionCount < 5 || req.QuestionCount > 100 || req.StartWindow < 5 || req.StartWindow > 240 {
		writeError(w, 400, "VALIDATION_ERROR", "Give the mock day a name, at least one class, 5 to 100 questions per paper, and 5 to 240 minutes to start.", nil)
		return
	}
	for _, classID := range req.ClassIDs {
		if !uuidPattern.MatchString(classID) {
			writeError(w, 400, "VALIDATION_ERROR", "A class could not be read.", nil)
			return
		}
	}
	slots := make([]mockSlot, 0, len(req.Papers))
	now := time.Now()
	for _, paper := range req.Papers {
		if !validPaper(paper.Paper) || paper.Minutes < 5 || paper.Minutes > 240 || slices.ContainsFunc(slots, func(slot mockSlot) bool { return slot.Paper == paper.Paper }) {
			writeError(w, 400, "VALIDATION_ERROR", "Each paper appears once and lasts 5 to 240 minutes.", nil)
			return
		}
		if !paper.OpensAt.After(now) {
			writeError(w, 400, "VALIDATION_ERROR", paperTitles[paper.Paper]+" must open in the future.", nil)
			return
		}
		slots = append(slots, mockSlot{paper.Paper, paper.OpensAt, paper.Minutes})
	}
	if problem := mockTimetableProblem(slots, req.StartWindow); problem != "" {
		writeError(w, 400, "VALIDATION_ERROR", problem, nil)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var classes int
	if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM classes WHERE school_id=$1 AND id=ANY($2::uuid[])`, id.SchoolID, req.ClassIDs).Scan(&classes); err != nil {
		a.serverError(w, r, err)
		return
	}
	if classes != len(slicesCompactSorted(req.ClassIDs)) {
		writeError(w, 400, "VALIDATION_ERROR", "A chosen class is not in this school.", nil)
		return
	}
	// Check every paper first, so the school sees all the shortages at once.
	drawn := map[string][]string{}
	shortages := []string{}
	for _, slot := range slots {
		questionIDs, err := drawMockQuestions(r.Context(), tx, id.SchoolID, slot.Paper, req.QuestionCount)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if len(questionIDs) < req.QuestionCount {
			shortages = append(shortages, fmt.Sprintf("%s has %d", paperTitles[slot.Paper], len(questionIDs)))
		}
		drawn[slot.Paper] = questionIDs
	}
	if len(shortages) > 0 {
		writeError(w, 409, "NOT_ENOUGH_QUESTIONS", fmt.Sprintf("Each paper needs %d approved questions. %s.", req.QuestionCount, strings.Join(shortages, ", ")), nil)
		return
	}
	var roundID string
	if err = tx.QueryRow(r.Context(), `INSERT INTO mock_rounds(school_id,name,created_by) VALUES($1,$2,$3) RETURNING id::text`, id.SchoolID, req.Name, id.UserID).Scan(&roundID); err != nil {
		a.serverError(w, r, err)
		return
	}
	for _, slot := range slots {
		var assessmentID string
		err = tx.QueryRow(r.Context(), `INSERT INTO assessments(school_id,title,instructions,duration_minutes,created_by,status,shuffle_questions,shuffle_options,locked_mode,national_paper,mock_round_id)
			VALUES($1,$2,'',$3,$4,'published',true,true,true,$5,$6) RETURNING id::text`,
			id.SchoolID, req.Name+": "+paperTitles[slot.Paper], slot.Minutes, id.UserID, slot.Paper, roundID).Scan(&assessmentID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		for index, questionID := range drawn[slot.Paper] {
			if _, err = tx.Exec(r.Context(), `INSERT INTO assessment_items(assessment_id,school_id,question_id,position,points) VALUES($1,$2,$3,$4,1)`, assessmentID, id.SchoolID, questionID, index+1); err != nil {
				a.serverError(w, r, err)
				return
			}
		}
		var stream *string
		if value := paperStream(slot.Paper); value != "" {
			stream = &value
		}
		due := slot.Opens.Add(time.Duration(req.StartWindow) * time.Minute)
		if _, err = tx.Exec(r.Context(), `INSERT INTO assessment_assignments(school_id,assessment_id,class_id,assigned_by,opens_at,due_at,requires_start_code,stream)
			SELECT $1,$2,c.id,$3,$4,$5,$6,$7 FROM classes c WHERE c.school_id=$1 AND c.id=ANY($8::uuid[])`,
			id.SchoolID, assessmentID, id.UserID, slot.Opens, due, req.RequiresStartCode, stream, req.ClassIDs); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'mock_round.created','mock_round',$3,jsonb_build_object('papers',$4::int,'classes',$5::int))`, id.SchoolID, id.UserID, roundID, len(slots), classes); err != nil {
		a.serverError(w, r, err)
		return
	}
	// Tell the school about students this timetable leaves out, while they can still fix it.
	var noStream, missing int
	covered := []string{}
	for _, stream := range []string{"natural", "social"} {
		if slices.ContainsFunc(slots, func(slot mockSlot) bool { return paperStream(slot.Paper) == stream }) {
			covered = append(covered, stream)
		}
	}
	err = tx.QueryRow(r.Context(), `SELECT count(DISTINCT s.id) FILTER (WHERE s.stream IS NULL),count(DISTINCT s.id) FILTER (WHERE s.stream IS NOT NULL AND NOT s.stream=ANY($3))
		FROM enrollments e JOIN students s ON s.id=e.student_id AND s.school_id=e.school_id
		WHERE e.school_id=$1 AND e.class_id=ANY($2::uuid[]) AND e.status='active' AND s.status='active'`, id.SchoolID, req.ClassIDs, covered).Scan(&noStream, &missing)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]any{"id": roundID, "papers": len(slots), "students_without_stream": noStream, "students_without_papers": missing}})
}

// drawMockQuestions picks a paper's questions. It prefers exam-only questions
// students have not practised, then those used least in earlier mock days.
func drawMockQuestions(ctx context.Context, tx pgx.Tx, schoolID, paper string, count int) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT q.id::text FROM questions q
		WHERE q.school_id=$1 AND q.status='published' AND q.question_type='single_choice' AND `+prepPaperFilterSQL+`
		ORDER BY q.exam_only DESC,
		         (SELECT count(*) FROM assessment_items ai JOIN assessments a ON a.id=ai.assessment_id WHERE ai.question_id=q.id AND a.mock_round_id IS NOT NULL) ASC,
		         random() LIMIT $3`, schoolID, paper, count)
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

// staffClassFilter limits a teacher to their own classes. Administrators see
// the whole school, which nil means.
func (a *API) staffClassFilter(ctx context.Context, id auth.Identity) ([]string, error) {
	if hasRole(id.Roles, "school_admin") {
		return nil, nil
	}
	rows, err := a.db.Query(ctx, `SELECT class_id::text FROM class_teachers WHERE school_id=$1 AND teacher_id=$2`, id.SchoolID, id.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	classIDs := []string{}
	for rows.Next() {
		var classID string
		if err = rows.Scan(&classID); err != nil {
			return nil, err
		}
		classIDs = append(classIDs, classID)
	}
	return classIDs, rows.Err()
}

type mockPaperInfo struct {
	Paper      string `json:"paper"`
	OpensAt    string `json:"opens_at"`
	DueAt      string `json:"due_at"`
	Minutes    int    `json:"duration_minutes"`
	Questions  int    `json:"question_count"`
	Stream     string `json:"stream"`
	InProgress int    `json:"in_progress"`
	Submitted  int    `json:"submitted"`
}

func (a *API) listMockRounds(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	classFilter, err := a.staffClassFilter(r.Context(), id)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT m.id::text,m.name,m.created_at::text,a.national_paper,a.duration_minutes,
		(SELECT count(*)::int FROM assessment_items ai WHERE ai.assessment_id=a.id),
		to_char(min(aa.opens_at) AT TIME ZONE 'UTC',`+isoTimestamp+`),to_char(min(aa.due_at) AT TIME ZONE 'UTC',`+isoTimestamp+`),COALESCE(min(aa.stream),''),
		(SELECT count(*)::int FROM assessment_attempts t JOIN assessment_assignments x ON x.id=t.assignment_id WHERE x.assessment_id=a.id AND t.status='in_progress' AND ($3 OR x.class_id=ANY($2::uuid[]))),
		(SELECT count(*)::int FROM assessment_attempts t JOIN assessment_assignments x ON x.id=t.assignment_id WHERE x.assessment_id=a.id AND t.status='submitted' AND ($3 OR x.class_id=ANY($2::uuid[])))
		FROM mock_rounds m JOIN assessments a ON a.mock_round_id=m.id AND a.school_id=m.school_id JOIN assessment_assignments aa ON aa.assessment_id=a.id AND aa.school_id=a.school_id
		WHERE m.school_id=$1 AND ($3 OR aa.class_id=ANY($2::uuid[]))
		GROUP BY m.id,m.name,m.created_at,a.id,a.national_paper,a.duration_minutes
		ORDER BY m.created_at DESC,min(aa.opens_at) ASC LIMIT 600`, id.SchoolID, classFilter, classFilter == nil)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type round struct {
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		CreatedAt string          `json:"created_at"`
		Papers    []mockPaperInfo `json:"papers"`
		Summary   mockSummary     `json:"summary"`
	}
	rounds := []*round{}
	byID := map[string]*round{}
	for rows.Next() {
		var roundID, name, created string
		var paper mockPaperInfo
		if err = rows.Scan(&roundID, &name, &created, &paper.Paper, &paper.Minutes, &paper.Questions, &paper.OpensAt, &paper.DueAt, &paper.Stream, &paper.InProgress, &paper.Submitted); err != nil {
			a.serverError(w, r, err)
			return
		}
		current := byID[roundID]
		if current == nil {
			current = &round{ID: roundID, Name: name, CreatedAt: created}
			byID[roundID] = current
			rounds = append(rounds, current)
		}
		current.Papers = append(current.Papers, paper)
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	rows.Close()
	for _, current := range rounds {
		students, err := a.mockRoundStudents(r.Context(), id.SchoolID, current.ID, classFilter)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		current.Summary = summariseMockRound(students)
	}
	writeJSON(w, 200, map[string]any{"data": rounds, "pass_line": readinessPassLine})
}

// mockStudent is one student's sitting of a mock round.
type mockStudent struct {
	StudentID string             `json:"student_id"`
	Number    string             `json:"student_number"`
	Name      string             `json:"student_name"`
	Stream    string             `json:"stream"`
	ClassID   string             `json:"class_id"`
	ClassName string             `json:"class_name"`
	Grade     string             `json:"grade_level"`
	Scores    map[string]float64 `json:"scores"`
	Sat       int                `json:"papers_sat"`
	Expected  int                `json:"papers_expected"`
	Total     *float64           `json:"total"`
}

func (a *API) mockRoundStudents(ctx context.Context, schoolID, roundID string, classFilter []string) ([]*mockStudent, error) {
	rows, err := a.db.Query(ctx, `SELECT s.id::text,s.student_number,s.legal_name,COALESCE(s.stream,''),c.id::text,c.name,c.grade_level,a.national_paper,
		COALESCE(t.status,''),t.score::float8,t.max_score::float8
		FROM assessments a
		JOIN assessment_assignments aa ON aa.assessment_id=a.id AND aa.school_id=a.school_id
		JOIN classes c ON c.id=aa.class_id AND c.school_id=aa.school_id
		JOIN enrollments e ON e.class_id=c.id AND e.school_id=c.school_id AND e.status='active'
		JOIN students s ON s.id=e.student_id AND s.school_id=e.school_id AND s.status='active'
		LEFT JOIN assessment_attempts t ON t.assignment_id=aa.id AND t.student_id=s.id
		WHERE a.school_id=$1 AND a.mock_round_id=$2 AND ($4 OR aa.class_id=ANY($3::uuid[])) AND (aa.stream IS NULL OR aa.stream=s.stream OR t.id IS NOT NULL)
		ORDER BY c.grade_level,c.name,s.legal_name`, schoolID, roundID, classFilter, classFilter == nil)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	students := []*mockStudent{}
	byID := map[string]*mockStudent{}
	for rows.Next() {
		var x mockStudent
		var paper, status string
		var score, maxScore *float64
		if err = rows.Scan(&x.StudentID, &x.Number, &x.Name, &x.Stream, &x.ClassID, &x.ClassName, &x.Grade, &paper, &status, &score, &maxScore); err != nil {
			return nil, err
		}
		current := byID[x.StudentID]
		if current == nil {
			current = &x
			current.Scores = map[string]float64{}
			current.Expected = len(streamPapers(x.Stream))
			byID[x.StudentID] = current
			students = append(students, current)
		}
		if status == "submitted" && score != nil && maxScore != nil && *maxScore > 0 {
			if _, seen := current.Scores[paper]; !seen {
				current.Sat++
			}
			current.Scores[paper] = *score / *maxScore * 100
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for _, student := range students {
		if student.Stream == "" || student.Sat < student.Expected {
			continue
		}
		total := 0.0
		complete := true
		for _, paper := range streamPapers(student.Stream) {
			score, ok := student.Scores[paper]
			complete = complete && ok
			total += score
		}
		if complete {
			student.Total = &total
		}
	}
	return students, nil
}

type mockGroup struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Grade    string   `json:"grade_level,omitempty"`
	Students int      `json:"students"`
	Complete int      `json:"complete"`
	Average  *float64 `json:"average_total"`
	Below    int      `json:"below_line"`
	sum      float64
}

func (g *mockGroup) add(student *mockStudent) {
	g.Students++
	if student.Total == nil {
		return
	}
	g.Complete++
	g.sum += *student.Total
	average := g.sum / float64(g.Complete)
	g.Average = &average
	if *student.Total < readinessPassLine {
		g.Below++
	}
}

type mockPaperAverage struct {
	Paper   string   `json:"paper"`
	Sat     int      `json:"sat"`
	Average *float64 `json:"average"`
}

type mockSummary struct {
	All     mockGroup          `json:"all"`
	Streams []*mockGroup       `json:"streams"`
	Classes []*mockGroup       `json:"classes"`
	Papers  []mockPaperAverage `json:"papers"`
}

// summariseMockRound rolls students up by stream, class and paper. Only students
// who sat every paper count toward a total, so a partial sitting cannot pull a
// class's average down or put a student below the line by mistake.
func summariseMockRound(students []*mockStudent) mockSummary {
	natural, social := &mockGroup{Key: "natural", Name: "natural"}, &mockGroup{Key: "social", Name: "social"}
	summary := mockSummary{All: mockGroup{Key: "all", Name: "all"}, Streams: []*mockGroup{natural, social}, Classes: []*mockGroup{}, Papers: []mockPaperAverage{}}
	classes := map[string]*mockGroup{}
	sums := map[string]float64{}
	sat := map[string]int{}
	for _, student := range students {
		summary.All.add(student)
		switch student.Stream {
		case "natural":
			natural.add(student)
		case "social":
			social.add(student)
		}
		group := classes[student.ClassID]
		if group == nil {
			group = &mockGroup{Key: student.ClassID, Name: student.ClassName, Grade: student.Grade}
			classes[student.ClassID] = group
			summary.Classes = append(summary.Classes, group)
		}
		group.add(student)
		for paper, score := range student.Scores {
			sums[paper] += score
			sat[paper]++
		}
	}
	for _, paper := range slices.Concat(commonPapers, naturalPapers, socialPapers) {
		item := mockPaperAverage{Paper: paper, Sat: sat[paper]}
		if sat[paper] > 0 {
			average := sums[paper] / float64(sat[paper])
			item.Average = &average
		}
		summary.Papers = append(summary.Papers, item)
	}
	return summary
}

func (a *API) mockRoundResults(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	roundID := r.PathValue("roundID")
	var exists bool
	if err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM mock_rounds WHERE id=$1 AND school_id=$2)`, roundID, id.SchoolID).Scan(&exists); err != nil {
		a.serverError(w, r, err)
		return
	}
	if !exists {
		writeError(w, 404, "NOT_FOUND", "Mock day not found.", nil)
		return
	}
	classFilter, err := a.staffClassFilter(r.Context(), id)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	students, err := a.mockRoundStudents(r.Context(), id.SchoolID, roundID, classFilter)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"students": students, "summary": summariseMockRound(students), "pass_line": readinessPassLine}})
}

// lockMockRound stops a change once any student has started. Their papers are
// already running, and a changed timetable would be unfair to them.
func lockMockRound(ctx context.Context, tx pgx.Tx, schoolID, roundID string) (bool, error) {
	var found string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM mock_rounds WHERE id=$1 AND school_id=$2 FOR UPDATE`, roundID, schoolID).Scan(&found); err != nil {
		return false, err
	}
	var started bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM assessment_attempts t JOIN assessment_assignments aa ON aa.id=t.assignment_id JOIN assessments a ON a.id=aa.assessment_id WHERE a.mock_round_id=$1)`, roundID).Scan(&started)
	return started, err
}

func (a *API) rescheduleMockPaper(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Paper   string    `json:"paper"`
		OpensAt time.Time `json:"opens_at"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	if !validPaper(req.Paper) || !req.OpensAt.After(time.Now()) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a paper and a time in the future.", nil)
		return
	}
	roundID := r.PathValue("roundID")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = lockMockRound(r.Context(), tx, id.SchoolID, roundID); err != nil {
		a.databaseError(w, r, err)
		return
	}
	var started bool
	err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM assessment_attempts t JOIN assessment_assignments aa ON aa.id=t.assignment_id JOIN assessments a ON a.id=aa.assessment_id WHERE a.mock_round_id=$1 AND a.national_paper=$2)`, roundID, req.Paper).Scan(&started)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if started {
		writeError(w, 409, "MOCK_STARTED", "Students have started this paper, so its time cannot change.", nil)
		return
	}
	rows, err := tx.Query(r.Context(), `SELECT a.national_paper,a.duration_minutes,min(aa.opens_at),(extract(epoch FROM min(aa.due_at)-min(aa.opens_at))/60)::int
		FROM assessments a JOIN assessment_assignments aa ON aa.assessment_id=a.id WHERE a.mock_round_id=$1 AND a.school_id=$2 GROUP BY a.id`, roundID, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	slots := []mockSlot{}
	window := 0
	found := false
	for rows.Next() {
		var slot mockSlot
		if err = rows.Scan(&slot.Paper, &slot.Minutes, &slot.Opens, &window); err != nil {
			rows.Close()
			a.serverError(w, r, err)
			return
		}
		if slot.Paper == req.Paper {
			slot.Opens = req.OpensAt
			found = true
		}
		slots = append(slots, slot)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	if !found {
		writeError(w, 404, "NOT_FOUND", "This mock day does not include that paper.", nil)
		return
	}
	if problem := mockTimetableProblem(slots, window); problem != "" {
		writeError(w, 400, "VALIDATION_ERROR", problem, nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE assessment_assignments aa SET opens_at=$3,due_at=$3::timestamptz+make_interval(mins=>$4::int)
		FROM assessments a WHERE a.id=aa.assessment_id AND a.mock_round_id=$1 AND a.national_paper=$2`, roundID, req.Paper, req.OpensAt, window); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'mock_round.rescheduled','mock_round',$3,jsonb_build_object('paper',$4::text))`, id.SchoolID, id.UserID, roundID, req.Paper); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": roundID, "paper": req.Paper}})
}

func (a *API) cancelMockRound(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	roundID := r.PathValue("roundID")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	started, err := lockMockRound(r.Context(), tx, id.SchoolID, roundID)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if started {
		writeError(w, 409, "MOCK_STARTED", "Students have started this mock day, so it cannot be cancelled. Their results are kept.", nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM mock_rounds WHERE id=$1 AND school_id=$2`, roundID, id.SchoolID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'mock_round.cancelled','mock_round',$3,'{}'::jsonb)`, id.SchoolID, id.UserID, roundID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": roundID, "status": "cancelled"}})
}

// studentMockRounds is a student's own mock day timetable and results.
func (a *API) studentMockRounds(ctx context.Context, schoolID, studentID string) ([]map[string]any, error) {
	rows, err := a.db.Query(ctx, `SELECT m.id::text,m.name,COALESCE(s.stream,''),a.national_paper,aa.id::text,
		to_char(aa.opens_at AT TIME ZONE 'UTC',`+isoTimestamp+`),to_char(aa.due_at AT TIME ZONE 'UTC',`+isoTimestamp+`),a.duration_minutes,
		COALESCE(t.status,'not_started'),t.score::float8,t.max_score::float8
		FROM enrollments e JOIN students s ON s.id=e.student_id AND s.school_id=e.school_id
		JOIN assessment_assignments aa ON aa.class_id=e.class_id AND aa.school_id=e.school_id
		JOIN assessments a ON a.id=aa.assessment_id AND a.school_id=aa.school_id
		JOIN mock_rounds m ON m.id=a.mock_round_id AND m.school_id=a.school_id
		LEFT JOIN assessment_attempts t ON t.assignment_id=aa.id AND t.student_id=s.id
		WHERE e.school_id=$1 AND e.student_id=$2 AND e.status='active' AND (aa.stream IS NULL OR aa.stream=s.stream OR t.id IS NOT NULL)
		ORDER BY m.created_at DESC,aa.opens_at ASC LIMIT 120`, schoolID, studentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type round struct {
		id, name, stream string
		papers           []map[string]any
		total            float64
		finished         map[string]bool
	}
	rounds := []*round{}
	byID := map[string]*round{}
	for rows.Next() {
		var roundID, name, stream, paper, assignmentID, status string
		var opens, due *string
		var minutes int
		var score, maxScore *float64
		if err = rows.Scan(&roundID, &name, &stream, &paper, &assignmentID, &opens, &due, &minutes, &status, &score, &maxScore); err != nil {
			return nil, err
		}
		current := byID[roundID]
		if current == nil {
			current = &round{id: roundID, name: name, stream: stream, finished: map[string]bool{}}
			byID[roundID] = current
			rounds = append(rounds, current)
		}
		entry := map[string]any{"paper": paper, "assignment_id": assignmentID, "opens_at": opens, "due_at": due, "duration_minutes": minutes, "status": status}
		if status == "submitted" && score != nil && maxScore != nil && *maxScore > 0 {
			scaled := *score / *maxScore * 100
			entry["score"] = scaled
			current.total += scaled
			current.finished[paper] = true
		}
		current.papers = append(current.papers, entry)
	}
	if err = rows.Err(); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	result := []map[string]any{}
	for _, current := range rounds {
		entry := map[string]any{"id": current.id, "name": current.name, "papers": current.papers}
		if current.stream != "" && len(current.finished) == len(streamPapers(current.stream)) {
			entry["total"] = current.total
		}
		result = append(result, entry)
	}
	return result, nil
}
