export interface Folder { id: number; name: string; count: number }
export interface Picture { id: string; name: string; size: number; width: number; height: number; mime: string; folder_id: number | null; created_at: string; url: string }
export interface ImageList { items: Picture[]; total: number; all_count: number; total_size: number; uncategorized_count: number; page: number; page_size: number }
export interface Config { max_file_size: number; auth_required: boolean; public_base_url: string }

export interface AuthStatus { initialized: boolean; authenticated: boolean; username?: string; setup_key_required: boolean }
export async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers)
  headers.set('X-Mio-Request', '1')
  if (typeof options.body === 'string') headers.set('Content-Type', 'application/json')
  const response = await fetch(`/api${path}`, { ...options, headers, credentials: 'same-origin' })
  if (response.status === 401 && path !== '/auth/login' && path !== '/auth/setup') window.dispatchEvent(new Event('mio-auth'))
  if (!response.ok) { const error = await response.json().catch(() => ({})); throw new Error(error.error || `请求失败 (${response.status})`) }
  return response.json()
}
export function upload(file: File, folderID: number | null, progress: (value: number) => void): Promise<Picture> {
  return new Promise((resolve, reject) => {
    const data = new FormData(); data.append('file', file)
    if (folderID != null && folderID > 0) data.append('folder_id', String(folderID))
    const xhr = new XMLHttpRequest(); xhr.open('POST', '/api/images'); xhr.timeout = 300000
    xhr.setRequestHeader('X-Mio-Request', '1')
    xhr.upload.onprogress = event => { if (event.lengthComputable) progress(Math.round(event.loaded / event.total * 100)) }
    xhr.onload = () => {
      if (xhr.status === 401) window.dispatchEvent(new Event('mio-auth'))
      try { const result = JSON.parse(xhr.responseText); if (xhr.status >= 200 && xhr.status < 300) resolve(result); else reject(new Error(result.error || '上传失败')) }
      catch { reject(new Error('服务器响应异常')) }
    }
    xhr.onerror = () => reject(new Error('网络连接失败，请重试'))
    xhr.ontimeout = () => reject(new Error('上传超时，请重试'))
    xhr.send(data)
  })
}
