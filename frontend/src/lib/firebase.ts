import { initializeApp, type FirebaseApp, type FirebaseOptions } from 'firebase/app'
import { getAuth, type Auth } from 'firebase/auth'
import { getFirestore, type Firestore } from 'firebase/firestore'

export function parseFirebaseConfig(raw: string | undefined): FirebaseOptions | null {
  const trimmed = raw?.trim()
  if (!trimmed) {
    return null
  }
  try {
    const parsed = JSON.parse(trimmed) as FirebaseOptions
    if (!parsed.apiKey || !parsed.projectId) {
      return null
    }
    return parsed
  } catch {
    return null
  }
}

const firebaseConfig = parseFirebaseConfig(import.meta.env.VITE_FIREBASE_CONFIG)

export function isFirebaseConfigured(): boolean {
  return firebaseConfig !== null
}

let app: FirebaseApp | null = null
let auth: Auth | null = null
let db: Firestore | null = null

export function getFirebaseApp(): FirebaseApp {
  if (!firebaseConfig) {
    throw new Error('Firebase is not configured')
  }
  if (!app) {
    app = initializeApp(firebaseConfig)
  }
  return app
}

export function getFirebaseAuth(): Auth {
  if (!auth) {
    auth = getAuth(getFirebaseApp())
  }
  return auth
}

export function getFirebaseDB(): Firestore {
  if (!db) {
    db = getFirestore(getFirebaseApp())
  }
  return db
}
