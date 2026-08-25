# 庐山悠宿资源协同服务

生产级 Go 后端，管理别墅客房、山居房型、房车泊位和营位的跨日房态、询价锁定、身份确认、担保预订、到店核验、退住清洁、损耗及退款结算。

## 运行

要求 Go 1.25 和 SQLite。服务首次启动会应用 `internal/store/sqlite/migrations` 中的版本化 migration，并创建演示资源。配置见 `.env.example`。

```bash
go run ./cmd/server
curl http://localhost:8080/health/live
curl http://localhost:8080/health/ready
```

内置运营账号为 `operator` / `admin`。登录后使用响应中的 Bearer token。会话保存在数据库中，支持过期清理和退出撤销。

## 主要接口

- `POST /v1/auth/login`、`POST /v1/auth/logout`
- `GET /v1/resources`，支持 property、kind、status 与分页条件
- `POST /v1/stays/hold`，原子占用全部住宿夜晚并提供幂等结果
- `POST /v1/stays/{id}/guarantee`，同一事务完成身份确认、担保支付和状态推进
- `POST /v1/stays/{id}/check-in` 与 `checkout`
- `POST /v1/stays/{id}/settlements`，汇总支付与已接受损耗并启动退款结算

worker 在启动时立即恢复超时锁、过期会话和持久化运营任务，之后按配置间隔执行。所有写路径保留审计事件和 request ID。

## 验证

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
docker build --platform linux/amd64 -t youstay:amd64 .
docker build --platform linux/arm64 -t youstay:arm64 .
```

