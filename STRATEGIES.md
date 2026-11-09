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

## 策略三：先分析，再路由 ★（决策层，不是固定流水线）

拿到一个页面，**先判断数据在哪、值不值得绕路**，再决定用哪条通道：

```
① 分析
   - 页面正文是完整的，还是只剩骨架？（对比 HTML 与渲染后）
   - 有没有 XHR/fetch 返回你要的数据？sf "<url>" --discover 看候选
   - 有没有 __NEXT_DATA__ / __NUXT__？（sf "<url>" --embedded）
   - 有没有风控/登录墙？（验证页、请登录、空壳）

② 路由（三选一，不是必选接口）
   - 接口重要且可直取   → 用接口：sf "<api>" --via http
   - 接口不重要/不存在  → 不走接口，直接选通道：
        静态可读 → sf "<url>" --via http
        必须渲染 → sf "<url>" --via browser
   - 需要登录          → 先 -s/--login，再按上面选

③ 执行；如果不理想，回到 ① 重新分析（可多轮：列表接口 → 详情接口）
```

**关键**：discovery 的产出不是"一定要用接口"，而是**一个路由决定**。
很多站点分析完的结论就是"没有接口，用 http/browser 就行"——那 discovery 已经完成任务了，
不该再硬找接口（硬找就是我们之前 H1/H2/H8 拿到 0 条的原因）。

判断依据（按优先级）：

1. 页面 HTML 里有没有条目内容（有 → http 就够，别再折腾接口）
2. XHR 里有没有结构化列表（有 → 接口优先，比渲染快 5–20 倍）
3. 页面是否必须执行 JS 才有内容（是 → browser）
4. 是否有登录/风控（有 → 先解决会话，再谈通道）

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
先分析（策略三的 ①）
  ├─ HTML 已有内容      → 策略一（http）
  ├─ 有 __NEXT_DATA__   → 策略四（embedded）
  ├─ XHR 有结构化列表    → 接口（discover + http）
  ├─ 必须 JS 渲染       → 策略二（browser）
  └─ 需要登录           → 策略五 + 上面任意一条
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
