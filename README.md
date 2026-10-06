# newapi-status-page

给 NewAPI 加一个自助状态页。用户自己就能看到哪个分组的模型出了问题，不用再来问客服。

功能点：

- 按分组聚合模型可用性，三档状态：正常 / 部分降级 / 不可用
- 每个模型一条 60 小时时间轴，一小时一格，绿黄红灰四色，悬停可看该小时的请求数和错误数
- 全站 60 小时可用率百分比
- 后台可定时探测所有渠道的连通性（默认关闭，可自定义间隔分钟数），结果不写日志表
- 渠道在状态页只显示编号（比如 #10），管理员可以给渠道设置一个对外展示的名字，不设置就只显示编号
- 探测失败的渠道行有一个查看按钮，点开弹窗直接看错误原文
- 页面按后台设定的间隔自动刷新（默认 30 秒，可开关），用户不用手动刷

状态页对未登录用户只展示汇总，渠道明细只有管理员登录后可见。

## 数据从哪来

不依赖任何外部监控服务。模型状态由 abilities 和 channels 两张表实时算出；时间轴从 logs 表按小时聚合，有消费日志算正常小时，有错误日志按比例标黄或标红。渠道探测是直接对渠道的 base_url 发一个带 key 的 GET 请求，记录响应时间和错误信息。

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
common.OptionMap["StatusPageAutoRefreshEnabled"] = "true"
common.OptionMap["StatusPageAutoRefreshInterval"] = "30"
common.OptionMap["StatusPageChannelTestEnabled"] = "false"
common.OptionMap["StatusPageChannelTestInterval"] = "30"
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

路由里用到的 `PublicLayout`、`PageTransition`、`Dialog` 都是 NewAPI 自带组件，不用额外装东西。

想在顶部导航加入口的话，改 web/src/hooks/use-top-nav-links.ts，参考 rankings 的写法加一条指向 /model-status 的链接，然后在后台的 HeaderNavModules 配置里加 `"model_status": {"enabled": true}`。

构建：`cd web && bun install && bun run build`。

## 设置项

都通过 NewAPI 的 option 系统改，四个键：

| 键 | 默认 | 说明 |
|---|---|---|
| StatusPageAutoRefreshEnabled | true | 状态页自动刷新开关 |
| StatusPageAutoRefreshInterval | 30 | 自动刷新间隔秒数，最小 5 |
| StatusPageChannelTestEnabled | false | 定时探测渠道开关 |
| StatusPageChannelTestInterval | 30 | 探测间隔分钟数，最小 1 |

渠道展示名的设置接口是 `PUT /api/user/channel/{id}/display-name`，body 传 `{"display_name": "名字"}`，传空串就恢复只显示编号。

## 注意

- 探测会对每个启用渠道真实发一次请求，key 有效的渠道会计入上游的调用次数，间隔别设太短
- 时间轴基于 logs 表，错误日志默认不记录，想让时间轴反映错误需要开 ERROR_LOG_ENABLED 或者用本仓库同系列的日志开关补丁
- 展示名只是对外显示，不影响渠道本身的 name 字段

## 许可

跟 NewAPI 主项目一致，AGPL-3.0。
