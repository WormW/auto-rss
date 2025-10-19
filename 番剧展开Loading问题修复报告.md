# 番剧展开Loading问题修复报告

## 🐛 问题诊断

**现象**: 搜索番剧后点击展开，一直显示loading，没有任何API请求发出

**根本原因**: **Naive UI collapse组件事件绑定错误**

## 🔍 问题分析

### 错误的代码
```vue
<!-- ❌ 错误：使用了不存在的事件名 -->
<n-collapse @item-header-click="handleCollapseChange">
  <n-collapse-item :name="anime.title" :title="anime.title">
    ...
  </n-collapse-item>
</n-collapse>
```

### 问题
1. `@item-header-click` 不是Naive UI collapse组件的有效事件
2. 点击展开时事件从未触发
3. `handleCollapseChange` 函数从未被调用
4. 字幕组数据从未加载
5. loading状态一直保持true

## ✅ 修复方案

### 1. 使用正确的事件名

Naive UI的`n-collapse`组件应该使用 `@update:expanded-names` 事件：

```vue
<!-- ✅ 正确：使用Naive UI的标准事件 -->
<n-collapse @update:expanded-names="handleCollapseChange">
  <n-collapse-item :name="anime.title" :title="anime.title">
    ...
  </n-collapse-item>
</n-collapse>
```

### 2. 更新事件处理函数

`@update:expanded-names` 传递的是**数组**，包含所有展开项的name：

```typescript
// ❌ 旧代码：期望单个name
const handleCollapseChange = async (name: string | number) => {
  const anime = animeList.value.find(a => a.title === name)
  if (anime && !fansubGroups.value[anime.url]) {
    await loadFansubGroups(anime)
  }
}

// ✅ 新代码：处理展开名称数组
const expandedNames = ref<(string | number)[]>([])

const handleCollapseChange = async (names: (string | number)[]) => {
  console.log('Collapse expanded names:', names)

  // 找出新展开的项
  const newExpanded = names.filter(name => !expandedNames.value.includes(name))
  console.log('Newly expanded:', newExpanded)

  // 更新展开状态
  expandedNames.value = names

  // 为每个新展开的项加载字幕组
  for (const name of newExpanded) {
    const anime = animeList.value.find(a => a.title === name)
    if (anime && !fansubGroups.value[anime.url]) {
      await loadFansubGroups(anime)
    }
  }
}
```

### 3. 保留调试日志

添加了详细的console.log用于调试：

```typescript
console.log('Collapse expanded names:', names)
console.log('Newly expanded:', newExpanded)
console.log('Found anime for:', name, anime)
console.log('Anime URL:', anime.url)
console.log('Already loaded:', !!fansubGroups.value[anime.url])
console.log('Loading fansub groups...')
```

## 🎯 工作流程

修复后的完整流程：

```
用户点击展开番剧
  ↓
触发 @update:expanded-names 事件
  ↓
handleCollapseChange(["葬送的芙莉莲"])
  ↓
检测到新展开的项: ["葬送的芙莉莲"]
  ↓
通过标题查找anime对象
  ↓
检查fansubGroups[anime.url]是否已缓存
  ↓
未缓存 → 调用 loadFansubGroups(anime)
  ↓
设置 loadingGroups[anime.url] = true
  ↓
调用API: mikanApi.getFansubGroups(anime.url)
  ↓
GET /api/v1/mikan/fansub-groups?url=https://...
  ↓
接收响应: { data: [...字幕组列表...] }
  ↓
存储: fansubGroups[anime.url] = result.data
  ↓
设置 loadingGroups[anime.url] = false
  ↓
UI更新：显示字幕组列表和语言选择按钮
```

## 📊 验证步骤

1. **刷新浏览器**: `http://localhost:7892`

2. **打开开发者工具**: F12 → Console标签

3. **搜索番剧**:
   - 点击"搜索番剧"
   - 输入"葬送"
   - 点击搜索

4. **展开番剧**:
   - 点击任一番剧展开
   - **应该看到Console输出**:
     ```
     Collapse expanded names: ["葬送的芙莉莲"]
     Newly expanded: ["葬送的芙莉莲"]
     Found anime for: 葬送的芙莉莲 {title: "...", url: "..."}
     Anime URL: https://mikanime.tv/Home/Bangumi/3349
     Already loaded: false
     Loading fansub groups...
     Starting to load fansub groups for: https://...
     Calling API...
     API response: {data: [...]}
     Stored groups: [...]
     Loading complete, loading state: false
     ```

5. **检查UI**:
   - Loading图标应该消失
   - 显示字幕组列表
   - 显示语言选择按钮（简体中文/繁體中文）

## 🚨 可能的问题

### 如果仍然没有API请求

检查Network标签是否有代理错误：

**解决方案**:
```bash
# 配置no_proxy排除localhost
export no_proxy="localhost,127.0.0.1,::1"
export NO_PROXY="localhost,127.0.0.1,::1"

# 或临时关闭代理
unset ALL_PROXY HTTP_PROXY HTTPS_PROXY
```

### 如果API返回错误

检查Console是否有错误信息：
- CORS错误 → 检查后端CORS配置
- 404错误 → 检查路由配置
- 超时 → 检查网络/代理设置

## 📁 修改的文件

### `web/src/components/AnimeSearch.vue`

**修改点**:
1. Line 36: `@item-header-click` → `@update:expanded-names`
2. Line 367-392: 完全重写`handleCollapseChange`函数
3. Line 295-316: 添加调试日志到`loadFansubGroups`

## 🎉 预期结果

修复后，用户应该能够：

1. ✅ 搜索番剧
2. ✅ 展开番剧查看字幕组
3. ✅ 看到字幕组的标签（1080P、简体、繁体等）
4. ✅ 看到语言选择按钮（简体中文/繁體中文）
5. ✅ 选择语言后订阅
6. ✅ 订阅表单自动填充语言信息

## 🔧 构建信息

```bash
✓ 3656 modules transformed
dist/assets/index-DT1IS8Qm.js   829.34 kB │ gzip: 241.35 kB
✓ built in 2.48s
```

## 📚 技术要点

### Naive UI Collapse事件

Naive UI的`n-collapse`组件提供以下事件：

- `@update:expanded-names`: 展开状态变化时触发，传递展开项name数组
- `@item-header-click`: **不存在** (这是错误的假设)

正确的用法：
```vue
<n-collapse v-model:expanded-names="expandedNames" @update:expanded-names="handleChange">
  <n-collapse-item name="item1" title="Item 1">...</n-collapse-item>
</n-collapse>
```

### 响应式数组处理

处理展开状态时需要跟踪历史状态：
```typescript
const expandedNames = ref<(string | number)[]>([])

const handleChange = (names: (string | number)[]) => {
  // 找出新展开的项
  const newExpanded = names.filter(n => !expandedNames.value.includes(n))

  // 为新展开项加载数据
  for (const name of newExpanded) {
    // ... load data
  }

  // 更新状态
  expandedNames.value = names
}
```

## 🎯 总结

**问题**: 事件绑定错误 (`@item-header-click`)
**修复**: 使用正确事件 (`@update:expanded-names`)
**结果**: 展开番剧时正确触发字幕组加载，显示语言选择功能

这是一个典型的**API误用**导致的问题，不是逻辑错误，而是对Naive UI组件API理解不正确。
