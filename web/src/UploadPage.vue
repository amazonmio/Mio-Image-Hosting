<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { ElMessage } from 'element-plus'
import 'element-plus/es/components/message/style/css'
import { UploadFilled, Download, Check, Close } from '@element-plus/icons-vue'
import { upload, type Folder, type Picture } from './api'
import { formatSize, shareText, type LinkFormat } from './share'

type QueueItem = { key: number; file: File; percent: number; status: 'waiting' | 'uploading' | 'done' | 'error'; error?: string; result?: Picture }

const props = defineProps<{
  folders: Folder[]
  authenticated: boolean
  maxFileSize: number
  linkFormat: LinkFormat
}>()
const uploadFolder = defineModel<number>('folder', { default: 0 })
const emit = defineEmits<{ uploaded: []; 'update:uploading': [value: boolean]; copy: [text: string] }>()

const fileInput = ref<HTMLInputElement>()
const dragging = ref(false)
const uploading = ref(false)
const queue = ref<QueueItem[]>([])
let key = 0, generation = 0
let controller: AbortController | undefined

function addFiles(files: FileList | File[]) {
  for (const file of Array.from(files)) {
    if (!/\.(jpe?g|png|gif|webp)$/i.test(file.name) || !file.size || file.size > props.maxFileSize) {
      ElMessage.warning(`${file.name}：仅支持 JPG、PNG、GIF、WebP，单张不超过 20 MB`)
      continue
    }
    queue.value.push({ key: ++key, file, percent: 0, status: 'waiting' })
  }
}
function drop(event: DragEvent) {
  dragging.value = false
  if (!uploading.value && event.dataTransfer?.files) addFiles(event.dataTransfer.files)
}
function inputFiles(event: Event) {
  const input = event.target as HTMLInputElement
  if (input.files) addFiles(input.files)
  input.value = ''
}
async function startUpload() {
  if (uploading.value) return
  const run = ++generation
  uploading.value = true
  emit('update:uploading', true)
  let success = 0
  const target = uploadFolder.value
  try {
    for (const item of queue.value) {
      if (run !== generation || !props.authenticated) break
      if (item.status === 'done') continue
      item.status = 'uploading'
      item.percent = 0
      item.error = undefined
      try {
        controller = new AbortController()
        item.result = await upload(item.file, target, value => { item.percent = value }, controller.signal)
        if (run !== generation) break
        item.status = 'done'
        success++
      } catch (e) {
        if (run !== generation) break
        item.status = 'error'
        item.error = e instanceof Error ? e.message : '上传失败'
      }
    }
    if (success && run === generation) {
      ElMessage.success(`已上传 ${success} 张图片`)
      emit('uploaded')
    }
  } finally {
    if (run === generation) { controller = undefined; uploading.value = false; emit('update:uploading', false) }
  }
}

function reset() {
  ++generation
  controller?.abort(); controller = undefined
  queue.value = []; uploading.value = false
  emit('update:uploading', false)
}
onBeforeUnmount(reset)
defineExpose({ reset })
</script>

<template>
  <section class="upload-page" aria-label="上传图片内容">
    <div class="upload-panel">
      <div class="card-heading">
        <h2>添加图片</h2>
        <p>选择保存位置，再添加你想分享的图片。</p>
      </div>
      <label class="field-label">保存到文件夹</label>
      <el-select v-model="uploadFolder" :disabled="uploading" class="upload-destination" aria-label="上传目标文件夹" placeholder="未分类">
        <el-option label="未分类" :value="0" />
        <el-option v-for="folder in folders" :key="folder.id" :label="folder.name" :value="folder.id" />
      </el-select>
      <input ref="fileInput" type="file" multiple accept="image/jpeg,image/png,image/gif,image/webp" hidden @change="inputFiles" />
      <button :class="['dropzone', { dragging }]" :disabled="uploading" @click="fileInput?.click()" @dragover.prevent="dragging = true" @dragleave.prevent="dragging = false" @drop.prevent="drop">
        <el-icon><UploadFilled /></el-icon>
        <strong>拖拽图片到这里，或<span>点击选择</span></strong>
        <small>支持 JPG、PNG、GIF、WebP · 单张最大 20 MB</small>
      </button>
      <div class="upload-controls">
        <span>{{ queue.filter(item => item.status === 'done').length }} / {{ queue.length }} 张已完成</span>
        <el-button :disabled="uploading || !queue.length" @click="queue = []">清空列表</el-button>
        <el-button type="primary" :loading="uploading" :disabled="!queue.some(item => item.status !== 'done')" @click="startUpload">{{ queue.some(item => item.status === 'error') ? '重试失败项' : '开始上传' }}</el-button>
      </div>
    </div>
    <div v-if="queue.length" class="queue-panel">
      <div class="card-heading">
        <h2>上传列表</h2>
        <p>上传完成后，可直接复制链接或下载原图。</p>
      </div>
      <div class="upload-queue">
        <div v-for="item in queue" :key="item.key" class="queue-item">
          <div class="queue-info">
            <span :title="item.file.name">{{ item.file.name }}</span>
            <small>{{ formatSize(item.file.size) }}</small>
            <el-button v-if="item.result" text type="primary" @click="emit('copy', shareText(item.result.name, item.result.url, linkFormat))">复制链接</el-button>
            <a v-if="item.result" :href="'/download/' + item.result.id" class="download-button" aria-label="下载已上传图片"><el-icon><Download /></el-icon></a>
            <el-icon v-if="item.status === 'done'" color="#299c76"><Check /></el-icon>
            <el-button v-else-if="!uploading" text circle size="small" aria-label="移除待上传图片" @click="queue = queue.filter(entry => entry.key !== item.key)"><el-icon><Close /></el-icon></el-button>
          </div>
          <el-progress v-if="item.status === 'uploading'" :percentage="item.percent" :stroke-width="4" />
          <small v-if="item.status === 'error'" class="danger-text">{{ item.error }}</small>
          <small v-else-if="item.status === 'done'" class="success-text">上传完成</small>
        </div>
      </div>
    </div>
  </section>
</template>
