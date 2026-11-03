// A value starting with one of these runs as a formula when a spreadsheet opens the file.
const csvCell = (value: string | number) => {
  const text = String(value)
  return `"${(/^[=+\-@\t\r]/.test(text) ? `'${text}` : text).replace(/"/g, '""')}"`
}

// downloadCSV saves rows as a file. The byte order mark lets Excel read Amharic.
export function downloadCSV(name: string, rows: (string | number)[][]) {
  const link = document.createElement('a')
  link.href = URL.createObjectURL(new Blob([`﻿${rows.map((row) => row.map(csvCell).join(',')).join('\r\n')}`], { type: 'text/csv;charset=utf-8' }))
  link.download = name
  link.click()
  URL.revokeObjectURL(link.href)
}
