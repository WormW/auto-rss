<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px">
      <h2>RSS 源管理</h2>
      <n-button type="primary" @click="showCreateModal = true">
        添加 RSS 源
      </n-button>
    </n-space>

    <n-data-table
      :columns="columns"
      :data="sources"
      :loading="loading"
      :pagination="pagination"
    />

    <!-- 添加/编辑 RSS 源对话框 -->
    <n-modal v-model:show="showCreateModal" preset="dialog" title="添加 RSS 源">
      <n-form ref="formRef" :model="formData">
        <n-form-item label="名称" path="name">
          <n-input v-model:value="formData.name" placeholder="如：Mikanani" />
        </n-form-item>
        <n-form-item label="RSS 地址" path="base_url">
          <n-input v-model:value="formData.base_url" placeholder="https://..." />
        </n-form-item>
        <n-form-item label="描述" path="description">
          <n-input
            v-model:value="formData.description"
            type="textarea"
            placeholder="可选"
          />
        </n-form-item>
        <n-form-item label="启用" path="enabled">
          <n-switch v-model:value="formData.enabled" />
        </n-form-item>
      </n-form>
      <template #action>
        <n-space>
          <n-button @click="showCreateModal = false">取消</n-button>
          <n-button type="primary" @click="handleCreate">确定</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- 查看番剧列表对话框 -->
    <n-modal
      v-model:show="showAnimesModal"
      preset="dialog"
      :title="`${currentSource?.name} - 番剧列表`"
      style="width: 80%; max-width: 1200px"
    >
      <n-spin :show="animesLoading">
        <n-list bordered>
          <n-list-item v-for="anime in animes" :key="anime.title">
            <template #prefix>
              <n-tag type="info" size="small">{{ anime.fansub }}</n-tag>
            </template>
            <n-thing :title="anime.title">
              <template #description>
                <n-space>
                  <span>更新日期: {{ anime.update_day || '未知' }}</span>
                  <span>集数: {{ anime.episodes.join(', ') || '暂无' }}</span>
                </n-space>
              </template>
            </n-thing>
            <template #suffix>
              <n-button size="small" @click="handleSubscribeAnime(anime)">
                订阅
              </n-button>
            </template>
          </n-list-item>
        </n-list>
      </n-spin>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import {
  NButton,
  NDataTable,
  NModal,
  NForm,
  NFormItem,
  NInput,
  NSpace,
  NSwitch,
  NTag,
  NList,
  NListItem,
  NThing,
  NSpin,
  useMessage,
  useDialog
} from 'naive-ui'
import { rssSourceApi, type RSSSource, type RSSAnime } from '@/api'
import { useRouter } from 'vue-router'

const router = useRouter()
const message = useMessage()
const dialog = useDialog()
const loading = ref(false)
const sources = ref<RSSSource[]>([])
const showCreateModal = ref(false)
const showAnimesModal = ref(false)
const animesLoading = ref(false)
const animes = ref<RSSAnime[]>([])
const currentSource = ref<RSSSource | null>(null)

const formData = ref({
  name: '',
  base_url: '',
  description: '',
  enabled: true
})

const pagination = ref({
  page: 1,
  pageSize: 20,
  itemCount: 0,
  onChange: (page: number) => {
    pagination.value.page = page
    loadSources()
  }
})

const columns = [
  { title: 'ID', key: 'id', width: 60 },
  { title: '名称', key: 'name', width: 150 },
  { title: 'RSS 地址', key: 'base_url', ellipsis: { tooltip: true } },
  { title: '描述', key: 'description', width: 200, ellipsis: { tooltip: true } },
  {
    title: '状态',
    key: 'enabled',
    width: 100,
    render: (row: RSSSource) => {
      return h(
        NTag,
        { type: row.enabled ? 'success' : 'default', size: 'small' },
        { default: () => (row.enabled ? '启用' : '禁用') }
      )
    }
  },
  {
    title: '操作',
    key: 'actions',
    width: 200,
    render: (row: RSSSource) => {
      return h(NSpace, null, {
        default: () => [
          h(
            NButton,
            { size: 'small', onClick: () => handleViewAnimes(row) },
            { default: () => '查看番剧' }
          ),
          h(
            NButton,
            { size: 'small', onClick: () => handleDelete(row.id) },
            { default: () => '删除' }
          )
        ]
      })
    }
  }
]

const loadSources = async () => {
  loading.value = true
  try {
    const res: any = await rssSourceApi.list(pagination.value.page, pagination.value.pageSize)
    sources.value = res.data?.list || []
    pagination.value.itemCount = res.data?.total || 0
  } catch (error) {
    message.error('加载 RSS 源列表失败')
    sources.value = []
  } finally {
    loading.value = false
  }
}

const handleCreate = async () => {
  try {
    await rssSourceApi.create(formData.value)
    message.success('添加成功')
    showCreateModal.value = false
    formData.value = {
      name: '',
      base_url: '',
      description: '',
      enabled: true
    }
    loadSources()
  } catch (error) {
    message.error('添加失败')
  }
}

const handleDelete = async (id: number) => {
  dialog.warning({
    title: '确认删除',
    content: '确定要删除这个 RSS 源吗？此操作不可恢复。',
    positiveText: '确定',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await rssSourceApi.delete(id)
        message.success('删除成功')
        loadSources()
      } catch (error) {
        message.error('删除失败')
      }
    }
  })
}

const handleViewAnimes = async (source: RSSSource) => {
  currentSource.value = source
  showAnimesModal.value = true
  animesLoading.value = true

  try {
    const res: any = await rssSourceApi.fetchAnimes(source.id)
    animes.value = res.data || []
  } catch (error) {
    message.error('获取番剧列表失败')
    animes.value = []
  } finally {
    animesLoading.value = false
  }
}

const handleSubscribeAnime = (anime: RSSAnime) => {
  showAnimesModal.value = false
  // 跳转到订阅页面并传递参数
  router.push({
    name: 'subscriptions',
    query: {
      from_rss: 'true',
      rss_url: anime.rss_url,
      name: anime.title,
      fansub: anime.fansub,
      rss_source_id: anime.source_id.toString()
    }
  })
}

onMounted(() => {
  loadSources()
})
</script>
