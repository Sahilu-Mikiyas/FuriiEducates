import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'
import type { AcademicRecord, Assessment, ClassRoom, Curriculum, CurriculumNode, DemoRequest, ExternalExam, LearnerAssignment, LearningSummary, Question, School, SchoolSetupProfile, StaffInvitation, StaffMember, Student, Subject, TeacherLearningRow, TeacherStudent, Year } from './types'

// Each hook is one server list or record, cached under its API path so pages
// share data and a change on one page refreshes the others.
function useResource<T>(path: string, enabled = true) {
  return useQuery<T>({ queryKey: [path], queryFn: () => api<T>(path), enabled })
}

export const useSchool = () => useResource<School>('/school')
export const useSetupProfile = () => useResource<SchoolSetupProfile | null>('/school/setup-profile')
export const useYears = () => useResource<Year[]>('/academic-years')
export const useClasses = () => useResource<ClassRoom[]>('/classes')
export const useStudents = () => useResource<Student[]>('/students')
export const useLearningSummary = () => useResource<LearningSummary>('/school/learning-summary')
export const useCurricula = () => useResource<Curriculum[]>('/curricula')
export const useSubjects = (curriculumID: string) => useResource<Subject[]>(`/curricula/${curriculumID}/subjects`, Boolean(curriculumID))
export const useNodes = (subjectID: string) => useResource<CurriculumNode[]>(`/subjects/${subjectID}/nodes`, Boolean(subjectID))
export const useQuestions = () => useResource<Question[]>('/questions')
export const useAssessments = () => useResource<Assessment[]>('/assessments')
export const useStaff = () => useResource<StaffMember[]>('/staff')
export const useStaffInvitations = () => useResource<StaffInvitation[]>('/staff/invitations')
export const useDemoRequests = () => useResource<DemoRequest[]>('/platform/demo-requests')
export const useAcademicRecords = (studentID: string) => useResource<AcademicRecord[]>(`/students/${studentID}/academic-records`, Boolean(studentID))
export const useExternalExams = (studentID: string) => useResource<ExternalExam[]>(`/students/${studentID}/external-exams`, Boolean(studentID))
export const useTeacherClasses = () => useResource<ClassRoom[]>('/teacher/classes')
export const useTeacherRoster = (classID: string) => useResource<TeacherStudent[]>(`/teacher/classes/${classID}/students`, Boolean(classID))
export const useTeacherLearning = (classID: string) => useResource<TeacherLearningRow[]>(`/teacher/classes/${classID}/learning-profile`, Boolean(classID))
export const useLearnerAssignments = () => useResource<LearnerAssignment[]>('/student/assessments')

// useRefresh refetches the given API paths after a change.
export function useRefresh() {
  const client = useQueryClient()
  return (...paths: string[]) => Promise.all(paths.map((path) => client.invalidateQueries({ queryKey: [path] })))
}
