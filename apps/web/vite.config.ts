import { readFileSync } from 'node:fs'
import { defineConfig, Plugin } from 'vite'
import react from '@vitejs/plugin-react'

// serviceWorker writes sw.js at build time with the list of files to store, so
// the app opens with no connection. Development runs without a service worker.
function serviceWorker(): Plugin {
  return {
    name: 'furii-service-worker',
    apply: 'build',
    generateBundle(_, bundle) {
      const files = Object.keys(bundle).filter((name) => name.startsWith('assets/')).map((name) => `/${name}`)
      const template = readFileSync(new URL('./sw/sw.js', import.meta.url), 'utf8')
      this.emitFile({ type: 'asset', fileName: 'sw.js', source: template.replaceAll('__PRECACHE__', JSON.stringify(files)) })
    },
  }
}

export default defineConfig({
  plugins: [react(), serviceWorker()],
  server: { port: 5173, proxy: { '/api': 'http://localhost:8080' } },
})
