import { describe, expect, it } from 'vitest'
import { folderSelectValue, folderTitle, formatSize, fullURL, markdown, normalizeFolderID, shareText } from './share'

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
})
