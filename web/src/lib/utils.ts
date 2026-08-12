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
  if (code >= 200 && code < 300) return 'text-success'
  if (code >= 300 && code < 400) return 'text-warning'
  if (code >= 400 && code < 500) return 'text-warning'
  if (code >= 500) return 'text-danger'
  return 'text-muted-foreground'
}

// 方法颜色 - HTTP 方法不携带成败语义,统一中性,不再按方法分色
export const METHOD_CLASS = 'text-muted-foreground'

/**
 * 焦点环。给裸 <input>/<textarea> 用 —— 走 Input/Textarea 组件的地方已经自带了,
 * 这里是为了让手写的那些跟组件保持同一套,别再各写一份淡的。
 *
 * 焦点指示是可访问性要求(WCAG 2.4.7 / 2.4.13),不是装饰,所以品牌色用在这里
 * 是它该出现的地方。用 focus-visible 而不是 focus:鼠标点击不该画环,
 * 但浏览器对文本输入框仍会判定为 focus-visible,键盘用户不受影响。
 */
export const FOCUS_RING = 'outline-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]'

/** 校验失败时的焦点环,配合 border-danger 使用 */
export const FOCUS_RING_DANGER = 'outline-none focus-visible:border-danger focus-visible:ring-danger/50 focus-visible:ring-[3px]'

// JSON 语法高亮
export function syntaxHighlightJson(json: string): string {
  if (!json) return ''

  // 预处理：防止 HTML 注入（简单的转义）
  // 为了性能，如果字符串太长（>50KB），则不进行高亮，仅转义
  if (json.length > 50000) {
    return json.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  }

  return json.replace(/("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+-]?\d+)?)/g, (match) => {
    let cls = 'text-warning' // number
    let isKey = false

    if (/^"/.test(match)) {
      if (/:$/.test(match)) {
        cls = 'text-info font-semibold' // key
        isKey = true
      } else {
        cls = 'text-success' // string
      }
    } else if (/true|false/.test(match)) {
      cls = 'text-primary font-semibold' // boolean
    } else if (/null/.test(match)) {
      cls = 'text-danger font-semibold' // null
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
