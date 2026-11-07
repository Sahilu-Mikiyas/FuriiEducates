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

// Staff roles. The administrator (the director) can do everything. A registrar
// keeps student records: admissions, families, class moves, past results and
// attendance follow-up. A finance officer keeps the ledger. A teacher teaches;
// one teacher of a class can be its homeroom teacher. A person can hold several
// roles, and each request checks them again.
var staffRoles = []string{"school_admin", "teacher", "registrar", "finance_officer"}

func (a *API) registerRoleRoutes(mux *http.ServeMux) {
	mux.HandleFunc("PUT /api/v1/staff/{staffID}/roles", a.admin(a.saveStaffRoles))
	mux.HandleFunc("PUT /api/v1/classes/{classID}/homeroom", a.admin(a.setHomeroomTeacher))
	mux.HandleFunc("POST /api/v1/teacher/classes/{classID}/announcements", a.teacher(a.announceToHomeroom))
}

// records allows the administrator and the registrar.
func (a *API) records(next func(http.ResponseWriter, *http.Request, auth.Identity)) http.HandlerFunc {
	return a.requireRole(next, "This action is for the registrar or an administrator.", "school_admin", "registrar")
}

// financeOffice allows the administrator and the finance officer.
func (a *API) financeOffice(next func(http.ResponseWriter, *http.Request, auth.Identity)) http.HandlerFunc {
	return a.requireRole(next, "This action is for the finance officer or an administrator.", "school_admin", "finance_officer")
}

// office allows anyone working in the school office, for the lists they all
// look things up in: students, classes, years and the school itself.
func (a *API) office(next func(http.ResponseWriter, *http.Request, auth.Identity)) http.HandlerFunc {
	return a.requireRole(next, "This action requires a school office account.", "school_admin", "registrar", "finance_officer")
}

// staffOrRecords allows teachers (for their own classes) and the records office.
func (a *API) staffOrRecords(next func(http.ResponseWriter, *http.Request, auth.Identity)) http.HandlerFunc {
	return a.requireRole(next, "This action requires a school staff account.", "school_admin", "teacher", "registrar")
}

// seesAllStudents reports whether a person may open any student's record.
func seesAllStudents(id auth.Identity) bool {
	return hasRole(id.Roles, "school_admin") || hasRole(id.Roles, "registrar")
}

// saveStaffRoles replaces a staff member's roles. The school always keeps an
// active administrator, and administrators cannot remove their own role.
func (a *API) saveStaffRoles(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Roles []string `json:"roles"`
	}
	if !decode(w, r, &req) || len(req.Roles) == 0 || len(req.Roles) > len(staffRoles) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose at least one role.", nil)
		return
	}
	roles := slicesCompactSorted(append([]string{}, req.Roles...))
	for _, role := range roles {
		if !slices.Contains(staffRoles, role) {
			writeError(w, 400, "VALIDATION_ERROR", "A role could not be read.", nil)
			return
		}
	}
	staffID := r.PathValue("staffID")
	if staffID == id.UserID && !slices.Contains(roles, "school_admin") {
		writeError(w, 409, "OWN_ADMIN_ROLE", "You cannot remove your own administrator role. Ask another administrator.", nil)
		return
	}
	if !a.runInTx(w, r, func(tx pgx.Tx) (bool, error) {
		var current []string
		err := tx.QueryRow(r.Context(), `SELECT COALESCE(array_agg(ur.role ORDER BY ur.role) FILTER (WHERE ur.role IS NOT NULL),'{}') FROM users u LEFT JOIN user_roles ur ON ur.user_id=u.id
			WHERE u.id=$1 AND u.school_id=$2 GROUP BY u.id`, staffID, id.SchoolID).Scan(&current)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 404, "NOT_FOUND", "Staff member not found.", nil)
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if slices.Contains(current, "student") || slices.Contains(current, "parent") || !slices.ContainsFunc(current, func(role string) bool { return slices.Contains(staffRoles, role) }) {
			writeError(w, 404, "NOT_FOUND", "Staff member not found.", nil)
			return true, nil
		}
		if _, err := tx.Exec(r.Context(), `DELETE FROM user_roles WHERE user_id=$1 AND role=ANY($2::text[]) AND NOT role=ANY($3::text[])`, staffID, staffRoles, roles); err != nil {
			return false, err
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO user_roles(user_id,role) SELECT $1,unnest($2::text[]) ON CONFLICT DO NOTHING`, staffID, roles); err != nil {
			return false, err
		}
		if !slices.Contains(roles, "teacher") {
			if _, err := tx.Exec(r.Context(), `UPDATE classes SET homeroom_teacher_id=NULL WHERE homeroom_teacher_id=$1`, staffID); err != nil {
				return false, err
			}
		}
		var admins int
		if err := tx.QueryRow(r.Context(), `SELECT count(*)::int FROM users u JOIN user_roles ur ON ur.user_id=u.id AND ur.role='school_admin' WHERE u.school_id=$1 AND u.status='active'`, id.SchoolID).Scan(&admins); err != nil {
			return false, err
		}
		if admins == 0 {
			writeError(w, 409, "LAST_ADMIN", "The school needs at least one active administrator.", nil)
			return true, nil
		}
		return false, audit(r.Context(), tx, id, "staff.roles_changed", "user", staffID, map[string]any{"from": current, "to": roles})
	}) {
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"id": staffID, "roles": roles}})
}

// setHomeroomTeacher names one of a class's teachers as its homeroom teacher,
// or clears it with an empty teacher_id.
func (a *API) setHomeroomTeacher(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		TeacherID string `json:"teacher_id"`
	}
	if !decode(w, r, &req) || (req.TeacherID != "" && !uuidPattern.MatchString(req.TeacherID)) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a teacher.", nil)
		return
	}
	classID := r.PathValue("classID")
	if !a.runInTx(w, r, func(tx pgx.Tx) (bool, error) {
		if req.TeacherID != "" {
			var assigned bool
			if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM class_teachers ct JOIN user_roles ur ON ur.user_id=ct.teacher_id AND ur.role='teacher'
				WHERE ct.class_id=$1 AND ct.teacher_id=$2 AND ct.school_id=$3)`, classID, req.TeacherID, id.SchoolID).Scan(&assigned); err != nil {
				return false, err
			}
			if !assigned {
				writeError(w, 400, "NOT_CLASS_TEACHER", "Assign this teacher to the class first.", nil)
				return true, nil
			}
		}
		tag, err := tx.Exec(r.Context(), `UPDATE classes SET homeroom_teacher_id=NULLIF($3,'')::uuid WHERE id=$1 AND school_id=$2`, classID, id.SchoolID, req.TeacherID)
		if err != nil {
			return false, err
		}
		if tag.RowsAffected() == 0 {
			writeError(w, 404, "NOT_FOUND", "Class not found.", nil)
			return true, nil
		}
		return false, audit(r.Context(), tx, id, "class.homeroom_set", "class", classID, map[string]any{"teacher_id": req.TeacherID})
	}) {
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"class_id": classID, "teacher_id": req.TeacherID}})
}

// homeroomOf returns the homeroom teacher of a class, or "".
func homeroomOf(ctx context.Context, q reportQuerier, schoolID, classID string) (string, error) {
	var teacher string
	err := q.QueryRow(ctx, `SELECT COALESCE(homeroom_teacher_id::text,'') FROM classes WHERE id=$1 AND school_id=$2`, classID, schoolID).Scan(&teacher)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return teacher, err
}

// announceToHomeroom lets a homeroom teacher message their class's students and
// families in the app, by email and by Telegram. SMS costs money and stays with
// the office.
func (a *API) announceToHomeroom(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req announcementRequest
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	classID := r.PathValue("classID")
	homeroom, err := homeroomOf(r.Context(), a.db, id.SchoolID, classID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if homeroom != id.UserID {
		writeError(w, 403, "HOMEROOM_ONLY", "Only the homeroom teacher can message this class.", nil)
		return
	}
	if slices.Contains(req.Channels, "sms") {
		writeError(w, 403, "SMS_OFFICE_ONLY", "SMS messages are sent by the school office.", nil)
		return
	}
	req.Audience, req.ClassID = "class", classID
	req.Title = strings.TrimSpace(req.Title)
	a.sendAnnouncement(w, r, id, req)
}
