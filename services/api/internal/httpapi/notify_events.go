package httpapi

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// absenceGrace gives a teacher time to correct a register before families are
// told about an absence.
const absenceGrace = 10 * time.Minute

func absenceKey(studentID, date string) string { return "absent:" + studentID + ":" + date }

// noticeAbsences tells the families of students marked absent today. A student
// whose mark changes away from absent before the message goes out is not
// reported, and the unread notice is taken back. Registers for other days send
// nothing.
func (a *API) noticeAbsences(ctx context.Context, tx pgx.Tx, schoolID, classID, date string, studentIDs, statuses []string) error {
	school, err := loadSchoolNoticeContext(ctx, tx, schoolID)
	if err != nil {
		return err
	}
	if date != time.Now().In(school.Location).Format("2006-01-02") {
		return nil
	}
	var absent, present, presentKeys []string
	for i, studentID := range studentIDs {
		if statuses[i] == "absent" {
			absent = append(absent, studentID)
		} else {
			present = append(present, studentID)
			presentKeys = append(presentKeys, absenceKey(studentID, date))
		}
	}
	if len(presentKeys) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE notification_deliveries SET status='cancelled',dedupe_key=NULL,last_error='The register was corrected'
			WHERE school_id=$1 AND status='pending' AND dedupe_key=ANY($2)`, schoolID, presentKeys); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM notifications WHERE school_id=$1 AND read_at IS NULL AND dedupe_key=ANY($2)`, schoolID, presentKeys); err != nil {
			return err
		}
	}
	if len(absent) == 0 {
		return nil
	}
	guardians, err := guardiansOf(ctx, tx, schoolID, absent)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT s.id::text,s.legal_name,c.name FROM students s, classes c WHERE s.id=ANY($1::uuid[]) AND s.school_id=$2 AND c.id=$3`, absent, schoolID, classID)
	if err != nil {
		return err
	}
	type student struct{ id, name, class string }
	var list []student
	for rows.Next() {
		var s student
		if err := rows.Scan(&s.id, &s.name, &s.class); err != nil {
			rows.Close()
			return err
		}
		list = append(list, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, s := range list {
		if _, err := a.queueNotice(ctx, tx, schoolID, notice{Topic: "attendance", Kind: "attendance.absent", Link: "/family/children/" + s.id,
			DedupeKey: absenceKey(s.id, date), SendAfter: time.Now().Add(absenceGrace),
			Params: map[string]string{"student": s.name, "class": s.class, "date": date}}, guardians[s.id]); err != nil {
			return err
		}
	}
	return nil
}

// noticeReports tells students and their families that report cards are out.
func (a *API) noticeReports(ctx context.Context, tx pgx.Tx, schoolID, termID string, studentIDs []string) error {
	if len(studentIDs) == 0 {
		return nil
	}
	var term string
	if err := tx.QueryRow(ctx, `SELECT name FROM academic_terms WHERE id=$1 AND school_id=$2`, termID, schoolID).Scan(&term); err != nil {
		return err
	}
	guardians, err := guardiansOf(ctx, tx, schoolID, studentIDs)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id::text,legal_name,COALESCE(user_id::text,'') FROM students WHERE id=ANY($1::uuid[]) AND school_id=$2`, studentIDs, schoolID)
	if err != nil {
		return err
	}
	type student struct{ id, name, user string }
	var list []student
	for rows.Next() {
		var s student
		if err := rows.Scan(&s.id, &s.name, &s.user); err != nil {
			rows.Close()
			return err
		}
		list = append(list, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, s := range list {
		key := "report:" + termID + ":" + s.id
		params := map[string]string{"student": s.name, "term": term}
		if _, err := a.queueNotice(ctx, tx, schoolID, notice{Topic: "reports", Kind: "reports.published", Link: "/family/children/" + s.id, DedupeKey: key, Params: params}, guardians[s.id]); err != nil {
			return err
		}
		if s.user != "" {
			if _, err := a.queueNotice(ctx, tx, schoolID, notice{Topic: "reports", Kind: "reports.published", Link: "/learn/report-cards", DedupeKey: key, Params: params}, []recipient{{UserID: s.user}}); err != nil {
				return err
			}
		}
	}
	return nil
}

// noticeSchoolwork tells a class's students about a newly published quiz or
// assignment. Outside messages wait until it opens.
func (a *API) noticeSchoolwork(ctx context.Context, tx pgx.Tx, schoolID, assignmentID string) error {
	var title, kind, subject, class string
	var opens, due *time.Time
	var users []string
	err := tx.QueryRow(ctx, `SELECT a.title,a.kind,COALESCE(cs.name,''),c.name,aa.opens_at,aa.due_at,
		COALESCE((SELECT array_agg(s.user_id::text) FROM enrollments e JOIN students s ON s.id=e.student_id AND s.status='active' AND s.user_id IS NOT NULL WHERE e.class_id=aa.class_id AND e.status='active'),'{}')
		FROM assessment_assignments aa JOIN assessments a ON a.id=aa.assessment_id JOIN classes c ON c.id=aa.class_id LEFT JOIN class_subjects cs ON cs.id=a.class_subject_id
		WHERE aa.id=$1 AND aa.school_id=$2`, assignmentID, schoolID).Scan(&title, &kind, &subject, &class, &opens, &due, &users)
	if err != nil {
		return err
	}
	if kind != "quiz" && kind != "assignment" {
		return nil
	}
	params := map[string]string{"title": title, "subject": subject, "class": class}
	if due != nil {
		params["due"] = rfc3339(*due)
	}
	people := make([]recipient, 0, len(users))
	for _, user := range users {
		people = append(people, recipient{UserID: user})
	}
	n := notice{Topic: "schoolwork", Kind: "schoolwork." + kind, Link: "/learn", DedupeKey: "work:" + assignmentID, Params: params}
	if opens != nil && opens.After(time.Now()) {
		n.SendAfter = *opens
	}
	_, err = a.queueNotice(ctx, tx, schoolID, n, people)
	return err
}
