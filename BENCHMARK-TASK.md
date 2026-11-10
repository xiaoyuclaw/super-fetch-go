# 抓取性能评测任务（Benchmark Task）

> 目的：给 `sf`（Go 版）和 DSH 内置 `web_fetch` 建立一套可复现的测速方法，
> 覆盖「默认策略 / 其他策略 / 三种浏览器后端」，并能公平地量出内置 `web_fetch` 的耗时。
> **只测抓取（sf），不含搜索（sv）。**
>
> 这是一份**任务说明**，不是脚本。执行者按第 5 节跑、按第 6 节记、按第 8 节出结果。

---

## 1. 被测对象

### A. sf（Go 版：`bin/sf`）

| 维度 | 取值 |
|---|---|
| 默认策略 | `sf "<url>"`（auto：cffi → 内嵌数据 → 浏览器兜底） |
| 其他策略 | `--via http` / `--embedded` / `--discover`（配合 `--via http` 取接口数据）/ `--via browser` / `--eval` / `-F` 批量 |
| 浏览器后端 | `SF_BROWSER=nodriver`（默认）/ `chromedp` / `gomoufox` |
| 输出档位 | `-L brief` / `-L normal` / `-L full`（体积与 token 口径） |

### B. 内置 `web_fetch`

DSH 宿主进程内的工具，无 CLI。耗时必须从会话日志反推（见 5.3）。

---

## 2. 测试语料（固定，不随日期变）

| # | 类别 | URL / 查询 | 预期 |
|---|---|---|---|
| 1 | 静态极小 | `https://example.com` | 两者都秒回 |
| 2 | 中文列表 | `https://www.gov.cn/` | sf 浏览器通道要全量 |
| 3 | 长文档 | `https://en.wikipedia.org/wiki/Web_scraping` | 考察截断与正文抽取 |
| 4 | 英文长文 | `https://www.joelonsoftware.com/2000/08/09/the-joel-test-12-steps-to-better-code/` | 高相似度基准 |
| 5 | 代码站 | `https://github.com/rust-lang/rust` | GitHub 对本机偶发 504，失败照记 |
| 6 | 中文 SPA | `https://sspai.com/` | 考察列表页选块 |
| 7 | 反爬站 | `https://36kr.com/` | sf 应升浏览器；内置大概率拿检测页 |
| 8 | 超大页 | `https://en.wikipedia.org/wiki/Python_(programming_language)` | 考察截断策略 |
| 9 | 错误页 | `https://httpbin.org/status/404` | sf 应 rc=1；内置应报 HTTP 404 |
| 10 | 重定向 | `https://httpbin.org/redirect/3` | 跟随并拿到最终页 |

> 本任务只测 `sf`（抓取），不测 `sv`（搜索）。

---

## 3. 指标定义

| 指标 | 定义 | 采集方式 |
|---|---|---|
| **墙钟耗时** | 进程启动到退出（sf），或工具调用到结果返回（内置） | `date +%s.%N` 前后差 / 日志时间戳 |
| **通道命中** | cffi / embedded / browser / playwright | sf 的 stderr `[*] 📝 提取内容... [x]` 或 `-j` 的 `via` |
| **正文体积** | 输出字符数 | `wc -c` / JSON 的 `content` 长度 |
| **内容质量** | 见 3.5 的 8 项指标 | `/tmp/quality.py <输出文件>` |
| **成功率** | 拿到有效正文且 rc=0 的比例 | 人工判定 |
| **稳定性** | 同一用例 3 次的最大/最小差 | 记录 3 次值 |
| **token 成本**（可选） | 输出字符 ÷ 1.5 估算，或内置会话 usage 实测 | 见 5.3 |

> 统一口径：**耗时保留两位小数，取中位数**；失败样本单独列出，不混进平均。

---

## 3.5 内容质量量化（必测，不能只看速度）

速度之外，每条结果都要算下面这些指标。脚本见本节末，直接跑即可。

| 指标 | 含义 | 好坏方向 |
|---|---|---|
| `link_ratio` | Markdown 链接字符 / 总字符 | 列表页高正常；文章页 >0.6 说明正文被导航淹没 |
| `img_ratio` | 图片占位符字符（含 base64）/ 总字符 | 越低越好；>0.3 基本是垃圾 |
| `imgs` | 图片占位符个数 | 同上 |
| `nav_line_ratio` | 命中导航词（首页/登录/注册/更多/版权/ICP/联系我们…）的行占比 | 越低越好，>0.15 说明没做正文抽取 |
| `short_line_ratio` | ≤8 字的行占比 | 越高越像目录/导航碎片 |
| `dup_line_ratio` | 出现 ≥3 次的重复行占比 | 越高说明模板块没去掉 |
| `headings` | 标题行数 | 反映结构保留程度 |
| `avg_line` | 平均行长 | 长文应显著大于列表页 |
| `chars` | 正文字符数 | 结合上面几个看，不是越大越好 |

**「正文命中率」补充指标**（可选，更贴近实用性）：

- 取页面 `<title>` 里 2–3 个关键词，统计它们在输出正文中的出现次数；
- 或人工摘 3 句正文，算输出里这些句子的字符覆盖率。

**判定**：

- 干净：`img_ratio < 0.05`、`nav_line_ratio < 0.05`、`dup_line_ratio < 0.02`
- 有噪音：任一指标超 2 倍但正文主体仍在
- 噪音为主：`img_ratio > 0.3` 或 `nav_line_ratio > 0.15` 或正文关键词几乎不出现

**脚本**（`/tmp/quality.py`，无第三方依赖）：

```python
import re, sys, json
NAV = ("首页","登录","注册","更多","版权","ICP备","联系我们","隐私政策","关于我们",
       "下载客户端","无障碍","网站地图","分享到","扫码","关注我们","返回顶部")
def metrics(md):
    lines=[l.strip() for l in md.splitlines() if l.strip()]
    total=sum(len(l) for l in lines) or 1
    link=sum(len(x) for l in lines for x in re.findall(r'\[[^\]]*\]\([^)]*\)',l))
    imgc=sum(len(x) for x in re.findall(r'!\[[^\]]*\]\([^)]*\)',md))
    return dict(chars=total, lines=len(lines),
        link_ratio=round(link/total,3), img_ratio=round(imgc/total,3),
        imgs=len(re.findall(r'!\[[^\]]*\]',md)),
        nav_line_ratio=round(sum(1 for l in lines if any(w in l for w in NAV))/len(lines),3),
        short_line_ratio=round(sum(1 for l in lines if len(l)<=8)/len(lines),3),
        dup_line_ratio=round(sum(1 for l in set(lines) if lines.count(l)>=3)/len(lines),3),
        headings=sum(1 for l in lines if l.startswith('#')),
        avg_line=round(total/len(lines),1))
for p in sys.argv[1:]:
    print(p, json.dumps(metrics(open(p).read()), ensure_ascii=False))
```

**内置 web_fetch 的质量怎么算**：日志里的返回文本可能被中段裁剪（`[... middle pruned ...]`），
所以它的 `chars` 是下界、比例类指标会偏；**比例仍可比**（同样的裁剪不影响图片/导航占比的大致判断）。
要精确值，改用 5.3 的「扣减法」在同一会话里抓两次，或直接对内置返回的完整首尾片段人工核对。


---

## 4. 环境固定项（每次跑之前确认）

- 机器：本机 VMware Ubuntu，出口机房 IP（同一天内不变）
- 网络：Clash TUN 正常，DNS 解析正常
- 显示：`DISPLAY=:0`（浏览器通道需要）
- **只允许一个浏览器 daemon 存活**（`pgrep -xc sfd` ≤ 1）
- 并发：一律 **1**（顺序执行），条目之间 sleep 1s，避免同一站点连击
- daemon 冷热：浏览器类用例必须**分别记录冷启动与热启动**

---

## 5. 执行方法

### 5.1 sf 单 URL

```bash
SF=~/agents/xiaoyu/.agents/skills/super-fetch/bin/sf
for i in 1 2 3; do
  /usr/bin/time -f "%e" $SF "<url>" -L full -o /tmp/out-$i.md 2>/tmp/err-$i.txt
done
```

记录：3 次秒数、`wc -c /tmp/out-1.md`、stderr 里的通道标记。

**冷/热启动**：浏览器通道第一次是冷启动（daemon 拉起），第二、三次是热启动；两者分列。

### 5.2 sf 策略与引擎

```bash
# 策略
$SF "<url>" --via http          # 纯 HTTP
$SF "<url>" --embedded          # SSR 注水数据（无则明确提示）
$SF "<url>" --discover          # 接口探测（记录候选接口数量与耗时）
$SF "<url>" --via browser       # 浏览器（默认 nodriver）
$SF "<url>" --eval "document.title"   # CDP 求值（固定走 chromedp）

# 后端
SF_BROWSER=chromedp $SF "<url>" --via browser
SF_BROWSER=gomoufox $SF "<url>" --via browser

# 批量（每次 3 个 URL，-c 2）
$SF -F /tmp/urls.txt -c 2 --silent -j
```

每条记录：通道（`-j` 的 `via` 或 stderr 的 `[cffi]/[playwright]`）、耗时、字符数、rc、是否成功。

### 5.3 内置 web_fetch 怎么量

它跑在 DSH 宿主进程里，没有独立进程，只能从**会话日志**取时间：

日志位置：`~/.dsh/sessions/<workspace-hash>/session-<id>/session.v4.jsonl.zstd`
（本机 workspace hash 形如 `--home-xunbu-agents-xiaoyu--`，取最新 mtime 的目录）

事件对应关系：

```
tool/call   (data.name == "web_fetch", data.callId)
        ↓  时间差 = 工具耗时
tool/result (data.callId 相同, content 以 "Fetched http" 开头)
```

解析脚本（只读，不依赖任何库）：

```bash
zstd -dc session.v4.jsonl.zstd | python3 -c '
import sys, json
calls = {}
for line in sys.stdin:
    d = json.loads(line)
    if d.get("type") == "tool/call":
        calls[d["data"].get("callId")] = d["time"]
    elif d.get("type") == "tool/result":
        cid = d["data"].get("callId")
        c = d["data"].get("message", {}).get("content")
        txt = c[0].get("text", "") if isinstance(c, list) else ""
        if txt.startswith("Fetched http"):
            url = txt.split(" ")[1]
            dur = (d["time"] - calls.get(cid, d["time"])) / 1000
            print(f"{dur:.2f}s\t{len(txt)}c\t{url}")
'
```

**注意三点**：

1. 这个时间是「工具调用 → 结果返回」的端到端耗时，**包含模型在两次工具调用之间的开销**；
   要更纯的数字，用**扣减法**：同一会话里连续两次 `web_fetch` 同一 URL，取第二次（模型已预热）。
2. 同一批并行调用会互相干扰，**必须串行**。
3. 日志里的结果文本可能被截断（`[... middle pruned ...]`），字符数只能当**下界**；
   要准确体积，用内置返回内容的完整首尾拼接估算，或对同类页面只比较相对大小。

**更可控的替代法（推荐用于精确对比）**：

- 在**新会话**里发一条固定 prompt：`用 web_fetch 抓 <url>，只回复 OK`，用 turn 起止时间作为上界；
- 或者直接测「同一 URL 两次抓取的时间差」作为内置的边际成本（不含模型思考）。

### 5.4 token 成本（可选）

内置 `web_fetch` 的返回内容会整体进上下文，按 `字符数 ÷ 1.5` 估算 token；
sf 的 `-L brief` 是硬截断（800 字），可直接对比。会话日志里 `assistant/message` 的
`usage.inputTokens / outputTokens / cacheReadTokens` 可做精确对照（见 SV-VS-SF-REPORT 的方法）。

---

## 6. 记录格式

每个用例一行，最后汇总成表：

```json
{
  "case": "wikipedia-long",
  "url": "https://en.wikipedia.org/wiki/Web_scraping",
  "subject": "sf|web_fetch",
  "channel": "cffi|browser|embedded",
  "runs": [3.36, 3.21, 3.40],
  "median_s": 3.36,
  "chars": 42327,
  "rc": 0,
  "note": ""
}
```

汇总表模板：

| 用例 | sf 默认 | sf 指定通道 | 内置 web_fetch | 正文体积(sf/内置) | 备注 |
|---|---|---|---|---|---|
| static-tiny | 0.38s | http 0.35s | ~1.9s | 203 / 210 | 内置含页脚噪音 |
| ... | | | | | |

---

## 7. 判定标准（跑完怎么下结论）

1. **速度**：同一用例，sf 中位数 < 内置 → 记 sf 胜；差 20% 以内记平。
2. **体积/噪音**：正文干净度（导航/页脚/图片占比）人工看首尾各 20 行，分「干净 / 有噪音 / 噪音为主」。
3. **能力**：内置拿不到的（JS 渲染、登录、反爬）直接记「不支持」，不参与速度比较。
4. **稳定性**：3 次极差 > 50% 的用例单独标注（通常是网络或站点风控）。

---

## 8. 已知基线（2026-11-09，供对照）

| 项目 | 数值 |
|---|---|
| `sf` 启动 | 17ms（Go） |
| sf 默认 cffi：example.com | 0.38s |
| sf 默认 cffi：gov.cn | 0.18s |
| sf 浏览器（nodriver 热） | 1.4s（静态页）/ 4.5s（含等待） |
| sf 浏览器（gomoufox 热） | 2.6s |
| 内置 web_fetch：gov.cn | ~1.9s（含模型开销） |
| 内置 web_fetch：36kr | 拿到「安全检测」页，判失败 |
| sf 批量（2 URL，-c 2） | 0.46s（Go） |

---

## 9. 注意事项（踩过的坑）

- **daemon 必须预热**：浏览器用例第一次是冷启动（nodriver ~4s、gomoufox 可达 30s+），别和热启动混算。
- **只能留一个 daemon**：内存 6G，两个浏览器同时活着会拖慢甚至 OOM；跑之前 `kill $(cat /run/user/1000/sf-*.pid)`。
- **IP 风控**：360 / 搜狗在本机被 IP 级拦截（验证码页），无论换成什么内核都过不了；记「失败(IP)」即可。
- **神马浏览器通道**：Chrome 下会返回「没有找到结果」的软拦截，HTTP 通道正常；浏览器用例记 0 条。
- **页面动态性**：新闻/首页类每 10 分钟内容就变，体积会波动，比速度别比字数。
- **内置 web_fetch 是「纯 HTTP + 全文转 Markdown」**：它没有正文抽取，体积天然偏大，不要拿体积直接判优劣。
- **重复次数**：每用例 3 次；风控类（baidu/30x）建议 5 次看中位数。

---

## 10. 执行清单（照做即可）

1. 确认环境：`pgrep -xc sfd` ≤1、`echo $DISPLAY`、磁盘 ≥1G
2. 跑 sf 语料：第 2 节 10 个 URL × {默认, --via http, --via browser}，记 5.1/5.2 数据
3. 跑三种浏览器后端各一轮（nodriver / chromedp / gomoufox），冷热分开
4. 跑内置 web_fetch：同一批 URL，串行调用，按 5.3 从日志取时间
5. 对每条输出跑 3.5 的质量脚本，把 8 项指标填进结果表
6. 填第 6 节表格，按第 7 节下结论，写进 `RESULTS-<日期>.md`
