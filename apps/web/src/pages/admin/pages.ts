// The administrator workspace's pages, in sidebar order.
export const adminPages = [
  { id: 'overview', icon: '▦' },
  { id: 'students', icon: '♧' },
  { id: 'classes', icon: '▤' },
  { id: 'academic-passport', icon: '▧' },
  { id: 'curriculum', icon: '⌘' },
  { id: 'assessments', icon: '✎' },
  { id: 'exam-sessions', icon: '◉' },
  { id: 'exam-prep', icon: '★' },
  { id: 'exam-questions', icon: '☰' },
  { id: 'mock-days', icon: '◷' },
  { id: 'results', icon: '▥' },
  { id: 'finance', icon: '¤' },
  { id: 'lesson-plans', icon: '▣' },
  { id: 'staff', icon: '♙' },
  { id: 'demo-requests', icon: '✦' },
  { id: 'settings', icon: '⚙' },
] as const

export type AdminPageID = typeof adminPages[number]['id']
