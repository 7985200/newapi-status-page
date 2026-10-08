# newapi-status-page

给 NewAPI 加一个自助状态页。用户自己就能看到哪个分组的模型出了问题，不用再来问客服。

功能点：

- 按分组聚合模型可用性，三档状态：正常 / 部分降级 / 不可用
- 每个模型一条 60 小时时间轴，一小时一格，绿黄红灰四色，悬停可看该小时的请求数和错误数
- 实时错误率判定：渠道状态看着正常，但某模型最近 10 分钟错误占比超过 30%（且错误数不少于 5），也会标为部分降级，不再只依赖渠道开关状态
- 全站 60 小时可用率百分比
- 后台可定时探测所有渠道的连通性（默认关闭，可自定义间隔分钟数），结果不写日志表；探测失败会计入时间轴对应小时
- 渠道在状态页只显示编号（比如 #10），管理员可以给渠道设置一个对外展示的名字，不设置就只显示编号
- 页面按后台设定的间隔自动刷新（默认 30 秒，可开关），用户不用手动刷
- 总开关（默认关）一键停用整个状态页
- 禁用渠道默认不出现在状态页，可在后台切换
- 时间轴与错误内容都不会把上游接口路径和密钥暴露出去；错误原文保留在数据库和后台渠道页，状态页不展示

状态页对未登录用户只展示汇总，渠道明细只有管理员登录后可见。

## 数据从哪来

不依赖任何外部监控服务。模型状态由三个信号合成：

1. abilities 和 channels 两张表的渠道状态（渠道禁用或整组不可用）
2. logs 表最近 10 分钟的实时错误率（错误日志开启时生效）
3. 渠道探测结果（定时探测开启时生效，失败按测试时间计入时间轴）

时间轴从 logs 表按小时聚合，有消费日志算正常小时，有错误日志按比例标黄或标红；探测失败也会补进对应小时。渠道探测是直接对渠道的 base_url 发一个带 key 的 GET 请求，并发执行，记录响应时间，错误内容只落库。

## 后端安装

把 backend 下两个文件放进 NewAPI 源码的 controller 目录：

```
model_status.go        -> controller/model_status.go
status_page_worker.go  -> controller/status_page_worker.go
```

再改这几处：

model/channel.go 的 Channel 结构体加两个字段，AutoMigrate 会自动建列：

```go
DisplayName string `json:"display_name" gorm:"type:varchar(128);column:display_name"`
TestError   string `json:"test_error" gorm:"type:text;column:test_error"`
```

model/option.go 的 InitOptionMap 里加：

```go
common.OptionMap["StatusPageEnabled"] = "false"
common.OptionMap["StatusPageAutoRefreshEnabled"] = "true"
common.OptionMap["StatusPageAutoRefreshInterval"] = "30"
common.OptionMap["StatusPageChannelTestEnabled"] = "false"
common.OptionMap["StatusPageChannelTestInterval"] = "30"
common.OptionMap["StatusPageHideDisabledChannels"] = "true"
```

router/api-router.go 里注册路由：

```go
apiRouter.GET("/model-status", middleware.TryUserAuth(), middleware.DisableCache(), controller.GetModelStatus)
```

adminRoute（管理员组）里加：

```go
adminRoute.PUT("/channel/:id/display-name", controller.UpdateChannelDisplayName)
```

main.go 里启动探测协程（不开定时探测可以不写）：

```go
go controller.StartStatusPageTestWorker()
```

然后 `go build -o new-api .`。

## 前端安装

frontend/ModelStatusPage.tsx 放到 `web/src/features/model-status/index.tsx`，
frontend/route.tsx 放到 `web/src/routes/model-status/index.tsx`。

路由里用到的 `PublicLayout`、`Main`、`PageTransition` 都是 NewAPI 自带组件，不用额外装东西。

想在控制台侧边栏加入口的话，改 web/src/hooks/use-sidebar-data.ts，在 general 分组里参考其他条目加一条指向 /status 的路由（需要配套建 `web/src/routes/_authenticated/status/index.tsx`，渲染组件导出的 StatusConsole）。

构建：`cd web && bun install && bun run build`。

## 设置项

都通过 NewAPI 的 option 系统改，六个键：

| 键 | 默认 | 说明 |
|---|---|---|
| StatusPageEnabled | false | 状态页总开关 |
| StatusPageAutoRefreshEnabled | true | 状态页自动刷新开关 |
| StatusPageAutoRefreshInterval | 30 | 自动刷新间隔秒数，最小 5 |
| StatusPageChannelTestEnabled | false | 定时探测渠道开关 |
| StatusPageChannelTestInterval | 30 | 探测间隔分钟数，最小 1 |
| StatusPageHideDisabledChannels | true | 禁用渠道是否从状态页隐藏 |

渠道展示名的设置接口是 `PUT /api/user/channel/{id}/display-name`，body 传 `{"display_name": "名字"}`，传空串就恢复只显示编号。

## 注意

- 探测会对每个启用渠道真实发一次请求，key 有效的渠道会计入上游的调用次数，间隔别设太短
- 实时错误率判定依赖错误日志（ERROR_LOG_ENABLED 或同系列日志开关补丁），错误日志关闭时该信号失效，但渠道状态和探测结果仍然生效
- 展示名只是对外显示，不影响渠道本身的 name 字段

## 许可

跟 NewAPI 主项目一致，AGPL-3.0。
