<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api, type AuthStatus } from './api'

const props = defineProps<{ status: AuthStatus; siteName?: string; avatarUrl?: string }>()
const emit = defineEmits<{ ready: [status: AuthStatus]; refresh: [] }>()
const step = ref(0)
const username = ref(''), password = ref(''), confirmation = ref(''), setupKey = ref('')
const busy = ref(false), error = ref('')
const completed = ref<AuthStatus | null>(null)
const initialized = computed(() => props.status.initialized)
const siteName = computed(() => props.siteName || 'Mio 图床')
const avatarUrl = computed(() => props.avatarUrl || '/branding/avatar')
watch(initialized, () => { step.value = 0; password.value = ''; confirmation.value = ''; error.value = '' })
async function submit() {
  if (busy.value) return
  error.value = ''
  if (!username.value.trim() || !password.value) { error.value = '请输入账号和密码'; return }
  if (!initialized.value) {
    if (!/^[a-zA-Z0-9_-]{3,32}$/.test(username.value.trim())) { error.value = '账号须为 3–32 位字母、数字、下划线或短横线'; return }
    if ([...password.value].length < 10 || new TextEncoder().encode(password.value).length > 256) { error.value = '密码至少 10 个字符且最多 256 字节'; return }
    if (password.value !== confirmation.value) { error.value = '两次输入的密码不一致'; return }
    if (props.status.setup_key_required && !setupKey.value.trim()) { error.value = '请输入初始化密钥'; return }
  }
  busy.value = true
  try {
    const body = initialized.value ? { username: username.value.trim(), password: password.value } : { username: username.value.trim(), password: password.value, setup_key: setupKey.value.trim() }
    const result = await api<AuthStatus>(initialized.value ? '/auth/login' : '/auth/setup', { method: 'POST', body: JSON.stringify(body) })
    password.value = ''; confirmation.value = ''; setupKey.value = ''
    if (initialized.value) emit('ready', result)
    else { completed.value = result; step.value = 2 }
  } catch (e) { error.value = e instanceof Error ? e.message : '请求失败，请重试' }
  finally { busy.value = false }
}
</script>

<template>
  <main class="auth-shell">
    <section class="auth-card" :aria-label="initialized ? '管理员登录' : '首次启动引导'">
      <div class="auth-brand"><img class="auth-logo" :src="avatarUrl" :alt="siteName + ' Logo'"/><div><strong>{{ siteName }}</strong><span>你的图片，井然有序。</span></div></div>
      <template v-if="!initialized">
        <div class="setup-steps" aria-label="初始化进度"><span :class="{active:step===0}">1 · 欢迎</span><span :class="{active:step===1}">2 · 创建账号</span><span :class="{active:step===2}">3 · 完成</span></div>
        <template v-if="step === 0">
          <h1>开启你的图片空间</h1>
          <p class="auth-description">只需创建一个管理员账号，就可以开始上传、分类和分享图片。</p>
          <div class="setup-features"><div><strong>轻松上传</strong><span>支持 JPG、PNG、GIF 和 WebP，单张最大 20 MB。</span></div><div><strong>有序收藏</strong><span>用文件夹整理图片，移动分类也不会改变直链。</span></div><div><strong>专属管理</strong><span>图片管理需要登录；分享出去的图片直链可公开访问。</span></div></div>
          <p class="auth-help">已有图片和文件夹会保留。初始化只需完成一次。</p>
          <el-button class="auth-submit" type="primary" size="large" @click="step = 1">开始设置</el-button>
        </template>
        <template v-else-if="step === 2">
          <div class="setup-success">✓</div><h1>图片空间已就绪</h1><p class="auth-description">管理员账号 {{ completed?.username }} 已创建，你现在可以开始使用图床了。</p>
          <el-button class="auth-submit" type="primary" size="large" @click="completed && emit('ready', completed)">进入图片空间</el-button>
        </template>
      </template>
      <template v-if="initialized || step === 1">
        <h1>{{ initialized ? '欢迎回来' : '创建管理员账号' }}</h1>
        <p class="auth-description">{{ initialized ? '登录后继续管理你的图片空间。' : '请记住账号和密码，之后使用它们登录。' }}</p>
        <form class="account-form" @submit.prevent="submit">
          <label>管理员账号<el-input v-model="username" autocomplete="username" name="username" aria-label="管理员账号" placeholder="例如：admin" :maxlength="32" :disabled="busy" size="large"/></label>
          <label>{{ initialized ? '密码' : '设置密码' }}<el-input v-model="password" type="password" show-password :autocomplete="initialized ? 'current-password' : 'new-password'" name="password" aria-label="密码" :placeholder="initialized ? '请输入密码' : '至少 10 个字符'" :disabled="busy" size="large"/></label>
          <label v-if="!initialized">确认密码<el-input v-model="confirmation" type="password" show-password autocomplete="new-password" aria-label="确认密码" placeholder="再次输入密码" :disabled="busy" size="large"/></label>
          <template v-if="!initialized && status.setup_key_required"><label>初始化密钥<el-input v-model="setupKey" type="password" show-password autocomplete="off" aria-label="初始化密钥" :disabled="busy" size="large"/></label><p class="auth-help">请在服务器数据目录的 setup-key.txt 中查看密钥，用于确认你有权初始化此图床。</p></template>
          <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon/>
          <el-button class="auth-submit" type="primary" native-type="submit" size="large" :loading="busy">{{ initialized ? '登录' : '创建账号并完成设置' }}</el-button>
          <el-button v-if="!initialized" text :disabled="busy" @click="step = 0; error = ''">返回上一步</el-button>
        </form>
        <p v-if="initialized" class="auth-help">登录状态有效期为 7 天。使用共享设备后，请记得退出登录。</p>
        <el-button v-if="error" text @click="emit('refresh')">重新检查初始化状态</el-button>
      </template>
    </section>
    <footer class="auth-footer">{{ siteName }} · 简单存，轻松分享</footer>
  </main>
</template>

<style>
.auth-shell { min-height: 100dvh; display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 40px 20px; background: radial-gradient(ellipse at 20% 10%, #e9edff, transparent 60%), #f6f7fb; }
.auth-card { width: 460px; max-width: 100%; background: #fff; border: 1px solid #e3e8f3; border-radius: 20px; padding: 36px; box-shadow: 0 18px 65px #3442770a; }
.auth-brand { display: flex; align-items: center; gap: 13px; margin-bottom: 28px; }
.auth-logo { width: 48px; height: 48px; object-fit: contain; border-radius: 13px; }
.auth-brand strong { font-size: 20px; display: block; }
.auth-brand span { display: block; color: #8994a8; font-size: 11px; margin-top: 6px; }
.auth-card h1 { font-size: 24px; margin: 0 0 12px; }
.auth-description { color: #7b879d; font-size: 13px; line-height: 1.8; margin: 0 0 24px; }
.setup-steps { display: flex; justify-content: space-between; gap: 8px; padding: 16px 0; border-top: 1px solid #eef0f5; margin-bottom: 22px; color: #97a1b3; font-size: 11px; }
.setup-steps .active { color: #5369db; font-weight: 600; }
.setup-features { display: grid; gap: 18px; padding: 21px; background: #f6f8fe; border-radius: 12px; margin-bottom: 21px; }
.setup-features strong { display: block; font-size: 13px; font-weight: 550; margin-bottom: 7px; }
.setup-features span { font-size: 12px; line-height: 1.7; color: #7b879d; }
.account-form { display: grid; gap: 18px; }
.account-form > label { display: grid; gap: 9px; font-size: 13px; }
.account-form .el-button + .el-button { margin-left: 0; }
.auth-submit { width: 100%; }
.auth-help { font-size: 11px; color: #8e99ad; line-height: 1.8; margin: 16px 0; }
.account-form .auth-help { margin: 0; }
.auth-footer { color: #98a3b5; font-size: 11px; margin-top: 25px; }
.setup-success { width: 56px; height: 56px; background: #e7f7ef; color: #299c76; display: grid; place-items: center; font-size: 30px; border-radius: 50%; margin: 0 0 24px; }
.account-actions { display: flex; flex-wrap: wrap; gap: 10px; }
html.dark .auth-shell { background: radial-gradient(ellipse at 20% 10%, #262f50, transparent 60%), #151922; }
html.dark .auth-card { background: #1d2330; border-color: #343e50; }
html.dark .auth-description, html.dark .auth-help, html.dark .auth-brand span, html.dark .auth-footer, html.dark .setup-features span { color: #a3b0c5; }
html.dark .setup-features { background: #252e43; }
html.dark .setup-steps { border-color: #343e50; }
html.dark .setup-steps .active { color: #b3bfff; }
html.dark .setup-success { background: #253e36; color: #78d9b4; }
@media(max-width:520px) { .auth-shell { padding: 24px 16px; }.auth-card { padding: 26px 22px; }.auth-card h1 { font-size: 22px; } }
</style>
