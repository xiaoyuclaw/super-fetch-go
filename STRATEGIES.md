# 抓取策略参考（给 agent 看的判断手册）

不是参数，是**遇到什么站点走什么路线**的判断方法。
工具只提供原语（`--via http` / `--via browser` / `--discover` / `--verify` / `--embedded`），
路线由 agent 根据页面情况自己决定。

---

## 策略一：HTTP 直取（默认）

适合：静态页、文章、文档、列表。

```bash
sf "<url>" -L normal
```

判断：直接 `sf "<url>"` 能拿到正文就走这条，不要开浏览器。

---

## 策略二：浏览器渲染

适合：SPA、无限滚动、需要 JS 才出现的内容、指纹级反爬。

```bash
sf "<url>" --via browser          # 有头 nodriver（默认）
sf "<url>" --via browser -w 3     # 等渲染
```

判断：
- HTTP 只拿到骨架/空壳 → 升浏览器；
- 页面有 `__NEXT_DATA__` / `__NUXT__` → 先试 `--embedded`（策略四）；
- 返回「验证 / 安全检测 / 请登录」→ 不要继续重试，换策略或放弃。

代价：浏览器比 HTTP 慢 5–20 倍，只在必要时用。

---

## 策略三：接口发现（三步走，可多轮）★

适合：**数据在 XHR/fetch 里**、页面正文是骨架的站点（列表页、信息流、后台表格）。

这是**流程**，不是参数。三步：

```
① 发现：浏览器打开页面，捕获接口
   sf "<url>" --discover

② 判断（agent 做，不要机械套规则）：
   - 这个接口是数据接口吗？看 body 是不是你要的条目（不是埋点/配置/推荐位）
   - 参数含义：limit/offset/page/cursor/token 各是什么
   - 是否需要 Referer / 特定头 / 登录 cookie
   - 需要翻页吗？怎么翻？
   不是数据接口 → 回 ①（换页面、点开详情、滚动加载后再发现）

③ 取数：用 HTTP 通道直接打接口
   sf "<api-url>" --via http -L full
   需要 POST / 特殊头 → sf "<url>" --discover --verify   # 用浏览器抓到的原请求回放
```

**多轮**很常见：先发现列表接口 → 拿到详情页 URL → 对详情页再发现一次详情接口。

判断要点：

- 别只看「是不是 JSON」——埋点、配置、推荐位也都是 JSON；
- 看条目字段（title/author/time/url）和数量，才分得清；
- 接口地址带 `created_at`/`ts` 这类时间戳参数时，翻页要跟着改，否则永远是同一页；
- 先小样本验证（1 页），确认结构和字段再批量翻页。

**收益**：通常比全浏览器快 5–20 倍，产出还更多更干净（JSON 无导航/广告/图片占位）。

**失败就回退**：接口需要登录态却没有、或者被签名保护 → 回策略二。

---

## 策略四：内嵌数据

适合：Next.js / Nuxt 等 SSR 框架，数据在 `__NEXT_DATA__`、`__NUXT__` 里。

```bash
sf "<url>" --embedded
```

判断：页面 HTML 里有 `__NEXT_DATA__` 关键字 → 先试这个，比浏览器快得多。

---

## 策略五：登录态

```bash
sf "<url>" --login -s 站点.json          # 交互登录并保存
sf "<url>" --via browser -s 站点.json    # 复用（自动按域名匹配）
sf "<url>" --via browser -s 站点.json --fresh   # 强制新浏览器，仍带登录态
```

判断：
- 需要登录才看得到内容 → 先 `--login`；
- 会话过期（返回登录页）→ 重新登录，不要反复重试。

---

## 选择顺序（经验值）

```
静态页        → 策略一
有 __NEXT_DATA__ → 策略四
列表/信息流/表格 → 策略三（三步走）
必须 JS 渲染   → 策略二
需要登录       → 策略五 + 上面任意一条
```

**不要**一上来就 `--via browser`：它是兜底，不是默认。
**不要**把接口发现做成自动黑盒：第 ② 步的判断需要看内容，机械规则会选错接口。

---

## 实战例子（sspai 首页）

```bash
# ① 发现
sf "https://sspai.com/" --discover
# → 列出 8 个候选：/api/v1/article/index/page/get、/api/v1/menu/page/get、/api/v1/setting/get …

# ② 判断：article/index/page/get 带 limit/offset/created_at，是列表数据接口
#     setting/get 是配置，menu/page/get 是栏目——都不是条目数据

# ③ 取数 + 翻页
sf "https://sspai.com/api/v1/article/index/page/get?limit=10&offset=0&created_at=<ts>&view=second" --via http -L full
sf "...offset=10..." --via http -L full
```

结果：3 页 166 条，2.5 秒；对比全浏览器 5 条 / 11.8 秒。
