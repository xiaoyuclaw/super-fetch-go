# super-fetch-go

`sf` / `sv` 的 Go 实现，功能对齐 Python 版（`../super-fetch`、`../super-search`），
与 Python 版**共用同一个数据目录**（`.data/links.db`、登录态、搜索 Cookie 都对得上）。

## 浏览器后端（三种，SF_BROWSER 切换）

| 后端 | 内核 | 默认 | 实测（example.com，-w 1） | 说明 |
|---|---|---|---|---|
| **gomoufox** | Camoufox/Firefox | ✅ | ~3s（常驻 daemon） | 反检测最强，webdriver 不泄漏 |
| **chromedp** | Playwright Chromium | | ~2.8s | 最快，进程内启动 |
| **nodriver** | 系统真实 Chrome | | ~3.5s | CDP 远程调试端口 + 无自动化参数，`navigator.webdriver=false` |

```bash
SF_BROWSER=gomoufox sf "<url>" --via browser   # 默认
SF_BROWSER=chromedp sf "<url>" --via browser
SF_BROWSER=nodriver sf "<url>" --via browser   # 需要系统 Chrome
SF_HEADLESS=true|virtual|false                 # gomoufox 默认 virtual
```

**热启动**：只有 gomoufox 有常驻 daemon（`bin/sfd`），首次抓取自动拉起，之后复用；
单实例 flock、随 DSH 退出、空闲 30 分钟退出。chromedp / nodriver 是进程内启动
（本来就只要 2–3 秒）。

## 构建

```bash
cd ~/agents/xiaoyu/.agents/skills/super-fetch-go
go build -o bin/sf  ./cmd/sf
go build -o bin/sv  ./cmd/sv
go build -o bin/sfl ./cmd/sfl
```

二进制是静态链接的单文件（约 27MB），拷到别的 Linux 机器直接能跑；
浏览器通道需要一个 Chrome/Chromium（自动查找 `CHROME_PATH` → Playwright 缓存 → 系统 chrome）。

## 用法

与 Python 版完全一致：

```bash
sf "https://example.com"                 # 默认 auto：先 HTTP，内容太薄再升浏览器
sf "https://36kr.com/" -e playwright     # 强制浏览器
sf "https://x.com/a" -j -L full          # JSON 输出、不截断
sf "https://x.com" --discover -j         # 列 XHR 候选接口
sf "https://x.com" --discover "api" --verify   # 回放接口
sf "https://x.com" --embedded            # 取 SSR 注水 JSON
sf "https://x.com" -i                    # 人工登录（有头浏览器 + 绿色按钮）
sf URL1 URL2 -c 3                        # 批量
sf -F urls.txt --silent                  # 从文件批量
sf --sites cnki                          # 接口库
sfl @abcd-1                              # 链接代号反查
sv "关键词" -L brief                      # 搜索
sv "关键词" -j                            # 结构化结果
sv --clear-state                         # 清搜索 Cookie
```

## 实现要点

| 层 | Python 版 | Go 版 |
|---|---|---|
| HTTP/TLS 指纹 | curl_cffi `impersonate=chrome` | `bogdanfinn/tls-client` `profiles.Chrome_152` + 手工按 Chrome 顺序排头 |
| 浏览器 | Playwright / Patchright | `chromedp`（CDP 直连，无 Node 依赖） |
| HTML 解析 | BeautifulSoup + lxml | `goquery` + `x/net/html` |
| Markdown | markdownify | `JohannesKaufmann/html-to-markdown/v2` |
| 存储 | sqlite3 | `modernc.org/sqlite`（纯 Go，无 cgo） |
| 反检测 | stealth 注入 + 真实窗口 | 同样的 init script + `--disable-blink-features=AutomationControlled` |

## 与 Python 版的差异

1. **内嵌 JSON 的字段顺序**：Go 的 `map` 无序，`--embedded` 的 outline 按 key 排序；
   Python 保插入顺序。内容一致，只是展示顺序不同。
2. **字符计数的截断点**：两边都按 Unicode 字符数截断，但 Markdown 生成的空白/列表缩进
   略有差异（同一页正文相似度 0.98–1.00）。
3. **`javascript:` 链接**：html-to-markdown 会把括号转义成 `%28`/`%29`，
   markdownify 保持原样；不影响正文。
4. **交互模式**：按钮点击、多标签页跟随、Cookie/localStorage 落盘都实现了，
   但窗口关闭时只能保存当前会话（与 Python 行为一致）。
5. 浏览器二进制默认复用 Playwright 缓存里的 Chromium；不想用它就设 `CHROME_PATH`。

## 数据目录

默认 `../super-fetch/.data`（与 Python 版共享）。二进制被挪到别处、找不到同级的
`super-fetch` 时，回退到 `~/.agents/super-fetch`；也可以显式指定：

```bash
SUPER_FETCH_HOME=/path/to/data ./bin/sf "..."
```
