import { api, APIError } from '../../lib/api'

export type PackItem = { question_id: string; topic: string; prompt: string; options: { id: string; text: string }[]; correct_option: string; explanation: string }
export type OfflinePack = { id: string; user_id: string; paper: string; downloaded_at: number; items: PackItem[] }
export type OfflineAnswer = { answer: string | null; flagged: boolean; answered_at: string }
export type OfflineSitting = {
  client_id: string
  pack_id: string
  user_id: string
  paper: string
  mode: 'drill' | 'timed'
  question_ids: string[]
  started_at: string
  deadline?: number
  finished_at?: number
  answers: Record<string, OfflineAnswer>
}

const DB_NAME = 'furii-offline-practice'
const PACKS = 'packs'
const SITTINGS = 'sittings'
let database: Promise<IDBDatabase> | undefined

function openDatabase() {
  if (database) return database
  database = new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, 1)
    request.onupgradeneeded = () => {
      const db = request.result
      if (!db.objectStoreNames.contains(PACKS)) db.createObjectStore(PACKS, { keyPath: 'id' })
      if (!db.objectStoreNames.contains(SITTINGS)) db.createObjectStore(SITTINGS, { keyPath: 'client_id' })
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error ?? new Error('Offline practice is not available in this browser.'))
  })
  return database
}

async function write(store: string, work: (objects: IDBObjectStore) => void) {
  const db = await openDatabase()
  await new Promise<void>((resolve, reject) => {
    const tx = db.transaction(store, 'readwrite')
    work(tx.objectStore(store))
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error ?? new Error('This device could not save your practice.'))
    tx.onabort = () => reject(tx.error ?? new Error('This device could not save your practice.'))
  })
}

async function readAll<T>(store: string) {
  const db = await openDatabase()
  return new Promise<T[]>((resolve, reject) => {
    const request = db.transaction(store).objectStore(store).getAll()
    request.onsuccess = () => resolve(request.result as T[])
    request.onerror = () => reject(request.error ?? new Error('This device could not read your practice.'))
  })
}

export const savePack = (pack: OfflinePack) => write(PACKS, (objects) => objects.put(pack))
export const deletePack = (id: string) => write(PACKS, (objects) => objects.delete(id))
export const saveSitting = (sitting: OfflineSitting) => write(SITTINGS, (objects) => objects.put(sitting))
const deleteSitting = (clientID: string) => write(SITTINGS, (objects) => objects.delete(clientID))

export const listPacks = async (userID: string) => (await readAll<OfflinePack>(PACKS)).filter((pack) => pack.user_id === userID).sort((a, b) => b.downloaded_at - a.downloaded_at)
export const listSittings = async (userID: string) => (await readAll<OfflineSitting>(SITTINGS)).filter((sitting) => sitting.user_id === userID)

// clearOfflinePacks removes downloaded questions at sign-out. Finished practice
// stays so it can still upload, and it holds only IDs and the student's answers.
export async function clearOfflinePacks() {
  const packs = await readAll<OfflinePack>(PACKS)
  const sittings = await readAll<OfflineSitting>(SITTINGS)
  await write(PACKS, (objects) => objects.clear())
  await write(SITTINGS, (objects) => { for (const sitting of sittings) if (!sitting.finished_at) objects.delete(sitting.client_id) })
  return packs.length
}

let syncing: Promise<{ uploaded: number; refused: number }> | null = null

// syncOfflineSittings uploads finished practice and removes what the server has
// accepted. A connection failure leaves everything in place for next time.
export function syncOfflineSittings(userID: string) {
  syncing ??= (async () => {
    let uploaded = 0
    let refused = 0
    for (const sitting of await listSittings(userID)) {
      if (!sitting.finished_at) continue
      try {
        await api('/student/prep/offline-sittings', { method: 'POST', body: JSON.stringify({
          client_id: sitting.client_id, paper: sitting.paper, mode: sitting.mode, started_at: sitting.started_at, question_ids: sitting.question_ids,
          answers: Object.entries(sitting.answers).map(([question_id, value]) => ({ question_id, ...value })),
        }) })
        uploaded++
      } catch (e) {
        // The server will never accept this one, for example because a question
        // was changed to exam-only. Keeping it would block the queue forever.
        if (e instanceof APIError && e.status === 400) refused++
        else break
      }
      await deleteSitting(sitting.client_id)
      await deletePack(sitting.pack_id)
    }
    return { uploaded, refused }
  })().finally(() => { syncing = null })
  return syncing
}
