# 可配置重命名规则 - 快速开始

## 🎯 功能概述

你现在可以自由配置文件重命名规则，支持：
- ✅ 6种预设模板 (包括 Plex/Jellyfin 标准格式)
- ✅ 完全自定义模板 (使用8种变量)
- ✅ 实时预览效果
- ✅ 自动提取分辨率

## 📖 使用指南

### 1. 访问配置页面
打开浏览器访问: `http://localhost:7892`
进入 **系统配置** 页面

### 2. 选择重命名规则

滚动到 **文件重命名规则** 卡片

#### 方式一: 使用预设模板 (推荐)
1. 点击 **模板预设** 下拉菜单
2. 选择你喜欢的格式:
   - **媒体库标准格式** (推荐): `葬送的芙莉莲/Season 1/葬送的芙莉莲 S01E03.mkv`
   - **媒体库完整信息**: `葬送的芙莉莲/Season 1/葬送的芙莉莲 S01E03 [1080p] [ANi].mkv`
   - **字幕组风格**: `[ANi] 葬送的芙莉莲 - 03 [1080p].mkv`
   - **简洁格式**: `葬送的芙莉莲 - 03.mkv`
3. 查看 **预览效果**
4. 点击 **保存模板**

#### 方式二: 自定义模板
1. 在 **自定义模板** 输入框中编辑模板
2. 点击 **可用变量** 标签快速插入变量
3. 查看 **预览效果** 确认
4. 点击 **保存模板**

## 🔧 可用变量

| 变量 | 说明 | 示例 |
|------|------|------|
| `${title}` | 番剧名称 | 葬送的芙莉莲 |
| `${season}` | 季度 (数字) | 1 |
| `${seasonFormat}` | 季度 (两位) | 01 |
| `${episode}` | 集数 (数字) | 3 |
| `${episodeFormat}` | 集数 (两位) | 03 |
| `${fansub}` | 字幕组 | ANi |
| `${resolution}` | 分辨率 | 1080p |
| `${language}` | 语言 | CHS |

## 💡 模板示例

### Plex/Jellyfin 标准格式
```
${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}
```
输出: `葬送的芙莉莲/Season 1/葬送的芙莉莲 S01E03.mkv`

### 带分辨率的媒体库格式
```
${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat} [${resolution}]
```
输出: `葬送的芙莉莲/Season 1/葬送的芙莉莲 S01E03 [1080p].mkv`

### 字幕组粉丝风格
```
[${fansub}] ${title} - ${episodeFormat} [${resolution}]
```
输出: `[ANi] 葬送的芙莉莲 - 03 [1080p].mkv`

### 极简风格
```
${title} - ${episodeFormat}
```
输出: `葬送的芙莉莲 - 03.mkv`

### 详细信息格式
```
[${fansub}] ${title} S${seasonFormat}E${episodeFormat} [${resolution}] [${language}]
```
输出: `[ANi] 葬送的芙莉莲 S01E03 [1080p] [CHS].mkv`

## 🎬 实际效果

配置保存后，当下载完成时，系统会自动：
1. 检测到下载完成
2. 提取视频文件信息 (分辨率、扩展名)
3. 根据模板生成新文件名
4. 在 qBittorrent 中重命名文件
5. 更新数据库记录

## ❓ 常见问题

**Q: 重命名会影响做种吗？**
A: 不会，qBittorrent 内部管理文件映射，重命名后仍可正常做种。

**Q: 如何恢复默认设置？**
A: 从预设模板中选择 "媒体库标准格式" 并保存。

**Q: 支持中文路径吗？**
A: 完全支持，番剧名称可以是任何语言。

**Q: 模板语法错误怎么办？**
A: 系统会在保存前验证模板，如果有错误会提示你修正。

**Q: 为什么预览和实际文件名不一致？**
A: 预览使用固定示例数据，实际文件名基于真实订阅信息。

## 🚀 快速测试

1. 保存你的模板配置
2. 进入订阅列表
3. 点击某个订阅的 **收集按钮** 补全番剧
4. 等待下载完成 (约30秒后监控器会检测到)
5. 查看 qBittorrent 中的文件名是否符合预期

## 📚 更多信息

详细技术文档: [claudedocs/rename_system_implementation.md](claudedocs/rename_system_implementation.md)
下载实现分析: [claudedocs/download_implementation_analysis.md](claudedocs/download_implementation_analysis.md)
