# 订阅发现：元数据与 RSS 来源核验

核验日期：2026-09-27。本文为方案研究，不表示以下发现流程已经实现。只读取公开文档、网页和 RSS；没有下载种子、调用下载器、提交订阅或读取生产凭据。

## 结论

自然语言指定作品和季后，自动查找 Bangumi 条目、补全别名、构造资源查询并交给人工复核，具备可行的数据基础。最初方案使用 **Bangumi + Mikan + Nyaa**；随后按用户要求补充核验并接入 **DMHY（动漫花园）**。新增网站数量不是首要问题，核心是保留每个匹配结论的来源，并把“候选收集”和“允许下载的规则”分开。

- **Bangumi**：提供作品身份、标题、别名、日期、类型、集数和关联条目；没有可直接通用于所有动画的数字季序字段。不能把搜索第一名直接作为“第二季”。
- **Mikan**：实际番剧页同时提供自身番剧 ID、Bangumi 链接和按字幕组划分的 RSS，可以取得明确的跨站映射；同一字幕组仍可能一集多版本。
- **Nyaa**：实际 RSS 支持搜索、类别和 trusted 筛选，能自动生成并试查查询 URL；关键词命中与 trusted 标记都不能证明资源属于目标季或包含目标语言。
- **AniList**：可作为后续别名、播出日期和关联关系的交叉核验源；首版无需增加它的运行依赖。

## Bangumi：自动补全 ID 的主要来源

依据 [官方 v0 OpenAPI](https://github.com/bangumi/api/blob/master/open-api/v0.yaml)：

| 接口/字段 | 可用于自动化的内容 | 限制 |
| --- | --- | --- |
| `POST /v0/search/subjects` | `keyword` 搜索；`filter.type=[2]` 限定动画；`air_date` 约束播出日期；保留多个候选 | 官方明确标注实验性；排名表示搜索相关性，不表示用户的季序选择正确 |
| `GET /v0/subjects/{subject_id}` | `name`、`name_cn`、`date`、`platform`、`infobox`、`eps`、`total_episodes` | `infobox` 是 Wiki 数据；别名不是每个条目必有的固定顶层字段 |
| `GET /v0/subjects/{subject_id}/subjects` | 关联条目的 ID、类型、名称和 `relation` | 返回的关联也包括书籍、歌曲、衍生作品；不能按数组顺序或时间顺序直接数季 |
| `GET /v0/episodes?subject_id=…&type=0` | 分页获取本篇章节，检查集数和 `ep`/`sort` | 单页最多 200；`ep` 与 `sort` 含义不同，特别篇不能混作本篇集数 |

官方对 `eps` 的定义是从 Wiki 解析的集数，对 `total_episodes` 的定义是数据库中的章节数量。二者不能无条件作为相同的“预计正片总集数”；章节数据也可能尚未录全。`series` 表示书籍系列主条目，不是动画季序。[来源](https://github.com/bangumi/api/blob/master/open-api/v0.yaml)

实际公开样本：[`/v0/subjects/576351`](https://api.bgm.tv/v0/subjects/576351) 返回《黑猫与魔女的教室》、日文名 `黒猫と魔女の教室`、`date=2026-04-12`、`platform=TV`、`eps=24`、`total_episodes=24`。`infobox` 的“别名”含 `Kuroneko to Majo no Kyoushitsu` 等罗马字和英文名。该别名可直接用作 Nyaa 查询候选，减少手工试词。

实际 [`/subjects/576351/subjects`](https://api.bgm.tv/v0/subjects/576351/subjects) 同时返回书籍、衍生和片头/片尾曲条目，说明关系遍历必须先检查条目类型及关系意义。

**季的匹配建议（设计建议）**：先保留用户输入的“第几季/第几部/Part”，结合名称、年份、TV/Web/剧场版、前后作关系提出候选。区分用户季序、Bangumi 条目边界、资源发布者编号和媒体库 `season_number`。分割放送、续篇改名、连续集号、重制版和剧场版存在歧义时，需要用户选定条目或集号映射，不应自动猜测。

请求必须设置包含开发者、应用名、版本及项目主页的明确 User-Agent，避免默认请求库 UA；这是 [Bangumi 官方建议](https://github.com/bangumi/api/blob/master/docs-raw/user%20agent.md)。详情接口规范标注 `cache with 300s`；本次所查官方文档没有提供可据此承诺的统一每分钟请求限额。

## Mikan：优先使用页面已有映射与专属 RSS

2026-09-27 实际读取 [Mikan 番剧页 `/Home/Bangumi/3928`](https://mikanime.tv/Home/Bangumi/3928)，页面提供：

- 整部番剧 RSS：`/RSS/Bangumi?bangumiId=3928`。
- Bangumi 条目链接：`https://bgm.tv/subject/576351`。
- 字幕组 RSS，例如 `/RSS/Bangumi?bangumiId=3928&subgroupid=615`；同页还包含其他组的 ID。

这里的 `bangumiId=3928` 是 **Mikan ID**，与 **Bangumi subject ID `576351`** 不同。应读取页面链接，避免仅凭同名搜索做映射，也不能混用两站 ID。

实际请求 [字幕组 615 RSS](https://mikanime.tv/RSS/Bangumi?bangumiId=3928&subgroupid=615) 成功，返回 72 条。最新一集 24 同时存在以下版本：

```text
[黒ネズミたち] 黑猫与魔女的教室 / Kuroneko to Majo no Kyoushitsu - 24 (ABEMA 1920x1080 AVC AAC MKV)
[黒ネズミたち] 黑猫与魔女的教室 / Kuroneko to Majo no Kyoushitsu - 24 (CR 1920x1080 AVC AAC MKV)
[黒ネズミたち] 黑猫与魔女的教室 / Kuroneko to Majo no Kyoushitsu - 24 (Baha 1920x1080 AVC AAC MP4)
```

因此，“指定作品 + 字幕组 RSS”仍需版本偏好及每集去重，不等于每集只有一条资源。标题没有明确语言时不能从组名推定简体/繁体。页面解析器也需要结构变化检测；本次未取得可作为稳定契约的官方开放 API、限频或 HTML 版本保证。

单番订阅应优先用番剧/字幕组范围的 RSS。账户汇总类 `/RSS/MyBangumi` 不是单部作品范围，本项目现有保护应继续保留。

## Nyaa：生成 URL 可自动化，精准性需要样本证明

### 公开源码证据

核验公开仓库 `nyaadevs/nyaa`，源码版本 `4fe0ff5b1aa7ec7c9bb2667d97e10ce2a318c676`。此版本用于解释公开实现，**不代表已确认 nyaa.si 部署的确切版本或搜索后端**。

[`nyaa/views/main.py`](https://github.com/nyaadevs/nyaa/blob/4fe0ff5b1aa7ec7c9bb2667d97e10ce2a318c676/nyaa/views/main.py) 接受：

| 参数 | 源码含义 |
| --- | --- |
| `page=rss` 或 `/rss` 路径 | RSS 输出 |
| `q` | 搜索关键词/表达式 |
| `c` | 类别，格式 `主类_子类` |
| `f` | `0` 不加质量标记筛选，`1` 排除 remake，`2` 只要 trusted |
| `u` | 按真实上传账号查找 `uploader_id`，不是标题中的字幕组字符串 |

类别初始化来自 [`db_create.py`](https://github.com/nyaadevs/nyaa/blob/4fe0ff5b1aa7ec7c9bb2667d97e10ce2a318c676/db_create.py)：Anime 下分 AMV、English-translated、Non-English-translated、Raw；当前实测 RSS 的 `1_2` 为 English-translated，`1_4` 为 Raw。**Non-English-translated 也不等于中文**，需要继续辨别具体语言。

[`help.html`](https://github.com/nyaadevs/nyaa/blob/4fe0ff5b1aa7ec7c9bb2667d97e10ce2a318c676/nyaa/templates/help.html) 与 [`search.py`](https://github.com/nyaadevs/nyaa/blob/4fe0ff5b1aa7ec7c9bb2667d97e10ce2a318c676/nyaa/search.py) 记录普通词组合、`"短语"`、`-排除词`、`|` OR；明确提示括号中的引号短语可能出现非预期结果。源码可使用 Elasticsearch 或数据库搜索，不能承诺所有复杂表达式跨实现完全相同。首版宜使用少量简单查询并合并候选，避免由模型自由拼接任意复杂查询。

帮助源码对 trusted 的定义是 **由 trusted 用户上传**。它不保证作品、季、字幕语言、资源安全性或版本偏好符合本次订阅；`remake` 也有再编码、重封装等特定含义，不能机械理解为质量差。

### 实际站点验证

`https://nyaa.si/help` 本次请求返回 504，但以下公开 RSS 请求成功，说明不能据一个帮助页错误认定整个来源不可用：

1. [宽查询](https://nyaa.si/?page=rss&q=Kuroneko+to+Majo&c=1_0&f=0)：HTTP 200，75 条。样本混有 Raw 和 English-translated，含单集及 `01-24`/`batch` 合集，亦有多种分辨率。
2. [收紧查询](https://nyaa.si/?page=rss&q=%22Kuroneko+to+Majo%22+1080+-batch&c=1_2&f=2)：HTTP 200，72 条。逐条检查这些条目均为 `categoryId=1_2`、`trusted=Yes`，标题均含 `1080` 且没有 `batch`。样本仍有同一集的 AVC/HEVC、多上传者和 `v2` 修订版本。

第二条查询的解码参数是：

```text
page=rss
q="Kuroneko to Majo" 1080 -batch
c=1_2
f=2
```

这些请求只核验了本次样本。没有逐项 A/B 证明每个查询操作符的完整行为，也没有实测 `u`、复杂 OR 或中文分词。RSS 的近期条目数不是全集资源总量；同样，排除 `batch` 不能排除没有这个单词的合集。只有在目标语种、上传者、单集/合集和季序也明确时，才能把候选升级为可执行规则。

**URL 构造建议（设计建议）**：程序从确认过的字段使用标准 URL 编码器生成参数，而非直接保存模型猜出的 URL。模型提出查询候选，服务读取 RSS、解析标题与元数据、展示匹配和排除样本；人工确认后冻结规则版本。RSS 层先收窄，服务端再用与预览相同的规则决定是否下载。

## DMHY：搜索结果页直接提供 RSS

2026-09-27 读取 [站点首页](https://share.dmhy.org/) 及[目标番剧搜索页](https://share.dmhy.org/topics/list?keyword=%E8%90%BD%E7%AC%AC%E8%B4%A4%E8%80%85%E7%9A%84%E5%AD%A6%E9%99%A2%E6%97%A0%E5%8F%8C)，均成功。搜索表单提交 `/topics/list` 的 `keyword` 参数，搜索结果内 RSS 链接指向 `/topics/rss/rss.xml?keyword=...`。首页和结果页都存在 `team_id:数字` 链接；实现现在先从宽搜结果读取团队 ID，再由人工选择后生成窄查询。

同日有限请求：“落第贤者的学院无双”RSS 返回 54 条；追加裸词 `1080` 返回 0 条，追加 `1080p` 返回 51 条。说明不能把 Nyaa 的画质检索词直接照搬。因此默认适配器按元数据作品名生成候选查询，并由本地规则校验画质；自定义 `dmhy_query` 保留用户表达式。

实际 RSS 中 `<link>` 是发布详情 HTML 页，`<enclosure>` 是带 Base32 BTIH 的 magnet；`length="1"` 是占位值。解析需要选择 magnet、转换真实 info-hash，并把该占位长度当作未知，避免资源去重和最小文件大小过滤出错。本次只读 RSS，没有访问 magnet 的 tracker、下载媒体或提交下载器。

核验覆盖公开站点当前返回格式，不承诺历史完整覆盖或搜索语法永久稳定。HTTP 请求仍使用既有超时、响应大小与来源跳转限制。完整预览结果见 [动漫花园验收样例](../examples/subscription-discovery-dmhy.md)。

## 可选扩展源：AniList

[AniList 官方 Media schema](https://docs.anilist.co/reference/object/media) 提供 `title`、`synonyms`、`startDate`、`format`、`episodes`、`relations`、`season` 和 `seasonYear`。可以补充英文/罗马字别名并交叉核验作品关系；它不是下载来源。

其中 `season`/`seasonYear` 是作品首播的季度和年份，**不是第二季、第三季的数字序号**。schema 列有 `idMal`，本次所查 Media 字段未见可直接依赖的 Bangumi ID 字段，因此跨站身份仍需匹配证据，不能自动把 ID 互换。

[官方限频说明](https://docs.anilist.co/guide/rate-limiting) 写常规 90 次/分钟，同时当日页面仍有“降级期间 30 次/分钟”的警告；实现应遵循实际响应中的 `X-RateLimit-*`、429 与 `Retry-After`，不能硬编码按 90 次跑满。此次只核对官方文档，没有额外调用 AniList GraphQL。

更多资源站仍需先核验其 RSS、搜索参数及资源标识，扩展来源并不能代替匹配质量控制。

## 请求成本与复核输出建议

以下是本项目实现建议，不是网站官方限额：

- 每次发现固定查询预算，例如至多 3 个标题变体、每源 2–3 个搜索变体；单站并发 1，设置超时。失败保留已取得的来源，不无界重试。
- 元数据详情缓存至少沿用 Bangumi 文档中的 300 秒；标题搜索短期缓存，人工确认的跨站映射长期保存并记录来源时间。
- 同一 RSS URL 合并请求、缓存预览；定时轮询沿用服务现有合理间隔并加随机抖动。只有服务实际返回 ETag/Last-Modified 时才使用条件请求，不预设站点支持。
- 遵循 429/`Retry-After`，遇 403/验证码暂停该来源并报告；不要让自然语言规划变成持续网页抓取。
- 复核结果应含原始需求、候选条目 ID/页面、季序解释、语言与版本要求、生成 RSS、查询时间、命中样本、排除样本及原因、未知字段、首轮补集策略。
- 空 RSS 要区分“站点读取失败”“查询有效但当前无结果”“规则过严”，不能用同一成功文案掩盖；无结果时不能自动移除用户的季或语言条件。
- 默认每集只选一个候选；`v2`、合集、不同编码和不同发布平台的处理必须明确，避免从精准查询退化为重复下载。

推荐将自动化边界放在 **生成可审查的订阅草案**：`prepare_subscription` 汇总证据和样本，用户确认后 `confirm_subscription` 才调用现有订阅创建逻辑。未来调度沿用人工确认过的同一套规则；匹配条件发生变化应再次给出差异供复核。
