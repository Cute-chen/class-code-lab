import { copyFile, mkdir } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const source = resolve(frontendRoot, 'node_modules/html2canvas/dist/html2canvas.min.js')
const target = resolve(frontendRoot, 'dist/runner-assets/html2canvas.min.js')

await mkdir(dirname(target), { recursive: true })
await copyFile(source, target)
