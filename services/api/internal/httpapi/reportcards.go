package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"
	"strings"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
)

var conductGrades = []string{"", "A", "B", "C", "D", "F"}

func (a *API) registerReportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/report-cards/classes/{classID}", a.staffMember(a.getClassReport))
	mux.HandleFunc("PUT /api/v1/report-cards/classes/{classID}/notes", a.staffMember(a.saveReportNotes))
	mux.HandleFunc("POST /api/v1/report-cards/classes/{classID}/publish", a.admin(a.publishReports))
	mux.HandleFunc("POST /api/v1/report-cards/classes/{classID}/unpublish", a.admin(a.unpublishReports))
	mux.HandleFunc("GET /api/v1/student/report-cards", a.student(a.studentReportCards))
}

// reportQuerier is what reports read from: the pool, or a publish's transaction.
type reportQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type markItem struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Weight float64 `json:"weight"`
	Max    float64 `json:"max_score"`
}

// subjectTotal adds a student's marks out of 100, each scaled to its weight. A
// missing mark leaves the total incomplete, never counted as a zero.
func subjectTotal(items []markItem, scores map[string]float64) (total float64, missing int) {
	for _, item := range items {
		score, found := scores[item.ID]
		if !found {
			missing++
			continue
		}
		total += score / item.Max * item.Weight
	}
	return round2(total), missing
}

// rankScores gives 1 to the highest score. Equal scores share a rank and the
// next rank is skipped, so two firsts are followed by a third.
func rankScores(scores map[string]float64) map[string]int {
	ids := make([]string, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(p, q string) int {
		switch {
		case scores[p] > scores[q]:
			return -1
		case scores[p] < scores[q]:
			return 1
		}
		return strings.Compare(p, q)
	})
	ranks := make(map[string]int, len(ids))
	for i, id := range ids {
		if i > 0 && scores[id] == scores[ids[i-1]] {
			ranks[id] = ranks[ids[i-1]]
		} else {
			ranks[id] = i + 1
		}
	}
	return ranks
}

type cardSubject struct {
	SubjectID string `json:"subject_id"`
	Name      string `json:"name"`
	// Total is out of 100, and empty while any of the student's marks are missing.
	Total       *float64            `json:"total"`
	Grade       string              `json:"grade"`
	Counts      bool                `json:"counts_in_average"`
	Earlier     map[string]*float64 `json:"earlier,omitempty"`
	YearAverage *float64            `json:"year_average,omitempty"`
}

type earlierTerm struct {
	TermID  string   `json:"term_id"`
	Name    string   `json:"name"`
	Average *float64 `json:"average"`
	Rank    *int     `json:"rank"`
}

type cardAttendance struct {
	Days    int `json:"days_marked"`
	Present int `json:"present"`
	Absent  int `json:"absent"`
	Late    int `json:"late"`
	Excused int `json:"excused"`
}

type reportCard struct {
	StudentID     string         `json:"student_id"`
	StudentNumber string         `json:"student_number"`
	StudentName   string         `json:"student_name"`
	SchoolName    string         `json:"school_name"`
	ClassName     string         `json:"class_name"`
	GradeLevel    string         `json:"grade_level"`
	YearName      string         `json:"year_name"`
	TermID        string         `json:"term_id"`
	TermName      string         `json:"term_name"`
	PassPercent   float64        `json:"pass_percent"`
	Subjects      []cardSubject  `json:"subjects"`
	Total         *float64       `json:"total"`
	Average       *float64       `json:"average"`
	Grade         string         `json:"grade"`
	Rank          *int           `json:"rank"`
	Ranked        int            `json:"students_ranked"`
	Complete      bool           `json:"complete"`
	Attendance    cardAttendance `json:"attendance"`
	Conduct       string         `json:"conduct"`
	Comment       string         `json:"comment"`
	EarlierTerms  []earlierTerm  `json:"earlier_terms"`
	YearAverage   *float64       `json:"year_average,omitempty"`
	YearRank      *int           `json:"year_rank,omitempty"`
}

type reportProblem struct {
	Kind      string  `json:"kind"`
	SubjectID string  `json:"subject_id"`
	Subject   string  `json:"subject"`
	Weight    float64 `json:"weight_total,omitempty"`
	Missing   int     `json:"missing_marks,omitempty"`
}

type classReport struct {
	Cards    []reportCard    `json:"cards"`
	Problems []reportProblem `json:"problems"`
}

// blocksPublishing says whether a report cannot go out at all. A total only
// means something if the weights add up. Missing marks can be real, such as a
// student who joined late, so the school can publish past those.
func blocksPublishing(problems []reportProblem) bool {
	return slices.ContainsFunc(problems, func(p reportProblem) bool { return p.Kind == "weights" })
}

type reportSubject struct {
	ID     string
	Name   string
	Counts bool
	Items  []markItem
	Weight float64
}

// buildClassReport works out every card for a class and term from the marks as
// they are now. It covers the students now in the class.
func (a *API) buildClassReport(ctx context.Context, q reportQuerier, schoolID, classID, termID string) (classReport, error) {
	report := classReport{Cards: []reportCard{}, Problems: []reportProblem{}}
	var base reportCard
	var yearID, starts, ends string
	err := q.QueryRow(ctx, `SELECT sc.name,c.name,c.grade_level,y.name,y.id::text,t.id::text,t.name,t.starts_on::text,t.ends_on::text
		FROM classes c JOIN schools sc ON sc.id=c.school_id JOIN academic_years y ON y.id=c.academic_year_id JOIN academic_terms t ON t.academic_year_id=y.id
		WHERE c.id=$1 AND c.school_id=$2 AND t.id=$3`, classID, schoolID, termID).Scan(&base.SchoolName, &base.ClassName, &base.GradeLevel, &base.YearName, &yearID, &base.TermID, &base.TermName, &starts, &ends)
	if err != nil {
		return report, err
	}
	settings, err := a.loadReportSettings(ctx, q, schoolID)
	if err != nil {
		return report, err
	}
	base.PassPercent = settings.PassPercent

	subjects, err := loadReportSubjects(ctx, q, schoolID, classID, termID)
	if err != nil {
		return report, err
	}
	studentRows, err := q.Query(ctx, `SELECT s.id::text,s.student_number,s.legal_name FROM students s
		WHERE s.school_id=$1 AND s.status='active' AND EXISTS(SELECT 1 FROM enrollments e WHERE e.student_id=s.id AND e.class_id=$2 AND e.status='active')
		ORDER BY s.legal_name,s.student_number LIMIT 1000`, schoolID, classID)
	if err != nil {
		return report, err
	}
	for studentRows.Next() {
		card := base
		if err = studentRows.Scan(&card.StudentID, &card.StudentNumber, &card.StudentName); err != nil {
			studentRows.Close()
			return report, err
		}
		report.Cards = append(report.Cards, card)
	}
	studentRows.Close()
	if err = studentRows.Err(); err != nil {
		return report, err
	}

	// Scores by student, then by item.
	marks := map[string]map[string]float64{}
	markRows, err := q.Query(ctx, `SELECT m.student_id::text,m.item_id::text,m.score::float8 FROM marks m JOIN mark_items i ON i.id=m.item_id JOIN class_subjects cs ON cs.id=i.class_subject_id WHERE cs.class_id=$1 AND i.term_id=$2`, classID, termID)
	if err != nil {
		return report, err
	}
	for markRows.Next() {
		var studentID, itemID string
		var score float64
		if err = markRows.Scan(&studentID, &itemID, &score); err != nil {
			markRows.Close()
			return report, err
		}
		if marks[studentID] == nil {
			marks[studentID] = map[string]float64{}
		}
		marks[studentID][itemID] = score
	}
	markRows.Close()
	if err = markRows.Err(); err != nil {
		return report, err
	}

	attendance, err := loadTermAttendance(ctx, q, classID, starts, ends)
	if err != nil {
		return report, err
	}
	notes, err := loadReportNotes(ctx, q, classID, termID)
	if err != nil {
		return report, err
	}

	// Every subject with no tests, wrong weights or missing marks is a problem to show.
	missingBySubject := map[string]int{}
	counted := 0
	for _, subject := range subjects {
		if len(subject.Items) > 0 && subject.Counts {
			counted++
		}
	}
	averages := map[string]float64{}
	for i := range report.Cards {
		card := &report.Cards[i]
		card.Subjects = []cardSubject{}
		card.EarlierTerms = []earlierTerm{}
		card.Attendance = attendance[card.StudentID]
		card.Conduct, card.Comment = notes[card.StudentID][0], notes[card.StudentID][1]
		sum, whole := 0.0, counted > 0
		for _, subject := range subjects {
			if len(subject.Items) == 0 {
				continue
			}
			line := cardSubject{SubjectID: subject.ID, Name: subject.Name, Counts: subject.Counts}
			total, missing := subjectTotal(subject.Items, marks[card.StudentID])
			missingBySubject[subject.ID] += missing
			if missing == 0 && math.Abs(subject.Weight-100) < 0.005 {
				value := total
				line.Total = &value
				line.Grade = gradeFor(settings.Bands, total)
				if subject.Counts {
					sum += total
				}
			} else if subject.Counts {
				whole = false
			}
			card.Subjects = append(card.Subjects, line)
		}
		card.Complete = whole
		if whole {
			totalValue, averageValue := round2(sum), round2(sum/float64(counted))
			card.Total, card.Average = &totalValue, &averageValue
			card.Grade = gradeFor(settings.Bands, averageValue)
			averages[card.StudentID] = averageValue
		}
	}
	ranks := rankScores(averages)
	for i := range report.Cards {
		card := &report.Cards[i]
		card.Ranked = len(averages)
		if rank, ranked := ranks[card.StudentID]; ranked {
			value := rank
			card.Rank = &value
		}
	}
	for _, subject := range subjects {
		switch {
		case len(subject.Items) == 0:
			report.Problems = append(report.Problems, reportProblem{Kind: "no_items", SubjectID: subject.ID, Subject: subject.Name})
		case math.Abs(subject.Weight-100) >= 0.005:
			report.Problems = append(report.Problems, reportProblem{Kind: "weights", SubjectID: subject.ID, Subject: subject.Name, Weight: subject.Weight})
		case missingBySubject[subject.ID] > 0:
			report.Problems = append(report.Problems, reportProblem{Kind: "missing_marks", SubjectID: subject.ID, Subject: subject.Name, Missing: missingBySubject[subject.ID]})
		}
	}
	return report, addEarlierTerms(ctx, q, classID, yearID, starts, &report)
}

func loadReportSubjects(ctx context.Context, q reportQuerier, schoolID, classID, termID string) ([]reportSubject, error) {
	rows, err := q.Query(ctx, `SELECT cs.id::text,cs.name,cs.counts_in_average,i.id::text,i.title,i.weight::float8,i.max_score::float8
		FROM class_subjects cs LEFT JOIN mark_items i ON i.class_subject_id=cs.id AND i.term_id=$3
		WHERE cs.class_id=$1 AND cs.school_id=$2 ORDER BY cs.sort_order,cs.name,i.created_at`, classID, schoolID, termID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	subjects := []reportSubject{}
	for rows.Next() {
		var id, name string
		var counts bool
		var itemID, title *string
		var weight, maxScore *float64
		if err = rows.Scan(&id, &name, &counts, &itemID, &title, &weight, &maxScore); err != nil {
			return nil, err
		}
		if len(subjects) == 0 || subjects[len(subjects)-1].ID != id {
			subjects = append(subjects, reportSubject{ID: id, Name: name, Counts: counts})
		}
		if itemID != nil {
			subject := &subjects[len(subjects)-1]
			subject.Items = append(subject.Items, markItem{ID: *itemID, Title: *title, Weight: *weight, Max: *maxScore})
			subject.Weight = round2(subject.Weight + *weight)
		}
	}
	return subjects, rows.Err()
}

func loadTermAttendance(ctx context.Context, q reportQuerier, classID, starts, ends string) (map[string]cardAttendance, error) {
	rows, err := q.Query(ctx, `SELECT student_id::text,count(*)::int,count(*) FILTER (WHERE status='present')::int,count(*) FILTER (WHERE status='absent')::int,count(*) FILTER (WHERE status='late')::int,count(*) FILTER (WHERE status='excused')::int
		FROM attendance_records WHERE class_id=$1 AND attendance_date BETWEEN $2::date AND $3::date GROUP BY student_id`, classID, starts, ends)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	attendance := map[string]cardAttendance{}
	for rows.Next() {
		var studentID string
		var x cardAttendance
		if err = rows.Scan(&studentID, &x.Days, &x.Present, &x.Absent, &x.Late, &x.Excused); err != nil {
			return nil, err
		}
		attendance[studentID] = x
	}
	return attendance, rows.Err()
}

func loadReportNotes(ctx context.Context, q reportQuerier, classID, termID string) (map[string][2]string, error) {
	rows, err := q.Query(ctx, `SELECT student_id::text,conduct,comment FROM report_notes WHERE class_id=$1 AND term_id=$2`, classID, termID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	notes := map[string][2]string{}
	for rows.Next() {
		var studentID, conduct, comment string
		if err = rows.Scan(&studentID, &conduct, &comment); err != nil {
			return nil, err
		}
		notes[studentID] = [2]string{conduct, comment}
	}
	return notes, rows.Err()
}

// addEarlierTerms brings in the year's earlier published terms, so a second
// semester's card also shows the first and the average of the two. It reads the
// published copies, so the figures match the cards families already have.
func addEarlierTerms(ctx context.Context, q reportQuerier, classID, yearID, starts string, report *classReport) error {
	rows, err := q.Query(ctx, `SELECT t.id::text,t.name,rc.student_id::text,rc.card FROM report_cards rc JOIN academic_terms t ON t.id=rc.term_id
		WHERE rc.class_id=$1 AND t.academic_year_id=$2 AND t.starts_on<$3::date ORDER BY t.starts_on`, classID, yearID, starts)
	if err != nil {
		return err
	}
	defer rows.Close()
	type termName struct{ ID, Name string }
	terms := []termName{}
	earlier := map[string]map[string]reportCard{}
	for rows.Next() {
		var termID, name, studentID string
		var raw []byte
		if err = rows.Scan(&termID, &name, &studentID, &raw); err != nil {
			return err
		}
		var card reportCard
		if err = json.Unmarshal(raw, &card); err != nil {
			return err
		}
		if earlier[termID] == nil {
			earlier[termID] = map[string]reportCard{}
			terms = append(terms, termName{termID, name})
		}
		earlier[termID][studentID] = card
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(terms) == 0 {
		return nil
	}
	yearAverages := map[string]float64{}
	for i := range report.Cards {
		card := &report.Cards[i]
		allAverages, yearSum := true, 0.0
		for _, term := range terms {
			past, found := earlier[term.ID][card.StudentID]
			line := earlierTerm{TermID: term.ID, Name: term.Name}
			if found {
				line.Average, line.Rank = past.Average, past.Rank
			}
			if line.Average == nil {
				allAverages = false
			} else {
				yearSum += *line.Average
			}
			card.EarlierTerms = append(card.EarlierTerms, line)
		}
		for s := range card.Subjects {
			line := &card.Subjects[s]
			line.Earlier = map[string]*float64{}
			subjectSum, whole := 0.0, line.Total != nil
			if line.Total != nil {
				subjectSum = *line.Total
			}
			for _, term := range terms {
				var total *float64
				for _, pastLine := range earlier[term.ID][card.StudentID].Subjects {
					if pastLine.SubjectID == line.SubjectID {
						total = pastLine.Total
					}
				}
				line.Earlier[term.ID] = total
				if total == nil {
					whole = false
				} else {
					subjectSum += *total
				}
			}
			if whole {
				value := round2(subjectSum / float64(len(terms)+1))
				line.YearAverage = &value
			}
		}
		if allAverages && card.Average != nil {
			value := round2((yearSum + *card.Average) / float64(len(terms)+1))
			card.YearAverage = &value
			yearAverages[card.StudentID] = value
		}
	}
	ranks := rankScores(yearAverages)
	for i := range report.Cards {
		if rank, ranked := ranks[report.Cards[i].StudentID]; ranked {
			value := rank
			report.Cards[i].YearRank = &value
		}
	}
	return nil
}

// reportTerm reads a term from the request and checks both it and the class.
func (a *API) reportTerm(w http.ResponseWriter, r *http.Request, id auth.Identity, classID, termID string) bool {
	if !a.requireClass(w, r, id, classID) {
		return false
	}
	if !uuidPattern.MatchString(termID) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a term.", nil)
		return false
	}
	found, err := a.termInClass(r.Context(), a.db, id.SchoolID, termID, classID)
	if err != nil {
		a.serverError(w, r, err)
		return false
	}
	if !found {
		writeError(w, 400, "VALIDATION_ERROR", "That term is not in this class's academic year.", nil)
		return false
	}
	return true
}

func publishedCards(ctx context.Context, q reportQuerier, termID, classID string) ([]reportCard, string, error) {
	var publishedAt string
	err := q.QueryRow(ctx, `SELECT to_json(published_at)#>>'{}' FROM report_publications WHERE term_id=$1 AND class_id=$2`, termID, classID).Scan(&publishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	rows, err := q.Query(ctx, `SELECT card FROM report_cards WHERE term_id=$1 AND class_id=$2 ORDER BY card->>'student_name',card->>'student_number'`, termID, classID)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	cards := []reportCard{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, "", err
		}
		var card reportCard
		if err = json.Unmarshal(raw, &card); err != nil {
			return nil, "", err
		}
		cards = append(cards, card)
	}
	return cards, publishedAt, rows.Err()
}

func (a *API) getClassReport(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	classID, termID := r.PathValue("classID"), r.URL.Query().Get("term_id")
	if !a.reportTerm(w, r, id, classID, termID) {
		return
	}
	cards, publishedAt, err := publishedCards(r.Context(), a.db, termID, classID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if publishedAt != "" {
		writeJSON(w, 200, map[string]any{"data": map[string]any{"published": true, "published_at": publishedAt, "cards": cards, "problems": []reportProblem{}}})
		return
	}
	report, err := a.buildClassReport(r.Context(), a.db, id.SchoolID, classID, termID)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"published": false, "published_at": "", "cards": report.Cards, "problems": report.Problems}})
}

func (a *API) saveReportNotes(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		TermID string `json:"term_id"`
		Notes  []struct {
			StudentID string `json:"student_id"`
			Conduct   string `json:"conduct"`
			Comment   string `json:"comment"`
		} `json:"notes"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	classID := r.PathValue("classID")
	if !a.reportTerm(w, r, id, classID, req.TermID) || a.refuseClosedClass(w, r, id.SchoolID, classID) {
		return
	}
	if len(req.Notes) == 0 || len(req.Notes) > 1000 {
		writeError(w, 400, "VALIDATION_ERROR", "Enter at least one note.", nil)
		return
	}
	var students, conducts, comments []string
	for _, note := range req.Notes {
		comment := strings.TrimSpace(note.Comment)
		if !uuidPattern.MatchString(note.StudentID) || !slices.Contains(conductGrades, note.Conduct) || len(comment) > 600 {
			writeError(w, 400, "VALIDATION_ERROR", "Choose a conduct grade and keep comments under 600 characters.", nil)
			return
		}
		students, conducts, comments = append(students, note.StudentID), append(conducts, note.Conduct), append(comments, comment)
	}
	if len(slicesCompactSorted(students)) != len(students) {
		writeError(w, 400, "VALIDATION_ERROR", "A student has two notes.", nil)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	if err = lockClass(r.Context(), tx, classID, "SHARE"); err != nil {
		a.serverError(w, r, err)
		return
	}
	published, err := reportPublished(r.Context(), tx, req.TermID, classID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if published {
		writeReportPublished(w)
		return
	}
	var known int
	err = tx.QueryRow(r.Context(), `SELECT count(*)::int FROM students s WHERE s.id=ANY($1::uuid[]) AND s.school_id=$2 AND EXISTS(SELECT 1 FROM enrollments e WHERE e.student_id=s.id AND e.class_id=$3 AND e.status='active')`, students, id.SchoolID, classID).Scan(&known)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if known != len(students) {
		writeError(w, 400, "VALIDATION_ERROR", "A student is not in this class.", nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO report_notes(school_id,term_id,class_id,student_id,conduct,comment,updated_by)
		SELECT $1,$2,$3,n.student_id,n.conduct,n.comment,$4 FROM unnest($5::uuid[],$6::text[],$7::text[]) AS n(student_id,conduct,comment)
		ON CONFLICT (term_id,class_id,student_id) DO UPDATE SET conduct=EXCLUDED.conduct,comment=EXCLUDED.comment,updated_by=EXCLUDED.updated_by,updated_at=now()`,
		id.SchoolID, req.TermID, classID, id.UserID, students, conducts, comments); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]int{"saved": len(students)}})
}

// publishReports issues a class's reports for a term. It locks the marks and
// keeps a copy of each card, and students can then see theirs.
func (a *API) publishReports(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		TermID          string `json:"term_id"`
		AllowIncomplete bool   `json:"allow_incomplete"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	classID := r.PathValue("classID")
	if !a.reportTerm(w, r, id, classID, req.TermID) || a.refuseClosedClass(w, r, id.SchoolID, classID) {
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	if err = lockClass(r.Context(), tx, classID, "UPDATE"); err != nil {
		a.serverError(w, r, err)
		return
	}
	published, err := reportPublished(r.Context(), tx, req.TermID, classID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if published {
		writeError(w, 409, "REPORT_PUBLISHED", "Reports for this term are already published.", nil)
		return
	}
	report, err := a.buildClassReport(r.Context(), tx, id.SchoolID, classID, req.TermID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	withSubjects := len(report.Cards) > 0 && len(report.Cards[0].Subjects) > 0
	if !withSubjects {
		writeError(w, 422, "NOTHING_TO_PUBLISH", "This class has no students or no marks for this term yet.", nil)
		return
	}
	if blocksPublishing(report.Problems) || (len(report.Problems) > 0 && !req.AllowIncomplete) {
		writeJSON(w, 422, map[string]any{"error": map[string]any{"code": "REPORT_NOT_READY", "message": "Some subjects are not ready.", "problems": report.Problems}})
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO report_publications(school_id,term_id,class_id,published_by) VALUES($1,$2,$3,$4)`, id.SchoolID, req.TermID, classID, id.UserID); err != nil {
		a.serverError(w, r, err)
		return
	}
	students := make([]string, 0, len(report.Cards))
	cards := make([]string, 0, len(report.Cards))
	for _, card := range report.Cards {
		raw, err := json.Marshal(card)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		students, cards = append(students, card.StudentID), append(cards, string(raw))
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO report_cards(school_id,term_id,class_id,student_id,card) SELECT $1,$2,$3,c.student_id,c.card::jsonb FROM unnest($4::uuid[],$5::text[]) AS c(student_id,card)`, id.SchoolID, req.TermID, classID, students, cards); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = a.noticeReports(r.Context(), tx, id.SchoolID, req.TermID, students); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'reports.published','class',$3,jsonb_build_object('term_id',$4::text,'cards',$5::int,'problems',$6::int))`, id.SchoolID, id.UserID, classID, req.TermID, len(cards), len(report.Problems)); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]int{"published": len(cards)}})
}

// unpublishReports takes reports back so marks can be corrected. Students stop
// seeing them, the reason is logged, and a later term must come back first
// because its cards carry this term's figures.
func (a *API) unpublishReports(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		TermID string `json:"term_id"`
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	classID := r.PathValue("classID")
	if !a.reportTerm(w, r, id, classID, req.TermID) || a.refuseClosedClass(w, r, id.SchoolID, classID) {
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if len(req.Reason) < 3 || len(req.Reason) > 500 {
		writeError(w, 400, "VALIDATION_ERROR", "Say why these reports are being taken back.", nil)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	if err = lockClass(r.Context(), tx, classID, "UPDATE"); err != nil {
		a.serverError(w, r, err)
		return
	}
	var later bool
	err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM report_publications p JOIN academic_terms later ON later.id=p.term_id JOIN academic_terms this ON this.id=$2 WHERE p.class_id=$1 AND later.academic_year_id=this.academic_year_id AND later.starts_on>this.starts_on)`, classID, req.TermID).Scan(&later)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if later {
		writeError(w, 409, "LATER_TERM_PUBLISHED", "A later term's reports are published and include this term. Unpublish that term first.", nil)
		return
	}
	tag, err := tx.Exec(r.Context(), `DELETE FROM report_publications WHERE term_id=$1 AND class_id=$2`, req.TermID, classID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, 409, "REPORT_NOT_PUBLISHED", "Reports for this term are not published.", nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'reports.unpublished','class',$3,jsonb_build_object('term_id',$4::text,'reason',$5::text))`, id.SchoolID, id.UserID, classID, req.TermID, req.Reason); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"term_id": req.TermID}})
}

// studentReportCards returns a student's own published cards.
func (a *API) studentReportCards(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID, ok := a.prepStudent(w, r, id)
	if !ok {
		return
	}
	cards, err := a.cardsForStudent(r.Context(), id.SchoolID, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": cards})
}

// cardsForStudent reads a student's published cards, newest first.
func (a *API) cardsForStudent(ctx context.Context, schoolID, studentID string) ([]reportCard, error) {
	rows, err := a.db.Query(ctx, `SELECT rc.card FROM report_cards rc JOIN academic_terms t ON t.id=rc.term_id WHERE rc.student_id=$1 AND rc.school_id=$2 ORDER BY t.starts_on DESC`, studentID, schoolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cards := []reportCard{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var card reportCard
		if err = json.Unmarshal(raw, &card); err != nil {
			return nil, err
		}
		cards = append(cards, card)
	}
	return cards, rows.Err()
}
