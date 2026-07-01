<template>
  <div class="subscriptions-page">
    <!-- 统计概览 -->
    <div class="stats-overview">
      <n-card class="stat-card" size="small">
        <div class="stat-value">{{ subscriptions.length }}</div>
        <div class="stat-label">总订阅</div>
      </n-card>
      <n-card class="stat-card" size="small">
        <div class="stat-value" style="color: #18a058;">{{ activeCount }}</div>
        <div class="stat-label">连载中</div>
      </n-card>
      <n-card class="stat-card" size="small">
        <div class="stat-value" style="color: #2080f0;">{{ todayUpdateCount }}</div>
        <div class="stat-label">今日更新</div>
      </n-card>
      <n-card class="stat-card" size="small">
        <div class="stat-value" style="color: #f0a020;">{{ downloadingCount }}</div>
        <div class="stat-label">下载中</div>
      </n-card>
      <n-card class="stat-card" size="small">
        <div class="stat-value" style="color: #d03050;">{{ missingEpisodesCount }}</div>
        <div class="stat-label">有缺失</div>
      </n-card>
    </div>

    <!-- 今日更新专区 -->
    <div v-if="todayUpdates.length > 0" class="today-updates-section">
      <div class="section-header">
        <h3>
          <n-icon size="18" color="#18a058"><CalendarOutlined /></n-icon>
          今日更新 ({{ todayUpdates.length }})
        </h3>
        <n-tag type="success" size="small" v-if="todayPendingCount > 0">
          {{ todayPendingCount }} 个待下载
        </n-tag>
      </div>
      <div class="today-grid">
        <n-card
          v-for="sub in todayUpdates"
          :key="sub.id"
          hoverable
          class="today-card"
          :class="{ 'is-downloaded': isTodayDownloaded(sub) }"
        >
          <div class="today-content">
            <img
              v-if="sub.bangumi_cover_local"
              :src="`/covers/${sub.bangumi_cover_local}`"
              :alt="sub.name"
              class="today-cover"
            />
            <div v-else class="today-cover-placeholder">{{ sub.name[0] }}</div>
            <div class="today-info">
              <n-ellipsis class="today-title">{{ sub.name }}</n-ellipsis>
              <div class="today-meta">
                <n-tag v-if="sub.air_time" size="tiny" type="info">
                  {{ formatAirTime(sub) }}
                </n-tag>
                <n-tag size="tiny" :type="isTodayDownloaded(sub) ? 'default' : 'success'">
                  {{ isTodayDownloaded(sub) ? '已下载' : `第${sub.latest_episode}集已更新` }}
                </n-tag>
              </div>
              <div class="today-progress">
                <span>已收集 {{ sub.current_episode || 0 }}/{{ sub.total_episodes || '?' }}</span>
              </div>
            </div>
            <div class="today-actions">
              <n-button
                v-if="!isTodayDownloaded(sub)"
                type="primary"
                size="tiny"
                @click="handleCollectEpisodes(sub.id)"
              >
                下载
              </n-button>
              <n-button v-else text size="tiny" @click="$router.push('/downloads')">
                查看
              </n-button>
            </div>
          </div>
        </n-card>
      </div>
    </div>

    <!-- 筛选栏 -->
    <div class="filter-bar">
      <n-input
        v-model:value="searchQuery"
        placeholder="搜索番剧名称..."
        clearable
        class="search-input"
      >
        <template #prefix>
          <n-icon><SearchOutlined /></n-icon>
        </template>
      </n-input>
      <n-select
        v-model:value="filterStatus"
        :options="statusOptions"
        placeholder="状态"
        clearable
        class="filter-select"
      />
      <n-select
        v-model:value="filterYear"
        :options="yearOptions"
        placeholder="年份"
        clearable
        class="filter-select"
      />
      <n-select
        v-model:value="filterFansub"
        :options="fansubOptions"
        placeholder="字幕组"
        clearable
        class="filter-select"
      />
      <n-button-group>
        <n-button
          :type="viewMode === 'card' ? 'primary' : 'default'"
          size="small"
          @click="viewMode = 'card'"
        >
          <n-icon><AppstoreOutlined /></n-icon>
        </n-button>
        <n-button
          :type="viewMode === 'list' ? 'primary' : 'default'"
          size="small"
          @click="viewMode = 'list'"
        >
          <n-icon><UnorderedListOutlined /></n-icon>
        </n-button>
      </n-button-group>
      <n-button type="primary" size="small" @click="showAddDialog">
        <template #icon>
          <n-icon><PlusOutlined /></n-icon>
        </template>
        添加订阅
      </n-button>
    </div>

    <!-- 批量操作栏 -->
    <div v-if="selectedIds.length > 0" class="batch-bar">
      <span>已选择 {{ selectedIds.length }} 项</span>
      <n-space>
        <n-button size="small" @click="batchToggle(true)">
          <template #icon><n-icon><PlayCircleOutlined /></n-icon></template>
          启用
        </n-button>
        <n-button size="small" @click="batchToggle(false)">
          <template #icon><n-icon><PauseCircleOutlined /></n-icon></template>
          禁用
        </n-button>
        <n-button size="small" @click="batchCollect">
          <template #icon><n-icon><DownloadOutlined /></n-icon></template>
          采集
        </n-button>
        <n-button size="small" type="error" @click="batchDelete">
          <template #icon><n-icon><DeleteOutlined /></n-icon></template>
          删除
        </n-button>
        <n-button text size="small" @click="selectedIds = []">取消</n-button>
      </n-space>
    </div>

    <!-- 订阅列表 - 卡片视图 -->
    <n-spin :show="loading">
      <!-- 卡片视图 -->
      <template v-if="viewMode === 'card'">
        <div v-for="week in filteredWeekList" :key="week.day" class="week-section">
          <div v-if="getSubscriptionsByWeekday(week.day).length > 0">
            <h3 class="week-title">{{ week.label }}</h3>
            <div class="grid-container">
              <n-card
                v-for="sub in getSubscriptionsByWeekday(week.day)"
                :key="sub.id"
                hoverable
                class="anime-card"
                :class="{ 'is-disabled': !sub.enabled, 'has-missing': getMissingEpisodes(sub).length > 0 }"
              >
                <!-- 选择框 -->
                <div class="card-checkbox">
                  <n-checkbox
                    :checked="selectedIds.includes(sub.id)"
                    @update:checked="(val) => toggleSelection(sub.id, val)"
                  />
                </div>

                <div class="card-content">
                  <!-- 封面图 -->
                  <div class="cover-wrapper" @click="showQuickPreview(sub)">
                    <img
                      v-if="sub.bangumi_cover_local"
                      :src="`/covers/${sub.bangumi_cover_local}`"
                      :alt="sub.name"
                      class="cover-img"
                    />
                    <div v-else class="cover-placeholder">{{ sub.name[0] }}</div>
                    <!-- 评分徽章 -->
                    <div v-if="sub.bangumi_score && sub.bangumi_score > 0" class="score-badge">
                      {{ sub.bangumi_score.toFixed(1) }}
                    </div>
                    <!-- 缺失提示 -->
                    <div v-if="getMissingEpisodes(sub).length > 0" class="missing-badge">
                      缺 {{ getMissingEpisodes(sub).length }} 集
                    </div>
                    <!-- 下载中标识 -->
                    <div v-if="(sub.downloading_count || 0) > 0" class="downloading-badge">
                      <n-spin size="small" /> {{ sub.downloading_count || 0 }}
                    </div>
                    <!-- 今日更新标识 -->
                    <div v-if="isTodayUpdate(sub)" class="today-badge">今日</div>
                  </div>

                  <!-- 信息区 -->
                  <div class="info-section">
                    <!-- 标题行 -->
                    <div class="title-row">
                      <n-ellipsis class="title" :tooltip="{ width: 300 }">{{ sub.name }}</n-ellipsis>
                      <n-switch
                        :value="sub.enabled"
                        @update:value="(val) => handleToggle(sub.id, val)"
                        size="small"
                      />
                    </div>

                    <!-- 标签组 -->
                    <div class="tags-row">
                      <n-tag v-if="sub.air_year" size="small" type="primary">
                        {{ getYearSeasonLabel(sub.air_year, sub.air_date) }}
                      </n-tag>
                      <n-tag size="small">S{{ sub.season }}</n-tag>
                      <n-tag v-if="sub.fansub" size="small" type="info">{{ sub.fansub }}</n-tag>
                      <n-tag
                        v-if="sub.bangumi_id"
                        size="small"
                        style="cursor: pointer;"
                        @click="openBangumiPage(sub.bangumi_id)"
                      >BGM</n-tag>
                    </div>

                    <!-- 进度条 -->
                    <div class="progress-row" v-if="sub.current_episode || sub.total_episodes">
                      <n-progress
                        :percentage="getProgressPercent(sub)"
                        :height="6"
                        :border-radius="3"
                        :fill-border-radius="3"
                        :status="isSeasonComplete(sub) ? 'success' : 'default'"
                        :show-indicator="false"
                      />
                      <div class="progress-info">
                        <span>{{ sub.current_episode || 0 }} / {{ sub.total_episodes || '?' }}</span>
                        <span v-if="sub.latest_episode && sub.latest_episode > (sub.current_episode || 0)" class="latest-ep">
                          最新 {{ sub.latest_episode }}
                        </span>
                        <span v-if="getMissingEpisodes(sub).length > 0" class="missing-ep" @click="showMissingEpisodes(sub)">
                          缺失 {{ getMissingEpisodes(sub).join(',') }} 集
                        </span>
                      </div>
                    </div>

                    <!-- RSS检查警告 -->
                    <div v-if="isRssCheckWarning(sub)" class="warning-row">
                      <n-icon size="12" color="#f0a020"><WarningOutlined /></n-icon>
                      <span>{{ getRssCheckWarningText(sub) }}</span>
                    </div>

                    <div v-if="getSmartFetchStatus(sub.id)" class="smart-fetch-row">
                      <div class="smart-fetch-main">
                        <n-tag
                          size="tiny"
                          :type="getSmartFetchTagType(getSmartFetchStatus(sub.id))"
                        >
                          {{ getSmartFetchStatus(sub.id)?.should_fetch ? '本轮拉取' : '本轮跳过' }}
                        </n-tag>
                        <span>{{ getSmartFetchStatus(sub.id)?.explanation }}</span>
                      </div>
                      <span class="smart-fetch-next">
                        {{ formatNextFetch(getSmartFetchStatus(sub.id)?.next_fetch_seconds) }}
                      </span>
                    </div>

                    <!-- 底部操作栏 -->
                    <div class="action-row">
                      <span v-if="sub.last_download_at" class="last-time">{{ formatTime(sub.last_download_at) }}</span>
                      <span v-else-if="sub.last_check_time" class="last-time">检查: {{ formatTime(sub.last_check_time) }}</span>
                      <div class="action-buttons">
                        <n-tooltip trigger="hover">
                          <template #trigger>
                            <n-button text size="small" @click="handleOffsetEdit(sub)">
                              <template #icon><n-icon size="16"><CalculatorOutlined /></n-icon></template>
                            </n-button>
                          </template>
                          调整偏移
                        </n-tooltip>
                        <n-tooltip trigger="hover">
                          <template #trigger>
                            <n-button text size="small" @click="handleCollectEpisodes(sub.id)">
                              <template #icon><n-icon size="16"><DownloadOutlined /></n-icon></template>
                            </n-button>
                          </template>
                          采集剧集
                        </n-tooltip>
                        <n-tooltip trigger="hover">
                          <template #trigger>
                            <n-button text size="small" @click="handleScanFolder(sub)">
                              <template #icon><n-icon size="16"><FolderOpenOutlined /></n-icon></template>
                            </n-button>
                          </template>
                          文件夹扫描
                        </n-tooltip>
                        <n-tooltip trigger="hover">
                          <template #trigger>
                            <n-button text size="small" @click="handleDiagnostics(sub)">
                              <template #icon><n-icon size="16"><ToolOutlined /></n-icon></template>
                            </n-button>
                          </template>
                          健康诊断
                        </n-tooltip>
                        <n-tooltip trigger="hover">
                          <template #trigger>
                            <n-button text size="small" @click="$router.push({ path: '/downloads', query: { sub_id: sub.id } })">
                              <template #icon><n-icon size="16"><FileSearchOutlined /></n-icon></template>
                            </n-button>
                          </template>
                          查看下载
                        </n-tooltip>
                        <n-tooltip trigger="hover">
                          <template #trigger>
                            <n-button text size="small" @click="handleEdit(sub)">
                              <template #icon><n-icon size="16"><EditOutlined /></n-icon></template>
                            </n-button>
                          </template>
                          编辑
                        </n-tooltip>
                        <n-tooltip trigger="hover">
                          <template #trigger>
                            <n-button text size="small" type="error" @click="handleDelete(sub.id)">
                              <template #icon><n-icon size="16"><DeleteOutlined /></n-icon></template>
                            </n-button>
                          </template>
                          删除
                        </n-tooltip>
                      </div>
                    </div>
                  </div>
                </div>
              </n-card>
            </div>
          </div>
        </div>

        <!-- 已完结番剧 -->
        <div v-if="filteredCompletedSubscriptions.length > 0" class="completed-section">
          <h3 class="week-title">已完结</h3>
          <div class="grid-container">
            <n-card
              v-for="sub in filteredCompletedSubscriptions"
              :key="sub.id"
              hoverable
              class="anime-card"
              :class="{ 'is-disabled': !sub.enabled }"
            >
              <div class="card-checkbox">
                <n-checkbox
                  :checked="selectedIds.includes(sub.id)"
                  @update:checked="(val) => toggleSelection(sub.id, val)"
                />
              </div>

              <div class="card-content">
                <div class="cover-wrapper">
                  <img
                    v-if="sub.bangumi_cover_local"
                    :src="`/covers/${sub.bangumi_cover_local}`"
                    :alt="sub.name"
                    class="cover-img"
                  />
                  <div v-else class="cover-placeholder">{{ sub.name[0] }}</div>
                  <div v-if="sub.bangumi_score && sub.bangumi_score > 0" class="score-badge">
                    {{ sub.bangumi_score.toFixed(1) }}
                  </div>
                </div>

                <div class="info-section">
                  <div class="title-row">
                    <n-ellipsis class="title">{{ sub.name }}</n-ellipsis>
                    <n-tag size="small" type="default">完结</n-tag>
                  </div>

                  <div class="tags-row">
                    <n-tag v-if="sub.air_year" size="small" type="primary">
                      {{ getYearSeasonLabel(sub.air_year, sub.air_date) }}
                    </n-tag>
                    <n-tag size="small">S{{ sub.season }}</n-tag>
                    <n-tag v-if="sub.fansub" size="small" type="info">{{ sub.fansub }}</n-tag>
                  </div>

                  <div class="progress-row">
                    <n-progress
                      :percentage="getProgressPercent(sub)"
                      :height="6"
                      :border-radius="3"
                      status="success"
                      :show-indicator="false"
                    />
                    <div class="progress-info">
                      <span :style="{ color: isSeasonComplete(sub) ? '#18a058' : '' }">
                        {{ sub.current_episode || 0 }} / {{ sub.total_episodes || '?' }}
                      </span>
                    </div>
                  </div>

                  <div v-if="getSmartFetchStatus(sub.id)" class="smart-fetch-row compact">
                    <div class="smart-fetch-main">
                      <n-tag
                        size="tiny"
                        :type="getSmartFetchTagType(getSmartFetchStatus(sub.id))"
                      >
                        {{ getSmartFetchStatus(sub.id)?.should_fetch ? '本轮拉取' : '本轮跳过' }}
                      </n-tag>
                      <span>{{ getSmartFetchStatus(sub.id)?.explanation }}</span>
                    </div>
                  </div>

                  <div class="action-row">
                    <span v-if="sub.last_download_at" class="last-time">{{ formatTime(sub.last_download_at) }}</span>
                    <div class="action-buttons">
                      <n-tooltip trigger="hover">
                        <template #trigger>
                          <n-button text size="small" @click="handleDiagnostics(sub)">
                            <template #icon><n-icon size="16"><ToolOutlined /></n-icon></template>
                          </n-button>
                        </template>
                        健康诊断
                      </n-tooltip>
                      <n-button text size="small" @click="$router.push({ path: '/downloads', query: { sub_id: sub.id } })">
                        <template #icon><n-icon size="16"><FileSearchOutlined /></n-icon></template>
                      </n-button>
                      <n-button text size="small" @click="handleScanFolder(sub)">
                        <template #icon><n-icon size="16"><FolderOpenOutlined /></n-icon></template>
                      </n-button>
                      <n-button text size="small" @click="handleEdit(sub)">
                        <template #icon><n-icon size="16"><EditOutlined /></n-icon></template>
                      </n-button>
                      <n-button text size="small" type="error" @click="handleDelete(sub.id)">
                        <template #icon><n-icon size="16"><DeleteOutlined /></n-icon></template>
                      </n-button>
                    </div>
                  </div>
                </div>
              </div>
            </n-card>
          </div>
        </div>
      </template>

      <!-- 列表视图 -->
      <template v-else>
        <n-data-table
          :columns="listColumns"
          :data="filteredAllSubscriptions"
          :row-key="row => row.id"
          :pagination="{ pageSize: 20 }"
          @update:checked-row-keys="handleCheck"
        />
      </template>

      <!-- 空状态 -->
      <n-empty v-if="filteredAllSubscriptions.length === 0 && !loading" description="暂无订阅">
        <template #extra>
          <n-button size="small" @click="showAddDialog">添加第一个订阅</n-button>
        </template>
      </n-empty>
    </n-spin>

    <!-- 番剧搜索组件 -->
    <AnimeSearch ref="animeSearchRef" @subscribe="handleSearchSubscribe" />

    <!-- 偏移量快速编辑弹窗 -->
    <n-modal v-model:show="showOffsetModal" preset="dialog" title="调整集数偏移">
      <div style="padding: 16px 0;">
        <p>当前偏移: {{ offsetEditingSub?.episode_offset || 0 }}</p>
        <p style="color: #666; font-size: 12px;">正数表示跳过前几集，负数表示从负数开始计数</p>
        <n-input-number v-model:value="tempOffset" style="width: 100%; margin-top: 12px;" />
      </div>
      <template #action>
        <n-button @click="showOffsetModal = false">取消</n-button>
        <n-button type="primary" @click="saveOffset">保存</n-button>
      </template>
    </n-modal>

    <!-- 缺失剧集弹窗 -->
    <n-modal v-model:show="showMissingModal" preset="card" title="缺失剧集" style="width: 400px;">
      <div v-if="missingEpisodesSub">
        <p>{{ missingEpisodesSub.name }}</p>
        <p style="color: #666; font-size: 14px;">以下剧集在RSS中已发布但尚未下载:</p>
        <n-space style="margin-top: 12px;">
          <n-tag v-for="ep in getMissingEpisodes(missingEpisodesSub)" :key="ep" type="warning">
            第 {{ ep }} 集
          </n-tag>
        </n-space>
        <div style="margin-top: 16px; text-align: right;">
          <n-button type="primary" size="small" @click="handleCollectEpisodes(missingEpisodesSub.id); showMissingModal = false;">
            一键补下载
          </n-button>
        </div>
      </div>
    </n-modal>

    <!-- 番剧简介预览弹窗 -->
    <n-modal v-model:show="showPreviewModal" preset="card" :title="previewSub?.name" style="width: 500px; max-width: 90vw;">
      <div v-if="previewSub" class="preview-content">
        <div class="preview-cover">
          <img v-if="previewSub.bangumi_cover_local" :src="`/covers/${previewSub.bangumi_cover_local}`" />
          <div v-else class="preview-cover-placeholder">{{ previewSub.name[0] }}</div>
        </div>
        <div class="preview-info">
          <p v-if="previewSub.bangumi_summary" class="preview-summary">{{ previewSub.bangumi_summary }}</p>
          <p v-else class="preview-summary" style="color: #999;">暂无简介</p>
          <div class="preview-meta">
            <n-tag v-if="previewSub.bangumi_score">评分: {{ previewSub.bangumi_score }}</n-tag>
            <n-tag v-if="previewSub.bangumi_rank">排名: {{ previewSub.bangumi_rank }}</n-tag>
          </div>
        </div>
      </div>
    </n-modal>

    <!-- 文件夹扫描弹窗 -->
    <n-modal v-model:show="showScanModal" preset="card" :title="`文件夹扫描 - ${scanSub?.name || ''}`" style="width: 700px; max-width: 95vw;">
      <div v-if="scanSub">
        <!-- 输入区域 -->
        <n-form label-placement="left" label-width="90px" v-if="!scanResult || scanLoading">
          <n-form-item label="文件夹路径">
            <n-input v-model:value="scanFolderPath" placeholder="/downloads/番剧名 S01" />
          </n-form-item>
          <n-form-item label="预览模式">
            <n-switch v-model:value="scanDryRun" />
            <span style="margin-left: 8px; font-size: 13px; color: #666;">{{ scanDryRun ? '仅预览，不修改文件' : '执行重命名并写入数据库' }}</span>
          </n-form-item>
          <n-form-item label="重命名文件">
            <n-switch v-model:value="scanRenameFiles" :disabled="!scanDryRun && scanRenameFiles === false ? false : false" />
            <span style="margin-left: 8px; font-size: 13px; color: #666;">扫描后按模板重命名到目标位置</span>
          </n-form-item>
          <n-form-item>
            <n-space>
              <n-button type="primary" :loading="scanLoading" @click="doScanFolder">
                {{ scanDryRun ? '预览扫描' : '开始扫描' }}
              </n-button>
              <n-button @click="showScanModal = false; scanResult = null">取消</n-button>
            </n-space>
          </n-form-item>
        </n-form>

        <!-- 扫描结果 -->
        <div v-if="scanResult && !scanLoading" class="scan-result">
          <!-- 统计概览 -->
          <div class="scan-stats">
            <n-tag type="info">扫描 {{ scanResult.scanned }} 个文件</n-tag>
            <n-tag type="success">匹配 {{ scanResult.matched }} 集</n-tag>
            <n-tag v-if="scanResult.orphan > 0" type="warning">未识别 {{ scanResult.orphan }} 个</n-tag>
            <n-tag v-if="scanResult.rename_count > 0" type="info">将重命名 {{ scanResult.rename_count }} 个</n-tag>
            <n-tag v-if="scanResult.renamed_count > 0" type="success">已重命名 {{ scanResult.renamed_count }} 个</n-tag>
          </div>

          <!-- 大小统计 -->
          <div v-if="scanResult.stats" class="scan-stats" style="margin-top: 8px;">
            <span style="font-size: 13px; color: #666;">总大小: {{ scanResult.stats.total_size_gb?.toFixed(1) }} GB</span>
          </div>

          <!-- 集数信息 -->
          <div v-if="scanResult.episodes_on_disk?.length" class="scan-episodes" style="margin-top: 12px;">
            <div><strong>已有集数:</strong> {{ scanResult.episodes_on_disk.join(', ') }}</div>
            <div v-if="scanResult.missing_episodes?.length" style="color: #d03050; margin-top: 4px;">
              <strong>缺失集数:</strong> {{ scanResult.missing_episodes.join(', ') }}
            </div>
          </div>

          <!-- 文件列表 -->
          <div v-if="scanResult.files?.length" class="scan-files" style="margin-top: 12px; max-height: 300px; overflow-y: auto;">
            <div v-for="(f, idx) in scanResult.files" :key="idx" class="scan-file-item" :class="{ 'is-orphan': f.episode <= 0 }">
              <div class="file-episode">
                <n-tag v-if="f.episode > 0" size="small" type="success">第{{ f.episode }}集</n-tag>
                <n-tag v-else size="small" type="default">未识别</n-tag>
              </div>
              <div class="file-info">
                <div class="file-name">{{ fileBaseName(f.path) }}</div>
                <div class="file-meta">
                  <n-tag v-if="f.resolution" size="tiny">{{ f.resolution }}</n-tag>
                  <n-tag v-if="f.video_codec" size="tiny" type="info">{{ f.video_codec }}</n-tag>
                  <n-tag v-if="f.fansub" size="tiny" type="warning">{{ f.fansub }}</n-tag>
                  <span style="font-size: 11px; color: #999; margin-left: 4px;">{{ f.size_mb?.toFixed(1) }} MB</span>
                </div>
                <div v-if="f.rename_to && f.will_rename" class="file-rename-preview" style="font-size: 11px; color: #18a058; margin-top: 2px;">
                  → {{ fileBaseName(f.rename_to) }}
                </div>
                <div v-if="f.renamed" class="file-renamed" style="font-size: 11px; color: #2080f0; margin-top: 2px;">
                  ✓ 已重命名
                </div>
                <div v-if="f.rename_error" class="file-rename-error" style="font-size: 11px; color: #d03050; margin-top: 2px;">
                  ✗ {{ f.rename_error }}
                </div>
              </div>
            </div>
          </div>

          <!-- 操作按钮 -->
          <div style="margin-top: 16px;">
            <n-space>
              <n-button v-if="scanResult.dry_run" type="primary" :loading="scanLoading" @click="scanDryRun = false; doScanFolder()">
                确认并执行
              </n-button>
              <n-button @click="showScanModal = false; scanResult = null; scanDryRun = true">关闭</n-button>
            </n-space>
          </div>
        </div>
      </div>
    </n-modal>

    <!-- 健康诊断面板 -->
    <n-modal v-model:show="showDiagnosticsModal" preset="card" :title="`健康诊断 - ${diagnosticsSub?.name || ''}`" style="width: 900px; max-width: 96vw;">
      <n-spin :show="diagnosticsLoading">
        <div v-if="diagnosticsData" class="diagnostics-panel">
          <div class="diagnostics-header">
            <div>
              <div class="diagnostics-title-row">
                <n-tag :type="getDiagnosticTagType(diagnosticsData.summary.overall)">
                  {{ getDiagnosticStatusLabel(diagnosticsData.summary.overall) }}
                </n-tag>
                <span class="diagnostics-checked">检查于 {{ formatTime(diagnosticsData.checked_at) }}</span>
              </div>
              <div class="diagnostics-counters">
                <span>正常 {{ diagnosticsData.summary.healthy }}</span>
                <span>警告 {{ diagnosticsData.summary.warning }}</span>
                <span>异常 {{ diagnosticsData.summary.error }}</span>
                <span>未知 {{ diagnosticsData.summary.unknown }}</span>
              </div>
            </div>
            <n-button size="small" @click="refreshDiagnostics" :loading="diagnosticsLoading">
              刷新
            </n-button>
          </div>

          <div class="diagnostics-grid">
            <div
              v-for="check in diagnosticsData.checks"
              :key="check.key"
              class="diagnostic-check"
              :class="`diagnostic-${check.status}`"
            >
              <div class="diagnostic-check-head">
                <span>{{ check.label }}</span>
                <n-tag size="tiny" :type="getDiagnosticTagType(check.status)">
                  {{ getDiagnosticStatusLabel(check.status) }}
                </n-tag>
              </div>
              <div class="diagnostic-summary">{{ check.summary }}</div>
              <div class="diagnostic-detail">{{ check.detail }}</div>
            </div>
          </div>

          <div class="diagnostics-metrics">
            <div class="diagnostic-metric">
              <span>下载任务</span>
              <strong>{{ diagnosticsData.downloads.total }}</strong>
              <small>失败 {{ diagnosticsData.downloads.failed }} / 停滞 {{ diagnosticsData.downloads.stalled }}</small>
            </div>
            <div class="diagnostic-metric">
              <span>本地文件</span>
              <strong>{{ diagnosticsData.files.completed_with_file }}</strong>
              <small>缺路径 {{ diagnosticsData.files.completed_missing_file }}</small>
            </div>
            <div class="diagnostic-metric">
              <span>磁盘剩余</span>
              <strong>{{ formatBytes(diagnosticsData.disk.free_bytes) }}</strong>
              <small>{{ diagnosticsData.disk.path }}</small>
            </div>
            <div class="diagnostic-metric">
              <span>缺失集数</span>
              <strong>{{ diagnosticsData.files.missing_episodes.length }}</strong>
              <small>{{ diagnosticsData.files.missing_episodes.length ? diagnosticsData.files.missing_episodes.join(', ') : '无' }}</small>
            </div>
          </div>

          <div v-if="diagnosticsData.downloads.failed_items.length" class="diagnostic-failures">
            <div class="diagnostic-section-title">异常下载</div>
            <div
              v-for="item in diagnosticsData.downloads.failed_items"
              :key="item.id"
              class="diagnostic-failure-item"
            >
              <div class="diagnostic-failure-main">
                <div class="diagnostic-failure-title">{{ item.title || `下载 #${item.id}` }}</div>
                <div class="diagnostic-failure-meta">
                  <n-tag size="tiny" type="default">第 {{ item.episode || '?' }} 集</n-tag>
                  <n-tag size="tiny" :type="item.status === 'failed' ? 'error' : 'warning'">{{ item.status }}</n-tag>
                  <n-tag size="tiny" type="info">{{ item.category }}</n-tag>
                </div>
              </div>
              <div class="diagnostic-failure-reason">{{ item.reason }}</div>
            </div>
          </div>

          <div class="diagnostic-actions">
            <n-button
              v-for="action in diagnosticsData.actions"
              :key="action.key"
              size="small"
              :type="getDiagnosticActionType(action.key)"
              :disabled="!action.enabled || diagnosticsActionLoading === action.key"
              :loading="diagnosticsActionLoading === action.key"
              @click="runDiagnosticAction(action)"
            >
              {{ action.label }}
            </n-button>
          </div>
          <div v-if="diagnosticsData.actions.some(action => !action.enabled && action.reason)" class="diagnostic-action-reasons">
            <span v-for="action in diagnosticsData.actions.filter(action => !action.enabled && action.reason)" :key="action.key">
              {{ action.label }}：{{ action.reason }}
            </span>
          </div>

          <div v-if="diagnosticsActionResult" class="diagnostic-action-result">
            {{ diagnosticsActionResult }}
          </div>
        </div>

        <n-empty v-else-if="!diagnosticsLoading" description="暂无诊断数据" />
      </n-spin>
    </n-modal>

    <!-- 添加/编辑订阅对话框 -->
    <n-modal v-model:show="showModal" :mask-closable="!step2Loading">
      <n-card
        class="modal-card"
        :title="editingId ? '编辑订阅' : '添加订阅'"
        :bordered="false"
        size="huge"
        role="dialog"
        aria-modal="true"
      >
        <!-- 第一步: 选择RSS源或手动输入 -->
        <div v-if="showRssStep">
          <n-tabs v-model:value="activeTab" type="line">
            <n-tab-pane name="rss_source" tab="从RSS源">
              <n-form label-width="80px">
                <n-form-item label="RSS 地址">
                  <n-input
                    v-model:value="formData.rss_url"
                    type="textarea"
                    :autosize="{ minRows: 2 }"
                    placeholder="https://mikanani.me/RSS/Bangumi?bangumiId=xxx"
                    :disabled="step2Loading"
                  />
                </n-form-item>
                <n-form-item>
                  <n-space justify="end" style="width: 100%;">
                    <n-button
                      text
                      type="primary"
                      @click="$router.push('/rss-sources')"
                      :disabled="step2Loading"
                    >
                      浏览 RSS 源
                    </n-button>
                  </n-space>
                </n-form-item>
              </n-form>
            </n-tab-pane>

            <n-tab-pane name="manual" tab="手动输入">
              <n-form label-width="80px">
                <n-form-item label="番剧名称">
                  <n-input
                    v-model:value="formData.name"
                    placeholder="请输入番剧名称"
                    :disabled="step2Loading"
                  />
                </n-form-item>
                <n-form-item label="RSS 地址">
                  <n-input
                    v-model:value="formData.rss_url"
                    type="textarea"
                    :autosize="{ minRows: 2 }"
                    placeholder="https://example.com/feed.xml"
                    :disabled="step2Loading"
                  />
                </n-form-item>
              </n-form>
            </n-tab-pane>

            <n-tab-pane name="calendar" tab="追番日历">
              <n-form label-width="80px">
                <n-form-item label="番剧名称">
                  <n-input
                    v-model:value="formData.name"
                    placeholder="请输入番剧名称"
                    :disabled="step2Loading"
                  />
                </n-form-item>
                <n-form-item label="更新日期">
                  <n-select
                    v-model:value="formData.air_day"
                    :options="weekdayOptions"
                    placeholder="选择更新日"
                    :disabled="step2Loading"
                  />
                </n-form-item>
              </n-form>
            </n-tab-pane>
          </n-tabs>

          <n-space justify="end" style="margin-top: 16px;">
              <n-button @click="showModal = false" :disabled="step2Loading">取消</n-button>
              <n-button type="primary" @click="handleGetRssData" :loading="step2Loading">
              {{ activeTab === 'calendar' ? '下一步' : '下一步并预览' }}
            </n-button>
          </n-space>
        </div>

        <!-- 第二步: 详细配置 -->
        <div v-else>
          <div style="max-height: 500px; overflow-y: auto; padding-right: 12px;">
            <n-form ref="formRef" :model="formData" label-width="100px" label-placement="left">
              <n-form-item label="番剧名称" path="name">
                <n-input v-model:value="formData.name" placeholder="请输入番剧名称" />
              </n-form-item>

              <n-form-item label="RSS 地址 (可选)">
                <n-input
                  v-model:value="formData.rss_url"
                  type="textarea"
                  :autosize="{ minRows: 2 }"
                  :placeholder="isCalendarOnlyForm ? '不填写 RSS，仅用于日历提醒' : 'RSS 地址 (与合集种子至少填一个)'"
                  :disabled="isCalendarOnlyForm"
                />
              </n-form-item>

              <n-form-item label="字幕组" v-if="!isCalendarOnlyForm">
                <n-input v-model:value="formData.fansub" placeholder="字幕组名称" />
              </n-form-item>

              <n-form-item label="字幕语言" v-if="formData.language">
                <n-tag type="info">{{ formData.language === 'CHS' ? '简体中文' : formData.language === 'CHT' ? '繁體中文' : formData.language }}</n-tag>
              </n-form-item>

              <n-form-item label="语言偏好" v-if="!isCalendarOnlyForm">
                <n-select
                  v-model:value="formData.language_preference"
                  :options="languagePreferenceOptions"
                  placeholder="选择语言偏好"
                />
              </n-form-item>

              <n-form-item label="更新日期">
                <n-select
                  v-model:value="formData.air_day"
                  :options="weekdayOptions"
                  placeholder="选择更新日"
                />
              </n-form-item>

              <n-form-item label="更新时间">
                <n-time-picker
                  v-model:value="airTimeValue"
                  format="HH:mm"
                  placeholder="选择更新时间"
                  style="width: 100%;"
                />
              </n-form-item>

              <n-form-item label="时区">
                <n-select
                  v-model:value="formData.air_timezone"
                  :options="timezoneOptions"
                  placeholder="选择时区"
                />
              </n-form-item>

              <n-form-item label="更新提醒">
                <n-space align="center">
                  <n-switch v-model:value="formData.notify_enabled" />
                  <span v-if="formData.notify_enabled">
                    提前 <n-input-number v-model:value="formData.notify_before_min" :min="0" :max="120" style="width: 80px;" /> 分钟提醒
                  </span>
                </n-space>
              </n-form-item>

              <n-form-item label="季数">
                <n-input-number v-model:value="formData.season" :min="1" style="width: 100%;" />
              </n-form-item>

              <n-form-item label="Bangumi ID">
                <n-input-number
                  v-model:value="formData.bangumi_id"
                  :min="0"
                  :show-button="false"
                  style="width: 100%;"
                  placeholder="可选：手动指定 Bangumi 条目 ID"
                >
                  <template #suffix>
                    <n-button
                      text
                      size="small"
                      tag="a"
                      :href="formData.bangumi_id ? `https://bgm.tv/subject/${formData.bangumi_id}` : 'https://bgm.tv'"
                      target="_blank"
                      :disabled="!formData.bangumi_id"
                    >
                      查看
                    </n-button>
                  </template>
                </n-input-number>
              </n-form-item>

              <n-form-item label="总集数">
                <n-input-number v-model:value="formData.total_episodes" :min="0" style="width: 100%;" placeholder="0表示未知" />
              </n-form-item>

              <n-form-item label="集数偏移">
                <n-input-number v-model:value="formData.episode_offset" style="width: 100%;" />
              </n-form-item>

              <n-form-item label="合集种子 (可选)" v-if="!isCalendarOnlyForm">
                <n-input
                  v-model:value="formData.collection_torrent"
                  type="textarea"
                  :autosize="{ minRows: 2 }"
                  placeholder="磁力链接或 .torrent 文件地址 (与 RSS 地址至少填一个)"
                />
              </n-form-item>

              <n-form-item label="过滤规则" v-if="!isCalendarOnlyForm">
                <n-input
                  v-model:value="formData.filter_rules"
                  type="textarea"
                  :autosize="{ minRows: 2 }"
                  placeholder="留空则下载所有集数"
                />
              </n-form-item>

              <n-form-item label="启用">
                <n-switch v-model:value="formData.enabled" />
              </n-form-item>

              <n-form-item label="智能拉取" v-if="!isCalendarOnlyForm">
                <n-radio-group v-model:value="formData.smart_fetch_override" size="small">
                  <n-radio-button
                    v-for="option in smartFetchOverrideOptions"
                    :key="option.value"
                    :value="option.value"
                    :label="option.label"
                  />
                </n-radio-group>
              </n-form-item>
            </n-form>

            <div class="rule-preview" v-if="!isCalendarOnlyForm && (previewResult || previewLoading)">
              <div class="preview-header-line">
                <div class="preview-title">RSS 预览</div>
                <n-space size="small" v-if="previewResult">
                  <n-tag size="small" type="success">新增 {{ previewResult.summary.download_items }}</n-tag>
                  <n-tag size="small" type="warning">替换 {{ previewResult.summary.replace_items }}</n-tag>
                  <n-tag size="small" type="default">跳过 {{ previewResult.summary.skipped_items + previewResult.summary.duplicate_items }}</n-tag>
                </n-space>
              </div>
              <n-spin :show="previewLoading">
                <n-empty v-if="previewResult && previewResult.items.length === 0" description="RSS 无条目" />
                <div v-else class="preview-list">
                  <div
                    v-for="item in previewResult?.items || []"
                    :key="`${item.torrent_hash || item.title}-${item.episode}`"
                    class="preview-item"
                    :class="`preview-${item.action}`"
                  >
                    <div class="preview-item-main">
                      <div class="preview-item-title">{{ item.title }}</div>
                      <div class="preview-item-meta">
                        <n-tag size="tiny" :type="getPreviewActionConfig(item.action).type">
                          {{ getPreviewActionConfig(item.action).label }}
                        </n-tag>
                        <n-tag size="tiny" v-if="item.episode">第 {{ item.episode }} 集</n-tag>
                        <n-tag size="tiny" v-if="item.fansub">{{ item.fansub }}</n-tag>
                        <n-tag size="tiny" v-if="item.language">{{ item.language }}</n-tag>
                        <span>{{ item.reason }}</span>
                      </div>
                      <div class="preview-rename" v-if="item.rename_preview">
                        {{ item.rename_preview }}
                      </div>
                    </div>
                  </div>
                </div>
              </n-spin>
            </div>
          </div>

          <n-space justify="space-between" style="margin-top: 16px;">
            <n-button @click="showRssStep = true">上一步</n-button>
            <n-space>
              <n-button @click="runSubscriptionPreview" :loading="previewLoading" :disabled="!formData.rss_url || isCalendarOnlyForm">
                刷新预览
              </n-button>
              <n-button @click="showModal = false">取消</n-button>
              <n-button type="primary" @click="handleSubmit" :loading="submitLoading">
                {{ editingId ? '更新' : '创建' }}
              </n-button>
            </n-space>
          </n-space>
        </div>
      </n-card>
    </n-modal>
  </div>
</template>

<script setup lang="tsx">
import { ref, onMounted, computed } from 'vue'
import {
  NButton,
  NCard,
  NModal,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NSpace,
  NTabs,
  NTabPane,
  NTag,
  NEllipsis,
  NSelect,
  NSwitch,
  NSpin,
  NEmpty,
  NIcon,
  NTooltip,
  NTimePicker,
  NProgress,
  NCheckbox,
  NButtonGroup,
  NDataTable,
  NRadioButton,
  NRadioGroup,
  useMessage,
  useDialog
} from 'naive-ui'
import {
  subscriptionApi,
  type DiagnosticStatus,
  type SmartFetchStatus,
  type Subscription,
  type SubscriptionDiagnosticAction,
  type SubscriptionDiagnostics,
  type SubscriptionPreview,
  type SubscriptionRetryFailedResponse
} from '@/api'
import { api } from '@/api'
import { useRoute } from 'vue-router'
import {
  EditOutlined,
  DeleteOutlined,
  SearchOutlined,
  DownloadOutlined,
  PlusOutlined,
  AppstoreOutlined,
  UnorderedListOutlined,
  CalendarOutlined,
  WarningOutlined,
  CalculatorOutlined,
  FileSearchOutlined,
  PlayCircleOutlined,
  PauseCircleOutlined,
  FolderOpenOutlined,
  ToolOutlined
} from '@vicons/antd'
import AnimeSearch from '@/components/AnimeSearch.vue'

const route = useRoute()
const message = useMessage()
const dialog = useDialog()

// 基础状态
const loading = ref(false)
const subscriptions = ref<Subscription[]>([])
const smartFetchStatusMap = ref<Record<number, SmartFetchStatus>>({})
const showModal = ref(false)
const showRssStep = ref(true)
const step2Loading = ref(false)
const submitLoading = ref(false)
const previewLoading = ref(false)
const previewResult = ref<SubscriptionPreview | null>(null)
const activeTab = ref('rss_source')
const editingId = ref<number | undefined>()
const animeSearchRef = ref<InstanceType<typeof AnimeSearch> | null>(null)

// 视图和筛选状态
const viewMode = ref<'card' | 'list'>('card')
const searchQuery = ref('')
const filterStatus = ref<string | null>(null)
const filterYear = ref<number | null>(null)
const filterFansub = ref<string | null>(null)
const selectedIds = ref<number[]>([])

// 弹窗状态
const showOffsetModal = ref(false)
const offsetEditingSub = ref<Subscription | null>(null)
const tempOffset = ref(0)
const showMissingModal = ref(false)
const missingEpisodesSub = ref<Subscription | null>(null)
const showPreviewModal = ref(false)
const previewSub = ref<Subscription | null>(null)

// 文件夹扫描弹窗
const showScanModal = ref(false)
const scanSub = ref<Subscription | null>(null)
const scanFolderPath = ref('')
const scanDryRun = ref(true)
const scanRenameFiles = ref(true)
const scanLoading = ref(false)
const scanResult = ref<any>(null)

// 健康诊断面板
const showDiagnosticsModal = ref(false)
const diagnosticsSub = ref<Subscription | null>(null)
const diagnosticsData = ref<SubscriptionDiagnostics | null>(null)
const diagnosticsLoading = ref(false)
const diagnosticsActionLoading = ref('')
const diagnosticsActionResult = ref('')

// 星期列表
const weekList = [
  { day: 0, label: '星期日' },
  { day: 1, label: '星期一' },
  { day: 2, label: '星期二' },
  { day: 3, label: '星期三' },
  { day: 4, label: '星期四' },
  { day: 5, label: '星期五' },
  { day: 6, label: '星期六' }
]

// 选项
const weekdayOptions = weekList.map(w => ({ label: w.label, value: w.day.toString() }))
const languagePreferenceOptions = [
  { label: '自动学习', value: 'auto' },
  { label: '简体中文优先', value: 'chs' },
  { label: '繁体中文优先', value: 'cht' },
  { label: '同时保留', value: 'both' }
]
const timezoneOptions = [
  { label: 'JST (日本)', value: 'JST' },
  { label: 'CST (中国)', value: 'CST' },
  { label: 'UTC', value: 'UTC' }
]
const statusOptions = [
  { label: '连载中', value: 'ongoing' },
  { label: '已完结', value: 'completed' },
  { label: '已禁用', value: 'disabled' }
]
const smartFetchOverrideOptions = [
  { label: '跟随全局', value: 'follow' },
  { label: '强制启用', value: 'always' },
  { label: '强制关闭', value: 'never' }
]

const previewActionConfig: Record<string, { label: string; type: 'success' | 'warning' | 'error' | 'info' | 'default' }> = {
  download: { label: '新增', type: 'success' },
  replace: { label: '替换', type: 'warning' },
  duplicate: { label: '重复', type: 'default' },
  skip: { label: '跳过', type: 'default' }
}

const getPreviewActionConfig = (action: string) => {
  return previewActionConfig[action] || { label: action, type: 'default' as const }
}

const getSmartFetchStatus = (id: number) => smartFetchStatusMap.value[id]

const getSmartFetchTagType = (status?: SmartFetchStatus): 'success' | 'warning' | 'error' | 'info' | 'default' => {
  if (!status) return 'default'
  if (!status.smart_fetch_enabled) return 'default'
  if (status.should_fetch) return status.is_in_active_window ? 'success' : 'info'
  return status.is_completed ? 'default' : 'warning'
}

const formatNextFetch = (seconds?: number) => {
  if (seconds === undefined || seconds === null) return ''
  if (seconds <= 0) return '下次检查: 即刻'
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `下次检查: ${minutes} 分钟后`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `下次检查: ${hours} 小时后`
  return `下次检查: ${Math.round(hours / 24)} 天后`
}

// 统计计算
const activeCount = computed(() => subscriptions.value.filter(s => !isCompleted(s) && s.enabled).length)
const todayUpdateCount = computed(() => todayUpdates.value.length)
const downloadingCount = computed(() => subscriptions.value.reduce((sum, s) => sum + (s.downloading_count || 0), 0))
const missingEpisodesCount = computed(() => subscriptions.value.filter(s => getMissingEpisodes(s).length > 0).length)
const todayPendingCount = computed(() => todayUpdates.value.filter(s => !isTodayDownloaded(s)).length)

// 年份选项
const yearOptions = computed(() => {
  const years = new Set<number>()
  subscriptions.value.forEach(s => {
    if (s.air_year) years.add(s.air_year)
  })
  return Array.from(years).sort((a, b) => b - a).map(y => ({ label: String(y), value: y }))
})

// 字幕组选项
const fansubOptions = computed(() => {
  const fansubs = new Set<string>()
  subscriptions.value.forEach(s => {
    if (s.fansub) fansubs.add(s.fansub)
  })
  return Array.from(fansubs).sort().map(f => ({ label: f, value: f }))
})

// 今日更新列表
const todayUpdates = computed(() => {
  const today = new Date().getDay()
  return subscriptions.value.filter(sub => {
    if (!sub.enabled || isCompleted(sub)) return false
    const dayValue = sub.air_day || sub.update_day
    if (dayValue === undefined || dayValue === null) return false
    return parseInt(String(dayValue)) === today
  }).sort((a, b) => {
    // 按更新时间排序
    const timeA = a.air_time || '00:00'
    const timeB = b.air_time || '00:00'
    return timeA.localeCompare(timeB)
  })
})

// 筛选后的所有订阅
const filteredAllSubscriptions = computed(() => {
  let result = subscriptions.value

  // 搜索过滤
  if (searchQuery.value) {
    const query = searchQuery.value.toLowerCase()
    result = result.filter(s => s.name.toLowerCase().includes(query))
  }

  // 状态过滤
  if (filterStatus.value) {
    switch (filterStatus.value) {
      case 'ongoing':
        result = result.filter(s => !isCompleted(s) && s.enabled)
        break
      case 'completed':
        result = result.filter(s => isCompleted(s))
        break
      case 'disabled':
        result = result.filter(s => !s.enabled)
        break
    }
  }

  // 年份过滤
  if (filterYear.value) {
    result = result.filter(s => s.air_year === filterYear.value)
  }

  // 字幕组过滤
  if (filterFansub.value) {
    result = result.filter(s => s.fansub === filterFansub.value)
  }

  return result
})

// 按星期分组获取订阅
const getSubscriptionsByWeekday = (day: number) => {
  return filteredAllSubscriptions.value.filter(sub => {
    if (isCompleted(sub)) return false
    if (!sub.enabled && filterStatus.value !== 'disabled') return false
    const dayValue = sub.air_day || sub.update_day
    if (dayValue === undefined || dayValue === null) return day === 0
    return parseInt(String(dayValue)) === day
  })
}

// 筛选后的星期列表
const filteredWeekList = computed(() => {
  const today = new Date().getDay()
  const sorted = []
  for (let i = 0; i < 7; i++) {
    const targetDay = (today + i) % 7
    const weekItem = weekList.find(w => w.day === targetDay)
    if (weekItem && getSubscriptionsByWeekday(targetDay).length > 0) {
      sorted.push(weekItem)
    }
  }
  return sorted
})

// 已完结的订阅
const filteredCompletedSubscriptions = computed(() => {
  return filteredAllSubscriptions.value.filter(sub => isCompleted(sub))
})

import type { DataTableColumns } from 'naive-ui'

// 列表视图列定义
const listColumns = computed<DataTableColumns<Subscription>>(() => [
  { type: 'selection', fixed: 'left' },
  {
    title: '番剧',
    key: 'name',
    fixed: 'left',
    width: 250,
    render: (row) => (
      <NSpace align="center" size={8}>
        {row.bangumi_cover_local ? (
          <img src={`/covers/${row.bangumi_cover_local}`} style="width: 40px; height: 56px; object-fit: cover; border-radius: 4px;" />
        ) : (
          <div style="width: 40px; height: 56px; background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); display: flex; align-items: center; justify-content: center; color: white; border-radius: 4px; font-weight: bold;">
            {row.name[0]}
          </div>
        )}
        <div>
          <div style="font-weight: 500;">{row.name}</div>
          <NSpace size={4} style="margin-top: 4px;">
            {row.fansub && <NTag size="tiny" type="info">{row.fansub}</NTag>}
            <NTag size="tiny">S{row.season}</NTag>
          </NSpace>
        </div>
      </NSpace>
    )
  },
  {
    title: '进度',
    key: 'progress',
    width: 150,
    render: (row) => (
      <div>
        <NProgress
          percentage={getProgressPercent(row)}
          height={6}
          status={isSeasonComplete(row) ? 'success' : 'default'}
          showIndicator={false}
        />
        <div style="font-size: 12px; color: #666; margin-top: 4px;">
          {row.current_episode || 0} / {row.total_episodes || '?'}
          {(row.latest_episode || 0) > (row.current_episode || 0) && (
            <span style="color: #18a058; margin-left: 8px;">最新 {row.latest_episode}</span>
          )}
        </div>
      </div>
    )
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render: (row) => (
      <NSpace>
        {!row.enabled && <NTag size="small" type="default">已禁用</NTag>}
        {isCompleted(row) && <NTag size="small" type="success">完结</NTag>}
        {(row.downloading_count || 0) > 0 && <NTag size="small" type="info">下载中</NTag>}
        {getMissingEpisodes(row).length > 0 && <NTag size="small" type="warning">有缺失</NTag>}
      </NSpace>
    )
  },
  {
    title: '智能拉取',
    key: 'smart_fetch',
    width: 260,
    render: (row) => {
      const status = getSmartFetchStatus(row.id)
      if (!status) {
        return <span style="font-size: 12px; color: #999;">计算中</span>
      }
      return (
        <div class="list-smart-fetch">
          <NTag size="tiny" type={getSmartFetchTagType(status)}>
            {status.should_fetch ? '拉取' : '跳过'}
          </NTag>
          <span>{status.explanation}</span>
        </div>
      )
    }
  },
  {
    title: '更新时间',
    key: 'update_time',
    width: 120,
    render: (row) => (
      <div>
        {row.air_time && <div>{row.air_time}</div>}
        {(() => {
          const day = row.air_day !== undefined ? row.air_day : row.update_day
          if (day !== undefined && day !== null) {
            return <div style="font-size: 12px; color: #666;">{weekList.find(w => w.day === parseInt(String(day)))?.label}</div>
          }
          return null
        })()}
      </div>
    )
  },
  {
    title: '操作',
    key: 'actions',
    fixed: 'right',
    width: 180,
    render: (row) => (
      <NSpace>
        <NButton text size="small" onClick={() => handleDiagnostics(row)}>
          <NIcon size={16}><ToolOutlined /></NIcon>
        </NButton>
        <NButton text size="small" onClick={() => handleCollectEpisodes(row.id)}>
          <NIcon size={16}><DownloadOutlined /></NIcon>
        </NButton>
        <NButton text size="small" onClick={() => handleEdit(row)}>
          <NIcon size={16}><EditOutlined /></NIcon>
        </NButton>
        <NButton text size="small" type="error" onClick={() => handleDelete(row.id)}>
          <NIcon size={16}><DeleteOutlined /></NIcon>
        </NButton>
      </NSpace>
    )
  }
])

// 表单数据
const formData = ref({
  name: '',
  rss_url: '',
  fansub: '',
  language: '',
  language_preference: 'auto',
  update_day: '',
  air_day: '',
  air_time: '',
  air_timezone: 'JST',
  notify_enabled: true,
  notify_before_min: 10,
  season: 1,
  bangumi_id: 0,
  total_episodes: 0,
  episode_offset: 0,
  collection_torrent: '',
  filter_rules: '',
  enabled: true,
  smart_fetch_enabled: null as boolean | null,
  smart_fetch_override: 'follow' as 'follow' | 'always' | 'never',
  rss_source_id: undefined as number | undefined,
  source_type: 'manual'
})

const isCalendarOnlyForm = computed(() => formData.value.source_type === 'calendar')

// 时间选择器
const airTimeValue = computed({
  get: () => {
    if (!formData.value.air_time) return null
    const [hours, minutes] = formData.value.air_time.split(':').map(Number)
    const date = new Date()
    date.setHours(hours, minutes, 0, 0)
    return date.getTime()
  },
  set: (val: number | null) => {
    if (!val) {
      formData.value.air_time = ''
      return
    }
    const date = new Date(val)
    formData.value.air_time = `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
  }
})

// 判断番剧是否已完结
const isCompleted = (sub: Subscription) => {
  if (!sub.total_episodes || sub.total_episodes <= 0) return false
  if (sub.current_episode && sub.current_episode >= sub.total_episodes) return true
  if (sub.latest_episode && sub.latest_episode >= sub.total_episodes && sub.air_date) {
    const airDate = new Date(sub.air_date)
    const estimatedEndDate = new Date(airDate)
    estimatedEndDate.setDate(estimatedEndDate.getDate() + (sub.total_episodes * 7) + 30)
    if (new Date() > estimatedEndDate) return true
  }
  if (sub.latest_episode && sub.latest_episode >= sub.total_episodes && sub.air_year) {
    const currentYear = new Date().getFullYear()
    if (sub.air_year < currentYear) return true
  }
  return false
}

// 判断是否今日更新
const isTodayUpdate = (sub: Subscription) => {
  const today = new Date().getDay()
  const dayValue = sub.air_day || sub.update_day
  if (dayValue === undefined || dayValue === null) return false
  return parseInt(String(dayValue)) === today
}

// 判断是否今日已下载
const isTodayDownloaded = (sub: Subscription) => {
  if (!sub.last_download_at) return false
  const lastDownload = new Date(sub.last_download_at)
  const today = new Date()
  return lastDownload.toDateString() === today.toDateString()
}

// 计算缺失剧集
const getMissingEpisodes = (sub: Subscription): number[] => {
  if (!sub.latest_episode || sub.latest_episode <= 0) return []
  const current = sub.current_episode || 0
  if (current >= sub.latest_episode) return []

  const missing: number[] = []
  for (let i = current + 1; i <= sub.latest_episode; i++) {
    missing.push(i)
  }
  return missing
}

// 计算进度百分比
const getProgressPercent = (sub: Subscription) => {
  if (!sub.total_episodes || sub.total_episodes <= 0) return 0
  const current = sub.current_episode || 0
  return Math.min(100, Math.round((current / sub.total_episodes) * 100))
}

// 检查RSS检查警告
const isRssCheckWarning = (sub: Subscription) => {
  if (!sub.last_check_time) return false
  const lastCheck = new Date(sub.last_check_time)
  const now = new Date()
  const diffHours = (now.getTime() - lastCheck.getTime()) / (1000 * 60 * 60)
  return diffHours > 24
}

// RSS检查警告文本
const getRssCheckWarningText = (sub: Subscription) => {
  if (!sub.last_check_time) return ''
  const lastCheck = new Date(sub.last_check_time)
  const now = new Date()
  const diffDays = Math.floor((now.getTime() - lastCheck.getTime()) / (1000 * 60 * 60 * 24))
  return `${diffDays}天未检查`
}

// 格式化时间
const formatTime = (time: string) => {
  const date = new Date(time)
  const now = new Date()
  const diff = now.getTime() - date.getTime()
  const days = Math.floor(diff / (1000 * 60 * 60 * 24))
  if (days === 0) {
    const hours = Math.floor(diff / (1000 * 60 * 60))
    if (hours === 0) {
      const minutes = Math.floor(diff / (1000 * 60))
      return `${minutes} 分钟前`
    }
    return `${hours} 小时前`
  }
  if (days === 1) return '昨天'
  if (days < 7) return `${days} 天前`
  return date.toLocaleDateString()
}

// 格式化播出时间显示
const formatAirTime = (sub: Subscription) => {
  if (!sub.air_time) return ''
  const now = new Date()
  const [hours, minutes] = sub.air_time.split(':').map(Number)
  const airTime = new Date()
  airTime.setHours(hours, minutes, 0, 0)

  if (now > airTime) {
    return `已更新 ${sub.air_time}`
  }
  const diffMinutes = Math.floor((airTime.getTime() - now.getTime()) / (1000 * 60))
  if (diffMinutes < 60) {
    return `${diffMinutes} 分钟后`
  }
  const diffHours = Math.floor(diffMinutes / 60)
  return `${diffHours} 小时后`
}

// 生成年份季度标签
const getYearSeasonLabel = (year: number, airDate?: string) => {
  let season = '未知'
  if (airDate && airDate.length >= 7) {
    const month = parseInt(airDate.substring(5, 7))
    if (month >= 1 && month <= 3) season = '冬'
    else if (month >= 4 && month <= 6) season = '春'
    else if (month >= 7 && month <= 9) season = '夏'
    else if (month >= 10 && month <= 12) season = '秋'
  }
  return `${year}${season}`
}

// 判断是否已完成本季
const isSeasonComplete = (sub: Subscription) => {
  const current = sub.current_episode ?? 0
  const total = sub.total_episodes ?? 0
  if (total <= 0) return false
  return current >= total
}

// 选择相关
const toggleSelection = (id: number, checked: boolean) => {
  if (checked) {
    selectedIds.value.push(id)
  } else {
    selectedIds.value = selectedIds.value.filter(i => i !== id)
  }
}

const handleCheck = (keys: (string | number)[]) => {
  selectedIds.value = keys.map(k => Number(k))
}

// 批量操作
const batchToggle = async (enabled: boolean) => {
  try {
    for (const id of selectedIds.value) {
      await api.post(`/subscriptions/${id}/toggle`)
    }
    message.success(`已${enabled ? '启用' : '禁用'} ${selectedIds.value.length} 个订阅`)
    selectedIds.value = []
    loadSubscriptions()
  } catch (error: any) {
    message.error(error.message || '操作失败')
  }
}

const batchDelete = () => {
  dialog.warning({
    title: '确认批量删除',
    content: `确定要删除选中的 ${selectedIds.value.length} 个订阅吗？`,
    positiveText: '确定',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        for (const id of selectedIds.value) {
          await subscriptionApi.delete(id)
        }
        message.success(`已删除 ${selectedIds.value.length} 个订阅`)
        selectedIds.value = []
        loadSubscriptions()
      } catch (error: any) {
        message.error(error.message || '删除失败')
      }
    }
  })
}

// 批量采集限制配置
const BATCH_COLLECT_CONFIG = {
  maxCount: 10,           // 最大批量数
  intervalMs: 500         // 间隔时间（毫秒）
}

const batchCollect = async () => {
  // 检查数量限制
  if (selectedIds.value.length > BATCH_COLLECT_CONFIG.maxCount) {
    message.warning(`批量采集一次最多 ${BATCH_COLLECT_CONFIG.maxCount} 个订阅，请减少选择数量`)
    return
  }

  // 确认对话框
  dialog.warning({
    title: '确认批量采集',
    content: `将为 ${selectedIds.value.length} 个订阅启动采集任务，是否继续？`,
    positiveText: '确定',
    negativeText: '取消',
    onPositiveClick: async () => {
      let successCount = 0
      let skipCount = 0

      for (let i = 0; i < selectedIds.value.length; i++) {
        const id = selectedIds.value[i]
        try {
          await api.post(`/subscriptions/${id}/collect-episodes`)
          successCount++
        } catch (e: any) {
          if (e.response?.status === 409) {
            skipCount++
          }
        }
        // 添加间隔，避免短时间创建大量任务
        if (i < selectedIds.value.length - 1) {
          await new Promise(resolve => setTimeout(resolve, BATCH_COLLECT_CONFIG.intervalMs))
        }
      }

      let msg = `已为 ${successCount} 个订阅启动采集任务`
      if (skipCount > 0) {
        msg += `，${skipCount} 个因已有任务在执行中而跳过`
      }
      message.success(msg)
    }
  })
}

// 快捷操作
const handleOffsetEdit = (sub: Subscription) => {
  offsetEditingSub.value = sub
  tempOffset.value = sub.episode_offset || 0
  showOffsetModal.value = true
}

const saveOffset = async () => {
  if (!offsetEditingSub.value) return
  try {
    await subscriptionApi.update(offsetEditingSub.value.id, {
      episode_offset: tempOffset.value
    })
    message.success('偏移量已更新')
    showOffsetModal.value = false
    loadSubscriptions()
  } catch (error: any) {
    message.error(error.message || '更新失败')
  }
}

const showMissingEpisodes = (sub: Subscription) => {
  missingEpisodesSub.value = sub
  showMissingModal.value = true
}

const showQuickPreview = (sub: Subscription) => {
  previewSub.value = sub
  showPreviewModal.value = true
}

const resetRulePreview = () => {
  previewResult.value = null
  previewLoading.value = false
}

const runSubscriptionPreview = async () => {
  if (isCalendarOnlyForm.value) {
    return
  }
  if (!formData.value.rss_url) {
    message.warning('请输入 RSS 地址')
    return
  }

  previewLoading.value = true
  try {
    const response: any = await subscriptionApi.preview({
      ...formData.value,
      id: editingId.value,
      limit: 50
    })
    if (!response?.data?.summary || !Array.isArray(response.data.items)) {
      throw new Error('预览接口返回异常')
    }
    previewResult.value = response.data
  } catch (error: any) {
    previewResult.value = null
    message.error(error.response?.data?.message || error.message || '预览失败')
  } finally {
    previewLoading.value = false
  }
}

// 打开Bangumi页面
const openBangumiPage = (bangumiId: number) => {
  if (bangumiId) {
    window.open(`https://bgm.tv/subject/${bangumiId}`, '_blank')
  }
}

// 对话框操作
const showAddDialog = () => {
  editingId.value = undefined
  resetRulePreview()
  formData.value = {
    name: '',
    rss_url: '',
    fansub: '',
    language: '',
    language_preference: 'auto',
    update_day: '',
    air_day: '',
    air_time: '',
    air_timezone: 'JST',
    notify_enabled: true,
    notify_before_min: 10,
    season: 1,
    bangumi_id: 0,
    total_episodes: 0,
    episode_offset: 0,
    collection_torrent: '',
    filter_rules: '',
    enabled: true,
    smart_fetch_enabled: null,
    smart_fetch_override: 'follow',
    rss_source_id: undefined,
    source_type: 'manual'
  }
  activeTab.value = 'rss_source'
  showRssStep.value = true
  showModal.value = true
}

const applyCalendarOnlyMode = () => {
  formData.value.source_type = 'calendar'
  formData.value.rss_url = ''
  formData.value.collection_torrent = ''
  formData.value.filter_rules = ''
  formData.value.fansub = ''
  formData.value.language = ''
  formData.value.language_preference = 'auto'
  formData.value.rss_source_id = undefined
  formData.value.smart_fetch_enabled = null
  formData.value.smart_fetch_override = 'never'
  resetRulePreview()
}

const handleSearchSubscribe = (data: {
  title: string
  rss_url: string
  fansub: string
  language?: string
  rss_source_id?: number
}) => {
  resetRulePreview()
  formData.value = {
    ...formData.value,
    name: data.title,
    rss_url: data.rss_url,
    fansub: data.fansub,
    language: data.language || '',
    language_preference: 'auto',
    smart_fetch_enabled: null,
    smart_fetch_override: 'follow',
    rss_source_id: data.rss_source_id,
    source_type: data.rss_source_id ? 'rss_source' : 'manual'
  }
  showRssStep.value = false
  showModal.value = true
}

const handleEdit = (sub: Subscription) => {
  editingId.value = sub.id
  resetRulePreview()
  formData.value = {
    name: sub.name,
    rss_url: sub.rss_url,
    fansub: sub.fansub || '',
    language: sub.language || '',
    language_preference: sub.language_preference || 'auto',
    update_day: sub.update_day || '',
    air_day: sub.air_day !== undefined ? String(sub.air_day) : '',
    air_time: sub.air_time || '',
    air_timezone: sub.air_timezone || 'JST',
    notify_enabled: sub.notify_enabled !== false,
    notify_before_min: sub.notify_before_min || 10,
    season: sub.season,
    bangumi_id: sub.bangumi_id || 0,
    total_episodes: sub.total_episodes || 0,
    episode_offset: sub.episode_offset || 0,
    collection_torrent: sub.collection_torrent || '',
    filter_rules: sub.filter_rules || '',
    enabled: sub.enabled !== false,
    smart_fetch_enabled: sub.smart_fetch_enabled ?? null,
    smart_fetch_override: sub.smart_fetch_override || 'follow',
    rss_source_id: sub.rss_source_id,
    source_type: sub.source_type || 'manual'
  }
  showRssStep.value = false
  showModal.value = true
}

const handleGetRssData = async () => {
  if (activeTab.value === 'calendar') {
    if (!formData.value.name) {
      message.error('请输入番剧名称')
      return
    }
    applyCalendarOnlyMode()
  } else if (activeTab.value === 'manual') {
    if (!formData.value.name) {
      message.error('请输入番剧名称')
      return
    }
    formData.value.source_type = 'manual'
  } else {
    if (!formData.value.rss_url) {
      message.error('请输入 RSS 地址')
      return
    }
    formData.value.source_type = formData.value.rss_source_id ? 'rss_source' : 'manual'
  }
  step2Loading.value = true
  try {
    showRssStep.value = false
    if (!isCalendarOnlyForm.value && formData.value.rss_url) {
      await runSubscriptionPreview()
    }
  } finally {
    step2Loading.value = false
  }
}

const handleSubmit = async () => {
  if (!formData.value.name) {
    message.error('请填写番剧名称')
    return
  }
  if (isCalendarOnlyForm.value) {
    applyCalendarOnlyMode()
  } else if (!formData.value.rss_url && !formData.value.collection_torrent) {
    message.error('请填写 RSS 地址或合集种子地址')
    return
  }

  submitLoading.value = true
  try {
    if (editingId.value) {
      await subscriptionApi.update(editingId.value, formData.value)
      message.success('更新成功')
    } else {
      await subscriptionApi.create(formData.value)
      message.success('创建成功')
    }
    showModal.value = false
    loadSubscriptions()
  } catch (error: any) {
    const status = error?.response?.status
    const responseData = error?.response?.data
    if (!editingId.value && status === 409) {
      message.warning(responseData?.message || '订阅已存在')
      const existing = responseData?.data as Subscription | undefined
      if (existing) {
        const index = subscriptions.value.findIndex((sub) => sub.id === existing.id)
        if (index >= 0) {
          subscriptions.value[index] = { ...subscriptions.value[index], ...existing }
        } else {
          subscriptions.value.unshift(existing)
        }
      }
      showModal.value = false
      return
    }
    message.error(responseData?.message || error.message || '操作失败')
  } finally {
    submitLoading.value = false
  }
}

const loadSubscriptions = async () => {
  loading.value = true
  try {
    const res: any = await subscriptionApi.list(1, 999)
    subscriptions.value = res.data?.list || []
    await loadSmartFetchStatus()
  } catch (error: any) {
    message.error(error.message || '加载订阅列表失败')
  } finally {
    loading.value = false
  }
}

const loadSmartFetchStatus = async () => {
  try {
    const res: any = await subscriptionApi.smartFetchStatus()
    const list = res.data?.list || []
    smartFetchStatusMap.value = Object.fromEntries(
      list.map((item: SmartFetchStatus) => [item.subscription_id, item])
    )
  } catch (error) {
    smartFetchStatusMap.value = {}
  }
}

const handleDelete = (id: number) => {
  dialog.warning({
    title: '确认删除',
    content: '确定要删除这个订阅吗?',
    positiveText: '确定',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await subscriptionApi.delete(id)
        message.success('删除成功')
        loadSubscriptions()
      } catch (error: any) {
        message.error(error.message || '删除失败')
      }
    }
  })
}

const handleToggle = async (id: number, enabled: boolean) => {
  try {
    const response: any = await api.post(`/subscriptions/${id}/toggle`)
    if (response.code === 0) {
      message.success(enabled ? '已启用' : '已禁用')
      loadSubscriptions()
    } else {
      message.error(response.message || '操作失败')
    }
  } catch (error: any) {
    message.error(error.message || '操作失败')
  }
}

const handleCollectEpisodes = async (id: number) => {
  try {
    const response: any = await api.post(`/subscriptions/${id}/collect-episodes`)
    if (response.code === 0) {
      message.success('采集任务已启动，请在右上角任务管理中查看进度')
    } else if (response.code === 409) {
      message.warning(response.message || '已有任务在执行中')
    } else {
      message.error(response.message || '启动采集任务失败')
    }
  } catch (error: any) {
    if (error.response?.status === 409) {
      message.warning(error.response?.data?.message || '已有任务在执行中')
    } else {
      message.error(error.response?.data?.message || error.message || '启动采集任务失败')
    }
  }
}

const handleScanFolder = (sub: Subscription) => {
  scanSub.value = sub
  scanFolderPath.value = ''
  scanDryRun.value = true
  scanRenameFiles.value = true
  scanResult.value = null
  scanLoading.value = false
  showScanModal.value = true
}

const handleDiagnostics = async (sub: Subscription) => {
  diagnosticsSub.value = sub
  diagnosticsData.value = null
  diagnosticsActionResult.value = ''
  showDiagnosticsModal.value = true
  await refreshDiagnostics()
}

const refreshDiagnostics = async () => {
  if (!diagnosticsSub.value) return
  diagnosticsLoading.value = true
  try {
    const response: any = await subscriptionApi.diagnostics(diagnosticsSub.value.id)
    diagnosticsData.value = response.data as SubscriptionDiagnostics
  } catch (error: any) {
    diagnosticsData.value = null
    message.error(error.response?.data?.message || error.message || '诊断失败')
  } finally {
    diagnosticsLoading.value = false
  }
}

const runDiagnosticAction = async (action: SubscriptionDiagnosticAction) => {
  if (!diagnosticsSub.value || !action.enabled) return

  if (action.key === 'scan_files') {
    showDiagnosticsModal.value = false
    handleScanFolder(diagnosticsSub.value)
    return
  }

  diagnosticsActionLoading.value = action.key
  diagnosticsActionResult.value = ''
  try {
    let response: any
    switch (action.key) {
      case 'refresh_rss':
        response = await api.post(`/subscriptions/${diagnosticsSub.value.id}/collect-episodes`)
        diagnosticsActionResult.value = response.message || 'RSS 采集任务已启动'
        break
      case 'retry_failed':
        response = await subscriptionApi.retryFailed(diagnosticsSub.value.id)
        diagnosticsActionResult.value = formatRetryFailedResult(response.data as SubscriptionRetryFailedResponse)
        break
      case 'reorganize_files':
        response = await api.post(`/subscriptions/${diagnosticsSub.value.id}/reorganize-files`)
        diagnosticsActionResult.value = response.message || '文件整理任务已启动'
        break
      case 'rename_files':
        response = await api.post(`/subscriptions/${diagnosticsSub.value.id}/rename-files`)
        diagnosticsActionResult.value = response.message || '重命名任务已启动'
        break
      case 'toggle_subscription':
        response = await api.post(`/subscriptions/${diagnosticsSub.value.id}/toggle`)
        diagnosticsActionResult.value = response.data?.enabled ? '订阅已启用' : '订阅已暂停'
        await loadSubscriptions()
        if (response.data) {
          diagnosticsSub.value = response.data as Subscription
        }
        break
      default:
        response = await api.request({ method: action.method || 'POST', url: action.endpoint.replace('/api/v1', '') })
        diagnosticsActionResult.value = response.message || '操作已执行'
    }
    message.success(diagnosticsActionResult.value)
    await refreshDiagnostics()
  } catch (error: any) {
    const msg = error.response?.data?.message || error.message || '操作失败'
    diagnosticsActionResult.value = msg
    message.error(msg)
  } finally {
    diagnosticsActionLoading.value = ''
  }
}

const formatRetryFailedResult = (result: SubscriptionRetryFailedResponse) => {
  return `已重试 ${result.retried} 个，跳过 ${result.skipped} 个，失败 ${result.failed} 个`
}

const getDiagnosticTagType = (status: DiagnosticStatus): 'success' | 'warning' | 'error' | 'info' | 'default' => {
  switch (status) {
    case 'healthy':
      return 'success'
    case 'warning':
      return 'warning'
    case 'error':
      return 'error'
    case 'unknown':
      return 'default'
    default:
      return 'default'
  }
}

const getDiagnosticStatusLabel = (status: DiagnosticStatus) => {
  const labels: Record<DiagnosticStatus, string> = {
    healthy: '正常',
    warning: '警告',
    error: '异常',
    unknown: '未知'
  }
  return labels[status] || status
}

const getDiagnosticActionType = (key: string): 'primary' | 'default' | 'tertiary' | 'info' | 'success' | 'warning' | 'error' => {
  if (key === 'retry_failed') return 'warning'
  if (key === 'toggle_subscription') return 'default'
  if (key === 'refresh_rss') return 'primary'
  return 'default'
}

const formatBytes = (bytes?: number) => {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unitIndex = 0
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024
    unitIndex++
  }
  return `${value.toFixed(unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`
}

const doScanFolder = async () => {
  if (!scanSub.value || !scanFolderPath.value) {
    message.warning('请输入文件夹路径')
    return
  }
  scanLoading.value = true
  scanResult.value = null
  try {
    const response: any = await api.post(`/subscriptions/${scanSub.value.id}/scan-folder`, {
      folder_path: scanFolderPath.value,
      dry_run: scanDryRun.value,
      rename_files: scanRenameFiles.value
    })
    if (response.code === 0) {
      scanResult.value = response.data
      if (!scanResult.value.dry_run) {
        message.success(response.message || '扫描完成')
        loadSubscriptions()
      }
    } else {
      message.error(response.message || '扫描失败')
    }
  } catch (error: any) {
    message.error(error.response?.data?.message || error.message || '扫描失败')
  } finally {
    scanLoading.value = false
  }
}

const fileBaseName = (path: string) => {
  if (!path) return ''
  return path.split('/').pop() || path.split('\\').pop() || path
}

onMounted(() => {
  if (route.query.from_rss === 'true') {
    showModal.value = true
    showRssStep.value = false
    formData.value = {
      name: (route.query.name as string) || '',
      rss_url: (route.query.rss_url as string) || '',
      fansub: (route.query.fansub as string) || '',
      language: '',
      language_preference: 'auto',
      update_day: '',
      air_day: '',
      air_time: '',
      air_timezone: 'JST',
      notify_enabled: true,
      notify_before_min: 10,
      season: 1,
      bangumi_id: 0,
      total_episodes: 0,
      episode_offset: 0,
      collection_torrent: '',
      filter_rules: '',
      enabled: true,
      smart_fetch_enabled: null,
      smart_fetch_override: 'follow',
      rss_source_id: route.query.rss_source_id ? parseInt(route.query.rss_source_id as string) : undefined,
      source_type: 'rss_source'
    }
  }
  loadSubscriptions()
})
</script>

<style scoped>
.subscriptions-page {
  max-width: 100%;
}

/* 统计概览 */
.stats-overview {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 12px;
  margin-bottom: 20px;
}

.stat-card {
  text-align: center;
  padding: 12px;
}

.stat-value {
  font-size: 24px;
  font-weight: 600;
  color: #2080f0;
}

.stat-label {
  font-size: 12px;
  color: #666;
  margin-top: 4px;
}

@media (max-width: 768px) {
  .stats-overview {
    grid-template-columns: repeat(3, 1fr);
  }
  .stat-card:nth-child(4),
  .stat-card:nth-child(5) {
    display: none;
  }
}

/* 今日更新专区 */
.today-updates-section {
  margin-bottom: 24px;
  padding: 16px;
  background: linear-gradient(135deg, #f0fff0 0%, #e6f7e6 100%);
  border-radius: 12px;
  border: 1px solid #c6f0c6;
}

.section-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

.section-header h3 {
  margin: 0;
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 16px;
  color: #18a058;
}

.today-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 12px;
}

.today-card {
  cursor: pointer;
}

.today-card :deep(.n-card__content) {
  padding: 12px;
}

.today-card.is-downloaded {
  opacity: 0.7;
}

.today-content {
  display: flex;
  gap: 12px;
  align-items: center;
}

.today-cover {
  width: 60px;
  height: 84px;
  object-fit: cover;
  border-radius: 6px;
  flex-shrink: 0;
}

.today-cover-placeholder {
  width: 60px;
  height: 84px;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: white;
  font-size: 20px;
  font-weight: bold;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  flex-shrink: 0;
}

.today-info {
  flex: 1;
  min-width: 0;
}

.today-title {
  font-weight: 600;
  font-size: 14px;
  margin-bottom: 6px;
}

.today-meta {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  margin-bottom: 4px;
}

.today-progress {
  font-size: 12px;
  color: #666;
}

.today-actions {
  flex-shrink: 0;
}

/* 筛选栏 */
.filter-bar {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
  flex-wrap: wrap;
  align-items: center;
}

.search-input {
  flex: 1;
  min-width: 200px;
  max-width: 300px;
}

.filter-select {
  width: 120px;
}

@media (max-width: 768px) {
  .filter-bar {
    gap: 8px;
  }
  .search-input {
    max-width: none;
    width: 100%;
  }
  .filter-select {
    width: calc(50% - 4px);
  }
}

/* 批量操作栏 */
.batch-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  background: #f0faff;
  border: 1px solid #d0e8ff;
  border-radius: 8px;
  margin-bottom: 16px;
}

/* 周分组 */
.week-section {
  margin-bottom: 20px;
}

.week-title {
  margin: 16px 0 12px 4px;
  background: linear-gradient(90deg, #667eea 0%, #764ba2 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  font-weight: 600;
  font-size: 16px;
}

/* 网格容器 */
.grid-container {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
  gap: 16px;
}

@media (max-width: 768px) {
  .grid-container {
    grid-template-columns: 1fr;
    gap: 12px;
  }
}

/* 卡片样式 */
.anime-card {
  cursor: default;
  position: relative;
}

.anime-card.is-disabled {
  opacity: 0.6;
}

.anime-card.has-missing :deep(.n-card) {
  border: 1px solid #f0a020;
}

.card-checkbox {
  position: absolute;
  top: 8px;
  left: 8px;
  z-index: 10;
  opacity: 0;
  transition: opacity 0.2s;
}

.anime-card:hover .card-checkbox {
  opacity: 1;
}

.card-checkbox :deep(.n-checkbox) {
  background: rgba(255, 255, 255, 0.9);
  border-radius: 4px;
  padding: 2px;
}

:deep(.n-card) {
  border-radius: 12px;
  transition: transform 0.2s, box-shadow 0.2s;
}

:deep(.n-card:hover) {
  transform: translateY(-2px);
  box-shadow: 0 8px 16px rgba(0, 0, 0, 0.1);
}

.card-content {
  display: flex;
  gap: 12px;
}

/* 封面区域 */
.cover-wrapper {
  position: relative;
  flex-shrink: 0;
  cursor: pointer;
}

.cover-img {
  width: 80px;
  height: 112px;
  object-fit: cover;
  border-radius: 6px;
}

.cover-placeholder {
  width: 80px;
  height: 112px;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: white;
  font-size: 24px;
  font-weight: bold;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}

.score-badge {
  position: absolute;
  top: -6px;
  right: -6px;
  background: linear-gradient(135deg, #f6d365 0%, #fda085 100%);
  color: white;
  padding: 2px 6px;
  border-radius: 8px;
  font-size: 11px;
  font-weight: 600;
}

.missing-badge {
  position: absolute;
  bottom: 6px;
  left: 6px;
  background: #f0a020;
  color: white;
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
}

.downloading-badge {
  position: absolute;
  bottom: 6px;
  right: 6px;
  background: #2080f0;
  color: white;
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: 4px;
}

.today-badge {
  position: absolute;
  top: 6px;
  left: 6px;
  background: #18a058;
  color: white;
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 10px;
  font-weight: 600;
}

/* 信息区域 */
.info-section {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.title {
  font-weight: 600;
  font-size: 15px;
  flex: 1;
  min-width: 0;
}

.tags-row {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.progress-row {
  font-size: 12px;
  color: var(--n-text-color-3);
}

.progress-info {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 4px;
}

.latest-ep {
  color: #18a058;
}

.missing-ep {
  color: #f0a020;
  cursor: pointer;
  text-decoration: underline;
}

.warning-row {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: #f0a020;
}

.smart-fetch-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;
  padding: 7px 8px;
  border: 1px solid #edf0f5;
  border-radius: 6px;
  background: #fafafa;
  font-size: 12px;
  line-height: 1.45;
  color: #4b5563;
}

.smart-fetch-row.compact {
  padding: 6px 8px;
}

.smart-fetch-main,
.list-smart-fetch {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  min-width: 0;
}

.smart-fetch-main span,
.list-smart-fetch span {
  overflow-wrap: anywhere;
}

.smart-fetch-next {
  flex-shrink: 0;
  color: #8a8f99;
  white-space: nowrap;
}

.list-smart-fetch {
  font-size: 12px;
  line-height: 1.45;
  color: #4b5563;
}

/* 操作行 */
.action-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: auto;
  padding-top: 4px;
}

.last-time {
  font-size: 11px;
  color: var(--n-text-color-3);
}

.action-buttons {
  display: flex;
  gap: 2px;
  opacity: 0.6;
  transition: opacity 0.2s;
}

.anime-card:hover .action-buttons {
  opacity: 1;
}

/* 预览弹窗 */
.preview-content {
  display: flex;
  gap: 16px;
}

.preview-cover {
  flex-shrink: 0;
}

.preview-cover img {
  width: 120px;
  height: 168px;
  object-fit: cover;
  border-radius: 8px;
}

.preview-cover-placeholder {
  width: 120px;
  height: 168px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: white;
  font-size: 32px;
  font-weight: bold;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}

.preview-info {
  flex: 1;
}

.preview-summary {
  font-size: 14px;
  line-height: 1.6;
  color: #333;
  margin-bottom: 12px;
}

.preview-meta {
  display: flex;
  gap: 8px;
}

.rule-preview {
  margin-top: 16px;
  padding: 12px;
  border: 1px solid #e0e0e6;
  border-radius: 8px;
  background: #fafafa;
}

.preview-header-line {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.preview-title {
  font-size: 14px;
  font-weight: 600;
}

.preview-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 260px;
  overflow-y: auto;
}

.preview-item {
  padding: 10px;
  border: 1px solid #e6e6eb;
  border-radius: 8px;
  background: #fff;
}

.preview-download {
  border-left: 3px solid #18a058;
}

.preview-replace {
  border-left: 3px solid #f0a020;
}

.preview-duplicate,
.preview-skip {
  opacity: 0.78;
}

.preview-item-title {
  font-size: 13px;
  font-weight: 500;
  line-height: 1.4;
}

.preview-item-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
  font-size: 12px;
  color: #666;
}

.preview-rename {
  margin-top: 6px;
  font-size: 12px;
  color: #18a058;
  word-break: break-all;
}

.diagnostics-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.diagnostics-header {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: flex-start;
}

.diagnostics-title-row {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}

.diagnostics-checked {
  font-size: 12px;
  color: #666;
}

.diagnostics-counters {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  margin-top: 8px;
  font-size: 12px;
  color: #666;
}

.diagnostics-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 10px;
}

.diagnostic-check {
  min-height: 128px;
  padding: 12px;
  border: 1px solid #e6e8ee;
  border-left-width: 3px;
  border-radius: 8px;
  background: #fff;
}

.diagnostic-check-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  font-weight: 600;
  font-size: 13px;
}

.diagnostic-summary {
  margin-top: 10px;
  font-size: 14px;
  font-weight: 600;
  color: #20242c;
  word-break: break-word;
}

.diagnostic-detail {
  margin-top: 6px;
  font-size: 12px;
  line-height: 1.5;
  color: #666;
  word-break: break-word;
}

.diagnostic-healthy {
  border-left-color: #18a058;
}

.diagnostic-warning {
  border-left-color: #f0a020;
}

.diagnostic-error {
  border-left-color: #d03050;
}

.diagnostic-unknown {
  border-left-color: #909399;
}

.diagnostics-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
}

.diagnostic-metric {
  min-width: 0;
  padding: 12px;
  border: 1px solid #e6e8ee;
  border-radius: 8px;
  background: #fafafa;
}

.diagnostic-metric span,
.diagnostic-metric small {
  display: block;
  color: #666;
  font-size: 12px;
  overflow-wrap: anywhere;
}

.diagnostic-metric strong {
  display: block;
  margin: 6px 0;
  font-size: 20px;
  line-height: 1.1;
  color: #20242c;
  font-variant-numeric: tabular-nums;
}

.diagnostic-section-title {
  margin-bottom: 8px;
  font-size: 14px;
  font-weight: 600;
}

.diagnostic-failures {
  padding-top: 2px;
}

.diagnostic-failure-item {
  display: grid;
  grid-template-columns: minmax(180px, 0.8fr) minmax(0, 1.2fr);
  gap: 12px;
  padding: 10px 0;
  border-top: 1px solid #edf0f5;
}

.diagnostic-failure-title {
  font-weight: 600;
  font-size: 13px;
  word-break: break-word;
}

.diagnostic-failure-meta {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
  margin-top: 6px;
}

.diagnostic-failure-reason {
  font-size: 12px;
  line-height: 1.5;
  color: #666;
  word-break: break-word;
}

.diagnostic-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  padding-top: 4px;
}

.diagnostic-action-reasons {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
  color: #888;
}

.diagnostic-action-result {
  padding: 10px 12px;
  border-radius: 8px;
  background: #f6f8fa;
  color: #333;
  font-size: 13px;
}

/* Modal 响应式 */
.modal-card {
  width: 600px;
  max-width: 95vw;
}

@media (max-width: 768px) {
  .modal-card :deep(.n-card__content) {
    padding: 12px !important;
  }

  .modal-card :deep(.n-form-item) {
    margin-bottom: 12px;
  }

  .modal-card :deep(.n-form-item-label) {
    padding-bottom: 4px;
  }

  .card-content {
    flex-direction: column;
  }

  .cover-wrapper {
    align-self: center;
    margin-bottom: 8px;
  }

  .cover-img,
  .cover-placeholder {
    width: 100px !important;
    height: 140px !important;
  }

  .info-section {
    width: 100%;
  }

  .title-row {
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
  }

  .action-row {
    flex-direction: column;
    align-items: stretch;
    gap: 8px;
  }

  .action-buttons {
    justify-content: center;
    opacity: 1 !important;
  }

  .last-time {
    text-align: center;
  }

  .card-checkbox {
    opacity: 1;
  }

  .preview-content {
    flex-direction: column;
    align-items: center;
  }

  .diagnostics-header,
  .diagnostic-failure-item {
    grid-template-columns: 1fr;
    flex-direction: column;
  }

  .diagnostics-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
