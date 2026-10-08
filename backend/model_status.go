package controller

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

type statusChannelItem struct {
	Id           int    `json:"id"`
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	Status       int    `json:"status"`
	ResponseTime int    `json:"response_time"`
	TestTime     int64  `json:"test_time"`
}

type statusModelItem struct {
	Model        string              `json:"model"`
	Group        string              `json:"group"`
	Total        int                 `json:"total"`
	Enabled      int                 `json:"enabled"`
	Status       string              `json:"status"`
	Channels     []statusChannelItem `json:"channels,omitempty"`
	Bars         []timelineBar       `json:"bars,omitempty"`
}

type statusGroupItem struct {
	Group  string            `json:"group"`
	Status string            `json:"status"`
	Models []statusModelItem `json:"models"`
}

// timelineBar 是 1 小时间轴格子的聚合结果
type timelineBar struct {
	Timestamp int64  `json:"ts"`
	Status    string `json:"status"`
	Errors    int    `json:"errors"`
	Total     int    `json:"total"`
}

func classify(total, enabled int) string {
	switch {
	case total == 0:
		return "none"
	case enabled == 0:
		return "outage"
	case enabled < total:
		return "degraded"
	default:
		return "operational"
	}
}

// GetModelStatus 公开状态页数据：按分组聚合模型可用性 + 每个模型 60 小时时间轴。
// 时间轴从 logs 表聚合：按 model_name 分组，每小时有消费日志=正常，有错误日志=故障。
func GetModelStatus(c *gin.Context) {
	// 总开关：管理员也遵循开关；关了之后接口直接 404（页面同样不渲染内容）
	if !model.StatusPageEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "状态页未开启"})
		return
	}
	type abilityRow struct {
		Group   string
		Model   string
		Channel int
		Enabled bool
	}
	hideDisabled := model.StatusPageHideDisabledChannels()
	var rows []abilityRow
	// 只取渠道仍存在的 ability 行：渠道删除后可能残留孤儿数据，
	// INNER JOIN channels 后不存在的渠道 id 自然被过滤掉。
	abilityQuery := model.DB.Table("abilities").
		Joins("INNER JOIN channels ON channels.id = abilities.channel_id")
	if hideDisabled {
		abilityQuery = abilityQuery.Where("channels.status = ?", common.ChannelStatusEnabled)
	}
	if err := abilityQuery.
		Select("abilities.`group` AS `group`, abilities.model AS model, abilities.channel_id AS channel, abilities.enabled AS enabled").
		Find(&rows).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	channelIds := make(map[int]bool)
	for _, r := range rows {
		channelIds[r.Channel] = true
	}
	type channelRow struct {
		Id           int
		Name         string
		DisplayName  string
		Status       int
		ResponseTime int
		TestTime     int64
		TestError    string
	}
	channels := map[int]channelRow{}
	if len(channelIds) > 0 {
		ids := make([]int, 0, len(channelIds))
		for id := range channelIds {
			ids = append(ids, id)
		}
		var chRows []channelRow
		if err := model.DB.Model(&model.Channel{}).
			Select("id", "name", "display_name", "status", "response_time", "test_time", "test_error").
			Where("id IN ?", ids).Find(&chRows).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		for _, ch := range chRows {
			channels[ch.Id] = ch
		}
	}

	type agg struct {
		total, enabled int
		chIds          []int
	}
	aggMap := map[string]*agg{}
	keyOf := func(g, m string) string { return g + "\x00" + m }
	for _, r := range rows {
		k := keyOf(r.Group, r.Model)
		a := aggMap[k]
		if a == nil {
			a = &agg{}
			aggMap[k] = a
		}
		a.total++
		ch := channels[r.Channel]
		channelOk := r.Enabled && ch.Status == common.ChannelStatusEnabled
		if channelOk {
			a.enabled++
		}
		a.chIds = append(a.chIds, r.Channel)
	}

	groupMap := map[string][]statusModelItem{}
	for k, a := range aggMap {
		parts := strings.SplitN(k, "\x00", 2)
		g, m := parts[0], parts[1]
		item := statusModelItem{
			Model:   m,
			Group:   g,
			Total:   a.total,
			Enabled: a.enabled,
			Status:  classify(a.total, a.enabled),
		}
		if c.GetInt("role") >= common.RoleAdminUser {
			for _, id := range a.chIds {
				ch := channels[id]
				item.Channels = append(item.Channels, statusChannelItem{
					Id:           ch.Id,
					Name:         ch.Name,
					DisplayName:  ch.DisplayName,
					Status:       ch.Status,
					ResponseTime: ch.ResponseTime,
					TestTime:     ch.TestTime,
				})
			}
		}
		groupMap[g] = append(groupMap[g], item)
	}

	// 按模型聚合时间轴：最近 60 小时
	now := time.Now()
	hourStart := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
	startTs := hourStart.Add(-59 * time.Hour).Unix()

	// 模型名 -> 各小时的消费/错误统计
	type modelHourAgg struct {
		consume int64
		errors  int64
	}
	modelHourMap := map[string]map[int64]*modelHourAgg{}

	type logHourRow struct {
		Hour      int64
		Type      int
		ModelName string
		Count     int64
	}
	var logRows []logHourRow
	// 从 LOG_DB 按 model_name + 小时 + 类型聚合
	queryDB := model.LOG_DB
	if err := queryDB.Model(&model.Log{}).
		Select("FLOOR(created_at/3600)*3600 AS hour, type, model_name, COUNT(*) AS count").
		Where("created_at >= ? AND model_name <> ''", startTs).
		Group("hour, type, model_name").
		Find(&logRows).Error; err != nil {
		// fallback 到 DB（SQLite 单库场景）
		model.DB.Model(&model.Log{}).
			Select("FLOOR(created_at/3600)*3600 AS hour, type, model_name, COUNT(*) AS count").
			Where("created_at >= ? AND model_name <> ''", startTs).
			Group("hour, type, model_name").
			Find(&logRows)
	}

	for _, r := range logRows {
		m := modelHourMap[r.ModelName]
		if m == nil {
			m = map[int64]*modelHourAgg{}
			modelHourMap[r.ModelName] = m
		}
		a := m[r.Hour]
		if a == nil {
			a = &modelHourAgg{}
			m[r.Hour] = a
		}
		if r.Type == int(model.LogTypeConsume) {
			a.consume += r.Count
		}
		if r.Type == int(model.LogTypeError) {
			a.errors += r.Count
		}
	}

	// 渠道探测结果也计入时间轴：探测失败（test_error 非空且 test_time 在窗口内）
	// 按渠道的模型列表归入对应小时，作为错误信号补充（错误日志可能被关闭）。
	type probeRow struct {
		Models   string
		TestTime int64
	}
	var probeRows []probeRow
	if err := model.DB.Model(&model.Channel{}).
		Select("models", "test_time").
		Where("test_error <> '' AND test_time >= ?", startTs).
		Find(&probeRows).Error; err == nil {
		for _, pr := range probeRows {
			hour := (pr.TestTime / 3600) * 3600
			for _, mn := range strings.Split(pr.Models, ",") {
				mn = strings.TrimSpace(mn)
				if mn == "" {
					continue
				}
				m := modelHourMap[mn]
				if m == nil {
					m = map[int64]*modelHourAgg{}
					modelHourMap[mn] = m
				}
				a := m[hour]
				if a == nil {
					a = &modelHourAgg{}
					m[hour] = a
				}
				a.errors++
			}
		}
	}

	// 为每个模型生成 60 格时间轴
	generateBars := func(modelName string) []timelineBar {
		hm := modelHourMap[modelName]
		result := make([]timelineBar, 0, 60)
		for i := 59; i >= 0; i-- {
			ts := hourStart.Add(-time.Duration(i) * time.Hour).Unix()
			a := hm[ts]
			bar := timelineBar{Timestamp: ts, Status: "none", Total: 0}
			if a != nil {
				bar.Total = int(a.consume + a.errors)
				if a.errors == 0 {
					bar.Status = "operational"
				} else if a.consume > 0 {
					bar.Status = "degraded"
					bar.Errors = int(a.errors)
				} else {
					bar.Status = "outage"
					bar.Errors = int(a.errors)
				}
			}
			result = append(result, bar)
		}
		return result
	}

	// 计算全局可用率
	var allBars []timelineBar
	type logGlobalRow struct {
		Hour  int64
		Type  int
		Count int64
	}
	var globalRows []logGlobalRow
	if err := queryDB.Model(&model.Log{}).
		Select("FLOOR(created_at/3600)*3600 AS hour, type, COUNT(*) AS count").
		Where("created_at >= ?", startTs).
		Group("hour, type").
		Find(&globalRows).Error; err != nil {
		model.DB.Model(&model.Log{}).
			Select("FLOOR(created_at/3600)*3600 AS hour, type, COUNT(*) AS count").
			Where("created_at >= ?", startTs).
			Group("hour, type").
			Find(&globalRows)
	}
	type globalHourAgg struct {
		consume int64
		errors  int64
	}
	gAggMap := map[int64]*globalHourAgg{}
	for _, r := range globalRows {
		a := gAggMap[r.Hour]
		if a == nil {
			a = &globalHourAgg{}
			gAggMap[r.Hour] = a
		}
		if r.Type == int(model.LogTypeConsume) {
			a.consume += r.Count
		}
		if r.Type == int(model.LogTypeError) {
			a.errors += r.Count
		}
	}
	for i := 59; i >= 0; i-- {
		ts := hourStart.Add(-time.Duration(i) * time.Hour).Unix()
		a := gAggMap[ts]
		bar := timelineBar{Timestamp: ts, Status: "none", Total: 0}
		if a != nil {
			bar.Total = int(a.consume + a.errors)
			if a.errors == 0 {
				bar.Status = "operational"
			} else if a.consume > 0 {
				bar.Status = "degraded"
				bar.Errors = int(a.errors)
			} else {
				bar.Status = "outage"
				bar.Errors = int(a.errors)
			}
		}
		allBars = append(allBars, bar)
	}
	var opHours int
	for _, b := range allBars {
		if b.Status == "operational" || b.Status == "degraded" {
			opHours++
		}
	}
	uptimePct := 100.0
	if len(allBars) > 0 {
		uptimePct = float64(opHours) / float64(len(allBars)) * 100
	}

	// 最近 10 分钟各模型的错误数与成功数：渠道状态正常但请求在持续失败时，
	// 依据实时错误率把模型标为异常（不依赖渠道自动禁用开关）。
	type recentRow struct {
		ModelName string
		Type      int
		Count     int64
	}
	var recentRows []recentRow
	recentSince := now.Add(-10 * time.Minute).Unix()
	if err := queryDB.Model(&model.Log{}).
		Select("model_name, type, COUNT(*) AS count").
		Where("created_at >= ? AND model_name <> ''", recentSince).
		Group("model_name, type").
		Find(&recentRows).Error; err != nil {
		model.DB.Model(&model.Log{}).
			Select("model_name, type, COUNT(*) AS count").
			Where("created_at >= ? AND model_name <> ''", recentSince).
			Group("model_name, type").
			Find(&recentRows)
	}
	type recentAgg struct {
		ok     int64
		errors int64
	}
	recentMap := map[string]*recentAgg{}
	for _, r := range recentRows {
		a := recentMap[r.ModelName]
		if a == nil {
			a = &recentAgg{}
			recentMap[r.ModelName] = a
		}
		if r.Type == int(model.LogTypeConsume) {
			a.ok += r.Count
		}
		if r.Type == int(model.LogTypeError) {
			a.errors += r.Count
		}
	}

	// 组装结果：每个模型附带自己的时间轴；渠道全启用时再看实时错误率，
	// 最近 10 分钟错误数 >= 5 且错误占比 > 30% 视为异常（degraded）。
	groups := make([]statusGroupItem, 0, len(groupMap))
	for g, models := range groupMap {
		gStatus := "operational"
		for i := range models {
			models[i].Bars = generateBars(models[i].Model)
			if models[i].Status == "operational" {
				if ra := recentMap[models[i].Model]; ra != nil && ra.errors >= 5 {
					total := ra.ok + ra.errors
					if total > 0 && float64(ra.errors)/float64(total) > 0.3 {
						models[i].Status = "degraded"
					}
				}
			}
			if models[i].Status == "outage" {
				gStatus = "outage"
			} else if models[i].Status == "degraded" || models[i].Status == "none" {
				if gStatus != "outage" {
					gStatus = "degraded"
				}
			}
		}
		groups = append(groups, statusGroupItem{Group: g, Status: gStatus, Models: models})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].Group == "default" {
			return true
		}
		if groups[j].Group == "default" {
			return false
		}
		return groups[i].Group < groups[j].Group
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"updated_at":           now.Unix(),
			"groups":               groups,
			"is_admin":             c.GetInt("role") >= common.RoleAdminUser,
			"bars":                 allBars,
			"uptime_pct":           uptimePct,
			"auto_refresh":         model.StatusPageAutoRefreshEnabled(),
			"auto_refresh_interval": model.StatusPageAutoRefreshInterval(),
			"hide_disabled_channels": hideDisabled,
		},
	})
}
