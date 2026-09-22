export type LinkFormat = 'url' | 'markdown'

const linkFormatKey = 'mio-link-format'

export function applySiteBranding(siteName: string, faviconURL: string) {
  if (typeof document === 'undefined') return
  if (siteName) document.title = siteName
  const href = faviconURL || '/branding/favicon'
  let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
  if (!link) {
    link = document.createElement('link')
    link.rel = 'icon'
    document.head.appendChild(link)
  }
  link.href = href
}

export function readLinkFormat(): LinkFormat {
  try {
    return localStorage.getItem(linkFormatKey) === 'markdown' ? 'markdown' : 'url'
  } catch {
    return 'url'
  }
}

export function writeLinkFormat(format: LinkFormat) {
  try {
    localStorage.setItem(linkFormatKey, format)
  } catch { /* Storage may be unavailable. */ }
}

export function formatSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1048576) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1073741824) return `${(bytes / 1048576).toFixed(1)} MB`
  return `${(bytes / 1073741824).toFixed(2)} GB`
}

export function fullURL(url: string, origin = globalThis.location?.origin ?? 'http://127.0.0.1') {
  return new URL(url, origin).href
}

export function markdown(name: string, url: string, origin?: string) {
  return '![' + name.replace(/[\[\]\\]/g, char => '\\' + char) + '](' + fullURL(url, origin) + ')'
}

export function shareText(name: string, url: string, format: LinkFormat, origin?: string) {
  return format === 'markdown' ? markdown(name, url, origin) : fullURL(url, origin)
}

export function normalizeFolderID(id: number | null | undefined): number | null {
  if (id == null || id === 0) return null
  return id
}

export function folderSelectValue(id: number | null | undefined): number {
  return normalizeFolderID(id) ?? 0
}

export function folderTitle(id: number | null, folders: { id: number; name: string }[]) {
  if (id === null) return '全部图片'
  if (id === 0) return '未分类'
  return folders.find(folder => folder.id === id)?.name || '文件夹'
}

export function validImageName(name: string) {
  const value = name.trim()
  const chars = [...value]
  if (chars.length < 1 || chars.length > 180) return false
  if (/[/\\]/.test(value)) return false
  return chars.every(char => {
    const code = char.codePointAt(0) ?? 0
    // Match Go unicode.IsControl and its rejection of U+FFFD. Lone UTF-16
    // surrogates become U+FFFD when decoded by the Go JSON parser.
    return code >= 0x20 && !(code >= 0x7f && code <= 0x9f)
      && code !== 0xfffd && !(code >= 0xd800 && code <= 0xdfff)
  })
}

const uploadExt: Record<string, string> = {
  'image/jpeg': '.jpg',
  'image/png': '.png',
  'image/gif': '.gif',
  'image/webp': '.webp',
}

function pad(value: number) {
  return String(value).padStart(2, '0')
}

export function pasteImageName(ext: string, now = Date.now()) {
  const date = new Date(now)
  return `粘贴图片-${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}-${pad(date.getHours())}${pad(date.getMinutes())}${pad(date.getSeconds())}${ext}`
}

export function normalizeUploadFile(file: File, now = Date.now()): File | null {
  const type = file.type.toLowerCase()
  let ext = uploadExt[type]
  if (!ext) {
    const match = file.name.match(/\.(jpe?g|png|gif|webp)$/i)
    if (!match) return null
    ext = match[0].toLowerCase() === '.jpeg' ? '.jpg' : match[0].toLowerCase()
  }
  if (/\.(jpe?g|png|gif|webp)$/i.test(file.name)) return file
  const mime = ext === '.jpg' ? 'image/jpeg' : ext === '.png' ? 'image/png' : ext === '.gif' ? 'image/gif' : 'image/webp'
  return new File([file], pasteImageName(ext, now), { type: mime, lastModified: file.lastModified })
}

export function filesFromClipboardItems(items: ArrayLike<{ kind: string; getAsFile: () => File | null }> | null | undefined, now = Date.now()): File[] {
  if (!items) return []
  const files: File[] = []
  for (const item of Array.from(items)) {
    if (item.kind !== 'file') continue
    const file = item.getAsFile()
    if (!file) continue
    const normalized = normalizeUploadFile(file, now)
    if (normalized) files.push(normalized)
  }
  return files
}

export function shouldIgnorePasteTarget(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) return false
  const tag = target.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target.isContentEditable
}
