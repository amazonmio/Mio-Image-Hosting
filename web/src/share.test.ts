import { describe, expect, it } from 'vitest'
import { filesFromClipboardItems, folderSelectValue, folderTitle, formatSize, fullURL, markdown, normalizeFolderID, normalizeUploadFile, pasteImageName, shareText, validImageName } from './share'

describe('normalizeFolderID', () => {
  it('treats missing and zero as uncategorized', () => {
    expect(normalizeFolderID(null)).toBeNull()
    expect(normalizeFolderID(undefined)).toBeNull()
    expect(normalizeFolderID(0)).toBeNull()
    expect(normalizeFolderID(4)).toBe(4)
  })
})

describe('folderSelectValue', () => {
  it('uses 0 in the UI select for uncategorized', () => {
    expect(folderSelectValue(null)).toBe(0)
    expect(folderSelectValue(0)).toBe(0)
    expect(folderSelectValue(9)).toBe(9)
  })
})

describe('share helpers', () => {
  it('formats sizes', () => {
    expect(formatSize(800)).toBe('800 B')
    expect(formatSize(2048)).toBe('2.0 KB')
    expect(formatSize(1048576)).toBe('1.0 MB')
  })

  it('builds absolute URLs and markdown from a relative path', () => {
    const origin = 'https://img.example.com'
    expect(fullURL('/i/abc.png', origin)).toBe('https://img.example.com/i/abc.png')
    expect(markdown('cover]shot.png', '/i/abc.png', origin)).toBe('![cover\\]shot.png](https://img.example.com/i/abc.png)')
    expect(shareText('cover.png', '/i/abc.png', 'url', origin)).toBe('https://img.example.com/i/abc.png')
    expect(shareText('cover.png', '/i/abc.png', 'markdown', origin)).toBe('![cover.png](https://img.example.com/i/abc.png)')
  })

  it('names the current folder view', () => {
    const folders = [{ id: 2, name: '博客' }]
    expect(folderTitle(null, folders)).toBe('全部图片')
    expect(folderTitle(0, folders)).toBe('未分类')
    expect(folderTitle(2, folders)).toBe('博客')
  })

  it('validates image display names', () => {
    expect(validImageName(' 封面.png ')).toBe(true)
    expect(validImageName('')).toBe(false)
    expect(validImageName('a/b.png')).toBe(false)
    expect(validImageName('a\\b.png')).toBe(false)
    expect(validImageName('x'.repeat(181))).toBe(false)
  })
})

describe('clipboard upload files', () => {
  it('keeps named images and names pasted screenshots', () => {
    const named = new File([new Uint8Array([1, 2, 3])], 'cover.PNG', { type: 'image/png' })
    expect(normalizeUploadFile(named)).toBe(named)
    const stamp = Date.UTC(2026, 8, 22, 2, 34, 5)
    const pasted = normalizeUploadFile(new File([new Uint8Array([1, 2, 3])], 'image', { type: 'image/png' }), stamp)
    expect(pasted?.name).toBe(pasteImageName('.png', stamp))
    expect(pasted?.type).toBe('image/png')
    expect(normalizeUploadFile(new File([new Uint8Array([1])], 'notes.txt', { type: 'text/plain' }))).toBeNull()
  })

  it('reads image items from a clipboard list and skips text', () => {
    const image = new File([new Uint8Array([9])], '', { type: 'image/jpeg' })
    const files = filesFromClipboardItems([
      { kind: 'string', getAsFile: () => null },
      { kind: 'file', getAsFile: () => image },
      { kind: 'file', getAsFile: () => new File([new Uint8Array([1])], 'readme.md', { type: 'text/markdown' }) },
    ], Date.UTC(2026, 0, 2, 3, 4, 5))
    expect(files).toHaveLength(1)
    expect(files[0].name).toMatch(/\.jpg$/)
    expect(files[0].type).toBe('image/jpeg')
  })
})


describe('image name Unicode validation', () => {
  it('rejects embedded C0 and C1 controls, including DEL', () => {
    const controls = [...Array.from({length:32},(_,i)=>i), ...Array.from({length:33},(_,i)=>0x7f+i)]
    for (const code of controls) expect(validImageName('a'+String.fromCodePoint(code)+'b.png')).toBe(false)
  })
  it('rejects replacement characters and lone UTF-16 surrogates', () => {
    for (const code of [0xfffd,0xd800,0xdbff,0xdc00,0xdfff]) expect(validImageName('a'+String.fromCharCode(code)+'b.png')).toBe(false)
  })
  it('keeps valid Unicode names and counts code points', () => {
    expect(validImageName(' 封面😀.png ')).toBe(true)
    expect(validImageName('a'+String.fromCodePoint(0x7e,0xa0,0xfffc,0x10000)+'b.png')).toBe(true)
    expect(validImageName('😀'.repeat(180))).toBe(true)
    expect(validImageName('😀'.repeat(181))).toBe(false)
  })
})
