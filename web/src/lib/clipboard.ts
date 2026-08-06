// 复制文本到剪贴板。
// navigator.clipboard 仅在安全上下文（HTTPS / localhost）可用，
// 面板常通过局域网 HTTP IP 访问，此时降级到 execCommand('copy')。
export async function copyText(text: string): Promise<boolean> {
    if (navigator.clipboard?.writeText) {
        try {
            await navigator.clipboard.writeText(text)
            return true
        } catch {
            // 非安全上下文、权限被拒或文档未聚焦，继续走降级
        }
    }
    return copyByExecCommand(text)
}

function copyByExecCommand(text: string): boolean {
    if (typeof document === 'undefined') return false

    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.setAttribute('readonly', '')
    // 置于视口外并避免滚动/缩放跳动
    textarea.style.position = 'fixed'
    textarea.style.top = '0'
    textarea.style.left = '-9999px'
    document.body.appendChild(textarea)

    const selection = document.getSelection()
    const previousRange = selection && selection.rangeCount > 0 ? selection.getRangeAt(0) : null

    try {
        textarea.select()
        textarea.setSelectionRange(0, text.length)
        return document.execCommand('copy')
    } catch {
        return false
    } finally {
        textarea.remove()
        if (selection && previousRange) {
            selection.removeAllRanges()
            selection.addRange(previousRange)
        }
    }
}
