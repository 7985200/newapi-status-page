package controller

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// 脱敏正则预编译（避免每渠道每次调用重复编译）
var (
	sanitizeKeyRe    = regexp.MustCompile(`sk-[A-Za-z0-9_-]+`)
	sanitizeBearerRe = regexp.MustCompile(`Bearer\s+[^\s"']+`)
	sanitizeURLRe    = regexp.MustCompile(`https?://([a-zA-Z0-9.-]+)(/[^\s"'<>]*)?`)
)

// UpdateChannelDisplayName 管理员设置渠道在状态页的显示名（留空则只显示 #ID）。
func UpdateChannelDisplayName(c *gin.Context) {
	var req struct {
		DisplayName string `json:"display_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "请求格式错误")
		return
	}
	// 解析 id
	channelId := 0
	fmt.Sscanf(c.Param("id"), "%d", &channelId)
	if channelId <= 0 {
		common.ApiErrorMsg(c, "无效的渠道 ID")
		return
	}
	if err := model.DB.Model(&model.Channel{}).
		Where("id = ?", channelId).
		Update("display_name", strings.TrimSpace(req.DisplayName)).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// StartStatusPageTestWorker 定时测试全部渠道的连通性，结果写入 channels 表（不写日志）。
// 每个渠道做一个简单的 HTTP GET 探测：请求 base_url/v1/models 带渠道 key，
// 记录响应时间、状态、错误内容。失败渠道自动标记 test_error。
func StartStatusPageTestWorker() {
	time.Sleep(2 * time.Minute) // 启动 2 分钟后首跑
	for {
		runStatusPageTest()
		interval := time.Duration(model.StatusPageChannelTestInterval()) * time.Minute
		if interval < time.Minute {
			interval = 30 * time.Minute
		}
		time.Sleep(interval)
	}
}

// sanitizeTestError 脱敏错误信息：保留上游域名方便定位是哪家渠道，
// 抹掉具体路径和密钥，防止泄露接口地址细节和 key。
func sanitizeTestError(raw string) string {
	out := raw
	// 抹掉密钥
	out = sanitizeKeyRe.ReplaceAllString(out, "[redacted-key]")
	out = sanitizeBearerRe.ReplaceAllString(out, "[redacted-key]")
	// URL 只保留域名：https://api.example.com/v1/models -> api.example.com
	out = sanitizeURLRe.ReplaceAllString(out, "$1")
	// 截断过长内容
	if len(out) > 300 {
		out = out[:300] + "..."
	}
	return out
}

func runStatusPageTest() {
	if !model.StatusPageChannelTestEnabled() {
		return
	}
	var channels []model.Channel
	if err := model.DB.Where("status = ?", common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
		common.SysLog("status page test: failed to load channels: " + err.Error())
		return
	}
	if len(channels) == 0 {
		return
	}
	common.SysLog(fmt.Sprintf("status page test: testing %d channels", len(channels)))
	client := &http.Client{Timeout: 15 * time.Second}
	now := time.Now().Unix()

	// 并发测试（最多 8 路同时），避免一个慢渠道阻塞整轮
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, ch := range channels {
		baseURL := ""
		if ch.BaseURL != nil {
			baseURL = *ch.BaseURL
		}
		if baseURL == "" {
			baseURL = "https://api.openai.com"
		}
		if ch.Key == "" {
			continue
		}
		testURL := strings.TrimRight(baseURL, "/") + "/v1/models"
		channelId := ch.Id
		wg.Add(1)
		sem <- struct{}{}
		go func(testURL, baseURL, key string, channelId int) {
			defer wg.Done()
			defer func() { <-sem }()

			req, err := http.NewRequest("GET", testURL, nil)
			if err != nil {
				return
			}
			req.Header.Set("Authorization", "Bearer "+key)
			start := time.Now()
			resp, err := client.Do(req)
			elapsed := time.Since(start).Milliseconds()
			if err != nil {
				// 测试失败：记录错误（脱敏），不改变渠道状态（不自动禁用）
				errMsg := sanitizeTestError(err.Error())
				model.DB.Model(&model.Channel{}).Where("id = ?", channelId).Updates(map[string]any{
					"test_time":     now,
					"response_time": 0,
					"test_error":    errMsg,
				})
				return
			}
			resp.Body.Close()
			errMsg := ""
			if resp.StatusCode >= 400 {
				errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
			}
			model.DB.Model(&model.Channel{}).Where("id = ?", channelId).Updates(map[string]any{
				"test_time":     now,
				"response_time": int(elapsed),
				"test_error":    errMsg,
			})
		}(testURL, baseURL, ch.Key, channelId)
	}
	wg.Wait()
	common.SysLog("status page test: done")
}
