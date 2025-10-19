# 订阅管理UI重构报告

## 概述

参考 ani-rss 的设计,完全重构了订阅管理页面,实现了更友好的两步式创建流程和卡片式展示。

## 主要变更

### 1. 前端变更

#### `web/src/views/Subscriptions.vue` - 完全重写

**新增功能**:

##### 两步式订阅创建流程
参考 ani-rss 的 Add.vue + Ani.vue 设计:

**第一步: 选择RSS源**
- Tab 1: 从 RSS 源 - 可以输入RSS地址或浏览RSS源
- Tab 2: 手动输入 - 直接填写番剧名称和RSS地址

**第二步: 详细配置**
- 番剧名称
- RSS 地址
- 字幕组
- 更新日期 (星期选择器)
- 季数
- 总集数
- 集数偏移
- 下载路径
- 过滤规则
- 启用/禁用开关

##### 卡片式网格展示
参考 ani-rss 的 List.vue 设计:

- **按星期分组**: 星期一到星期日,自动分组显示
- **卡片布局**: 每个订阅一张卡片,包含:
  - 封面图 (92x130px,使用渐变色背景+首字母)
  - 标题 (支持省略号截断)
  - RSS 地址 (2行省略)
  - 标签组:
    - 季数标签
    - 启用状态 (已启用/未启用)
    - 字幕组 (如果有)
    - 进度 (当前集数/总集数)
  - 最后下载时间 (相对时间显示)
  - 操作按钮 (编辑/删除)

- **响应式设计**:
  - 桌面端: 自动填充,最小380px
  - 移动端: 单列布局

- **空状态**: 无订阅时显示友好的空状态提示

##### 时间格式化
智能相对时间显示:
- X 分钟前
- X 小时前
- 昨天
- X 天前
- 完整日期 (超过7天)

##### 从RSS源页面跳转
支持从 RSS 源管理页面点击"订阅"按钮跳转过来,自动填充表单数据并直接进入第二步。

#### `web/src/api/index.ts` - 更新类型定义

扩展 `Subscription` 接口,新增字段:
```typescript
fansub?: string           // 字幕组
update_day?: string       // 更新星期 (0-6)
total_episodes?: number   // 总集数
current_episode?: number  // 当前集数
episode_offset?: number   // 集数偏移
filter_rules?: string     // 过滤规则
enabled?: boolean         // 是否启用
last_download_at?: string // 最后下载时间
rss_source_id?: number    // RSS源ID
source_type?: string      // 来源类型
```

#### 依赖更新

安装 `@vicons/antd` 图标库:
```bash
npm install @vicons/antd
```

使用图标:
- `EditOutlined` - 编辑按钮
- `DeleteOutlined` - 删除按钮

### 2. 后端变更

#### `internal/model/subscription.go` - 扩展订阅模型

新增字段 (参考 ani-rss):
```go
// 新增字段 - 参考ani-rss
Fansub          string     `json:"fansub" gorm:"type:varchar(100)"`         // 字幕组名称
UpdateDay       string     `json:"update_day" gorm:"type:varchar(10)"`      // 更新星期 (0-6)
TotalEpisodes   int        `json:"total_episodes" gorm:"default:0"`         // 总集数 (0表示未知)
CurrentEpisode  int        `json:"current_episode" gorm:"default:0"`        // 当前集数
EpisodeOffset   int        `json:"episode_offset" gorm:"default:0"`         // 集数偏移
FilterRules     string     `json:"filter_rules" gorm:"type:text"`           // 过滤规则
Enabled         bool       `json:"enabled" gorm:"default:true;index"`       // 是否启用
LastDownloadAt  *time.Time `json:"last_download_at"`                        // 最后下载时间
```

## UI 设计对比

### ani-rss 设计特点

1. **两步式创建**: 先选RSS/填基本信息 → 再配置详细参数
2. **卡片展示**: 封面+标题+标签+操作按钮
3. **星期分组**: 按更新日自动分组显示
4. **标签可视化**: 状态、季数、集数、字幕组等信息一目了然

### 我们的实现

✅ **完全实现了以上所有特点**:
- ✅ 两步式创建流程 (Tab切换 + 步骤切换)
- ✅ 卡片网格布局
- ✅ 按星期自动分组
- ✅ 标签系统 (季数、状态、字幕组、进度)
- ✅ 响应式设计
- ✅ 空状态处理
- ✅ 智能时间显示

## 用户体验提升

### 旧版设计 (表格式)
❌ 数据表格展示,信息密集
❌ 单步创建,字段较少
❌ 无分组,难以按更新日查找
❌ 操作按钮在行末,不够直观

### 新版设计 (卡片式)
✅ 卡片式展示,视觉友好
✅ 两步创建,引导明确
✅ 按星期分组,方便管理
✅ 信息可视化 (标签+图标)
✅ 支持从RSS源快速订阅
✅ 响应式布局,移动端友好

## 数据流

### 创建订阅流程

1. **从RSS源页面**:
   ```
   RSS源管理 → 查看番剧 → 点击订阅
   → 自动跳转到订阅管理
   → 预填表单 (name, rss_url, fansub, rss_source_id)
   → 直接进入第二步配置
   ```

2. **手动创建**:
   ```
   订阅管理 → 添加订阅
   → 第一步: 选择RSS源类型 (从RSS源 / 手动输入)
   → 填写基本信息
   → 第二步: 配置详细参数
   → 提交创建
   ```

### 展示逻辑

```
加载订阅列表
→ 按 update_day 字段分组 (0-6对应星期日到星期六)
→ 每个星期组显示该组的所有订阅卡片
→ 未设置 update_day 的订阅归入星期日组
```

## 构建结果

### 前端
```
✓ 3652 modules transformed
dist/assets/index-BGhHskUT.js   811.20 kB │ gzip: 236.35 kB
✓ built in 2.68s
```

### 后端
```
go build -o auto-rss cmd/server/main.go
✓ 编译成功
```

## 技术亮点

1. **组件化设计**: 清晰的组件职责划分
2. **类型安全**: TypeScript + 完整类型定义
3. **响应式布局**: Grid + Flexbox 实现自适应
4. **状态管理**: ref + computed 管理复杂状态
5. **用户反馈**: 加载状态、错误提示、确认对话框
6. **代码复用**: formatTime 等工具函数
7. **数据验证**: 表单验证 + 业务逻辑验证

## 待完善功能

以下功能可在后续迭代中实现:

1. **RSS数据解析**: 第一步点击"下一步"时实际调用API解析RSS
2. **封面图上传/抓取**: 替代当前的渐变色背景
3. **拖拽排序**: 支持手动调整订阅顺序
4. **批量操作**: 批量启用/禁用/删除
5. **搜索过滤**: 按名称、字幕组、状态筛选
6. **番剧详情**: 点击标题查看详细信息
7. **进度追踪**: 自动更新当前集数
8. **下载历史**: 显示下载记录列表

## 数据库迁移说明

由于新增了多个字段,首次启动时 GORM 会自动执行数据库迁移:

新增列:
- `fansub` VARCHAR(100)
- `update_day` VARCHAR(10)
- `total_episodes` INT DEFAULT 0
- `current_episode` INT DEFAULT 0
- `episode_offset` INT DEFAULT 0
- `filter_rules` TEXT
- `enabled` BOOLEAN DEFAULT TRUE
- `last_download_at` DATETIME

**兼容性**: 现有数据不受影响,新增字段都有默认值。

## 总结

成功参考 ani-rss 的优秀设计,实现了更现代、更友好的订阅管理界面。新版UI在视觉设计、用户体验、功能完整性方面都有显著提升,为后续功能扩展打下了良好基础。
