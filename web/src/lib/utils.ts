import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function generateId() {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) {
    return crypto.randomUUID()
  }
  return Math.random().toString(36).substring(2, 11)
}

// 日期格式化
export function formatDate(date: Date | string, locale: string = 'zh-CN'): string {
  const d = new Date(date)
  return d.toLocaleString(locale === 'zh' ? 'zh-CN' : 'en-US', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

// 耗时格式化
export function formatLatency(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(2)}s`
  return `${(ms / 60000).toFixed(2)}m`
}

// 文件大小格式化
export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`
}

// 状态码颜色
export function getStatusColor(code: number): string {
  if (code >= 200 && code < 300) return 'text-green-400'
  if (code >= 300 && code < 400) return 'text-yellow-400'
  if (code >= 400 && code < 500) return 'text-orange-400'
  if (code >= 500) return 'text-red-400'
  return 'text-gray-400'
}

// 方法颜色
export function getMethodColor(method: string): string {
  const colors: Record<string, string> = {
    GET: 'bg-blue-500/15 text-blue-500 border-blue-500/25',
    POST: 'bg-emerald-500/15 text-emerald-500 border-emerald-500/25',
    PUT: 'bg-amber-500/15 text-amber-500 border-amber-500/25',
    DELETE: 'bg-rose-500/15 text-rose-500 border-rose-500/25',
    PATCH: 'bg-violet-500/15 text-violet-500 border-violet-500/25',
  }
  return colors[method.toUpperCase()] || 'bg-slate-500/15 text-slate-500 border-slate-500/25'
}

// JSON 语法高亮
export function syntaxHighlightJson(json: string): string {
  if (!json) return ''

  // 预处理：防止 HTML 注入（简单的转义）
  // 为了性能，如果字符串太长（>50KB），则不进行高亮，仅转义
  if (json.length > 50000) {
    return json.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  }

  return json.replace(/("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+\-]?\d+)?)/g, (match) => {
    let cls = 'text-orange-600 dark:text-orange-400' // number
    let isKey = false

    if (/^"/.test(match)) {
      if (/:$/.test(match)) {
        cls = 'text-sky-600 dark:text-sky-400 font-semibold' // key
        isKey = true
      } else {
        cls = 'text-emerald-600 dark:text-emerald-400' // string
      }
    } else if (/true|false/.test(match)) {
      cls = 'text-indigo-600 dark:text-indigo-400 font-semibold' // boolean
    } else if (/null/.test(match)) {
      cls = 'text-rose-600 dark:text-rose-400 font-semibold' // null
    }

    // 辅助函数：转义 HTML 字符
    const escapeHtml = (str: string) => str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')

    if (isKey) {
      const colonIndex = match.lastIndexOf(':')
      const content = match.substring(0, colonIndex)
      const colon = match.substring(colonIndex)
      return `<span class="${cls}">${escapeHtml(content)}</span>${colon}`
    }

    return `<span class="${cls}">${escapeHtml(match)}</span>`
  })
}
