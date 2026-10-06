package controller

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
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
	for _, ch := range channels {
		baseURL := ""
		if ch.BaseURL != nil {
			baseURL = *ch.BaseURL
		}
		if baseURL == "" {
			baseURL = "https://api.openai.com"
		}
		testURL := strings.TrimRight(baseURL, "/") + "/v1/models"
		key := ch.Key
		if key == "" {
			continue
		}
		req, err := http.NewRequest("GET", testURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+key)
		start := time.Now()
		resp, err := client.Do(req)
		elapsed := time.Since(start).Milliseconds()
		if err != nil {
			// 测试失败：记录错误，不改变渠道状态（不自动禁用）
			model.DB.Model(&model.Channel{}).Where("id = ?", ch.Id).Updates(map[string]any{
				"test_time":     now,
				"response_time": 0,
				"test_error":    err.Error(),
			})
			continue
		}
		resp.Body.Close()
		errMsg := ""
		if resp.StatusCode >= 400 {
			errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		model.DB.Model(&model.Channel{}).Where("id = ?", ch.Id).Updates(map[string]any{
			"test_time":     now,
			"response_time": int(elapsed),
			"test_error":    errMsg,
		})
	}
	common.SysLog("status page test: done")
}
