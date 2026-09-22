import { describe, expect, it } from 'vitest'
import { folderSelectValue, folderTitle, formatSize, fullURL, markdown, normalizeFolderID, shareText, sharexRequestURL, sharexTokenPlaceholder, sharexUploader } from './share'

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

describe('ShareX uploader', () => {
  it('builds a custom uploader that builds absolute result URLs from the uploaded image ID', () => {
    expect(sharexRequestURL('https://img.example.com/')).toBe('https://img.example.com/api/images')
    const uploader = sharexUploader({ siteName: '猫猫图床', requestURL: 'https://img.example.com/api/images', token: ' secret ' })
    expect(uploader.RequestURL).toBe('https://img.example.com/api/images')
    expect(uploader.FileFormName).toBe('file')
    expect(uploader.Headers.Authorization).toBe('Bearer secret')
    expect(uploader.URL).toBe('https://img.example.com/i/{json:id}')
    expect(uploader.ThumbnailURL).toBe('https://img.example.com/t/{json:id}')
    expect(uploader.ErrorMessage).toBe('{json:error}')
    expect(uploader.Name).toBe('猫猫图床')
  })

  it('keeps a placeholder when the token is empty', () => {
    const uploader = sharexUploader({ siteName: '', requestURL: 'http://127.0.0.1:8080/api/images', token: '' })
    expect(uploader.Headers.Authorization).toBe('Bearer ' + sharexTokenPlaceholder)
    expect(uploader.Name).toBe('Mio 图床')
  })
})


describe('ShareX without a public domain', () => {
  it('keeps absolute localhost links even when API response URLs are relative', () => {
    const u = sharexUploader({siteName:'Mio',requestURL:sharexRequestURL('http://127.0.0.1:8080'),token:'test'})
    const response = {id:'abc.png',url:'/i/abc.png',thumb_url:'/t/abc.png'}
    expect(u.URL.replace('{json:id}',response.id)).toBe('http://127.0.0.1:8080/i/abc.png')
    expect(u.ThumbnailURL.replace('{json:id}',response.id)).toBe('http://127.0.0.1:8080/t/abc.png')
  })
  it('does not concatenate a returned absolute URL twice', () => {
    const u = sharexUploader({siteName:'Mio',requestURL:'https://img.example.com/api/images',token:'test'})
    const response = {id:'abc.png',url:'https://img.example.com/i/abc.png'}
    expect(u.URL.replace('{json:id}',response.id)).toBe(response.url)
  })
  it('rejects unusable uploader origins', () => {
    expect(sharexRequestURL('')).toBe('')
    expect(sharexRequestURL('file:///tmp/images')).toBe('')
    expect(sharexRequestURL('not-a-url')).toBe('')
  })
})
