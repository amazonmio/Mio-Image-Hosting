const extensionByMIME: Record<string, string> = {
  'image/jpeg': '.jpg',
  'image/png': '.png',
  'image/gif': '.gif',
  'image/webp': '.webp',
}

const mimeByExtension: Record<string, string> = {
  '.jpg': 'image/jpeg',
  '.png': 'image/png',
  '.gif': 'image/gif',
  '.webp': 'image/webp',
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
  let ext = extensionByMIME[type]
  if (!ext) {
    const match = file.name.match(/\.(jpe?g|png|gif|webp)$/i)
    if (!match) {
      return null
    }
    ext = match[0].toLowerCase() === '.jpeg' ? '.jpg' : match[0].toLowerCase()
  }
  if (/\.(jpe?g|png|gif|webp)$/i.test(file.name)) {
    return file
  }
  const mime = mimeByExtension[ext]
  return new File([file], pasteImageName(ext, now), { type: mime, lastModified: file.lastModified })
}

export function filesFromClipboardItems(
  items: ArrayLike<{ kind: string; getAsFile: () => File | null }> | null | undefined,
  now = Date.now(),
): File[] {
  if (!items) {
    return []
  }
  const files: File[] = []
  for (const item of Array.from(items)) {
    if (item.kind !== 'file') {
      continue
    }
    const file = item.getAsFile()
    if (!file) {
      continue
    }
    const normalized = normalizeUploadFile(file, now)
    if (normalized) {
      files.push(normalized)
    }
  }
  return files
}

type ClipboardFiles = {
  files?: ArrayLike<File> | null
  items?: ArrayLike<{ kind: string; getAsFile: () => File | null }> | null
}

export function filesFromClipboardData(data: ClipboardFiles | null, now = Date.now()): File[] {
  if (!data) {
    return []
  }
  const files = Array.from(data.files ?? [])
    .map((file) => normalizeUploadFile(file, now))
    .filter((file): file is File => file !== null)
  const items = filesFromClipboardItems(data.items, now)
  // Browsers can expose the same clipboard files through both lists. Choose
  // the more complete view instead of queueing every image twice.
  return files.length >= items.length ? files : items
}

export function shouldIgnorePasteTarget(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) {
    return false
  }
  const tag = target.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target.isContentEditable
}
