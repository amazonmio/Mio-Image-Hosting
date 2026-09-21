import { onUnmounted, ref, watch } from 'vue'

export type ThemeMode = 'light' | 'dark' | 'system'
const storageKey = 'mio-theme'

export function useTheme() {
  const system = window.matchMedia('(prefers-color-scheme: dark)')
  let stored: string | null = null
  try { stored = localStorage.getItem(storageKey) } catch { /* Storage may be unavailable. */ }
  const themeMode = ref<ThemeMode>(stored === 'light' || stored === 'dark' ? stored : 'system')
  const isDark = ref(false)
  function apply() {
    isDark.value = themeMode.value === 'dark' || (themeMode.value === 'system' && system.matches)
    document.documentElement.classList.toggle('dark', isDark.value)
    document.documentElement.style.colorScheme = isDark.value ? 'dark' : 'light'
    document.documentElement.style.backgroundColor = isDark.value ? '#151922' : '#f6f7fb'
    document.querySelector('meta[name="theme-color"]')?.setAttribute('content', isDark.value ? '#151922' : '#f6f7fb')
  }
  watch(themeMode, () => {
    try { localStorage.setItem(storageKey, themeMode.value) } catch { /* Keep the selection for this session. */ }
    apply()
  }, { flush: 'sync' })
  system.addEventListener('change', apply)
  function sync(event: StorageEvent) {
    if (event.key !== storageKey && event.key !== null) return
    themeMode.value = event.newValue === 'light' || event.newValue === 'dark' ? event.newValue : 'system'
  }
  window.addEventListener('storage', sync)
  onUnmounted(() => { system.removeEventListener('change', apply); window.removeEventListener('storage', sync) })
  apply()
  return { themeMode, isDark }
}
