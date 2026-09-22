<script setup lang="ts">
import { computed, ref } from 'vue'
import { downloadTextFile, formatSize, sharexRequestURL, sharexUploaderJSON, type LinkFormat } from './share'
import type { ThemeMode } from './theme'
import type { Config } from './api'

const props = defineProps<{
  username?: string
  isDark: boolean
  config: Config
  allCount: number
  totalSize: number
  uploading: boolean
}>()
const themeMode = defineModel<ThemeMode>('themeMode', { required: true })
const linkFormat = defineModel<LinkFormat>('linkFormat', { required: true })
defineEmits<{ openPassword: []; logout: [] }>()

const token = ref('')
const requestURL = computed(() => sharexRequestURL(props.config.public_base_url || (typeof location === 'undefined' ? '' : location.origin)))
function downloadShareX() {
  downloadTextFile('mio-image-hosting.sxcu', sharexUploaderJSON({
    siteName: props.config.site_name,
    requestURL: requestURL.value,
    token: token.value,
  }))
}
</script>

<template>
  <section class="settings-page" aria-label="系统设置内容">
    <div class="settings-card">
      <div class="card-heading">
        <h2>管理员账号</h2>
        <p>当前登录：{{ username }}。登录状态有效期为 7 天。</p>
      </div>
      <div class="account-actions">
        <el-button @click="$emit('openPassword')">修改密码</el-button>
        <el-button :disabled="uploading" @click="$emit('logout')">退出登录</el-button>
      </div>
    </div>
    <div class="settings-card theme-card">
      <div class="card-heading">
        <h2>外观主题</h2>
        <p>选择你喜欢的显示方式，让图片空间看起来更舒适。</p>
      </div>
      <div class="setting-row">
        <div>
          <strong>显示模式</strong>
          <p>当前为{{ isDark ? '深色' : '浅色' }}模式，切换后立即生效。</p>
        </div>
        <el-select v-model="themeMode" aria-label="显示模式">
          <el-option label="浅色模式" value="light" />
          <el-option label="深色模式" value="dark" />
          <el-option label="跟随系统" value="system" />
        </el-select>
      </div>
      <div class="theme-note">自动记住当前浏览器的选择；跟随系统时，会随系统外观一起切换。</div>
    </div>
    <div class="settings-card">
      <div class="card-heading">
        <h2>分享偏好</h2>
        <p>设置“复制链接”按钮的默认输出格式。</p>
      </div>
      <div class="setting-row">
        <div>
          <strong>默认链接格式</strong>
          <p>适用于上传结果及图片列表中的复制操作。</p>
        </div>
        <el-select v-model="linkFormat" aria-label="默认链接格式">
          <el-option label="图片直链（URL）" value="url" />
          <el-option label="Markdown" value="markdown" />
        </el-select>
      </div>
      <div class="theme-note">自动记住当前浏览器的选择，仅影响本机。</div>
    </div>
    <div class="settings-card">
      <div class="card-heading">
        <h2>ShareX 上传</h2>
        <p>截图后可直接传到本图床。ShareX 使用环境变量 <code>ADMIN_TOKEN</code>，不使用网页登录密码。</p>
      </div>
      <el-alert v-if="!config.admin_token_configured" title="当前服务还没有设置 ADMIN_TOKEN，ShareX 无法上传。写入环境变量后重启服务。" type="warning" show-icon :closable="false" />
      <dl class="config-list">
        <div><dt>上传地址</dt><dd>{{ requestURL }}</dd></div>
        <div><dt>ADMIN_TOKEN</dt><dd>{{ config.admin_token_configured ? '已配置（不会显示原文）' : '未设置' }}</dd></div>
      </dl>
      <label class="field-label sharex-token">粘贴 ADMIN_TOKEN，仅用于生成本地配置文件
        <el-input v-model="token" type="password" show-password autocomplete="off" aria-label="ADMIN_TOKEN" placeholder="不会发送到服务器" />
      </label>
      <div class="account-actions">
        <el-button type="primary" :disabled="!requestURL" @click="downloadShareX">下载 ShareX 配置</el-button>
      </div>
      <p class="configuration-note">导入后，在 ShareX 里把该上传器设为默认图片目标。建议同时设置 PUBLIC_BASE_URL，这样返回的直链是公网域名。不要把 Token 发给别人或写进公开仓库。</p>
    </div>
    <div class="settings-card">
      <div class="card-heading">
        <h2>服务配置</h2>
        <p>以下为当前服务的生效配置。</p>
      </div>
      <dl class="config-list">
        <div><dt>站点名称</dt><dd>{{ config.site_name }}</dd></div>
        <div><dt>分享域名</dt><dd>{{ config.public_base_url || '跟随当前访问域名' }}</dd></div>
        <div><dt>单张上传限制</dt><dd>{{ formatSize(config.max_file_size) }}</dd></div>
        <div><dt>支持的图片格式</dt><dd>JPG、PNG、GIF、WebP</dd></div>
        <div><dt>访问控制</dt><dd>管理员账号登录</dd></div>
        <div><dt>存储用量</dt><dd>{{ allCount }} 张图片 · {{ formatSize(totalSize) }}</dd></div>
      </dl>
      <p class="configuration-note">站点名称写在数据目录的 config.yaml；头像和标签页图标分别替换 avatar.webp、favicon.webp。修改后需重启服务。</p>
    </div>
  </section>
</template>
