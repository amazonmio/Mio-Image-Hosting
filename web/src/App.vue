<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import 'element-plus/es/components/message/style/css'
import 'element-plus/es/components/message-box/style/css'
import { Plus, UploadFilled, Folder as FolderIcon, Setting, Lock } from '@element-plus/icons-vue'
import { useTheme } from './theme'
import AuthGate from './AuthGate.vue'
import UploadPage from './UploadPage.vue'
import FolderPage from './FolderPage.vue'
import SettingsPage from './SettingsPage.vue'
import { api, type AuthStatus, type Config, type Folder, type ImageList } from './api'
import { folderSelectValue, formatSize, normalizeFolderID, readLinkFormat, writeLinkFormat, applySiteBranding, type LinkFormat } from './share'

type View = 'upload' | 'folders' | 'settings'
const sections = [
  { id: 'upload' as View, label: '上传图片', icon: UploadFilled, description: '拖拽上传图片，轻松获取分享链接。' },
  { id: 'folders' as View, label: '文件夹管理', icon: FolderIcon, description: '按文件夹整理图片，管理你的全部收藏。' },
  { id: 'settings' as View, label: '系统设置', icon: Setting, description: '调整使用偏好，配置 ShareX，查看当前服务配置。' },
]
function readView(): View {
  const value = location.hash.slice(1)
  return value === 'folders' || value === 'settings' ? value : 'upload'
}

const { themeMode, isDark } = useTheme()
const view = ref<View>(readView())
const section = computed(() => sections.find(item => item.id === view.value)!)
function changeView(value: View) { view.value = value; location.hash = value }
function syncView() { view.value = readView() }

const linkFormat = ref<LinkFormat>(readLinkFormat())
watch(linkFormat, writeLinkFormat)

const folders = ref<Folder[]>([])
const data = ref<ImageList>({ items: [], total: 0, all_count: 0, total_size: 0, uncategorized_count: 0, page: 1, page_size: 48 })
const currentFolder = ref<number | null>(null)
const page = ref(1), search = ref(''), loading = ref(false), error = ref('')
const config = ref<Config>({ max_file_size: 20 * 1024 * 1024, auth_required: false, public_base_url: '', admin_token_configured: false, site_name: 'Mio 图床', avatar_url: '/logo.webp', favicon_url: '/logo.webp' })
const selected = ref<string[]>([])
const auth = ref<AuthStatus>({ initialized: false, authenticated: false, setup_key_required: false })
const booting = ref(true), bootError = ref('')
const passwordDialog = ref(false), currentPassword = ref(''), newPassword = ref(''), confirmPassword = ref(''), changingPassword = ref(false), passwordError = ref('')
const uploadFolder = ref(0), uploading = ref(false)
const uploadPage = ref<{ reset: () => void }>()
let request = 0, searchTimer: ReturnType<typeof setTimeout>
const moveDialog = ref(false), moveTarget = ref(0), movingIDs = ref<string[]>([]), mutating = ref(false)
const copiedDialog = ref(false), copiedText = ref('')

function showError(e: unknown) { ElMessage.error(e instanceof Error ? e.message : '操作失败') }
async function load() {
  if (!auth.value.authenticated) return
  const run = ++request; loading.value = true; error.value = ''
  try {
    const params = new URLSearchParams({ page: String(page.value), q: search.value })
    if (currentFolder.value !== null) params.set('folder', String(currentFolder.value))
    const [images, list] = await Promise.all([api<ImageList>(`/images?${params}`), api<Folder[]>('/folders')])
    if (run !== request) return
    data.value = images; folders.value = list; selected.value = []
    if (page.value > 1 && images.items.length === 0) { page.value = Math.max(1, Math.ceil(images.total / 48)); return }
  } catch (e) { if (run === request) error.value = e instanceof Error ? e.message : '加载失败' }
  finally { if (run === request) loading.value = false }
}
function navigate(id: number | null) { if (currentFolder.value === id) return; currentFolder.value = id; page.value = 1; selected.value = []; load() }
watch(page, load)
watch(search, () => { clearTimeout(searchTimer); searchTimer = setTimeout(() => { if (page.value !== 1) page.value = 1; else load() }, 250) })
function requireAuth() {
  ++request; auth.value.authenticated = false
  currentPassword.value = ''; newPassword.value = ''; confirmPassword.value = ''; passwordError.value = ''; loading.value = false
  data.value = { items: [], total: 0, all_count: 0, total_size: 0, uncategorized_count: 0, page: 1, page_size: 48 }
  folders.value = []; selected.value = []; uploadPage.value?.reset()
  moveDialog.value = false; passwordDialog.value = false; copiedDialog.value = false
  clearTimeout(searchTimer)
}
async function enterSpace(status: AuthStatus) { auth.value = status; await load() }
async function logout() { try { await api('/auth/logout', { method: 'POST' }); requireAuth() } catch (e) { showError(e) } }
async function bootstrap() {
  booting.value = true; bootError.value = ''
  try {
    const [status, settings] = await Promise.all([api<AuthStatus>('/auth/status'), api<Config>('/config')])
    auth.value = status; config.value = settings
    applySiteBranding(settings.site_name, settings.favicon_url)
    if (status.authenticated) await load()
  } catch (e) { bootError.value = e instanceof Error ? e.message : '无法连接服务' }
  finally { booting.value = false }
}
function openPassword() { currentPassword.value = ''; newPassword.value = ''; confirmPassword.value = ''; passwordError.value = ''; passwordDialog.value = true }
async function changePassword() {
  passwordError.value = ''
  if ([...newPassword.value].length < 10 || new TextEncoder().encode(newPassword.value).length > 256) { passwordError.value = '新密码至少 10 个字符且最多 256 字节'; return }
  if (newPassword.value !== confirmPassword.value) { passwordError.value = '两次输入的新密码不一致'; return }
  changingPassword.value = true
  try {
    await api('/auth/password', { method: 'POST', body: JSON.stringify({ current_password: currentPassword.value, new_password: newPassword.value }) })
    currentPassword.value = ''; newPassword.value = ''; confirmPassword.value = ''
    requireAuth(); ElMessage.success('密码已修改，请重新登录')
  } catch (e) { passwordError.value = e instanceof Error ? e.message : '修改失败' }
  finally { changingPassword.value = false }
}
onMounted(() => { window.addEventListener('mio-auth', requireAuth); window.addEventListener('hashchange', syncView); bootstrap() })
onUnmounted(() => { window.removeEventListener('mio-auth', requireAuth); window.removeEventListener('hashchange', syncView); clearTimeout(searchTimer) })

async function editFolder(folder?: Folder) {
  try {
    const { value } = await ElMessageBox.prompt('名称不超过 60 个字符', folder ? '重命名文件夹' : '新建文件夹', { inputValue: folder?.name || '', inputPlaceholder: '例如：博客配图', confirmButtonText: '保存', cancelButtonText: '取消', inputValidator: (value: string) => !!value?.trim() && [...value.trim()].length <= 60 || '请输入 1–60 个字符' })
    await api(folder ? `/folders/${folder.id}` : '/folders', { method: folder ? 'PATCH' : 'POST', body: JSON.stringify({ name: value.trim() }) })
    ElMessage.success('文件夹已保存'); await load()
  } catch (e) { if (e !== 'cancel' && e !== 'close') showError(e) }
}
async function deleteFolder(folder: Folder) {
  try {
    await ElMessageBox.confirm(`删除“${folder.name}”后，其中的图片会移至未分类，直链保持不变。`, '删除文件夹', { type: 'warning', confirmButtonText: '删除文件夹', cancelButtonText: '取消' })
    await api(`/folders/${folder.id}`, { method: 'DELETE' })
    currentFolder.value = null; page.value = 1; await load(); ElMessage.success('文件夹已删除，图片已保留')
  } catch (e) { if (e !== 'cancel' && e !== 'close') showError(e) }
}
function openUpload() {
  if (!uploading.value) uploadFolder.value = folderSelectValue(currentFolder.value)
  changeView('upload')
}
async function copy(text: string) {
  try { await navigator.clipboard.writeText(text); ElMessage.success('已复制到剪贴板') }
  catch { copiedText.value = text; copiedDialog.value = true }
}
async function deleteImages(ids: string[]) {
  if (!ids.length) return
  try {
    await ElMessageBox.confirm(`将永久删除 ${ids.length} 张图片，已分享的直链会失效。此操作无法撤销。`, '删除图片', { type: 'warning', confirmButtonText: '永久删除', cancelButtonText: '取消' })
    mutating.value = true
    const results = await Promise.allSettled(ids.map(id => api(`/images/${id}`, { method: 'DELETE' })))
    const failed = results.filter(result => result.status === 'rejected').length
    if (failed) ElMessage.error(`${ids.length - failed} 张已删除，${failed} 张失败，请刷新后重试`)
    else ElMessage.success('图片已删除')
    await load()
  } catch (e) { if (e !== 'cancel' && e !== 'close') showError(e) }
  finally { mutating.value = false }
}
function openMove(ids: string[]) {
  movingIDs.value = [...ids]
  moveTarget.value = folderSelectValue(currentFolder.value)
  moveDialog.value = true
}
async function moveImages() {
  mutating.value = true
  try {
    const folderID = normalizeFolderID(moveTarget.value)
    const results = await Promise.allSettled(movingIDs.value.map(id => api(`/images/${id}`, { method: 'PATCH', body: JSON.stringify({ folder_id: folderID }) })))
    const failed = results.filter(result => result.status === 'rejected').length
    if (failed) ElMessage.error(`${failed} 张移动失败，请刷新后重试`)
    else ElMessage.success('分类已更新，直链保持不变')
    moveDialog.value = false; await load()
  } finally { mutating.value = false }
}
</script>

<template>
  <div v-if="booting || bootError" class="auth-shell">
    <div class="auth-card">
      <img class="auth-logo" :src="config.avatar_url" :alt="config.site_name + ' Logo'" />
      <h1>{{ bootError ? '暂时无法连接' : '正在打开图片空间' }}</h1>
      <p>{{ bootError || '正在检查初始化和登录状态…' }}</p>
      <el-button v-if="bootError" type="primary" @click="bootstrap">重新连接</el-button>
    </div>
  </div>
  <AuthGate v-else-if="!auth.authenticated" :status="auth" :site-name="config.site_name" :avatar-url="config.avatar_url" @ready="enterSpace" @refresh="bootstrap" />
  <div v-else class="app-shell">
    <aside class="sidebar">
      <a class="brand" href="/" :aria-label="config.site_name + '首页'">
        <img class="brand-logo" :src="config.avatar_url" :alt="config.site_name + ' Logo'" width="44" height="44" />
        <span class="brand-name">{{ config.site_name }}</span>
      </a>
      <div class="workspace-label">工作空间</div>
      <nav class="section-nav" aria-label="功能导航">
        <button v-for="item in sections" :key="item.id" :class="['nav-item', { active: view === item.id }]" :aria-current="view === item.id ? 'page' : undefined" @click="changeView(item.id)">
          <el-icon><component :is="item.icon" /></el-icon><span>{{ item.label }}</span>
        </button>
      </nav>
      <div class="storage-card"><span class="storage-dot"></span> 本地存储<span class="storage-value">{{ formatSize(data.total_size) }}</span><p>{{ data.all_count }} 张图片，妥善收藏每一帧。</p></div>
      <div class="sidebar-footer">{{ config.site_name }} · 简单存，轻松分享 <el-button v-if="config.auth_required" text circle :disabled="uploading" aria-label="退出登录" @click="logout"><el-icon><Lock /></el-icon></el-button></div>
    </aside>

    <main class="main-content">
      <header class="topbar">
        <span>图片空间 <span class="breadcrumb-divider">/</span> <strong>{{ section.label }}</strong></span>
        <span class="private-badge"><span></span> {{ auth.username }} · 已登录</span>
      </header>
      <section class="page-content">
        <div class="page-heading">
          <div>
            <div class="eyebrow">{{ config.site_name }} · 个人图片空间</div>
            <h1>{{ section.label }}</h1>
            <p>{{ section.description }}</p>
          </div>
          <el-button v-if="view === 'folders'" type="primary" size="large" :icon="Plus" @click="editFolder()">新建文件夹</el-button>
        </div>

        <UploadPage v-show="view === 'upload'" ref="uploadPage" v-model:folder="uploadFolder" :folders="folders" :authenticated="auth.authenticated" :max-file-size="config.max_file_size" :link-format="linkFormat" @update:uploading="uploading = $event" @uploaded="load" @copy="copy" />
        <FolderPage v-if="view === 'folders'" :folders="folders" :data="data" :current-folder="currentFolder" :page="page" :search="search" :loading="loading" :error="error" :selected="selected" :mutating="mutating" :link-format="linkFormat" @navigate="navigate" @load="load" @update:search="search = $event" @update:page="page = $event" @update:selected="selected = $event" @edit-folder="editFolder" @delete-folder="deleteFolder" @open-upload="openUpload" @copy="copy" @open-move="openMove" @delete-images="deleteImages" />
        <SettingsPage v-if="view === 'settings'" v-model:theme-mode="themeMode" v-model:link-format="linkFormat" :username="auth.username" :is-dark="isDark" :config="config" :all-count="data.all_count" :total-size="data.total_size" :uploading="uploading" @open-password="openPassword" @logout="logout" />
        <footer class="page-footer"><span>你的图片，井然有序。</span><span>{{ config.site_name }}</span></footer>
      </section>
    </main>

    <el-dialog v-model="moveDialog" title="移动图片" width="420px" :close-on-click-modal="!mutating" :show-close="!mutating">
      <p class="dialog-description">将 {{ movingIDs.length }} 张图片移动到指定文件夹，直链保持不变。</p>
      <el-select v-model="moveTarget" class="full-width" placeholder="未分类" :disabled="mutating">
        <el-option label="未分类" :value="0" />
        <el-option v-for="folder in folders" :key="folder.id" :label="folder.name" :value="folder.id" />
      </el-select>
      <template #footer>
        <el-button :disabled="mutating" @click="moveDialog = false">取消</el-button>
        <el-button type="primary" :loading="mutating" @click="moveImages">确认移动</el-button>
      </template>
    </el-dialog>
    <el-dialog v-model="passwordDialog" title="修改密码" width="440px" :close-on-click-modal="!changingPassword" :show-close="!changingPassword">
      <form class="account-form" @submit.prevent="changePassword">
        <label>当前密码<el-input v-model="currentPassword" type="password" show-password autocomplete="current-password" aria-label="当前密码" :disabled="changingPassword" /></label>
        <label>新密码<el-input v-model="newPassword" type="password" show-password autocomplete="new-password" aria-label="新密码" :disabled="changingPassword" /></label>
        <label>确认新密码<el-input v-model="confirmPassword" type="password" show-password autocomplete="new-password" aria-label="确认新密码" :disabled="changingPassword" /></label>
        <p class="auth-help">至少 10 个字符。修改后，所有浏览器的登录状态都会失效。</p>
        <el-alert v-if="passwordError" :title="passwordError" type="error" :closable="false" show-icon />
        <el-button type="primary" native-type="submit" :loading="changingPassword">确认修改</el-button>
      </form>
    </el-dialog>
    <el-dialog v-model="copiedDialog" title="复制链接" width="520px">
      <p class="dialog-description">浏览器未开放剪贴板权限，请选中下方内容手动复制。</p>
      <el-input v-model="copiedText" type="textarea" :rows="5" readonly aria-label="待复制链接" @focus="(event: FocusEvent) => (event.target as HTMLTextAreaElement).select()" />
    </el-dialog>
  </div>
</template>
