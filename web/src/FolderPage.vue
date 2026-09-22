<script setup lang="ts">
import { computed } from 'vue'
import { Picture as PictureIcon, Folder as FolderIcon, UploadFilled, Search, Link, Download, MoreFilled, Refresh } from '@element-plus/icons-vue'
import type { Folder, ImageList, Picture } from './api'
import { folderTitle, formatSize, markdown, shareText, type LinkFormat } from './share'

const props = defineProps<{
  folders: Folder[]
  data: ImageList
  currentFolder: number | null
  page: number
  search: string
  loading: boolean
  error: string
  selected: string[]
  mutating: boolean
  linkFormat: LinkFormat
}>()
const emit = defineEmits<{
  navigate: [id: number | null]
  load: []
  'update:search': [value: string]
  'update:page': [value: number]
  'update:selected': [value: string[]]
  editFolder: [folder: Folder]
  deleteFolder: [folder: Folder]
  openUpload: []
  copy: [text: string]
  openMove: [ids: string[]]
  deleteImages: [ids: string[]]
  renameImage: [picture: Picture]
}>()

const title = computed(() => folderTitle(props.currentFolder, props.folders))
const activeFolder = computed(() => props.folders.find(folder => folder.id === props.currentFolder))
const previewList = computed(() => props.data.items.map(item => item.url))
function thumbSrc(picture: Picture) {
  return picture.thumb_url || picture.url
}

function toggle(id: string, checked: unknown) {
  emit('update:selected', checked ? [...props.selected, id] : props.selected.filter(value => value !== id))
}
function selectAll(checked: unknown) {
  emit('update:selected', checked ? props.data.items.map(item => item.id) : [])
}
function imageCommand(command: string, picture: Picture) {
  if (command === 'markdown') emit('copy', markdown(picture.name, picture.url))
  else if (command === 'rename') emit('renameImage', picture)
  else if (command === 'move') emit('openMove', [picture.id])
  else if (command === 'delete') emit('deleteImages', [picture.id])
}
</script>

<template>
  <section class="folder-page" aria-label="文件夹管理内容">
    <div class="folder-grid">
      <button :class="['folder-card', { active: currentFolder === null }]" @click="emit('navigate', null)">
        <el-icon><PictureIcon /></el-icon><strong>全部图片</strong><span>{{ data.all_count }} 张图片</span>
      </button>
      <button :class="['folder-card', { active: currentFolder === 0 }]" @click="emit('navigate', 0)">
        <el-icon><FolderIcon /></el-icon><strong>未分类</strong><span>{{ data.uncategorized_count }} 张图片</span>
      </button>
      <button v-for="folder in folders" :key="folder.id" :class="['folder-card', { active: currentFolder === folder.id }]" @click="emit('navigate', folder.id)">
        <el-icon><FolderIcon /></el-icon><strong :title="folder.name">{{ folder.name }}</strong><span>{{ folder.count }} 张图片</span>
      </button>
    </div>
    <div class="library-heading">
      <h2>{{ title }}</h2>
      <el-button :icon="UploadFilled" @click="emit('openUpload')">上传到此处</el-button>
    </div>
    <div class="toolbar">
      <div class="toolbar-left">
        <span class="image-count">{{ data.total }} 张图片</span>
        <el-button :icon="Refresh" text :loading="loading" aria-label="刷新图片" @click="emit('load')" />
        <template v-if="activeFolder">
          <el-button text @click="emit('editFolder', activeFolder)">重命名</el-button>
          <el-button text type="danger" @click="emit('deleteFolder', activeFolder)">删除文件夹</el-button>
        </template>
      </div>
      <el-input :model-value="search" :prefix-icon="Search" clearable placeholder="搜索图片名称…" aria-label="搜索图片名称" class="search-input" @update:model-value="value => emit('update:search', String(value))" />
    </div>
    <div v-if="data.items.length && !error" class="selection-bar">
      <el-checkbox :model-value="selected.length === data.items.length" :indeterminate="selected.length > 0 && selected.length < data.items.length" @change="selectAll">{{ selected.length ? `已选择 ${selected.length} 张` : '选择本页' }}</el-checkbox>
      <template v-if="selected.length">
        <el-button text size="small" @click="emit('copy', data.items.filter(item => selected.includes(item.id)).map(item => shareText(item.name, item.url, linkFormat)).join('\n'))">复制链接</el-button>
        <el-button text size="small" :disabled="mutating" @click="emit('openMove', selected)">移动到</el-button>
        <el-button text type="danger" size="small" :disabled="mutating" @click="emit('deleteImages', [...selected])">删除</el-button>
      </template>
    </div>
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false">
      <el-button text @click="emit('load')">重新加载</el-button>
    </el-alert>
    <div v-else-if="loading && !data.items.length" class="loading-state"><el-skeleton :rows="6" animated /></div>
    <div v-else-if="!data.items.length" class="empty-state">
      <div class="empty-art">
        <span class="empty-art-back"></span>
        <div class="empty-art-front"><el-icon><PictureIcon /></el-icon><span class="empty-art-dot"></span></div>
      </div>
      <h2>{{ search ? '没有找到匹配的图片' : '给灵感一个安放的地方' }}</h2>
      <p>{{ search ? '试试其他关键词，或清空搜索查看所有图片。' : '上传第一张图片，开启你的轻量图片空间。' }}</p>
      <el-button v-if="search" @click="emit('update:search', '')">清空搜索</el-button>
      <el-button v-else type="primary" :icon="UploadFilled" @click="emit('openUpload')">上传第一张图片</el-button>
      <span v-if="!search" class="empty-hint">支持拖拽或粘贴上传 · 单张最大 20 MB</span>
    </div>
    <div v-else class="image-grid" :aria-busy="loading">
      <article v-for="(picture, index) in data.items" :key="picture.id" :class="['image-card', { selected: selected.includes(picture.id) }]">
        <div class="image-preview">
          <el-image :src="thumbSrc(picture)" :alt="picture.name" fit="cover" loading="lazy" :preview-src-list="previewList" :initial-index="index" preview-teleported>
            <template #error>
              <div class="preview-error"><el-icon><PictureIcon /></el-icon><span>图片加载失败</span></div>
            </template>
          </el-image>
          <el-checkbox class="image-checkbox" :model-value="selected.includes(picture.id)" :aria-label="`选择 ${picture.name}`" @change="(value: unknown) => toggle(picture.id, value)" />
          <span class="file-type">{{ picture.mime.split('/')[1]?.toUpperCase() }}</span>
        </div>
        <div class="image-info">
          <h3 :title="picture.name">{{ picture.name }}</h3>
          <div class="image-meta"><span>{{ picture.width }} × {{ picture.height }}</span><span>{{ formatSize(picture.size) }}</span></div>
          <div class="image-actions">
            <el-button text size="small" :icon="Link" @click="emit('copy', shareText(picture.name, picture.url, linkFormat))">复制链接</el-button>
            <a :href="`/download/${picture.id}`" :aria-label="`下载 ${picture.name}`" title="下载原图" class="download-button"><el-icon><Download /></el-icon></a>
            <el-dropdown trigger="click" @command="(command: string) => imageCommand(command, picture)">
              <el-button text circle :aria-label="`${picture.name} 的更多操作`"><el-icon><MoreFilled /></el-icon></el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="markdown">复制 Markdown</el-dropdown-item>
                  <el-dropdown-item command="rename" :disabled="mutating">重命名</el-dropdown-item>
                  <el-dropdown-item command="move">移动到文件夹</el-dropdown-item>
                  <el-dropdown-item command="delete" divided :disabled="mutating"><span class="danger-text">删除图片</span></el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </div>
      </article>
    </div>
    <div v-if="data.total > 48" class="pagination">
      <el-pagination :current-page="page" :page-size="48" :total="data.total" layout="prev, pager, next" background @update:current-page="value => emit('update:page', value)" />
    </div>
  </section>
</template>
