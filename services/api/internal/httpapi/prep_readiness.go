package httpapi

import (
	"context"
	"math"
	"net/http"
	"slices"

	"github.com/furii/school-os/services/api/internal/auth"
)

const (
	// The direct admission line. It can shift each cycle, so results are worded
	// as distance from this line rather than as a pass or fail.
	readinessPassLine = 300
	// A paper needs this many answers before it counts toward an estimate.
	readinessMinResponses = 15
	// Only recent work counts, so an estimate moves as the student improves.
	readinessRecentResponses = 150
	readinessZ               = 1.96
)

// readinessResponsesSQL is every graded answer that counts toward readiness:
// practice, timed papers, and school mock papers. $1 is the school and $2 the
// students. Timed work and school mocks weigh more than practice with answers shown.
const readinessResponsesSQL = `
	SELECT ps.student_id,ps.paper,i.question_id,i.is_correct,i.answered_at,
	       CASE WHEN ps.mode='timed' THEN 1.0 ELSE 0.5 END::float8 AS weight
	FROM prep_session_items i JOIN prep_sessions ps ON ps.id=i.session_id
	WHERE ps.school_id=$1 AND ps.student_id=ANY($2::uuid[]) AND i.is_correct IS NOT NULL
	UNION ALL
	SELECT at.student_id,a.national_paper,ans.question_id,ans.is_correct,at.submitted_at,1.0::float8
	FROM assessment_answers ans
	JOIN assessment_attempts at ON at.id=ans.attempt_id
	JOIN assessment_assignments aa ON aa.id=at.assignment_id
	JOIN assessments a ON a.id=aa.assessment_id
	WHERE at.school_id=$1 AND at.student_id=ANY($2::uuid[]) AND at.status='submitted' AND a.national_paper IS NOT NULL AND ans.is_correct IS NOT NULL`

// paperStat is a student's recent work on one paper.
type paperStat struct {
	Responses int
	Weight    float64
	Correct   float64
}

type paperEstimate struct {
	Paper     string   `json:"paper"`
	Responses int      `json:"responses"`
	Enough    bool     `json:"enough_evidence"`
	Score     *float64 `json:"score"`
	Low       *float64 `json:"low"`
	High      *float64 `json:"high"`
}

type readinessTotal struct {
	Predicted float64 `json:"predicted"`
	Low       float64 `json:"low"`
	High      float64 `json:"high"`
	PassLine  int     `json:"pass_line"`
	Distance  float64 `json:"distance_to_pass_line"`
	Status    string  `json:"status"`
}

type readinessEstimate struct {
	Stream       string          `json:"stream"`
	Status       string          `json:"status"`
	Papers       []paperEstimate `json:"papers"`
	Total        *readinessTotal `json:"total"`
	MinResponses int             `json:"min_responses"`
}

// estimateReadiness turns answers into a predicted score. Each paper's score is
// the student's weighted share correct, pulled slightly toward the middle so a
// short run of luck does not swing it, with a 95% range from how many answers
// support it. The six papers add up to a total out of 600. This describes the
// student's work on this school's questions. It is not a forecast of the
// official result, and it is withheld until every paper has enough answers.
func estimateReadiness(stream string, stats map[string]paperStat) readinessEstimate {
	papers := []string{}
	if stream == "" {
		for _, paper := range slices.Concat(commonPapers, naturalPapers, socialPapers) {
			if stats[paper].Responses > 0 || slices.Contains(commonPapers, paper) {
				papers = append(papers, paper)
			}
		}
	} else {
		papers = streamPapers(stream)
	}
	result := readinessEstimate{Stream: stream, Papers: []paperEstimate{}, MinResponses: readinessMinResponses}
	sum, variance := 0.0, 0.0
	enough := 0
	for _, paper := range papers {
		stat := stats[paper]
		estimate := paperEstimate{Paper: paper, Responses: stat.Responses, Enough: stat.Responses >= readinessMinResponses}
		if stat.Responses > 0 {
			share := (stat.Correct + 1) / (stat.Weight + 2)
			margin := readinessZ * math.Sqrt(share*(1-share)/(stat.Weight+2))
			score, low, high := share*100, math.Max(0, share-margin)*100, math.Min(1, share+margin)*100
			estimate.Score, estimate.Low, estimate.High = &score, &low, &high
			if estimate.Enough {
				enough++
				sum += score
				variance += math.Pow(margin*100, 2)
			}
		}
		result.Papers = append(result.Papers, estimate)
	}
	switch {
	case stream == "":
		result.Status = "no_stream"
	case enough < len(papers):
		result.Status = "need_more_evidence"
	default:
		result.Status = "estimated"
		margin := math.Sqrt(variance)
		total := readinessTotal{Predicted: sum, Low: math.Max(0, sum-margin), High: math.Min(600, sum+margin), PassLine: readinessPassLine, Distance: sum - readinessPassLine}
		switch {
		case total.Low >= readinessPassLine:
			total.Status = "above_line"
		case total.High < readinessPassLine:
			total.Status = "at_risk"
		default:
			total.Status = "near_line"
		}
		result.Total = &total
	}
	return result
}

// readinessStats reads recent work per student and paper.
func (a *API) readinessStats(ctx context.Context, schoolID string, studentIDs []string) (map[string]map[string]paperStat, error) {
	result := map[string]map[string]paperStat{}
	rows, err := a.db.Query(ctx, `
		WITH responses AS (`+readinessResponsesSQL+`),
		ranked AS (SELECT *,row_number() OVER (PARTITION BY student_id,paper ORDER BY answered_at DESC) AS recency FROM responses)
		SELECT student_id::text,paper,count(*)::int,sum(weight)::float8,sum(CASE WHEN is_correct THEN weight ELSE 0 END)::float8
		FROM ranked WHERE recency<=$3 GROUP BY student_id,paper`, schoolID, studentIDs, readinessRecentResponses)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var studentID, paper string
		var stat paperStat
		if err = rows.Scan(&studentID, &paper, &stat.Responses, &stat.Weight, &stat.Correct); err != nil {
			return nil, err
		}
		if result[studentID] == nil {
			result[studentID] = map[string]paperStat{}
		}
		result[studentID][paper] = stat
	}
	return result, rows.Err()
}

type topicStat struct {
	Paper     string  `json:"paper"`
	SkillID   string  `json:"skill_id"`
	Title     string  `json:"title"`
	Responses int     `json:"responses"`
	Percent   float64 `json:"percent"`
	Students  int     `json:"students"`
}

// readinessTopics reads topic results, weakest first. A topic needs at least
// minResponses answers to appear.
func (a *API) readinessTopics(ctx context.Context, schoolID string, studentIDs []string, minResponses, limit int) ([]topicStat, error) {
	rows, err := a.db.Query(ctx, `
		WITH responses AS (`+readinessResponsesSQL+`)
		SELECT r.paper,t.skill_id::text,n.title,count(*)::int,
		       avg(CASE WHEN r.is_correct THEN 100.0 ELSE 0 END)::float8,count(DISTINCT r.student_id)::int
		FROM responses r JOIN question_skill_tags t ON t.question_id=r.question_id JOIN curriculum_nodes n ON n.id=t.skill_id
		GROUP BY r.paper,t.skill_id,n.title HAVING count(*)>=$3
		ORDER BY 5 ASC,3 LIMIT $4`, schoolID, studentIDs, minResponses, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []topicStat{}
	for rows.Next() {
		var x topicStat
		if err = rows.Scan(&x.Paper, &x.SkillID, &x.Title, &x.Responses, &x.Percent, &x.Students); err != nil {
			return nil, err
		}
		items = append(items, x)
	}
	return items, rows.Err()
}

func (a *API) studentReadiness(ctx context.Context, schoolID, studentID, stream string) (map[string]any, error) {
	stats, err := a.readinessStats(ctx, schoolID, []string{studentID})
	if err != nil {
		return nil, err
	}
	topics, err := a.readinessTopics(ctx, schoolID, []string{studentID}, 3, 8)
	if err != nil {
		return nil, err
	}
	estimate := estimateReadiness(stream, stats[studentID])
	return map[string]any{"stream": estimate.Stream, "status": estimate.Status, "papers": estimate.Papers, "total": estimate.Total, "min_responses": estimate.MinResponses, "weak_topics": topics}, nil
}

// classReadiness is the teacher's view: each student's estimate, who is at risk,
// and the class's weakest topics.
func (a *API) classReadiness(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	classID := r.PathValue("classID")
	allowed, err := a.canManageClass(r.Context(), id, classID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !allowed {
		writeError(w, 403, "FORBIDDEN", "You are not assigned to this class.", nil)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT s.id::text,s.student_number,s.legal_name,COALESCE(s.stream,''),s.prep_onboarded_at IS NOT NULL
		FROM enrollments e JOIN students s ON s.id=e.student_id AND s.school_id=e.school_id
		WHERE e.school_id=$1 AND e.class_id=$2 AND e.status='active' AND s.status='active' ORDER BY s.legal_name LIMIT 500`, id.SchoolID, classID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type studentRow struct {
		StudentID string `json:"student_id"`
		Number    string `json:"student_number"`
		Name      string `json:"student_name"`
		Onboarded bool   `json:"onboarded"`
		readinessEstimate
	}
	students := []studentRow{}
	ids := []string{}
	for rows.Next() {
		var x studentRow
		if err = rows.Scan(&x.StudentID, &x.Number, &x.Name, &x.Stream, &x.Onboarded); err != nil {
			a.serverError(w, r, err)
			return
		}
		students = append(students, x)
		ids = append(ids, x.StudentID)
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	rows.Close()
	stats, err := a.readinessStats(r.Context(), id.SchoolID, ids)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	topics, err := a.readinessTopics(r.Context(), id.SchoolID, ids, 5, 40)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	atRisk := 0
	for i := range students {
		stream := students[i].Stream
		students[i].readinessEstimate = estimateReadiness(stream, stats[students[i].StudentID])
		if total := students[i].Total; total != nil && total.Status == "at_risk" {
			atRisk++
		}
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"students": students, "topics": topics, "at_risk": atRisk, "pass_line": readinessPassLine}})
}

type readinessGroup struct {
	Key       string   `json:"key"`
	Grade     string   `json:"grade_level,omitempty"`
	Name      string   `json:"name"`
	Students  int      `json:"students"`
	Estimated int      `json:"estimated"`
	Average   *float64 `json:"average_predicted"`
	Above     int      `json:"above_line"`
	Near      int      `json:"near_line"`
	AtRisk    int      `json:"at_risk"`
	sum       float64
}

func (g *readinessGroup) add(estimate readinessEstimate) {
	g.Students++
	if estimate.Total == nil {
		return
	}
	g.Estimated++
	g.sum += estimate.Total.Predicted
	average := g.sum / float64(g.Estimated)
	g.Average = &average
	switch estimate.Total.Status {
	case "above_line":
		g.Above++
	case "near_line":
		g.Near++
	default:
		g.AtRisk++
	}
}

// schoolReadiness is the leader's view: readiness by stream and by class.
func (a *API) schoolReadiness(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, err := a.db.Query(r.Context(), `SELECT s.id::text,COALESCE(s.stream,''),c.id::text,c.name,c.grade_level
		FROM enrollments e JOIN students s ON s.id=e.student_id AND s.school_id=e.school_id JOIN classes c ON c.id=e.class_id
		WHERE e.school_id=$1 AND e.status='active' AND s.status='active' ORDER BY c.grade_level,c.name LIMIT 5000`, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type enrolled struct{ studentID, stream, classID, className, grade string }
	found := []enrolled{}
	ids := []string{}
	for rows.Next() {
		var x enrolled
		if err = rows.Scan(&x.studentID, &x.stream, &x.classID, &x.className, &x.grade); err != nil {
			a.serverError(w, r, err)
			return
		}
		found = append(found, x)
		ids = append(ids, x.studentID)
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	rows.Close()
	stats, err := a.readinessStats(r.Context(), id.SchoolID, ids)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	streams := map[string]*readinessGroup{"natural": {Key: "natural", Name: "natural"}, "social": {Key: "social", Name: "social"}, "": {Key: "", Name: "none"}}
	classes := []*readinessGroup{}
	byClass := map[string]*readinessGroup{}
	for _, x := range found {
		estimate := estimateReadiness(x.stream, stats[x.studentID])
		streams[x.stream].add(estimate)
		group := byClass[x.classID]
		if group == nil {
			group = &readinessGroup{Key: x.classID, Name: x.className, Grade: x.grade}
			byClass[x.classID] = group
			classes = append(classes, group)
		}
		group.add(estimate)
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"streams": []*readinessGroup{streams["natural"], streams["social"], streams[""]}, "classes": classes, "pass_line": readinessPassLine}})
}
