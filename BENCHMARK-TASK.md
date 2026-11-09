# 抓取性能评测任务（Benchmark Task）

> 目的：给 `sf`（Go 版）和 DSH 内置 `web_fetch` 建立一套可复现的测速方法，
> 覆盖「默认策略 / 默认引擎 / 其他策略 / 其他引擎」，并能公平地量出内置 `web_fetch` 的耗时。
>
> 这是一份**任务说明**，不是脚本。执行者按第 5 节跑、按第 6 节记、按第 8 节出结果。

---

## 1. 被测对象

### A. sf（Go 版：`bin/sf`、`bin/sv`）

| 维度 | 取值 |
|---|---|
| 默认策略 | `sf "<url>"`（auto：cffi → 内嵌数据 → 浏览器兜底） |
| 默认引擎 | `sv "<q>"`（当前默认顺序 bing → baidu → toutiao → 360 → sogou → shenma → ddg） |
| 其他策略 | `--via http` / `--embedded` / `--discover` / `--via browser` / `--eval` / `-F` 批量 |
| 其他引擎 | `sv "<q>" -e bing\|baidu\|toutiao\|shenma\|ddg`（360/sogou 本机 IP 被封，只记失败） |
| 浏览器后端 | `SF_BROWSER=nodriver`（默认）/ `chromedp` / `gomoufox` |

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

搜索语料（`sv`）固定 3 条：

```
python release notes      （英文技术，结果稳定）
成都 天气                  （中文生活，结果稳定）
DeepSeek V4 发布           （中文时效，考察新鲜度）
```

---

## 3. 指标定义

| 指标 | 定义 | 采集方式 |
|---|---|---|
| **墙钟耗时** | 进程启动到退出（sf），或工具调用到结果返回（内置） | `date +%s.%N` 前后差 / 日志时间戳 |
| **通道命中** | cffi / embedded / browser / playwright | sf 的 stderr `[*] 📝 提取内容... [x]` 或 `-j` 的 `via` |
| **正文体积** | 输出字符数 | `wc -c` / JSON 的 `content` 长度 |
| **成功率** | 拿到有效正文且 rc=0 的比例 | 人工判定 |
| **稳定性** | 同一用例 3 次的最大/最小差 | 记录 3 次值 |
| **token 成本**（可选） | 输出字符 ÷ 1.5 估算，或内置会话 usage 实测 | 见 5.3 |

> 统一口径：**耗时保留两位小数，取中位数**；失败样本单独列出，不混进平均。

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
SF=~/agents/xiaoyu/.agents/skills/super-fetch-go/bin/sf
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
$SF "<url>" --discover          # 接口探测
$SF "<url>" --via browser       # 浏览器（默认 nodriver）

# 后端
SF_BROWSER=chromedp $SF "<url>" --via browser
SF_BROWSER=gomoufox $SF "<url>" --via browser

# 引擎（sv）
SV=~/agents/xiaoyu/.agents/skills/super-fetch-go/bin/sv
for e in bing baidu toutiao shenma ddg 360 sogou; do
  $SV "python release notes" -e $e -j -n 5
done
```

每条记录：`count`、`elapsed`（sv 自己报的）、`via`、是否成功。

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
| sv 默认（bing，cffi） | 1.25s / 5 条 |
| sv 浏览器（nodriver 热） | 4.2–5.3s / 5 条 |

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
3. 跑 sv 引擎：3 条查询 × 7 个引擎（cffi 与 browser 各一轮）
4. 跑内置 web_fetch：同一批 URL，串行调用，按 5.3 从日志取时间
5. 填第 6 节表格，按第 7 节下结论，写进 `RESULTS-<日期>.md`
