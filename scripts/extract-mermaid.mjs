import { mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

const outputDirectory = process.argv[2]
if (!outputDirectory) throw new Error('usage: node scripts/extract-mermaid.mjs <output-directory>')

rmSync(outputDirectory, { force: true, recursive: true })
mkdirSync(outputDirectory, { recursive: true })

function markdownFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) return markdownFiles(path)
    return entry.name.endsWith('.md') ? [path] : []
  })
}

let index = 0
for (const path of markdownFiles('docs')) {
  const content = readFileSync(path, 'utf8')
  for (const match of content.matchAll(/```mermaid\r?\n([\s\S]*?)```/g)) {
    writeFileSync(join(outputDirectory, `${String(index++).padStart(3, '0')}.mmd`), match[1])
  }
}

if (index === 0) throw new Error('no Mermaid diagrams found in docs')
