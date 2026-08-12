// 生成中文 UI 子集字体。
//
// 为什么需要这一步:Inter 没有中文字形,Windows 上中文会落到微软雅黑,
// 与拉丁字母的字重/字面对不上,是中文界面"显旧"的主要来源。
// 直接打包完整 Noto Sans SC 会让内嵌前端的二进制涨 7-10MB,不划算。
//
// 做法分两层:
//   1. fontsource 已按 unicode-range 把字体切成上百个分片,先只挑出
//      命中本项目字符集的分片;
//   2. 对挑出的每个分片再做一次子集化,只留实际用到的字形。
//
// 字符集来源:locales/*.json 的全部文案 + 源码里内联的中文兜底文案。
// 日志正文里的任意中文不在其中,会回落到系统字体 —— 那部分本来就走等宽字体。
//
// 用法:npm run font:subset(改动文案后重跑)

import { readFileSync, writeFileSync, mkdirSync, readdirSync, rmSync, existsSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import subsetFont from 'subset-font'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const fontPkg = join(root, 'node_modules/@fontsource/noto-sans-sc')
const outDir = join(root, 'src/assets/fonts')
const WEIGHTS = [400, 500]

function collectChars() {
  const chars = new Set()
  const add = (text) => {
    for (const ch of text) chars.add(ch)
  }

  const localeDir = join(root, 'src/locales')
  for (const file of readdirSync(localeDir)) {
    if (!file.endsWith('.json')) continue
    const walk = (node) => {
      if (typeof node === 'string') add(node)
      else if (node && typeof node === 'object') Object.values(node).forEach(walk)
    }
    walk(JSON.parse(readFileSync(join(localeDir, file), 'utf8')))
  }

  // 源码里内联的中文兜底文案,例如 t('common.success', '成功')
  const walkSrc = (dir) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const full = join(dir, entry.name)
      if (entry.isDirectory()) walkSrc(full)
      else if (/\.(tsx?|css)$/.test(entry.name)) {
        const matches = readFileSync(full, 'utf8').match(/[　-〿一-鿿＀-￯]/g)
        if (matches) matches.forEach(add)
      }
    }
  }
  walkSrc(join(root, 'src'))

  // 基础拉丁与常用符号,保证标点不会中西混排断裂
  add(' !"#$%&\'()*+,-./0123456789:;<=>?@[\\]^_`{|}~')
  for (let code = 0x41; code <= 0x7a; code++) add(String.fromCodePoint(code))
  return chars
}

function parseUnicodeRange(spec) {
  const ranges = []
  for (const part of spec.split(',')) {
    const token = part.trim().replace(/^U\+/i, '')
    if (token.includes('-')) {
      const [from, to] = token.split('-')
      ranges.push([parseInt(from, 16), parseInt(to, 16)])
    } else if (token.includes('?')) {
      ranges.push([parseInt(token.replace(/\?/g, '0'), 16), parseInt(token.replace(/\?/g, 'f'), 16)])
    } else {
      const code = parseInt(token, 16)
      ranges.push([code, code])
    }
  }
  return ranges
}

function parseFontFaces(css) {
  const faces = []
  for (const block of css.split('@font-face').slice(1)) {
    const file = block.match(/url\(\.\/files\/([^)]+\.woff2)\)/)?.[1]
    const range = block.match(/unicode-range:\s*([^;]+);/)?.[1]
    if (file && range) faces.push({ file, ranges: parseUnicodeRange(range) })
  }
  return faces
}

const chars = collectChars()
const codepoints = new Set([...chars].map(ch => ch.codePointAt(0)))

if (existsSync(outDir)) rmSync(outDir, { recursive: true })
mkdirSync(outDir, { recursive: true })

let css = '/* 由 scripts/build-font.mjs 生成,不要手改。改文案后跑 npm run font:subset */\n'
let totalBytes = 0
let keptFaces = 0

for (const weight of WEIGHTS) {
  const faces = parseFontFaces(readFileSync(join(fontPkg, `${weight}.css`), 'utf8'))

  for (const face of faces) {
    const hits = [...codepoints].filter(code => face.ranges.some(([from, to]) => code >= from && code <= to))
    if (hits.length === 0) continue

    const text = hits.map(code => String.fromCodePoint(code)).join('')
    const source = readFileSync(join(fontPkg, 'files', face.file))
    const subset = await subsetFont(source, text, { targetFormat: 'woff2' })

    const outName = face.file
    writeFileSync(join(outDir, outName), subset)
    totalBytes += subset.length
    keptFaces++

    const range = hits.map(code => `U+${code.toString(16)}`).join(',')
    css += `@font-face{font-family:'Noto Sans SC';font-style:normal;font-display:swap;font-weight:${weight};`
      + `src:url(./${outName}) format('woff2');unicode-range:${range};}\n`
  }
}

writeFileSync(join(outDir, 'noto-sans-sc.css'), css)

console.log(`字符集 ${codepoints.size} 个码点`)
console.log(`保留分片 ${keptFaces} / ${WEIGHTS.length * 101}`)
console.log(`产物 ${(totalBytes / 1024).toFixed(1)} KB`)
