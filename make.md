## 🚀 常用命令

| 命令 | 说明 |
|------|------|
| `make test` | 完整测试（自动启动 Docker + 服务器） |
| `make test-quick` | 快速测试（假设 Docker 已运行） |
| `make test-specific TEST=TestAuth` | 运行指定测试 |
| `make docker-up` | 启动 MySQL 和 Redis |
| `make docker-down` | 停止并清理容器 |
| `make server-bg` | 后台启动服务器 |
| `make server-stop` | 停止服务器 |
| `make test-ci` | CI 模式（完全干净环境） |