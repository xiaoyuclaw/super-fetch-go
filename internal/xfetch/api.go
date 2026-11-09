package xfetch

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"superfetch/internal/xcore"
)

var apiCacheMu sync.Mutex

func apiCachePath() string { return filepath.Join(xcore.DataDir(), "api-cache.json") }

func loadAPICache() map[string]string {
	m := map[string]string{}
	data, err := os.ReadFile(apiCachePath())
	if err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return m
}

func saveAPICache(m map[string]string) {
	data, err := json.MarshalIndent(m, "", " ")
	if err == nil {
		_ = os.MkdirAll(filepath.Dir(apiCachePath()), 0o755)
		_ = os.WriteFile(apiCachePath(), data, 0o600)
	}
}

// APICandidate 一个被判定为「数据接口」的候选。
type APICandidate struct {
	URL    string `json:"url"`
	Status int    `json:"status"`
	Items  int    `json:"items"`
	Bytes  int    `json:"bytes"`
	Score  int    `json:"score"`
}

// APIResult --via api 的结果。
type APIResult struct {
	URL        string         `json:"url"`
	Picked     APICandidate   `json:"picked"`
	Candidates []APICandidate `json:"candidates"`
	Body       string         `json:"body"`
}

// FetchViaAPI 先发现接口，再用 cffi 回放，挑数据量最大的那个返回。
// 适用于「数据在 XHR/fetch 里」的站点：浏览器只用来发现，正文由 cffi 直取。
func FetchViaAPI(opts BrowserOptions, rawURL, sessionFile string) (*APIResult, error) {
	host := hostOf(rawURL)

	// 先查缓存：同一个站第二次直接用 cffi 打接口，不开浏览器
	apiCacheMu.Lock()
	cache := loadAPICache()
	apiCacheMu.Unlock()
	if cached, ok := cache[host]; ok && cached != "" {
		rep := replayURL(cached)
		if rep.Status == 200 && rep.Bytes > 0 && jsonItemCount(rep.Body) > 0 {
			return &APIResult{
				URL: rawURL,
				Picked: APICandidate{URL: shortenURL(cached), Status: rep.Status,
					Items: jsonItemCount(rep.Body), Bytes: rep.Bytes},
				Body: rep.Body,
			}, nil
		}
	}

	// 缓存未命中：开浏览器发现接口
	pattern := ""
	raw, err := Probe(opts, rawURL, sessionFile, "", &pattern, false, nil)
	if err != nil {
		return nil, err
	}

	var probe ProbeResult
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return nil, err
	}

	type scored struct {
		cand APICandidate
		body string
	}
	var all []scored
	seen := map[string]bool{}
	tried := 0
	for _, c := range probe.Captured {
		target := c.URLFull
		if target == "" {
			target = c.URL
		}
		method := c.Method
		if method == "" {
			method = "GET"
		}
		isJSON := c.LooksJSON || strings.Contains(strings.ToLower(c.ContentType), "json")
		if !isJSON || method != "GET" || seen[target] {
			continue
		}
		seen[target] = true
		tried++
		if tried > 8 {
			break
		}
		rep := replayCaptured(c, nil)
		if rep.Error != "" || rep.Status >= 400 || rep.Bytes == 0 {
			continue
		}
		items := jsonItemCount(rep.Body)
		sc := items*10000 + rep.Bytes
		all = append(all, scored{
			cand: APICandidate{URL: shortenURL(target), Status: rep.Status, Items: items, Bytes: rep.Bytes, Score: sc},
			body: rep.Body,
		})
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("没有找到可用的 JSON 接口")
	}
	sort.Slice(all, func(i, j int) bool { return all[i].cand.Score > all[j].cand.Score })
	apiCacheMu.Lock()
	cache = loadAPICache()
	cache[host] = all[0].cand.URL
	saveAPICache(cache)
	apiCacheMu.Unlock()
	res := &APIResult{URL: rawURL, Picked: all[0].cand, Body: all[0].body}
	for _, x := range all {
		res.Candidates = append(res.Candidates, x.cand)
	}
	return res, nil
}

// jsonItemCount 估算 JSON 里的条目数：顶层数组取长度，对象取第一个数组字段。
func jsonItemCount(body string) int {
	var v any
	if json.Unmarshal([]byte(body), &v) != nil {
		return 0
	}
	switch t := v.(type) {
	case []any:
		return len(t)
	case map[string]any:
		best := 0
		for _, val := range t {
			arr, ok := val.([]any)
			if !ok {
				continue
			}
			if len(arr) > best {
				best = len(arr)
			}
			for _, item := range arr {
				if m, ok := item.(map[string]any); ok {
					if n := len(m); n > best && len(arr) <= 3 {
						best = n
					}
				}
			}
		}
		return best
	}
	return 0
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Host
	}
	return raw
}

// replayURL 直接 GET 一个接口地址（缓存命中时用）。
func replayURL(target string) ReplayResult {
	return replayCaptured(Captured{URL: target, URLFull: target, Method: "GET"}, nil)
}

func shortenURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if len(raw) > 120 {
		return u.Scheme + "://" + u.Host + u.Path
	}
	return raw
}
