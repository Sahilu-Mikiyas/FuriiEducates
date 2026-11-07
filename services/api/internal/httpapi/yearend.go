package httpapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
)

// The end of a school year happens in three steps: copy the year's classes,
// subjects and terms into the next year; move each student on (promoted,
// repeating, graduated or leaving); then close the old year so its records
// stay as they were. An administrator can reopen a closed year to correct it.

func (a *API) registerYearEndRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/academic-years/{yearID}/copy", a.admin(a.copyYearStructure))
	mux.HandleFunc("GET /api/v1/academic-years/{yearID}/promotion", a.admin(a.promotionPlan))
	mux.HandleFunc("POST /api/v1/academic-years/{yearID}/promotion", a.admin(a.promoteStudents))
	mux.HandleFunc("POST /api/v1/academic-years/{yearID}/close", a.admin(a.closeYear))
	mux.HandleFunc("DELETE /api/v1/academic-years/{yearID}/close", a.admin(a.reopenYear))
}

var gradeNumberPattern = regexp.MustCompile(`\d+`)

// gradeNumber reads the number in a grade such as "Grade 9" or "9".
func gradeNumber(level string) (int, bool) {
	found := gradeNumberPattern.FindString(level)
	if found == "" {
		return 0, false
	}
	n, err := strconv.Atoi(found)
	return n, err == nil
}

// nextGradeLabel writes the grade after this one in the same style.
func nextGradeLabel(level string) string {
	n, ok := gradeNumber(level)
	if !ok {
		return ""
	}
	loc := gradeNumberPattern.FindStringIndex(level)
	return level[:loc[0]] + strconv.Itoa(n+1) + level[loc[1]:]
}

func writeYearClosed(w http.ResponseWriter) {
	writeError(w, 409, "YEAR_CLOSED", "This school year is closed. An administrator can reopen it to make changes.", nil)
}

// classYearClosed reports whether a class belongs to a closed year.
func classYearClosed(ctx context.Context, q reportQuerier, schoolID, classID string) (bool, error) {
	var closed bool
	err := q.QueryRow(ctx, `SELECT y.closed_at IS NOT NULL FROM classes c JOIN academic_years y ON y.id=c.academic_year_id AND y.school_id=c.school_id WHERE c.id=$1 AND c.school_id=$2`, classID, schoolID).Scan(&closed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return closed, err
}

// refuseClosedClass answers 409 when a class's year is closed.
func (a *API) refuseClosedClass(w http.ResponseWriter, r *http.Request, schoolID, classID string) bool {
	closed, err := classYearClosed(r.Context(), a.db, schoolID, classID)
	if err != nil {
		a.serverError(w, r, err)
		return true
	}
	if closed {
		writeYearClosed(w)
	}
	return closed
}

type yearRow struct {
	ID, Name     string
	Starts, Ends time.Time
	Closed       bool
}

func loadYear(ctx context.Context, q reportQuerier, schoolID, yearID string) (yearRow, error) {
	var y yearRow
	err := q.QueryRow(ctx, `SELECT id::text,name,starts_on,ends_on,closed_at IS NOT NULL FROM academic_years WHERE id=$1 AND school_id=$2`, yearID, schoolID).Scan(&y.ID, &y.Name, &y.Starts, &y.Ends, &y.Closed)
	return y, err
}

// loadYearPair reads the year in the path and the year named in the request,
// and answers the request itself when either is missing or they are the same.
func (a *API) loadYearPair(w http.ResponseWriter, r *http.Request, id auth.Identity, toYearID string) (yearRow, yearRow, bool) {
	if !uuidPattern.MatchString(toYearID) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose the next school year.", nil)
		return yearRow{}, yearRow{}, false
	}
	from, err := loadYear(r.Context(), a.db, id.SchoolID, r.PathValue("yearID"))
	if err != nil {
		a.databaseError(w, r, err)
		return from, yearRow{}, false
	}
	to, err := loadYear(r.Context(), a.db, id.SchoolID, toYearID)
	if err != nil {
		a.databaseError(w, r, err)
		return from, to, false
	}
	if from.ID == to.ID {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a different school year to move into.", nil)
		return from, to, false
	}
	if to.Closed {
		writeYearClosed(w)
		return from, to, false
	}
	return from, to, true
}

// copyYearStructure creates in the next year each class of this year with the
// same grade and name, with its subjects, and optionally its teachers and its
// terms moved to the new dates. Classes already there are kept and filled in,
// so it can be run again safely.
func (a *API) copyYearStructure(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		ToYearID string `json:"to_year_id"`
		Teachers bool   `json:"teachers"`
		Terms    bool   `json:"terms"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	from, to, ok := a.loadYearPair(w, r, id, req.ToYearID)
	if !ok {
		return
	}
	result := map[string]int{"classes_created": 0, "classes_kept": 0, "subjects_created": 0, "teachers_assigned": 0, "terms_created": 0}
	if !a.runInTx(w, r, func(tx pgx.Tx) (bool, error) {
		rows, err := tx.Query(r.Context(), `SELECT id::text,name,grade_level FROM classes WHERE academic_year_id=$1 AND school_id=$2 ORDER BY grade_level,name`, from.ID, id.SchoolID)
		if err != nil {
			return false, err
		}
		type class struct{ id, name, grade string }
		var classes []class
		for rows.Next() {
			var c class
			if err := rows.Scan(&c.id, &c.name, &c.grade); err != nil {
				rows.Close()
				return false, err
			}
			classes = append(classes, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return false, err
		}
		for _, c := range classes {
			var target string
			err := tx.QueryRow(r.Context(), `SELECT id::text FROM classes WHERE academic_year_id=$1 AND school_id=$2 AND lower(grade_level)=lower($3) AND lower(name)=lower($4) LIMIT 1`, to.ID, id.SchoolID, c.grade, c.name).Scan(&target)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				if err := tx.QueryRow(r.Context(), `INSERT INTO classes(school_id,academic_year_id,name,grade_level) VALUES($1,$2,$3,$4) RETURNING id::text`, id.SchoolID, to.ID, c.name, c.grade).Scan(&target); err != nil {
					return false, err
				}
				result["classes_created"]++
			case err != nil:
				return false, err
			default:
				result["classes_kept"]++
			}
			tag, err := tx.Exec(r.Context(), `INSERT INTO class_subjects(school_id,class_id,name,teacher_id,counts_in_average,sort_order)
				SELECT cs.school_id,$2,cs.name,CASE WHEN $4 AND u.status='active' THEN cs.teacher_id END,cs.counts_in_average,cs.sort_order
				FROM class_subjects cs LEFT JOIN users u ON u.id=cs.teacher_id
				WHERE cs.class_id=$1 AND cs.school_id=$3 AND NOT EXISTS(SELECT 1 FROM class_subjects x WHERE x.class_id=$2 AND lower(x.name)=lower(cs.name))`,
				c.id, target, id.SchoolID, req.Teachers)
			if err != nil {
				return false, err
			}
			result["subjects_created"] += int(tag.RowsAffected())
			if req.Teachers {
				tag, err := tx.Exec(r.Context(), `INSERT INTO class_teachers(school_id,class_id,teacher_id)
					SELECT ct.school_id,$2,ct.teacher_id FROM class_teachers ct JOIN users u ON u.id=ct.teacher_id AND u.status='active'
					WHERE ct.class_id=$1 AND ct.school_id=$3 ON CONFLICT DO NOTHING`, c.id, target, id.SchoolID)
				if err != nil {
					return false, err
				}
				result["teachers_assigned"] += int(tag.RowsAffected())
			}
		}
		if req.Terms {
			var existing int
			if err := tx.QueryRow(r.Context(), `SELECT count(*)::int FROM academic_terms WHERE academic_year_id=$1`, to.ID).Scan(&existing); err != nil {
				return false, err
			}
			if existing == 0 {
				offset := int(to.Starts.Sub(from.Starts).Hours() / 24)
				tag, err := tx.Exec(r.Context(), `INSERT INTO academic_terms(school_id,academic_year_id,name,starts_on,ends_on)
					SELECT school_id,$2,name,GREATEST(starts_on+$3::int,$4::date),LEAST(ends_on+$3::int,$5::date) FROM academic_terms
					WHERE academic_year_id=$1 AND school_id=$6 AND GREATEST(starts_on+$3::int,$4::date) <= LEAST(ends_on+$3::int,$5::date) ORDER BY starts_on`,
					from.ID, to.ID, offset, to.Starts, to.Ends, id.SchoolID)
				if err != nil {
					return false, err
				}
				result["terms_created"] = int(tag.RowsAffected())
			}
		}
		changes := map[string]any{"from": from.Name, "to": to.Name}
		for k, v := range result {
			changes[k] = v
		}
		return false, audit(r.Context(), tx, id, "year.structure_copied", "academic_year", to.ID, changes)
	}) {
		return
	}
	writeJSON(w, 200, map[string]any{"data": result})
}

type promotionStudent struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Number      string   `json:"student_number"`
	Average     *float64 `json:"average"`
	PassPercent *float64 `json:"pass_percent"`
	ResultFrom  string   `json:"result_from"`
	Suggestion  string   `json:"suggestion"`
}

type promotionClass struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	Grade           string             `json:"grade_level"`
	Final           bool               `json:"final_grade"`
	PromoteTo       string             `json:"promote_to"`
	RepeatIn        string             `json:"repeat_in"`
	CreateSuggested *map[string]string `json:"create_suggested"`
	Students        []promotionStudent `json:"students"`
}

// promotionPlan lists each class of the year with its students still enrolled
// and a suggested decision for each: repeat when the latest published report
// card in the year is below the pass mark, graduate in the school's top grade,
// and promote otherwise. It also suggests the classes to move them into.
func (a *API) promotionPlan(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	from, to, ok := a.loadYearPair(w, r, id, r.URL.Query().Get("to"))
	if !ok {
		return
	}
	type class struct{ id, name, grade string }
	readClasses := func(yearID string) ([]class, error) {
		rows, err := a.db.Query(r.Context(), `SELECT id::text,name,grade_level FROM classes WHERE academic_year_id=$1 AND school_id=$2 ORDER BY grade_level,name`, yearID, id.SchoolID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []class
		for rows.Next() {
			var c class
			if err := rows.Scan(&c.id, &c.name, &c.grade); err != nil {
				return nil, err
			}
			out = append(out, c)
		}
		return out, rows.Err()
	}
	source, err := readClasses(from.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	targets, err := readClasses(to.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	top := 0
	for _, c := range source {
		if n, ok := gradeNumber(c.grade); ok && n > top {
			top = n
		}
	}
	// pick finds the target class with a grade number, preferring the same name.
	pick := func(grade int, label, name string) string {
		first := ""
		for _, t := range targets {
			n, ok := gradeNumber(t.grade)
			sameGrade := (ok && n == grade) || (!ok && grade < 0 && strings.EqualFold(t.grade, label))
			if !sameGrade {
				continue
			}
			if strings.EqualFold(t.name, name) {
				return t.id
			}
			if first == "" {
				first = t.id
			}
		}
		return first
	}
	plan := []promotionClass{}
	var studentIDs []string
	index := map[string][2]int{}
	for _, c := range source {
		pc := promotionClass{ID: c.id, Name: c.name, Grade: c.grade, Students: []promotionStudent{}}
		if n, ok := gradeNumber(c.grade); ok {
			pc.PromoteTo = pick(n+1, "", c.name)
			pc.RepeatIn = pick(n, "", c.name)
			pc.Final = n == top && pc.PromoteTo == ""
			if pc.PromoteTo == "" && !pc.Final {
				pc.CreateSuggested = &map[string]string{"grade_level": nextGradeLabel(c.grade), "name": c.name}
			}
		} else {
			pc.RepeatIn = pick(-1, c.grade, c.name)
		}
		rows, err := a.db.Query(r.Context(), `SELECT s.id::text,s.legal_name,s.student_number FROM enrollments e JOIN students s ON s.id=e.student_id
			WHERE e.class_id=$1 AND e.school_id=$2 AND e.status='active' ORDER BY s.legal_name`, c.id, id.SchoolID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		for rows.Next() {
			var s promotionStudent
			if err := rows.Scan(&s.ID, &s.Name, &s.Number); err != nil {
				rows.Close()
				a.serverError(w, r, err)
				return
			}
			index[s.ID] = [2]int{len(plan), len(pc.Students)}
			studentIDs = append(studentIDs, s.ID)
			pc.Students = append(pc.Students, s)
		}
		rows.Close()
		plan = append(plan, pc)
	}
	if len(studentIDs) > 0 {
		rows, err := a.db.Query(r.Context(), `SELECT DISTINCT ON (rc.student_id) rc.student_id::text,
			COALESCE(rc.card->>'year_average',rc.card->>'average'),rc.card->>'pass_percent',t.name
			FROM report_cards rc JOIN academic_terms t ON t.id=rc.term_id
			WHERE rc.school_id=$1 AND t.academic_year_id=$2 AND rc.student_id=ANY($3::uuid[]) ORDER BY rc.student_id,t.ends_on DESC`, id.SchoolID, from.ID, studentIDs)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		for rows.Next() {
			var studentID, term string
			var average, pass *string
			if err := rows.Scan(&studentID, &average, &pass, &term); err != nil {
				rows.Close()
				a.serverError(w, r, err)
				return
			}
			at := index[studentID]
			s := &plan[at[0]].Students[at[1]]
			s.ResultFrom = term
			if average != nil {
				if v, err := strconv.ParseFloat(*average, 64); err == nil {
					s.Average = &v
				}
			}
			if pass != nil {
				if v, err := strconv.ParseFloat(*pass, 64); err == nil {
					s.PassPercent = &v
				}
			}
		}
		rows.Close()
	}
	for i := range plan {
		for j := range plan[i].Students {
			s := &plan[i].Students[j]
			failed := s.Average != nil && s.PassPercent != nil && *s.Average < *s.PassPercent
			switch {
			case failed:
				s.Suggestion = "repeat"
			case plan[i].Final:
				s.Suggestion = "graduate"
			default:
				s.Suggestion = "promote"
			}
		}
	}
	type option struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Grade string `json:"grade_level"`
	}
	options := []option{}
	for _, t := range targets {
		options = append(options, option{t.id, t.name, t.grade})
	}
	sort.SliceStable(options, func(i, j int) bool {
		a, _ := gradeNumber(options[i].Grade)
		b, _ := gradeNumber(options[j].Grade)
		return a < b
	})
	writeJSON(w, 200, map[string]any{"data": map[string]any{"from": from.Name, "to": to.Name, "from_closed": from.Closed, "classes": plan, "target_classes": options}})
}

var studentStatuses = []string{"active", "inactive", "graduated", "left"}

var promotionStatus = map[string]string{"promote": "promoted", "repeat": "repeated", "graduate": "graduated", "leave": "left"}

// promoteStudents carries out the decisions: each student's place in this
// year's class ends with the reason, and a promoted or repeating student joins
// the chosen class in the next year. Graduates and leavers keep their records
// and can no longer sign in.
func (a *API) promoteStudents(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		ToYearID  string `json:"to_year_id"`
		Decisions []struct {
			StudentID string `json:"student_id"`
			Action    string `json:"action"`
			ClassID   string `json:"class_id"`
		} `json:"decisions"`
	}
	if !decode(w, r, &req) || len(req.Decisions) == 0 || len(req.Decisions) > 5000 {
		writeError(w, 400, "VALIDATION_ERROR", "Choose what happens to at least one student.", nil)
		return
	}
	from, to, ok := a.loadYearPair(w, r, id, req.ToYearID)
	if !ok {
		return
	}
	if from.Closed {
		writeYearClosed(w)
		return
	}
	seen := map[string]bool{}
	for _, d := range req.Decisions {
		_, known := promotionStatus[d.Action]
		needsClass := d.Action == "promote" || d.Action == "repeat"
		if !uuidPattern.MatchString(d.StudentID) || !known || needsClass != uuidPattern.MatchString(d.ClassID) || seen[d.StudentID] {
			writeError(w, 400, "VALIDATION_ERROR", "A decision could not be read. Promoted and repeating students need a class.", nil)
			return
		}
		seen[d.StudentID] = true
	}
	counts := map[string]int{"promoted": 0, "repeated": 0, "graduated": 0, "left": 0}
	if !a.runInTx(w, r, func(tx pgx.Tx) (bool, error) {
		for _, d := range req.Decisions {
			var enrollment string
			err := tx.QueryRow(r.Context(), `SELECT e.id::text FROM enrollments e JOIN classes c ON c.id=e.class_id
				WHERE e.student_id=$1 AND e.school_id=$2 AND e.status='active' AND c.academic_year_id=$3 FOR UPDATE OF e`, d.StudentID, id.SchoolID, from.ID).Scan(&enrollment)
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, 409, "NOT_IN_YEAR", "A student is no longer in a class of this year. Reload the list.", nil)
				return true, nil
			}
			if err != nil {
				return false, err
			}
			if d.ClassID != "" {
				var inYear bool
				if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM classes WHERE id=$1 AND school_id=$2 AND academic_year_id=$3)`, d.ClassID, id.SchoolID, to.ID).Scan(&inYear); err != nil {
					return false, err
				}
				if !inYear {
					writeError(w, 400, "VALIDATION_ERROR", "A chosen class is not in the next school year.", nil)
					return true, nil
				}
			}
			status := promotionStatus[d.Action]
			if _, err := tx.Exec(r.Context(), `UPDATE enrollments SET status=$2,ended_at=GREATEST(LEAST(CURRENT_DATE,$3::date),enrolled_at) WHERE id=$1`, enrollment, status, from.Ends); err != nil {
				return false, err
			}
			// Any other place still open in the next year is replaced.
			if _, err := tx.Exec(r.Context(), `UPDATE enrollments SET status='ended',ended_at=GREATEST(CURRENT_DATE,enrolled_at) WHERE student_id=$1 AND school_id=$2 AND status='active'`, d.StudentID, id.SchoolID); err != nil {
				return false, err
			}
			switch d.Action {
			case "promote", "repeat":
				if _, err := tx.Exec(r.Context(), `INSERT INTO enrollments(school_id,student_id,class_id) VALUES($1,$2,$3)`, id.SchoolID, d.StudentID, d.ClassID); err != nil {
					return false, err
				}
			case "graduate", "leave":
				if _, err := tx.Exec(r.Context(), `UPDATE students SET status=$3 WHERE id=$1 AND school_id=$2`, d.StudentID, id.SchoolID, status); err != nil {
					return false, err
				}
				if err := syncStudentLogin(r.Context(), tx, id.SchoolID, d.StudentID, status); err != nil {
					return false, err
				}
			}
			if err := audit(r.Context(), tx, id, "student."+status, "student", d.StudentID, map[string]any{"from_year": from.Name, "to_year": to.Name, "class_id": d.ClassID}); err != nil {
				return false, err
			}
			counts[status]++
		}
		return false, audit(r.Context(), tx, id, "year.students_moved", "academic_year", from.ID, map[string]any{"to": to.Name, "promoted": counts["promoted"], "repeated": counts["repeated"], "graduated": counts["graduated"], "left": counts["left"]})
	}) {
		return
	}
	writeJSON(w, 200, map[string]any{"data": counts})
}

// closeYear makes a year read-only. Every student must have been moved on first.
func (a *API) closeYear(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	yearID := r.PathValue("yearID")
	if !a.runInTx(w, r, func(tx pgx.Tx) (bool, error) {
		year, err := loadYear(r.Context(), tx, id.SchoolID, yearID)
		if err != nil {
			return false, err
		}
		if year.Closed {
			writeError(w, 409, "YEAR_CLOSED", "This school year is already closed.", nil)
			return true, nil
		}
		var remaining int
		if err := tx.QueryRow(r.Context(), `SELECT count(*)::int FROM enrollments e JOIN classes c ON c.id=e.class_id WHERE c.academic_year_id=$1 AND e.status='active'`, yearID).Scan(&remaining); err != nil {
			return false, err
		}
		if remaining > 0 {
			writeError(w, 409, "YEAR_HAS_STUDENTS", strconv.Itoa(remaining)+" students are still in this year's classes. Move them on before closing the year.", map[string]string{"remaining": strconv.Itoa(remaining)})
			return true, nil
		}
		if _, err := tx.Exec(r.Context(), `UPDATE academic_years SET closed_at=now(),closed_by=$2 WHERE id=$1`, yearID, id.UserID); err != nil {
			return false, err
		}
		return false, audit(r.Context(), tx, id, "year.closed", "academic_year", yearID, map[string]any{"name": year.Name})
	}) {
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]bool{"closed": true}})
}

func (a *API) reopenYear(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	yearID := r.PathValue("yearID")
	if !a.runInTx(w, r, func(tx pgx.Tx) (bool, error) {
		year, err := loadYear(r.Context(), tx, id.SchoolID, yearID)
		if err != nil {
			return false, err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE academic_years SET closed_at=NULL,closed_by=NULL WHERE id=$1`, yearID); err != nil {
			return false, err
		}
		return false, audit(r.Context(), tx, id, "year.reopened", "academic_year", yearID, map[string]any{"name": year.Name})
	}) {
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]bool{"closed": false}})
}
