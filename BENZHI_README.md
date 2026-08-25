# BENZHI_README

这是一个 Go 后端服务，用于生产级 Go 后端，管理别墅客房、山居房型、房车泊位和营位的跨日房态、询价锁定、身份确认、担保预订、到店核验、退住清洁、损耗及退款结算。

## 项目说明

- 项目：11DingKing/lushan-youstay-allocation
- 项目用途：生产级 Go 后端，管理别墅客房、山居房型、房车泊位和营位的跨日房态、询价锁定、身份确认、担保预订、到店核验、退住清洁、损耗及退款结算。
- Go 工具链：`golang:1.25.0`
- 前端工具链：无

## 标准构建、运行和测试命令

进入容器后执行：

```bash
# 编译
cd '/app' && GOTOOLCHAIN=local go build ./...

# 启动
cd '/app' && GOTOOLCHAIN=local go run ./cmd/server

# 测试
cd '/app' && GOTOOLCHAIN=local go test ./...
```

## Docker 构建和进入容器

```bash
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-task-9-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-task-9-arm64 linux/arm64
docker run -it benzhi-task-9-amd64:latest
docker run -it --platform linux/arm64 benzhi-task-9-arm64:latest
```

## 题目验证命令

1. 预期退出码 0：`go test ./internal/httpapi -run '^TestSettlementStartIsSingleUseAfterCheckout$' -count=1`
2. 预期退出码 0：`go test ./...`
3. 预期退出码 0：`GOTOOLCHAIN=local go build -buildvcs=false ./... && GOTOOLCHAIN=local go vet ./...`
