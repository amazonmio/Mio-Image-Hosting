import { afterEach, describe, expect, it, vi } from 'vitest'
import { upload } from './api'

class FakeXHR {
  static last: FakeXHR
  upload = { onprogress: null }
  status = 201
  responseText = '{"id":"test.png"}'
  onload?: () => void
  onabort?: () => void
  onerror?: () => void
  ontimeout?: () => void
  open() { FakeXHR.last = this }
  setRequestHeader() {}
  send = vi.fn()
  abort = vi.fn(() => this.onabort?.())
}
afterEach(() => vi.unstubAllGlobals())
describe('upload cancellation', () => {
  it('does not send a pre-cancelled upload', async () => {
    vi.stubGlobal('XMLHttpRequest', FakeXHR)
    const controller = new AbortController(); controller.abort()
    await expect(upload(new File(['x'], 'x.png'), null, () => {}, controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
  })
  it('aborts an in-flight upload and settles the promise', async () => {
    vi.stubGlobal('XMLHttpRequest', FakeXHR)
    const controller = new AbortController()
    const result = upload(new File(['x'], 'x.png'), null, () => {}, controller.signal)
    const expected = expect(result).rejects.toMatchObject({ name: 'AbortError' })
    controller.abort()
    await expected
    expect(FakeXHR.last.abort).toHaveBeenCalledOnce()
  })
  it('removes the cancellation listener after success', async () => {
    vi.stubGlobal('XMLHttpRequest', FakeXHR)
    const controller = new AbortController()
    const result = upload(new File(['x'], 'x.png'), null, () => {}, controller.signal)
    FakeXHR.last.onload?.()
    await expect(result).resolves.toMatchObject({ id: 'test.png' })
    controller.abort()
    expect(FakeXHR.last.abort).not.toHaveBeenCalled()
  })
})
