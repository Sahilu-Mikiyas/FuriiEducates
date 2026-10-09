package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5/pgconn"
)

func (a *API) registerCurriculumRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/curricula", a.admin(a.listCurricula))
	mux.HandleFunc("POST /api/v1/curricula", a.admin(a.createCurriculum))
	mux.HandleFunc("GET /api/v1/curricula/{curriculumID}/subjects", a.admin(a.listSubjects))
	mux.HandleFunc("POST /api/v1/curricula/{curriculumID}/subjects", a.admin(a.createSubject))
	mux.HandleFunc("GET /api/v1/subjects/{subjectID}/nodes", a.admin(a.listCurriculumNodes))
	mux.HandleFunc("POST /api/v1/subjects/{subjectID}/nodes", a.admin(a.createCurriculumNode))
	mux.HandleFunc("POST /api/v1/curriculum-mappings/review", a.admin(a.reviewCurriculumMapping))
	mux.HandleFunc("GET /api/v1/curriculum-mappings", a.admin(a.listCurriculumMappings))
	mux.HandleFunc("GET /api/v1/questions", a.admin(a.listQuestions))
	mux.HandleFunc("POST /api/v1/questions", a.admin(a.createQuestion))
	mux.HandleFunc("GET /api/v1/questions/{questionID}", a.admin(a.getQuestion))
	mux.HandleFunc("PATCH /api/v1/questions/{questionID}", a.admin(a.updateQuestion))
	mux.HandleFunc("POST /api/v1/questions/{questionID}/publish", a.admin(a.publishQuestion))
}

func (a *API) reviewCurriculumMapping(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var q struct {
		SourceID   string   `json:"source_node_id"`
		TargetID   string   `json:"target_node_id"`
		Type       string   `json:"mapping_type"`
		Confidence *float64 `json:"confidence"`
	}
	if !decode(w, r, &q) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	allowed := map[string]bool{"equivalent": true, "overlaps": true, "prerequisite": true, "related": true}
	if q.SourceID == "" || q.TargetID == "" || q.SourceID == q.TargetID || !allowed[q.Type] || q.Confidence != nil && (*q.Confidence < 0 || *q.Confidence > 1 || math.IsNaN(*q.Confidence) || math.IsInf(*q.Confidence, 0)) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose distinct curriculum nodes, a valid mapping type, and confidence between 0 and 1.", nil)
		return
	}
	var sourceExists, targetExists bool
	err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM curriculum_nodes WHERE id=$1),EXISTS(SELECT 1 FROM curriculum_nodes WHERE id=$2)`, q.SourceID, q.TargetID).Scan(&sourceExists, &targetExists)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !sourceExists || !targetExists {
		writeError(w, 404, "NOT_FOUND", "Curriculum node not found.", nil)
		return
	}
	var mappingID string
	err = a.db.QueryRow(r.Context(), `INSERT INTO curriculum_mappings(source_node_id,target_node_id,mapping_type,confidence,reviewed_by) VALUES($1,$2,$3,$4,$5) ON CONFLICT(source_node_id,target_node_id,mapping_type) DO UPDATE SET confidence=EXCLUDED.confidence,reviewed_by=EXCLUDED.reviewed_by RETURNING id::text`, q.SourceID, q.TargetID, q.Type, q.Confidence, id.UserID).Scan(&mappingID)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if err = a.auditCurriculum(r, id, "curriculum_mapping.reviewed", "curriculum_mapping", mappingID, map[string]any{"mapping_type": q.Type, "confidence": q.Confidence}); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": mappingID, "status": "reviewed"}})
}

func (a *API) listCurriculumMappings(w http.ResponseWriter, r *http.Request, _ auth.Identity) {
	rows, err := a.db.Query(r.Context(), `SELECT m.id::text,m.source_node_id::text,s.title,m.target_node_id::text,t.title,m.mapping_type,m.confidence,m.reviewed_by::text FROM curriculum_mappings m JOIN curriculum_nodes s ON s.id=m.source_node_id JOIN curriculum_nodes t ON t.id=m.target_node_id ORDER BY m.created_at DESC LIMIT 500`)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, sourceID, sourceTitle, targetID, targetTitle, typ string
		var confidence *float64
		var reviewer *string
		if err = rows.Scan(&id, &sourceID, &sourceTitle, &targetID, &targetTitle, &typ, &confidence, &reviewer); err != nil {
			a.serverError(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": id, "source_node_id": sourceID, "source_title": sourceTitle, "target_node_id": targetID, "target_title": targetTitle, "mapping_type": typ, "confidence": confidence, "reviewed_by": reviewer, "status": "reviewed"})
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

func (a *API) listCurricula(w http.ResponseWriter, r *http.Request, _ auth.Identity) {
	rows, err := a.db.Query(r.Context(), `SELECT id::text,name,provider,COALESCE(country_code,''),version_label,language_code,COALESCE(source_reference,''),COALESCE(license_notes,'') FROM curricula ORDER BY provider,name,version_label`)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, name, provider, country, version, language, source, license string
		if err = rows.Scan(&id, &name, &provider, &country, &version, &language, &source, &license); err != nil {
			a.serverError(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": id, "name": name, "provider": provider, "country_code": country, "version_label": version, "language_code": language, "source_reference": source, "license_notes": license})
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

func (a *API) createCurriculum(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var q struct {
		Name        string `json:"name"`
		Provider    string `json:"provider"`
		CountryCode string `json:"country_code"`
		Version     string `json:"version_label"`
		Language    string `json:"language_code"`
		Source      string `json:"source_reference"`
		License     string `json:"license_notes"`
	}
	if !decode(w, r, &q) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	q.Name = strings.TrimSpace(q.Name)
	q.Provider = strings.TrimSpace(q.Provider)
	q.Version = strings.TrimSpace(q.Version)
	q.Language = strings.TrimSpace(q.Language)
	if q.Name == "" || q.Provider == "" || q.Version == "" {
		writeError(w, 400, "VALIDATION_ERROR", "Curriculum name, provider, and version are required.", nil)
		return
	}
	if q.Language == "" {
		q.Language = "en"
	}
	var cid string
	err := a.db.QueryRow(r.Context(), `INSERT INTO curricula(name,provider,country_code,version_label,language_code,source_reference,license_notes) VALUES($1,$2,NULLIF($3,''),$4,$5,NULLIF($6,''),NULLIF($7,'')) RETURNING id::text`, q.Name, q.Provider, strings.TrimSpace(q.CountryCode), q.Version, q.Language, strings.TrimSpace(q.Source), strings.TrimSpace(q.License)).Scan(&cid)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if err = a.auditCurriculum(r, id, "curriculum.created", "curriculum", cid, map[string]any{"name": q.Name, "provider": q.Provider, "version_label": q.Version}); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]string{"id": cid}})
}

func (a *API) listSubjects(w http.ResponseWriter, r *http.Request, _ auth.Identity) {
	rows, err := a.db.Query(r.Context(), `SELECT id::text,name,COALESCE(grade_level,''),COALESCE(subject_code,'') FROM subjects WHERE curriculum_id=$1 ORDER BY grade_level,name`, r.PathValue("curriculumID"))
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, name, grade, code string
		if err = rows.Scan(&id, &name, &grade, &code); err != nil {
			a.serverError(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": id, "name": name, "grade_level": grade, "subject_code": code})
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

func (a *API) createSubject(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var q struct {
		Name  string `json:"name"`
		Grade string `json:"grade_level"`
		Code  string `json:"subject_code"`
	}
	if !decode(w, r, &q) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	q.Name = strings.TrimSpace(q.Name)
	if q.Name == "" {
		writeError(w, 400, "VALIDATION_ERROR", "Subject name is required.", nil)
		return
	}
	var sid string
	err := a.db.QueryRow(r.Context(), `INSERT INTO subjects(curriculum_id,name,grade_level,subject_code) VALUES($1,$2,NULLIF($3,''),NULLIF($4,'')) RETURNING id::text`, r.PathValue("curriculumID"), q.Name, strings.TrimSpace(q.Grade), strings.TrimSpace(q.Code)).Scan(&sid)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if err = a.auditCurriculum(r, id, "subject.created", "subject", sid, map[string]any{"name": q.Name}); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]string{"id": sid}})
}

func (a *API) listCurriculumNodes(w http.ResponseWriter, r *http.Request, _ auth.Identity) {
	rows, err := a.db.Query(r.Context(), `SELECT id::text,COALESCE(parent_id::text,''),node_type,COALESCE(code,''),title,COALESCE(description,''),sort_order FROM curriculum_nodes WHERE subject_id=$1 ORDER BY sort_order,title`, r.PathValue("subjectID"))
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, parent, typ, code, title, desc string
		var order int
		if err = rows.Scan(&id, &parent, &typ, &code, &title, &desc, &order); err != nil {
			a.serverError(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": id, "parent_id": parent, "node_type": typ, "code": code, "title": title, "description": desc, "sort_order": order})
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

func (a *API) createCurriculumNode(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var q struct {
		ParentID    string `json:"parent_id"`
		Type        string `json:"node_type"`
		Code        string `json:"code"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Order       int    `json:"sort_order"`
	}
	if !decode(w, r, &q) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	q.Title = strings.TrimSpace(q.Title)
	valid := map[string]bool{"unit": true, "topic": true, "concept": true, "skill": true, "objective": true}
	if q.Title == "" || !valid[q.Type] {
		writeError(w, 400, "VALIDATION_ERROR", "Title and a valid node type are required.", nil)
		return
	}
	if q.ParentID != "" {
		var same bool
		err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM curriculum_nodes p JOIN subjects s ON s.id=p.subject_id WHERE p.id=$1 AND p.subject_id=$2)`, q.ParentID, r.PathValue("subjectID")).Scan(&same)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if !same {
			writeError(w, 400, "VALIDATION_ERROR", "Parent node must belong to the same subject.", nil)
			return
		}
	}
	var nid string
	err := a.db.QueryRow(r.Context(), `INSERT INTO curriculum_nodes(subject_id,parent_id,node_type,code,title,description,sort_order) VALUES($1,NULLIF($2,'')::uuid,$3,NULLIF($4,''),$5,NULLIF($6,''),$7) RETURNING id::text`, r.PathValue("subjectID"), q.ParentID, q.Type, strings.TrimSpace(q.Code), q.Title, strings.TrimSpace(q.Description), q.Order).Scan(&nid)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if err = a.auditCurriculum(r, id, "curriculum_node.created", "curriculum_node", nid, map[string]any{"title": q.Title, "node_type": q.Type}); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]string{"id": nid}})
}

type questionPayload struct {
	Type        string          `json:"question_type"`
	Prompt      json.RawMessage `json:"prompt"`
	Options     json.RawMessage `json:"options"`
	Answer      json.RawMessage `json:"answer_key"`
	Explanation json.RawMessage `json:"explanation"`
	Difficulty  *int            `json:"difficulty"`
	Language    string          `json:"language_code"`
	Source      string          `json:"source_name"`
	Reference   string          `json:"source_reference"`
	License     string          `json:"license_notes"`
	Tags        []struct {
		SkillID string  `json:"skill_id"`
		Weight  float64 `json:"weight"`
	} `json:"skill_tags"`
}

var errInvalidQuestionTags = errors.New("skill tags are invalid")
var errQuestionNoLongerDraft = errors.New("question is no longer a draft")

func validateQuestion(q questionPayload) string {
	if q.Type != "single_choice" && q.Type != "multiple_choice" && q.Type != "numeric" && q.Type != "text" {
		return "Choose a supported question type."
	}
	if !json.Valid(q.Prompt) || bytes.Equal(bytes.TrimSpace(q.Prompt), []byte("null")) {
		return "A valid question prompt is required."
	}
	if !json.Valid(q.Answer) || bytes.Equal(bytes.TrimSpace(q.Answer), []byte("null")) {
		return "A valid server-side answer key is required."
	}
	if q.Difficulty != nil && (*q.Difficulty < 1 || *q.Difficulty > 5) {
		return "Difficulty must be between 1 and 5."
	}
	if q.Type == "single_choice" || q.Type == "multiple_choice" {
		var options []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}
		if json.Unmarshal(q.Options, &options) != nil || len(options) < 2 {
			return "Choice questions need at least two options."
		}
		seen := map[string]bool{}
		for _, o := range options {
			if strings.TrimSpace(o.ID) == "" || strings.TrimSpace(o.Text) == "" || seen[o.ID] {
				return "Each option needs a unique id and text."
			}
			seen[o.ID] = true
		}
		var keys []string
		if json.Unmarshal(q.Answer, &keys) != nil || len(keys) == 0 {
			return "Choice answer_key must be an array of option ids."
		}
		for _, k := range keys {
			if !seen[k] {
				return "Every answer key must match an option id."
			}
		}
		if q.Type == "single_choice" && len(keys) != 1 {
			return "Single-choice questions need exactly one correct option."
		}
	} else if len(q.Options) > 0 && !bytes.Equal(bytes.TrimSpace(q.Options), []byte("null")) {
		return "Only choice questions may include options."
	}
	if q.Type == "numeric" {
		var key struct {
			Value     *float64 `json:"value"`
			Tolerance *float64 `json:"tolerance"`
		}
		if json.Unmarshal(q.Answer, &key) != nil || key.Value == nil || math.IsNaN(*key.Value) || math.IsInf(*key.Value, 0) || key.Tolerance != nil && (*key.Tolerance < 0 || math.IsNaN(*key.Tolerance) || math.IsInf(*key.Tolerance, 0)) {
			return "Numeric answer_key needs a finite value and nonnegative tolerance."
		}
	}
	return ""
}

func (a *API) listQuestions(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, err := a.db.Query(r.Context(), `SELECT q.id::text,q.question_type,q.prompt,q.options,q.explanation,q.difficulty,q.language_code,COALESCE(q.source_name,''),COALESCE(q.source_reference,''),COALESCE(q.license_notes,''),q.status,q.created_at,COALESCE((SELECT jsonb_agg(jsonb_build_object('skill_id',t.skill_id,'title',n.title,'weight',t.weight) ORDER BY n.title) FROM question_skill_tags t JOIN curriculum_nodes n ON n.id=t.skill_id WHERE t.question_id=q.id),'[]'::jsonb) FROM questions q WHERE q.school_id=$1 AND ($2='' OR q.status=$2) ORDER BY q.created_at DESC LIMIT 200`, id.SchoolID, r.URL.Query().Get("status"))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		item, scanErr := scanQuestion(rows)
		if scanErr != nil {
			a.serverError(w, r, scanErr)
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

type rowScanner interface{ Scan(...any) error }

func scanQuestion(row rowScanner) (map[string]any, error) {
	var id, typ, language, source, reference, license, status string
	var prompt, options, explanation, tags []byte
	var difficulty *int
	var created any
	err := row.Scan(&id, &typ, &prompt, &options, &explanation, &difficulty, &language, &source, &reference, &license, &status, &created, &tags)
	if err != nil {
		return nil, err
	}
	var p, o, e, t any
	_ = json.Unmarshal(prompt, &p)
	_ = json.Unmarshal(options, &o)
	_ = json.Unmarshal(explanation, &e)
	_ = json.Unmarshal(tags, &t)
	return map[string]any{"id": id, "question_type": typ, "prompt": p, "options": o, "explanation": e, "difficulty": difficulty, "language_code": language, "source_name": source, "source_reference": reference, "license_notes": license, "status": status, "created_at": created, "skill_tags": t}, nil
}

func (a *API) createQuestion(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var q questionPayload
	if !decode(w, r, &q) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	if msg := validateQuestion(q); msg != "" {
		writeError(w, 400, "VALIDATION_ERROR", msg, nil)
		return
	}
	qid, err := a.saveQuestion(r, id, q, "")
	if errors.Is(err, errInvalidQuestionTags) {
		writeError(w, 400, "VALIDATION_ERROR", "Skill tags must reference skills and their weights must sum to 1.", nil)
		return
	}
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]string{"id": qid, "status": "draft"}})
}

func (a *API) updateQuestion(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var q questionPayload
	if !decode(w, r, &q) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	if msg := validateQuestion(q); msg != "" {
		writeError(w, 400, "VALIDATION_ERROR", msg, nil)
		return
	}
	qid := r.PathValue("questionID")
	var status string
	err := a.db.QueryRow(r.Context(), `SELECT status FROM questions WHERE id=$1 AND school_id=$2`, qid, id.SchoolID).Scan(&status)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if status != "draft" {
		writeError(w, 409, "CONFLICT", "Only draft questions can be edited.", nil)
		return
	}
	if _, err = a.saveQuestion(r, id, q, qid); errors.Is(err, errInvalidQuestionTags) {
		writeError(w, 400, "VALIDATION_ERROR", "Skill tags must reference skills and their weights must sum to 1.", nil)
		return
	} else if errors.Is(err, errQuestionNoLongerDraft) {
		writeError(w, 409, "CONFLICT", "Only draft questions can be edited.", nil)
		return
	} else if err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": qid, "status": "draft"}})
}

func (a *API) saveQuestion(r *http.Request, id auth.Identity, q questionPayload, qid string) (string, error) {
	if q.Language == "" {
		q.Language = "en"
	}
	if len(q.Tags) > 0 {
		sum := 0.0
		for _, tag := range q.Tags {
			if tag.Weight <= 0 || tag.Weight > 1 || math.IsNaN(tag.Weight) || math.IsInf(tag.Weight, 0) {
				return "", errInvalidQuestionTags
			}
			sum += tag.Weight
			var valid bool
			if err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM curriculum_nodes WHERE id=$1 AND node_type='skill')`, tag.SkillID).Scan(&valid); err != nil {
				return "", err
			}
			if !valid {
				return "", errInvalidQuestionTags
			}
		}
		if math.Abs(sum-1) > 0.0001 {
			return "", errInvalidQuestionTags
		}
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		return "", err
	}
	defer tx.Rollback(r.Context())
	if qid == "" {
		err = tx.QueryRow(r.Context(), `INSERT INTO questions(school_id,question_type,prompt,options,answer_key,explanation,difficulty,language_code,source_name,source_reference,license_notes,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),$12) RETURNING id::text`, id.SchoolID, q.Type, []byte(q.Prompt), nullableJSON(q.Options), []byte(q.Answer), nullableJSON(q.Explanation), q.Difficulty, q.Language, strings.TrimSpace(q.Source), strings.TrimSpace(q.Reference), strings.TrimSpace(q.License), id.UserID).Scan(&qid)
	} else {
		var tag pgconn.CommandTag
		tag, err = tx.Exec(r.Context(), `UPDATE questions SET question_type=$3,prompt=$4,options=$5,answer_key=$6,explanation=$7,difficulty=$8,language_code=$9,source_name=NULLIF($10,''),source_reference=NULLIF($11,''),license_notes=NULLIF($12,''),updated_at=now() WHERE id=$1 AND school_id=$2 AND status='draft'`, qid, id.SchoolID, q.Type, []byte(q.Prompt), nullableJSON(q.Options), []byte(q.Answer), nullableJSON(q.Explanation), q.Difficulty, q.Language, strings.TrimSpace(q.Source), strings.TrimSpace(q.Reference), strings.TrimSpace(q.License))
		if err == nil && tag.RowsAffected() == 0 {
			return "", errQuestionNoLongerDraft
		}
	}
	if err != nil {
		return "", err
	}
	if qid != "" {
		if _, err = tx.Exec(r.Context(), `DELETE FROM question_skill_tags WHERE question_id=$1`, qid); err != nil {
			return "", err
		}
	}
	for _, tag := range q.Tags {
		if _, err = tx.Exec(r.Context(), `INSERT INTO question_skill_tags(question_id,skill_id,weight) VALUES($1,$2,$3)`, qid, tag.SkillID, tag.Weight); err != nil {
			return "", err
		}
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'question.saved','question',$3,jsonb_build_object('question_type',$4::text,'tag_count',$5::int))`, id.SchoolID, id.UserID, qid, q.Type, len(q.Tags)); err != nil {
		return "", err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return "", err
	}
	return qid, nil
}
func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	return []byte(raw)
}

func (a *API) getQuestion(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	row := a.db.QueryRow(r.Context(), `SELECT q.id::text,q.question_type,q.prompt,q.options,q.explanation,q.difficulty,q.language_code,COALESCE(q.source_name,''),COALESCE(q.source_reference,''),COALESCE(q.license_notes,''),q.status,q.created_at,COALESCE((SELECT jsonb_agg(jsonb_build_object('skill_id',t.skill_id,'title',n.title,'weight',t.weight) ORDER BY n.title) FROM question_skill_tags t JOIN curriculum_nodes n ON n.id=t.skill_id WHERE t.question_id=q.id),'[]'::jsonb) FROM questions q WHERE q.id=$1 AND q.school_id=$2`, r.PathValue("questionID"), id.SchoolID)
	item, err := scanQuestion(row)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": item})
}

func (a *API) publishQuestion(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	qid := r.PathValue("questionID")
	var status, source, license string
	var tags int
	var total float64
	err := a.db.QueryRow(r.Context(), `SELECT q.status,COALESCE(q.source_name,''),COALESCE(q.license_notes,''),count(t.skill_id),COALESCE(sum(t.weight),0) FROM questions q LEFT JOIN question_skill_tags t ON t.question_id=q.id WHERE q.id=$1 AND q.school_id=$2 GROUP BY q.id`, qid, id.SchoolID).Scan(&status, &source, &license, &tags, &total)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if status != "draft" {
		writeError(w, 409, "CONFLICT", "Only draft questions can be published.", nil)
		return
	}
	if source == "" || license == "" {
		writeError(w, 409, "QUESTION_INCOMPLETE", "Add the question source and license or permission notes before publishing.", nil)
		return
	}
	if tags == 0 || math.Abs(total-1) > 0.0001 {
		writeError(w, 409, "QUESTION_INCOMPLETE", "Add one or more skill tags whose weights sum to 1.0000 before publishing.", nil)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	err = tx.QueryRow(r.Context(), `UPDATE questions SET status='published',updated_at=now() WHERE id=$1 AND school_id=$2 AND status='draft' RETURNING status`, qid, id.SchoolID).Scan(&status)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'question.published','question',$3,'{}'::jsonb)`, id.SchoolID, id.UserID, qid); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": qid, "status": status}})
}

func (a *API) auditCurriculum(r *http.Request, id auth.Identity, action, entity, entityID string, changes map[string]any) error {
	raw, _ := json.Marshal(changes)
	_, err := a.db.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,$3,$4,$5,$6)`, id.SchoolID, id.UserID, action, entity, entityID, raw)
	return err
}
