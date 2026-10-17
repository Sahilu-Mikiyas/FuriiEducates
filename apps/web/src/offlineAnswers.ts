export type PendingAnswer = {
  attempt_id: string
  question_id: string
  answer: string | string[] | null
  revision: number
  idempotency_key: string
  flagged: boolean
  saved_at: number
}

const DB_NAME = 'furii-exam-recovery'
const STORE = 'pending-answers'
let database: Promise<IDBDatabase> | undefined

function openDatabase() {
  if (database) return database
  database = new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, 1)
    request.onupgradeneeded = () => {
      const db = request.result
      if (!db.objectStoreNames.contains(STORE)) db.createObjectStore(STORE, { keyPath: ['attempt_id', 'question_id'] })
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error ?? new Error('Unable to open local answer recovery.'))
  })
  return database
}

export async function savePendingAnswer(answer: PendingAnswer) {
  const db = await openDatabase()
  await new Promise<void>((resolve, reject) => {
    const tx = db.transaction(STORE, 'readwrite')
    tx.objectStore(STORE).put(answer)
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error ?? new Error('Unable to save this answer on this device.'))
    tx.onabort = () => reject(tx.error ?? new Error('Unable to save this answer on this device.'))
  })
}

export async function listPendingAnswers(attemptID: string) {
  const db = await openDatabase()
  return new Promise<PendingAnswer[]>((resolve, reject) => {
    const request = db.transaction(STORE).objectStore(STORE).getAll()
    request.onsuccess = () => resolve((request.result as PendingAnswer[]).filter(item => item.attempt_id === attemptID))
    request.onerror = () => reject(request.error ?? new Error('Unable to read saved answers from this device.'))
  })
}

export async function removePendingAnswer(attemptID: string, questionID: string, requestID: string) {
  const db = await openDatabase()
  await new Promise<void>((resolve, reject) => {
    const tx = db.transaction(STORE, 'readwrite')
    const store = tx.objectStore(STORE)
    const request = store.get([attemptID, questionID])
    request.onsuccess = () => {
      const item = request.result as PendingAnswer | undefined
      if (item?.idempotency_key === requestID) store.delete([attemptID, questionID])
    }
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error ?? new Error('Unable to clear a synchronized answer.'))
  })
}

export async function clearPendingAnswers(attemptID: string) {
  const db = await openDatabase()
  const items = await listPendingAnswers(attemptID)
  await new Promise<void>((resolve, reject) => {
    const tx = db.transaction(STORE, 'readwrite')
    const store = tx.objectStore(STORE)
    for (const item of items) store.delete([item.attempt_id, item.question_id])
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error ?? new Error('Unable to clear local exam recovery data.'))
  })
}
