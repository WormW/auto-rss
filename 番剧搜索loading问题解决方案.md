# 番剧搜索展开只显示loading的问题解决方案

## 问题现象

用户搜索番剧后,点击展开番剧查看字幕组时,只显示loading图标,无法加载字幕组列表。

## 问题原因

系统代理设置导致浏览器通过代理服务器(socks5://127.0.0.1:7890)访问本地API(localhost:7892),但代理服务器无法正确处理到localhost的连接,导致请求失败并返回空响应。

## 验证方法

使用curl测试API:

```bash
# 通过代理访问 - 失败
curl "http://localhost:7892/api/v1/mikan/search?text=test"
# 输出: curl: (52) Empty reply from server

# 绕过代理访问 - 成功
curl --noproxy "*" "http://localhost:7892/api/v1/mikan/search?text=test"
# 输出: {"data":{"groups":[...],"seasons":[]}}
```

## 解决方案

###选项1: 配置代理排除localhost (推荐)

#### macOS
1. 打开"系统设置" → "网络"
2. 点击当前网络 → "详细信息"
3. 选择"代理"标签页
4. 在"排除主机名和域名"中添加:
   ```
   localhost, 127.0.0.1, ::1
   ```
5. 点击"好"保存

#### Windows
1. 打开"设置" → "网络和Internet" → "代理"
2. 在"手动代理设置"中找到"请勿将代理服务器用于以下条目"
3. 添加:
   ```
   localhost;127.0.0.1
   ```

#### Linux
编辑 `~/.bashrc` 或 `~/.zshrc`:
```bash
export no_proxy="localhost,127.0.0.1,::1"
export NO_PROXY="localhost,127.0.0.1,::1"
```

### 选项2: 浏览器扩展配置

如果使用浏览器代理扩展(如SwitchyOmega):

1. 打开扩展设置
2. 编辑代理配置
3. 添加规则:
   - 条件类型: "域名"
   - 条件: "localhost"
   - 操作: "直接连接"

### 选项3: 临时关闭代理(开发环境)

开发时临时关闭系统代理:

```bash
# macOS - 使用网络设置
# 或者临时取消代理环境变量
unset ALL_PROXY HTTP_PROXY HTTPS_PROXY

# 重新打开浏览器访问 http://localhost:7892
```

### 选项4: 修改hosts (不推荐)

如果以上方案都不起作用,可以使用非localhost的域名:

1. 编辑 `/etc/hosts`:
   ```
   127.0.0.1  auto-rss.local
   ```

2. 修改前端API baseURL:
   ```typescript
   // web/src/api/index.ts
   export const api = axios.create({
     baseURL: 'http://auto-rss.local:7892/api/v1',
     timeout: 10000
   })
   ```

3. 访问 `http://auto-rss.local:7892`

## 验证修复

配置完成后,重启浏览器并测试:

1. 打开 `http://localhost:7892`
2. 点击"搜索番剧"
3. 搜索"葬送的芙莉莲"
4. 点击展开番剧
5. 应该能看到字幕组列表和语言选择按钮

## 后端API正常工作确认

后端API已验证可以正常返回数据:

```bash
# 搜索API
$ curl --noproxy "*" "http://localhost:7892/api/v1/mikan/search?text=葬送" | jq .
{
  "data": {
    "groups": [...],
    "seasons": [...]
  }
}

# 字幕组API
$ curl --noproxy "*" "http://localhost:7892/api/v1/mikan/fansub-groups?url=..." | jq .
{
  "data": [
    {
      "name": "MCE汉化组",
      "rss": "https://mikanime.tv/RSS/...",
      "tags": ["1080P", "繁体", "简体"],
      "episodes": ["25", "24", "23"]
    }
  ]
}
```

## 技术细节

- **前端**: axios通过浏览器发起HTTP请求
- **浏览器**: 遵循系统代理设置 (ALL_PROXY=socks5://127.0.0.1:7890)
- **代理服务器**: 无法正确处理localhost连接,返回空响应
- **解决**: 配置no_proxy排除localhost,使浏览器直接连接本地服务

## 相关文件

- 前端API配置: `web/src/api/index.ts`
- 搜索组件: `web/src/components/AnimeSearch.vue`
- 后端路由: `internal/api/router/router.go`
- Mikan服务: `internal/service/mikan/mikan.go`

## 推荐配置(macOS示例)

```bash
# ~/.zshrc 或 ~/.bashrc
export ALL_PROXY=socks5://127.0.0.1:7890
export HTTP_PROXY=http://127.0.0.1:7890
export HTTPS_PROXY=http://127.0.0.1:7890
export no_proxy="localhost,127.0.0.1,::1,*.local"  # 关键配置
export NO_PROXY="localhost,127.0.0.1,::1,*.local"   # 关键配置
```

重启终端和浏览器生效。
