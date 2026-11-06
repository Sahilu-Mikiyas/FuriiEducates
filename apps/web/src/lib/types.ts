// Shapes of the API's responses, matching api-spec/openapi.yaml.

export type Identity = { user_id: string; school_id: string; display_name: string; roles: string[]; platform_operator?: boolean; default_language?: string }
export type DemoRequest = { id: string; name: string; email: string; school_name: string; role: string; school_size: string; timezone: string; region: string; default_language: string; priorities: string[]; prepared_school_id: string; message: string; status: 'new' | 'contacted' | 'closed'; created_at: string; updated_at: string }
export type SchoolSetupProfile = { learner_count_band: string; region: string; priorities: string[] }
export type School = { id: string; name: string; timezone: string; default_language: string }
export type Year = { id: string; name: string; starts_on: string; ends_on: string }
export type ClassRoom = { id: string; name: string; grade_level: string; academic_year_id: string; academic_year_name?: string }
export type Student = { id: string; student_number: string; legal_name: string; preferred_language: string; status: string; has_account: boolean; stream?: string; class_id?: string; class_name?: string; grade_level?: string }
export type Term = { id: string; academic_year_id: string; academic_year_name: string; name: string; starts_on: string; ends_on: string; in_use: boolean }
export type GradeBand = { label: string; min_percent: number }
export type ReportSettings = { pass_percent: number; grade_bands: GradeBand[]; custom: boolean }
export type ClassSubject = { id: string; name: string; teacher_id: string; teacher_name: string; counts_in_average: boolean; can_edit: boolean; items: number; weight_total: number; missing_marks: number }
export type MarkSheetItem = { id: string; title: string; weight: number; max_score: number }
export type MarkSheetData = { subject_name: string; can_edit: boolean; published: boolean; weight_total: number; items: MarkSheetItem[]; students: { student_id: string; student_number: string; student_name: string; in_class: boolean; scores: Record<string, number> }[] }
export type ReportCardData = {
  student_id: string; student_number: string; student_name: string; school_name: string; class_name: string; grade_level: string; year_name: string; term_id: string; term_name: string; pass_percent: number
  subjects: { subject_id: string; name: string; total: number | null; grade: string; counts_in_average: boolean; earlier?: Record<string, number | null>; year_average?: number | null }[]
  total: number | null; average: number | null; grade: string; rank: number | null; students_ranked: number; complete: boolean
  attendance: { days_marked: number; present: number; absent: number; late: number; excused: number }
  conduct: string; comment: string
  earlier_terms: { term_id: string; name: string; average: number | null; rank: number | null }[]
  year_average?: number | null; year_rank?: number | null
}
export type ReportProblem = { kind: 'no_items' | 'weights' | 'missing_marks'; subject_id: string; subject: string; weight_total?: number; missing_marks?: number }
export type ClassReportData = { published: boolean; published_at: string; cards: ReportCardData[]; problems: ReportProblem[] }
export type QuizQuestion = { question_type: 'single_choice' | 'numeric' | 'text'; prompt: string; options: string[]; correct: number; value: number | null; tolerance: number; guide: string; topic: string; points: number; explanation: string }
export type QuizRow = { id: string; title: string; kind: 'quiz' | 'assignment'; status: string; subject: string; class_subject_id: string; duration_minutes: number; assignment_id: string; opens_at: string | null; due_at: string | null; question_count: number; total_points: number; students: number; submitted: number; waiting_review: number; average_percent: number | null; in_marks: boolean; can_edit: boolean }
export type QuizData = { id: string; class_subject_id: string; subject: string; title: string; kind: 'quiz' | 'assignment'; status: string; instructions: string; duration_minutes: number; resource_id: string; show_answers: boolean; shuffle: boolean; attempts: number; can_edit: boolean; questions: QuizQuestion[] }
export type QuizResults = {
  title: string; kind: 'quiz' | 'assignment'; assignment_id: string; can_edit: boolean
  students: { student_id: string; student_number: string; student_name: string; status: string; submitted_at: string | null; results_released: boolean; final_score: number | null; final_max_score: number | null; auto_score: number | null; auto_max_score: number | null }[]
  questions: { position: number; prompt: string; question_type: string; topic: string; points: number; answers_marked: number; percent: number | null }[]
  topics: { topic: string; percent: number }[]
}
export type LearningTopic = { skill_id: string; title: string; subject: string; paper: string; percent: number; responses: number; can_practice: boolean }
export type LearningSubject = { name: string; total: number; change: number | null; below_pass: boolean; topics: LearningTopic[] }
export type LearningAction = { type: 'assignment' | 'resume' | 'practice' | 'review' | 'resource'; title?: string; subject?: string; paper?: string; skill_id?: string; assignment_id?: string; resource_id?: string; session_id?: string; due_at?: string; count?: number }
export type LearningPlanData = {
  report_term: string; going_well: LearningSubject[]; room_to_improve: LearningSubject[]; strong_topics: LearningTopic[]; weak_topics: LearningTopic[]
  attendance: { days_marked: number; days_absent: number; current_streak: number; reason: string } | null
  next: LearningAction[]; has_evidence: boolean; min_responses: number
}
export type ChildStudent = { id: string; student_number: string; legal_name: string; status: string; stream: string; class_name: string; grade_level: string }
export type ChildSummary = ChildStudent & { attendance: AttendanceSummaryData; latest_term: string; latest_average: number | null; latest_rank: number | null; students_ranked: number }
export type ChildData = {
  student: ChildStudent
  attendance: AttendanceSummaryData
  report_cards: ReportCardData[]
  fees: {
    balances: { currency_code: string; charged_minor: number; paid_minor: number; due_minor: number }[]
    entries: { entry_type: string; category: string; description: string; amount_minor: number; currency_code: string; due_on: string | null; paid_on: string | null; recorded_on: string }[]
  }
  readiness: (ReadinessEstimate & { weak_topics: TopicStat[] }) | null
  learning_plan: LearningPlanData
}
export type Guardian = { id: string; full_name: string; relationship: string; phone: string; email: string; is_primary: boolean; has_account: boolean; invite_pending: boolean }
export type AttendanceSummaryData = { days_marked: number; present: number; absent: number; late: number; excused: number; recent: { date: string; status: string; note: string }[] }
export type StudentProfileData = {
  student: { id: string; student_number: string; legal_name: string; date_of_birth: string; gender: string; preferred_language: string; address: string; medical_notes: string; stream: string; status: string; has_account: boolean; class_id: string; class_name: string; grade_level: string }
  guardians: Guardian[]
  attendance: AttendanceSummaryData
}
export type RegisterData = { date: string; taken: boolean; students: { student_id: string; student_number: string; student_name: string; status: string; note: string }[] }
export type AttendanceOverview = {
  date: string
  classes: { class_id: string; name: string; grade_level: string; students: number; marked: number; present: number; absent: number; late: number; excused: number }[]
  absentees: { student_id: string; student_number: string; student_name: string; grade_level: string; class_name: string; note: string; guardian_name: string; guardian_relationship: string; guardian_phone: string }[]
}
export type AttendanceAlert = { student_id: string; student_number: string; student_name: string; grade_level: string; class_name: string; guardian_name: string; guardian_phone: string; days_marked: number; days_absent: number; current_streak: number; reason: 'streak' | 'rate' }
export type AttendanceReportData = { days_taken: number; students: { student_id: string; student_number: string; student_name: string; present: number; absent: number; late: number; excused: number }[] }
export type AcademicRecord = { id: string; school_name: string; academic_year_label: string; grade_level: string; subject_name: string; score: number | null; maximum_score: number | null; grade_label: string; grading_scale_name: string; source_type: string; verification_status: string }
export type ExternalExam = { id: string; exam_name: string; exam_provider: string; exam_date: string; subject_or_section: string; score: number | null; maximum_score: number | null; score_label: string; verification_status: string; breakdowns: { section_name: string; score: number | null; maximum_score: number | null }[] }
export type ImportPreview = { id: string; status: string; row_count: number; valid_rows: number; error_rows: number; can_commit: boolean; row_errors: { row: number; errors: string[] }[] }
export type Curriculum = { id: string; name: string; provider: string; country_code: string; version_label: string; language_code: string; source_reference: string; license_notes: string }
export type Subject = { id: string; name: string; grade_level: string; subject_code: string }
export type CurriculumNode = { id: string; parent_id: string; node_type: string; code: string; title: string; description: string; sort_order: number }
export type Question = { id: string; question_type: string; prompt: { text?: string }; options: { id: string; text: string }[] | null; explanation: { text?: string } | null; difficulty: number | null; language_code: string; source_name: string; source_reference: string; license_notes: string; status: string; skill_tags: { skill_id: string; title: string; weight: number }[] }
export type StaffMember = { id: string; email: string; display_name: string; status: string; class_ids: string[] }
export type StaffInvitation = { id: string; email: string; role: string; created_at: string; expires_at: string }
export type TeacherStudent = { id: string; student_number: string; legal_name: string; preferred_language: string; status: string }
export type Assessment = { id: string; title: string; instructions: string; status: string; created_at: string; duration_minutes: number; shuffle_questions: boolean; shuffle_options: boolean; locked_mode: boolean; question_count: number; question_ids: string[]; assigned_class_ids: string[] }
export type LearningSummary = { assigned_assessments: number; submitted_attempts: number; average_score_percent: number; active_recommendations: number }

export type LearnerAssignment = { assignment_id: string; title: string; instructions: string; class_name: string; grade_level: string; assigned_at: string; due_at: string; attempt_status: string; attempt_id: string }
export type LearnerQuestion = { id: string; question_type: string; prompt: { text?: string }; options?: { id: string; text: string }[]; points: string; answer?: unknown; revision: number; flagged: boolean; correct_answer?: unknown; is_correct?: boolean; explanation?: string; awarded_points?: string; feedback?: string }
export type LearnerAssessment = { kind?: string; resource_id?: string; resource_title?: string; answers_after_due?: boolean; assignment_id: string; title: string; instructions: string; status: string; attempt_id: string; deadline_at: string | null; score: number | null; max_score: number | null; results_released: boolean; final_score: number | null; final_max_score: number | null; manual_review_count: number; question_count: number; duration_minutes: number; requires_start_code: boolean; session_open: boolean; locked_mode: boolean; national_paper?: string | null; opens_at: string | null; due_at: string | null; not_open_yet: boolean; past_due: boolean; questions: LearnerQuestion[] }
export type StudentLearningProfile = {
  algorithm_version: string
  skills: { skill_id: string; title: string; estimated_mastery: number; confidence: number; evidence_count: number; confidence_band: string }[]
  recommendations: { id: string; skill_id: string; skill_title: string; reason_code: string; evidence_summary: { estimated_mastery?: number; evidence_count?: number; confidence?: number }; recommended_action: { title?: string; description?: string; target_mastery?: number }; priority: string; status: string; pre_mastery: number | null; post_mastery: number | null; change_points: number | null }[]
}

export type TeacherReview = { attempt_id: string; question_id: string; assignment_id?: string; assessment_title: string; student_id?: string; student_number: string; student_name: string; prompt: { text?: string }; grading_guide?: unknown; answer: unknown; max_points: number; feedback: string; reviewed_at?: string }
export type TeacherLearningRow = { student_id: string; student_number: string; student_name: string; skill_id: string; skill_title: string; estimated_mastery: number; confidence: number; evidence_count: number; confidence_band: string; active_recommendations: number }

export type FinanceEntry = { id: string; entry_type: string; category: string; description: string; amount_minor: number; currency_code: string; student_name: string; staff_name: string; due_on?: string; paid_on?: string; status: string; reference: string; created_at: string }
export type FinanceTotals = { tuition_billed_minor: number; payments_recorded_minor: number; expenses_recorded_minor: number; salaries_recorded_minor: number; outstanding_charges_minor: number }
export type FinanceSummary = FinanceTotals & { by_currency: (FinanceTotals & { currency_code: string })[] }
export type LessonPlan = { id: string; teacher_name?: string; class_name?: string; grade_level?: string; class_id: string; subject: string; lesson_date: string; duration_minutes: number; topic: string; objectives: string; prior_knowledge: string; materials: string; lesson_phases: { phase: string; minutes: number; activities: string }[]; differentiation: string; assessment: string; homework: string; reflection: string; status: string }
export type Resource = { id: string; class_id: string; title: string; description: string; subject: string; file_name: string; media_type: string; file_size: number; created_at: string; can_delete?: boolean }

export type SessionSummary = { assignment_id: string; title: string; class_name: string; grade_level: string; duration_minutes: number; locked_mode: boolean; requires_start_code: boolean; session_open: boolean; opens_at: string | null; due_at: string | null; enrolled: number; in_progress: number; submitted: number }
export type RosterRow = { student_id: string; student_number: string; student_name: string; attempt_id: string; status: string; deadline_at: string | null; submitted_at: string | null; answered: number; last_saved_at: string | null; integrity_flags: number; incidents: number }
export type SessionDetail = { assignment_id: string; title: string; duration_minutes: number; question_count: number; locked_mode: boolean; requires_start_code: boolean; session_open: boolean; start_code: string | null; roster: RosterRow[] }
export type TimelineEvent = { event_type: string; event_data: Record<string, unknown>; occurred_at: string }
export type GradebookCell = { status: string; score: number | null; max_score: number | null; final_score: number | null; final_max_score: number | null; results_released: boolean; pending_review: number }
export type GradebookData = { assignments: { assignment_id: string; title: string; max_points: number; due_at: string | null }[]; students: { student_id: string; student_number: string; student_name: string; results: Record<string, GradebookCell> }[] }

export type PrepMode = 'drill' | 'weak' | 'review' | 'timed'
export type PrepBankRow = { paper: string; practice: number; exam_only: number; drafts: number }
export type PrepQuestion = { id: string; paper: string; grade: string; topic: string; prompt: string; options: { id: string; text: string }[]; correct?: string; explanation?: string; difficulty: number; status: string; exam_only: boolean; source_name: string; license_notes: string; author: string; created_at: string }
export type PaperEstimate = { paper: string; responses: number; enough_evidence: boolean; score: number | null; low: number | null; high: number | null }
export type ReadinessTotal = { predicted: number; low: number; high: number; pass_line: number; distance_to_pass_line: number; status: 'above_line' | 'near_line' | 'at_risk' }
export type TopicStat = { paper: string; skill_id: string; title: string; responses: number; percent: number; students: number }
export type ReadinessEstimate = { stream: string; status: string; papers: PaperEstimate[]; total: ReadinessTotal | null; min_responses: number }
export type PrepMock = { id: string; stream: string; status: string; started_at: string; total?: number; papers: { paper: string; status: string; session_id?: string; score?: number }[] }
export type PrepHub = { stream: string | null; onboarded: boolean; papers: { paper: string; available: number; due_review: number }[]; readiness: ReadinessEstimate & { weak_topics: TopicStat[] }; active_session: { id?: string; mode?: string; paper?: string }; mocks: PrepMock[]; school_mocks: StudentMockRound[] }
export type PrepSessionItem = { question_id: string; position: number; topic: string; prompt: string; options: { id: string; text: string }[]; answer: string | null; flagged: boolean; is_correct?: boolean; correct_option?: string; explanation?: string }
export type PrepSessionView = { id: string; mode: PrepMode; paper: string; status: string; mock_id: string | null; seconds_left: number | null; question_count: number; answered_count: number; correct_count?: number; score_out_of_100?: number; items: PrepSessionItem[] }
export type ClassReadinessData = { students: (ReadinessEstimate & { student_id: string; student_number: string; student_name: string; onboarded: boolean })[]; topics: TopicStat[]; at_risk: number; pass_line: number }
export type ReadinessGroup = { key: string; grade_level?: string; name: string; students: number; estimated: number; average_predicted: number | null; above_line: number; near_line: number; at_risk: number }
export type SchoolReadinessData = { streams: ReadinessGroup[]; classes: ReadinessGroup[]; pass_line: number }

export type MockPaper = { paper: string; opens_at: string; due_at: string; duration_minutes: number; question_count: number; stream: string; in_progress: number; submitted: number }
export type MockGroup = { key: string; name: string; grade_level?: string; students: number; complete: number; average_total: number | null; below_line: number }
export type MockSummary = { all: MockGroup; streams: MockGroup[]; classes: MockGroup[]; papers: { paper: string; sat: number; average: number | null }[] }
export type MockRound = { id: string; name: string; created_at: string; papers: MockPaper[]; summary: MockSummary }
export type MockStudent = { student_id: string; student_number: string; student_name: string; stream: string; class_id: string; class_name: string; grade_level: string; scores: Record<string, number>; papers_sat: number; papers_expected: number; total: number | null }
export type MockResults = { students: MockStudent[]; summary: MockSummary; pass_line: number }
export type StudentMockRound = { id: string; name: string; total?: number; papers: { paper: string; assignment_id: string; opens_at: string | null; due_at: string | null; duration_minutes: number; status: string; score?: number }[] }

export type ClassOption = { id: string; name: string; grade_level: string }
export const classLabel = (classroom: Pick<ClassOption, 'grade_level' | 'name'>) => `${classroom.grade_level} · ${classroom.name}`

export type NoticeTopic = 'attendance' | 'reports' | 'schoolwork' | 'announcements' | 'account'
export type NoticeChannel = 'email' | 'sms' | 'telegram'
export type NotificationItem = { id: string; topic: NoticeTopic; kind: string; title: string; body: string; link: string; created_at: string; read: boolean }
export type NotificationInbox = { items: NotificationItem[]; unread: number }
export type ChannelState = { channel: NoticeChannel; ready: boolean; provider: string }
export type NotificationSettings = {
  language: 'en' | 'am'; email_enabled: boolean; sms_enabled: boolean; telegram_enabled: boolean; muted_topics: NoticeTopic[]
  email: string; phone: string; telegram_connected: boolean; channels: ChannelState[]
}
export type NotificationOverview = {
  channels: ChannelState[]; links_ready: boolean; rules: Record<NoticeChannel, NoticeTopic[]>
  last_30_days: { channel: NoticeChannel; status: string; count: number }[]; sms_this_month: number
}
export type Delivery = {
  id: string; channel: NoticeChannel; address: string; topic: NoticeTopic; kind: string; subject: string; body: string
  status: 'pending' | 'sending' | 'sent' | 'failed' | 'cancelled' | 'skipped'; attempts: number; error: string; created_at: string; sent_at?: string; name: string
}
export type Announcement = {
  id: string; title: string; body: string; audience: string; class_name: string; channels: NoticeChannel[]; recipients: number
  created_at: string; sent_by: string; deliveries: Record<string, number>
}
export type AnnouncementResult = { id: string; recipients: number; channels: Partial<Record<NoticeChannel, Record<string, number>>>; dry_run: boolean }
