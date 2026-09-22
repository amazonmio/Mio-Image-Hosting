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

export const sharexTokenPlaceholder = 'YOUR_ADMIN_TOKEN'

export function sharexRequestURL(base: string) {
  try { const url = new URL(base.trim()); return ['http:', 'https:'].includes(url.protocol) ? url.origin + '/api/images' : '' } catch { return '' }
}

export function sharexUploader(options: { siteName: string; requestURL: string; token: string }) {
  const token = options.token.trim() || sharexTokenPlaceholder
  const origin = new URL(options.requestURL).origin
  return {
    Version: '17.0.0',
    Name: options.siteName.trim() || 'Mio 图床',
    DestinationType: 'ImageUploader',
    RequestMethod: 'POST',
    RequestURL: options.requestURL,
    Headers: { Authorization: 'Bearer ' + token },
    Body: 'MultipartFormData',
    FileFormName: 'file',
    URL: origin + '/i/{json:id}',
    ThumbnailURL: origin + '/t/{json:id}',
    ErrorMessage: '{json:error}',
  }
}

export function sharexUploaderJSON(options: { siteName: string; requestURL: string; token: string }) {
  return JSON.stringify(sharexUploader(options), null, 2)
}

export function downloadTextFile(name: string, text: string) {
  if (typeof document === 'undefined') return
  const href = URL.createObjectURL(new Blob([text], { type: 'application/json' }))
  const link = document.createElement('a')
  link.href = href
  link.download = name
  link.click()
  URL.revokeObjectURL(href)
}
