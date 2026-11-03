package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
)

const (
	// A topic needs this many answers before the plan says anything about it, so
	// one bad quiz does not label a student.
	learningMinResponses = 5
	learningStrongTopic  = 75.0
	// The line below which a skill also earns a recommendation.
	learningWeakTopic = 65.0
	learningMaxTopics = 3
	learningMaxNext   = 5
)

func (a *API) registerLearningRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/student/learning-plan", a.student(a.studentLearningPlan))
	mux.HandleFunc("GET /api/v1/students/{studentID}/learning-plan", a.staffMember(a.staffLearningPlan))
}

type learningTopic struct {
	SkillID     string  `json:"skill_id"`
	Title       string  `json:"title"`
	Subject     string  `json:"subject"`
	Paper       string  `json:"paper"`
	Percent     float64 `json:"percent"`
	Responses   int     `json:"responses"`
	CanPractice bool    `json:"can_practice"`
}

type learningSubject struct {
	Name      string          `json:"name"`
	Total     float64         `json:"total"`
	Change    *float64        `json:"change"`
	BelowPass bool            `json:"below_pass"`
	Topics    []learningTopic `json:"topics"`
}

type learningAttendance struct {
	Marked int    `json:"days_marked"`
	Absent int    `json:"days_absent"`
	Streak int    `json:"current_streak"`
	Reason string `json:"reason"`
}

type learningAction struct {
	Type         string `json:"type"`
	Title        string `json:"title"`
	Subject      string `json:"subject,omitempty"`
	Paper        string `json:"paper,omitempty"`
	SkillID      string `json:"skill_id,omitempty"`
	AssignmentID string `json:"assignment_id,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	DueAt        string `json:"due_at,omitempty"`
	Count        int    `json:"count,omitempty"`
}

// learningPlan brings a student's evidence into one view: what is going well,
// where there is most room to improve, and what to do next. It compares a student
// only with themself and uses published marks only.
type learningPlan struct {
	ReportTerm    string              `json:"report_term"`
	GoingWell     []learningSubject   `json:"going_well"`
	RoomToImprove []learningSubject   `json:"room_to_improve"`
	StrongTopics  []learningTopic     `json:"strong_topics"`
	WeakTopics    []learningTopic     `json:"weak_topics"`
	Attendance    *learningAttendance `json:"attendance"`
	Next          []learningAction    `json:"next"`
	HasEvidence   bool                `json:"has_evidence"`
	MinResponses  int                 `json:"min_responses"`
}

// pickSubjects names a card's strongest subjects and those with most room to
// improve, with the change since the term before. A subject below the pass mark
// is never "going well", and when every subject scored the same it says nothing.
func pickSubjects(card reportCard) (best, focus []learningSubject) {
	best, focus = []learningSubject{}, []learningSubject{}
	previous := ""
	if len(card.EarlierTerms) > 0 {
		previous = card.EarlierTerms[len(card.EarlierTerms)-1].TermID
	}
	subjects := []learningSubject{}
	for _, line := range card.Subjects {
		if line.Total == nil {
			continue
		}
		x := learningSubject{Name: line.Name, Total: *line.Total, BelowPass: *line.Total < card.PassPercent, Topics: []learningTopic{}}
		if before := line.Earlier[previous]; before != nil {
			change := round2(*line.Total - *before)
			x.Change = &change
		}
		subjects = append(subjects, x)
	}
	slices.SortStableFunc(subjects, func(p, q learningSubject) int {
		switch {
		case p.Total > q.Total:
			return -1
		case p.Total < q.Total:
			return 1
		}
		return 0
	})
	if len(subjects) < 2 || subjects[0].Total == subjects[len(subjects)-1].Total {
		return best, focus
	}
	count := 1
	if len(subjects) >= 4 {
		count = 2
	}
	for _, x := range subjects[:count] {
		if !x.BelowPass {
			best = append(best, x)
		}
	}
	for i := len(subjects) - 1; i >= len(subjects)-count; i-- {
		if subjects[i].Total < subjects[0].Total {
			focus = append(focus, subjects[i])
		}
	}
	return best, focus
}

// pickTopics sorts topics with enough answers into strong and weak, strongest and
// weakest first. Those in the middle are not worth a mention either way.
func pickTopics(topics []learningTopic) (strong, weak []learningTopic) {
	strong, weak = []learningTopic{}, []learningTopic{}
	for _, topic := range topics {
		switch {
		case topic.Responses < learningMinResponses:
		case topic.Percent >= learningStrongTopic:
			strong = append(strong, topic)
		case topic.Percent < learningWeakTopic:
			weak = append(weak, topic)
		}
	}
	byPercent := func(p, q learningTopic) int {
		switch {
		case p.Percent < q.Percent:
			return -1
		case p.Percent > q.Percent:
			return 1
		}
		return strings.Compare(p.Title, q.Title)
	}
	slices.SortFunc(weak, byPercent)
	slices.SortFunc(strong, func(p, q learningTopic) int { return byPercent(q, p) })
	return strong, weak
}

// sameSubject matches names typed by a school against each other. A class's
// "Physics" and the curriculum's "Physics" are the same subject, whatever the
// capitals, and a topic also counts through its national exam paper.
func sameSubject(name string, topic learningTopic) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return name != "" && (name == strings.ToLower(strings.TrimSpace(topic.Subject)) || name == topic.Paper)
}

// attachTopics puts each weak topic under its subject, so the plan can say
// "Physics, especially Motion", and returns the topics with no subject on the card.
func attachTopics(subjects []learningSubject, weak []learningTopic) []learningTopic {
	rest := []learningTopic{}
	for _, topic := range weak {
		attached := false
		for i := range subjects {
			if sameSubject(subjects[i].Name, topic) {
				subjects[i].Topics = append(subjects[i].Topics, topic)
				attached = true
				break
			}
		}
		if !attached {
			rest = append(rest, topic)
		}
	}
	return rest
}

type learningResource struct{ ID, Title, Subject string }

// buildNext orders what to do: unfinished practice, then work the school has set
// with the nearest deadline first, then practice on the weakest topics, then class
// files for those subjects, then questions due for another try.
func buildNext(resume *learningAction, assignments []learningAction, weak []learningTopic, focus []learningSubject, resources []learningResource, review *learningAction) []learningAction {
	next := []learningAction{}
	if resume != nil {
		next = append(next, *resume)
	}
	next = append(next, assignments...)
	practiced := 0
	for _, topic := range weak {
		if topic.CanPractice && practiced < 2 {
			next = append(next, learningAction{Type: "practice", Title: topic.Title, Subject: topic.Subject, Paper: topic.Paper, SkillID: topic.SkillID})
			practiced++
		}
	}
	shared, seen := 0, map[string]bool{}
	share := func(subject string, matches func(string) bool) {
		for _, resource := range resources {
			if shared < 2 && !seen[resource.ID] && matches(resource.Subject) {
				next = append(next, learningAction{Type: "resource", Title: resource.Title, Subject: subject, ResourceID: resource.ID})
				seen[resource.ID] = true
				shared++
			}
		}
	}
	for _, subject := range focus {
		share(subject.Name, func(name string) bool {
			return strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(subject.Name))
		})
	}
	for _, topic := range weak {
		share(topic.Subject, func(name string) bool { return sameSubject(name, topic) })
	}
	if review != nil {
		next = append(next, *review)
	}
	if len(next) > learningMaxNext {
		next = next[:learningMaxNext]
	}
	return next
}

// loadLearningTopics reads a student's results by topic. It uses the mastery
// estimate wherever there is one, which is the figure the student sees elsewhere
// and holds back until there is enough evidence. Exam work with no estimate yet
// counts by its share of correct answers.
func (a *API) loadLearningTopics(ctx context.Context, schoolID, studentID string) ([]learningTopic, error) {
	found := map[string]learningTopic{}
	practice, err := a.readinessTopics(ctx, schoolID, []string{studentID}, 1, 200)
	if err != nil {
		return nil, err
	}
	for _, topic := range practice {
		found[topic.SkillID] = learningTopic{SkillID: topic.SkillID, Title: topic.Title, Percent: round2(topic.Percent), Responses: topic.Responses}
	}
	rows, err := a.db.Query(ctx, `SELECT DISTINCT ON(ms.skill_id) ms.skill_id::text,n.title,ms.estimated_mastery::float8,ms.evidence_count
		FROM mastery_snapshots ms JOIN curriculum_nodes n ON n.id=ms.skill_id WHERE ms.school_id=$1 AND ms.student_id=$2 ORDER BY ms.skill_id,ms.calculated_at DESC`, schoolID, studentID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var x learningTopic
		if err = rows.Scan(&x.SkillID, &x.Title, &x.Percent, &x.Responses); err != nil {
			rows.Close()
			return nil, err
		}
		x.Percent = round2(x.Percent)
		found[x.SkillID] = x
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(found))
	for id := range found {
		ids = append(ids, id)
	}
	// A topic can be practised if it belongs to an exam paper and has practice questions.
	details, err := a.db.Query(ctx, `SELECT n.id::text,s.name,COALESCE(s.national_paper,''),
		s.national_paper IS NOT NULL AND EXISTS(SELECT 1 FROM question_skill_tags t JOIN questions q ON q.id=t.question_id WHERE t.skill_id=n.id AND q.school_id=$1 AND q.status='published' AND q.question_type='single_choice' AND NOT q.exam_only)
		FROM curriculum_nodes n JOIN subjects s ON s.id=n.subject_id WHERE n.id=ANY($2::uuid[])`, schoolID, ids)
	if err != nil {
		return nil, err
	}
	defer details.Close()
	for details.Next() {
		var id, subject, paper string
		var canPractice bool
		if err = details.Scan(&id, &subject, &paper, &canPractice); err != nil {
			return nil, err
		}
		x := found[id]
		x.Subject, x.Paper, x.CanPractice = subject, paper, canPractice
		found[id] = x
	}
	if err = details.Err(); err != nil {
		return nil, err
	}
	topics := make([]learningTopic, 0, len(found))
	for _, topic := range found {
		topics = append(topics, topic)
	}
	return topics, nil
}

// loadOpenAssignments finds tests a student can take now. A paper that has not
// opened, or an invigilated one still waiting for its session, is not work to do yet.
func (a *API) loadOpenAssignments(ctx context.Context, schoolID, studentID string) ([]learningAction, error) {
	rows, err := a.db.Query(ctx, `SELECT aa.id::text,a.title,COALESCE(to_json(aa.due_at)#>>'{}','')
		FROM assessment_assignments aa JOIN assessments a ON a.id=aa.assessment_id
		JOIN enrollments e ON e.class_id=aa.class_id AND e.student_id=$2 AND e.status='active'
		JOIN students s ON s.id=e.student_id AND s.status='active'
		LEFT JOIN assessment_attempts at ON at.assignment_id=aa.id AND at.student_id=$2
		WHERE aa.school_id=$1 AND a.status='published' AND (aa.stream IS NULL OR aa.stream=s.stream OR at.id IS NOT NULL)
			AND COALESCE(at.status,'')<>'submitted' AND (aa.opens_at IS NULL OR aa.opens_at<=now()) AND (aa.due_at IS NULL OR aa.due_at>now())
			AND (NOT a.locked_mode OR aa.session_open)
		ORDER BY aa.due_at NULLS LAST,aa.assigned_at LIMIT 3`, schoolID, studentID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (learningAction, error) {
		x := learningAction{Type: "assignment"}
		err := row.Scan(&x.AssignmentID, &x.Title, &x.DueAt)
		return x, err
	})
}

// buildLearningPlan assembles the plan. It is the same for the student, their
// parents and their teachers, and only the student's screen acts on the next steps.
func (a *API) buildLearningPlan(ctx context.Context, schoolID, studentID string) (learningPlan, error) {
	plan := learningPlan{GoingWell: []learningSubject{}, RoomToImprove: []learningSubject{}, StrongTopics: []learningTopic{}, WeakTopics: []learningTopic{}, Next: []learningAction{}, MinResponses: learningMinResponses}
	cards, err := a.cardsForStudent(ctx, schoolID, studentID)
	if err != nil {
		return plan, err
	}
	if len(cards) > 0 {
		plan.ReportTerm = cards[0].TermName
		plan.GoingWell, plan.RoomToImprove = pickSubjects(cards[0])
	}
	topics, err := a.loadLearningTopics(ctx, schoolID, studentID)
	if err != nil {
		return plan, err
	}
	strong, weak := pickTopics(topics)
	if len(strong) > learningMaxTopics {
		strong = strong[:learningMaxTopics]
	}
	plan.StrongTopics = strong
	plan.WeakTopics = attachTopics(plan.RoomToImprove, weak)
	if len(plan.WeakTopics) > learningMaxTopics {
		plan.WeakTopics = plan.WeakTopics[:learningMaxTopics]
	}
	plan.HasEvidence = len(cards) > 0 || len(strong)+len(weak) > 0

	// Attendance, across any class the student was marked in.
	var statuses []string
	if err = a.db.QueryRow(ctx, `SELECT COALESCE(array_agg(status ORDER BY attendance_date DESC),'{}') FROM attendance_records WHERE school_id=$1 AND student_id=$2 AND attendance_date>=CURRENT_DATE-$3::int`, schoolID, studentID, attendanceAlertWindowDays).Scan(&statuses); err != nil {
		return plan, err
	}
	if len(statuses) >= attendanceAlertMinDays {
		x := learningAttendance{Marked: len(statuses), Streak: absenceStreak(statuses)}
		for _, status := range statuses {
			if status == "absent" {
				x.Absent++
			}
		}
		x.Reason = attendanceAlertReason(x.Marked, x.Absent, x.Streak)
		plan.Attendance = &x
	}

	var resume *learningAction
	var sessionID, paper string
	err = a.db.QueryRow(ctx, `SELECT id::text,paper FROM prep_sessions WHERE student_id=$1 AND school_id=$2 AND status='in_progress' ORDER BY (mode='timed') DESC,started_at DESC LIMIT 1`, studentID, schoolID).Scan(&sessionID, &paper)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return plan, err
	}
	if err == nil {
		resume = &learningAction{Type: "resume", SessionID: sessionID, Paper: paper}
	}
	assignments, err := a.loadOpenAssignments(ctx, schoolID, studentID)
	if err != nil {
		return plan, err
	}
	rows, err := a.db.Query(ctx, `SELECT r.id::text,r.title,r.subject FROM teaching_resources r JOIN enrollments e ON e.class_id=r.class_id AND e.student_id=$2 AND e.status='active' WHERE r.school_id=$1 AND r.subject<>'' ORDER BY r.created_at DESC LIMIT 200`, schoolID, studentID)
	if err != nil {
		return plan, err
	}
	resources, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (learningResource, error) {
		var x learningResource
		err := row.Scan(&x.ID, &x.Title, &x.Subject)
		return x, err
	})
	if err != nil {
		return plan, err
	}
	var review *learningAction
	var reviewPaper string
	var reviewCount int
	err = a.db.QueryRow(ctx, `WITH latest AS (SELECT DISTINCT ON (i.question_id) i.question_id,i.is_correct,i.answered_at FROM prep_session_items i JOIN prep_sessions ps ON ps.id=i.session_id WHERE ps.student_id=$2 AND i.answered_at IS NOT NULL ORDER BY i.question_id,i.answered_at DESC)
		SELECT s.national_paper,count(DISTINCT q.id)::int FROM latest l JOIN questions q ON q.id=l.question_id JOIN question_skill_tags t ON t.question_id=q.id JOIN curriculum_nodes n ON n.id=t.skill_id JOIN subjects s ON s.id=n.subject_id
		WHERE q.school_id=$1 AND q.status='published' AND q.question_type='single_choice' AND NOT q.exam_only AND s.national_paper IS NOT NULL AND l.is_correct IS FALSE AND l.answered_at<now()-interval '`+prepReviewDelay+`'
		GROUP BY s.national_paper ORDER BY 2 DESC,1 LIMIT 1`, schoolID, studentID).Scan(&reviewPaper, &reviewCount)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return plan, err
	}
	if err == nil {
		review = &learningAction{Type: "review", Paper: reviewPaper, Count: reviewCount}
	}
	plan.Next = buildNext(resume, assignments, weak, plan.RoomToImprove, resources, review)
	return plan, nil
}

func (a *API) studentLearningPlan(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID, ok := a.prepStudent(w, r, id)
	if !ok {
		return
	}
	plan, err := a.buildLearningPlan(r.Context(), id.SchoolID, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": plan})
}

func (a *API) staffLearningPlan(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID := r.PathValue("studentID")
	allowed, err := a.canSeeStudent(r.Context(), id, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !allowed {
		writeError(w, 404, "NOT_FOUND", "Student not found.", nil)
		return
	}
	plan, err := a.buildLearningPlan(r.Context(), id.SchoolID, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": plan})
}
