# Fate 起名系统 — 功能规划

> fate 现仅保留核心起名计算能力。以下内容保留作为历史参考。

## 一、古诗词/名言名句/名著来源取名

### 1.1 目标
用户可以选择"诗词取名"模式，系统从古诗词、名言名句、经典名著中提取适合起名的字词，结合八字五行和五格数理进行推荐。

### 1.2 数据模型（已实现）
- `poem` 表：存储诗词/名言/名著条目（title, author, dynasty, content, keywords, tags, type）
- `poem_char` 表：存储诗词中每个可起名字的汉字（char, position, sentence, context）
- `type` 枚举：shi(诗), ci(词), fu(赋), jing(经), other(其他)

### 1.3 需要新增的功能

#### 1.3.1 数据导入
- [x] `cmd/seeddb` 增加诗词数据导入（chinese-poetry 数据集：全唐诗、宋词、诗经、楚辞、论语、四书五经、元曲、蒙学等）
- [x] 支持从 JSON 批量导入诗词数据（含繁体自动转简体映射）
- [x] 自动分词提取可起名字的汉字，写入 poem_char 表
- [x] 名言名句数据源：增广贤文、菜根谭、围炉夜话等（蒙学子集）
- [ ] 名著数据源：红楼梦、西游记、三国演义、水浒传中适合起名的字词

#### 1.3.2 诗词取名逻辑
- [x] 新增 `internal/poetry` 包，提供诗词取名功能
- [x] `PoetryNamer` 接口：
  - `FindByChar(char string) []PoemCharResult` — 查找包含指定字的诗词
  - `FindByWuXing(wuxing string, limit int) []PoemCharResult` — 按五行查找适合的字
  - `FindPair(wuxing1, wuxing2 string) []PoemPairResult` — 查找同句/同联中五行匹配的字对
  - `GetSentence(poemID int, position int) string` — 获取字的上下文句子
- [x] 诗词取名评分维度：
  - 出处权威性（经 > 诗 > 词 > 赋 > 其他）
  - 句意美好度（含"吉"关键词加分，`SentenceJiBonus`）
  - 五行匹配度（与八字喜用神匹配，`FindByWuXing`/`FindPair`）
  - 上下文连贯性（两字来自同一句/同一联加分，`PoemPairBoost` + `poetryPairBonus`）

#### 1.3.3 集成到生成流程
- [x] `FilterOption` 的 `PoetryMode` 字段语义明确：0=none(关闭), 1=prefer(优先), 2=only(仅诗词)
- [x] pipeline.generate() 中，当 PoetryMode 开启时：
  1. `character.has_poetry` 由 seeddb 导入后回填，`queryPoetrySet` 提供诗词字集合
  2. 同句/同联的字对通过 `PoemPairBoost` 采样并获得连贯性加分
  3. PoetryMode=only 时仅保留诗词字组合
- [x] `CharInfo.PoetryOrigin` 字段（title/author/dynasty/sentence/type）由 pipeline 与 name-score 填充

#### 1.3.4 命令行支持
- [x] `name --poetry=prefer` — 优先诗词取名
- [x] `name --poetry=only` — 仅从诗词中取名
- [x] `name detail` 输出中显示诗词来源信息

### 1.4 实施步骤
1. ✅ 创建 `internal/poetry` 包，实现查询逻辑
2. ✅ 在 `internal/repository` 添加 Poem/PoemChar 的查询方法
3. ✅ 扩展 FilterOption 和 pipeline.generate()
4. ✅ 扩展 NameResult 和 analysis 包
5. ✅ 更新 cmd/console name.go
6. ✅ 准备种子数据并导入（cmd/seeddb）

---

## 二、浏览页面

### 2.1 目标
提供 Web 界面，用户可以：
- 在线输入生日/姓氏/性别，生成名字
- 浏览 Top10 详细分析
- 查看所有候选名字
- 点击名字查看完整分析报告
- 浏览古诗词来源

### 2.2 技术方案

#### 方案A：嵌入式 Web 服务（推荐）
- 使用 Go 标准库 `net/http` 或 `gin`/`echo` 提供 API
- 前端使用嵌入式静态文件（embed.FS）
- 单二进制部署，无需额外依赖

#### 方案B：独立前端 + API
- 后端提供 RESTful API
- 前端独立部署（React/Vue）

### 2.3 API 设计
```
POST /api/generate
  Body: { "surname": "张", "born": "2024/06/15 10:30", "sex": "boy", "poetry_mode": 1 }
  Response: { "task_id": "xxx", "state": "done", "total": 1234 }

GET /api/generate/status?task_id=
  Response: { "task_id": "xxx", "state": "done" }

GET /api/generate/names?task_id=&page=1&size=20
  Response: { "names": [...], "total": 1234 }

GET /api/name-detail?task_id=&char1=瑞&char2=霖
  Response: { "name_result": {...} }

GET /api/poetry/search?keyword=瑞&author=&dynasty=&type=&page=1&size=20
  Response: { "results": [...], "total": 100, "page": 1, "size": 20, "poetry_available": true }
```

### 2.4 前端页面
- [x] 首页：输入表单（姓氏、生日、性别、选项）
- [x] 结果页：Top10 卡片 + 全部名字列表（分页）
- [x] 详情页：完整分析报告（五格、三才、周易、诗词来源）— NameDetailModal 弹窗
- [x] 诗词浏览页：按朝代/体裁/关键词浏览 — /poetry 路由

### 2.5 实施步骤
1. ✅ Web 框架：Go 标准库 net/http + embed.FS
2. ✅ `internal/http` 包，实现 RESTful API
3. ✅ `cmd/server/main.go`，启动 Web 服务
4. ✅ 前端页面（React + rsbuild，嵌入到二进制中）
5. ✅ 集成名字生成和详情查询 API
6. ✅ 添加诗词搜索 API

---

## 三、优先级排序

| 优先级 | 功能 | 预计工作量 | 状态 |
|--------|------|-----------|------|
| P0 | 诗词取名核心逻辑（internal/poetry） | 中 | 已完成 |
| P0 | 诗词数据导入（cmd/seeddb） | 中 | 已完成 |
| P1 | 集成到生成流程 | 小 | 已完成 |
| P1 | Web API 层 | 中 | 已完成 |
| P2 | 前端页面 | 大 | 已完成 |
| P2 | 诗词浏览功能 | 小 | 已完成 |
| P3 | 名著数据源（四大名著字词） | 中 | 未开始 |
