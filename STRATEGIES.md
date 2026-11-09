# 抓取策略参考（给 agent 看的判断手册）

**只有四种策略**：

| 策略 | 含义 | 命令 |
|---|---|---|
| **auto** | 先 cffi，拿不到再升浏览器 | `sf "<url>"` |
| **http** | 纯 cffi，不开浏览器 | `sf "<url>" --via http` |
| **browser** | 全程有头浏览器 | `sf "<url>" --via browser` |
| **discover** | 先分析、再决定走接口还是别的通道 | `sf "<url>" --discover` |

登录态（`-s`）和输出档位（`-L brief`）**不是策略**，是所有策略都能用的能力/参数。

---

## 选择顺序

```
先分析（discover 的 ①）
  ├─ HTML 已有内容      → http
  ├─ 有 __NEXT_DATA__   → --embedded + http
  ├─ XHR 有结构化列表    → 接口（http 打接口）
  ├─ 必须 JS 渲染       → browser
  └─ 需要登录           → 先 -s/--login，再按上面选
```

经验：**默认 auto**；确认是静态页就直接 http；确认要渲染才 browser；
数据在 XHR 里才用 discover 走接口。

---

## 策略一：auto（默认）

```bash
sf "<url>" -L normal
```

先 HTTP 拿，拿到的正文太短/是空壳，自动升浏览器。多数场景用它就够。

判断什么时候不该用它：已知必须渲染的站（SPA 首页、无限滚动），直接走 browser 少一次往返。

---

## 策略二：http（纯 cffi）

```bash
sf "<url>" --via http
```

适合：文章、文档、列表页、搜索结果页（bing）、后端返回 JSON 的接口。

优点：1 秒级，无浏览器开销。
缺点：拿不到 JS 渲染的内容；反爬站可能返回风控页（36kr 就是）。

**判断依据**：`curl` 能拿到正文的站，这里就能拿到，而且带完整 Chrome 头序。

---

## 策略三：browser（全程有头浏览器）

```bash
sf "<url>" --via browser
sf "<url>" --via browser -w 3        # 多等一会渲染
SF_BROWSER=chromedp sf "<url>" --via browser   # 换后端
```

适合：SPA、懒加载、必须执行 JS 才有内容的页、指纹类反爬（36kr）。

代价：比 http 慢 5–50 倍（实测维基：http 1.1s vs browser 59s），
而且首页/列表页抓下来的 markdown 噪声很重（空链接、图片占位、卡片重复）。

**只在 http/auto 明确失败时用。**

---

## 策略四：discover（先分析，再决定）

**这是决策层，不是"一定要用接口"。**

```
① 分析
   - HTML 里有没有条目内容？（有 → 就不用折腾接口了）
   - XHR/fetch 里有没有结构化列表？   sf "<url>" --discover
   - 有没有 __NEXT_DATA__？            sf "<url>" --embedded
   - 有没有风控/登录墙？

② 路由（四选一）
   - HTML 已有        → http
   - XHR 有列表       → 接口：sf "<api>" --via http
   - 必须渲染         → browser
   - 要登录           → 会话 + 上面任意一条

③ 执行；不理想就回到 ①（可多轮：列表接口 → 详情接口）
```

判断要点：

- 别只看"是不是 JSON"——埋点、配置、推荐位也是 JSON，要看字段和条目数
- 接口带 `created_at`/`ts` 这类时间戳参数时，翻页要跟着改
- 先小样本验证结构，再批量翻页
- **分析完发现"没有值得用的接口"是正常结论**，直接转 http/browser 即可

**收益**：接口型站点比全浏览器快 5–20 倍、产出更干净；
判断错了（强行找接口）会比 http/browser 都差——实测 SSR 站点和搜索页就是这样，白花 8–10 秒。

---

## 能力（不是策略）

### 登录态

```bash
sf "<url>" --login -s 站点.json          # 交互登录并保存
sf "<url>" --via browser -s 站点.json    # 复用（按域名自动匹配）
sf "<url>" --via browser -s 站点.json --fresh   # 强制新浏览器，仍带登录态
```

三种后端都支持；session 文件是 Playwright storageState 格式，**跨后端通用**。

### 输出档位

```bash
sf "<url>" -L brief     # 800 字
sf "<url>" -L normal    # 默认
sf "<url>" -L full      # 全量
sf "<url>" --max-chars 20000
```

注意：`brief` 是**硬截断**，结构化任务（要标题层级/表格）别用——
实测维基长文被截到 787 字、只剩 2 个标题，全花在语言列表上。

### 其它常用

| 参数 | 作用 |
|---|---|
| `-w/--wait` | 渲染后多等几秒 |
| `-o/--output` | 落盘 |
| `-j/--json` | 结构化输出（含 via 字段） |
| `--embedded` | 取 SSR 内嵌数据 |
| `--eval` | 页面里执行 JS |
| `-s` + `--remember` | 会话读写 |
| `--fresh` | 不用 daemon，强制新浏览器 |

---

## 实战例子（sspai 首页）

```bash
# ① 分析
sf "https://sspai.com/" --discover
# → 候选 8 个：article/index/page/get、menu/page/get、setting/get …

# ② 判断：article/index/page/get 带 limit/offset，是列表数据接口
#     setting/get 是配置、menu/page/get 是栏目——都不该用

# ③ 取数 + 翻页
sf "https://sspai.com/api/v1/article/index/page/get?limit=10&offset=0&created_at=<ts>&view=second" --via http -L full
```

结果：10 条 / 0.5s（发现 9s 是一次性成本）。
对比全浏览器 5 条 / 4.1s 且九成噪声。
