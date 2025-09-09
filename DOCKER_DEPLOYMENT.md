# TegolaProGIS Docker 部署指南

## 概述

本项目提供了完整的Docker容器化解决方案，包括：
- TegolaProGIS主服务
- PostgreSQL/PostGIS数据库
- Redis缓存
- Nacos配置中心

## 快速开始

### 1. 构建和启动所有服务

```bash
# 克隆项目
git clone <repository-url>
cd tegolaPro

# 启动所有服务
docker-compose up -d
```

### 2. 查看服务状态

```bash
# 查看所有容器状态
docker-compose ps

# 查看日志
docker-compose logs -f tegola-pro
```

### 3. 访问服务

- **TegolaProGIS**: http://localhost:19089
- **Nacos控制台**: http://localhost:8848/nacos (用户名/密码: nacos/nacos)
- **PgAdmin** (可选): http://localhost:5555 (用户名/密码: admin@tegola.io/admin)

## 配置说明

### 端口映射

| 服务 | 容器端口 | 主机端口 | 说明 |
|------|----------|----------|------|
| tegola-pro | 19089 | 19089 | GIS服务主端口 |
| nacos | 8848 | 8848 | Nacos控制台 |
| nacos | 9848 | 9848 | Nacos gRPC端口 |
| postgis | 5432 | 5432 | PostgreSQL数据库 |
| redis | 6379 | 6379 | Redis缓存 |
| pgadmin | 80 | 5555 | 数据库管理界面 |

### 数据持久化

项目使用Docker volumes进行数据持久化：

- `redis_data`: Redis数据
- `postgres_data`: PostgreSQL数据
- `nacos_data`: Nacos配置数据
- `nacos_logs`: Nacos日志
- `tegola_data`: Tegola应用数据

### 配置文件

主配置文件 `config.toml` 会被挂载到容器内的 `/opt/tegola_config/config.toml`

## 单独构建和运行

### 构建TegolaProGIS镜像

```bash
docker build -t tegola-pro .
```

### 运行单个容器

```bash
# 运行TegolaProGIS (需要先启动依赖服务)
docker run -d \
  --name tegola-pro \
  -p 19089:19089 \
  -v $(pwd)/config.toml:/opt/tegola_config/config.toml:ro \
  tegola-pro
```

## 开发模式

### 启动开发环境

```bash
# 启动依赖服务(不包括tegola-pro)
docker-compose up -d redis postgis nacos

# 本地运行tegola-pro进行开发
go run cmd/tegola/main.go serve --config config.toml
```

### 启动PgAdmin进行数据库管理

```bash
docker-compose --profile pgadmin up -d
```

## 生产部署建议

### 1. 环境变量配置

创建 `.env` 文件：

```env
# 数据库配置
POSTGRES_USER=your_user
POSTGRES_PASSWORD=your_password
POSTGRES_DB=your_database

# Nacos配置
NACOS_AUTH_TOKEN=your_secret_token
NACOS_USERNAME=your_username
NACOS_PASSWORD=your_password

# 应用配置
TEGOLA_PORT=19089
TZ=Asia/Shanghai
```

### 2. 生产配置优化

```yaml
# docker-compose.prod.yml
version: '3.8'
services:
  tegola-pro:
    build: .
    deploy:
      replicas: 2
      resources:
        limits:
          cpus: '1.0'
          memory: 1G
        reservations:
          cpus: '0.5'
          memory: 512M
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:19089/"]
      interval: 30s
      timeout: 10s
      retries: 3
```

### 3. 日志管理

```yaml
services:
  tegola-pro:
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
```

## 故障排除

### 常见问题

1. **容器启动失败**
   ```bash
   # 查看详细错误信息
   docker-compose logs tegola-pro
   ```

2. **端口冲突**
   ```bash
   # 检查端口占用
   netstat -tulpn | grep :19089
   ```

3. **数据库连接失败**
   ```bash
   # 检查PostgreSQL状态
   docker-compose exec postgis pg_isready -U postgres
   ```

4. **配置文件问题**
   ```bash
   # 验证配置文件
   docker-compose exec tegola-pro cat /opt/tegola_config/config.toml
   ```

### 重置环境

```bash
# 停止并删除所有容器和数据卷
docker-compose down -v

# 删除镜像
docker rmi tegola-pro

# 重新构建和启动
docker-compose up -d --build
```

## 监控和维护

### 健康检查

```bash
# 检查所有服务健康状态
docker-compose ps

# 查看特定服务健康状态
docker inspect --format='{{.State.Health.Status}}' tegola-pro
```

### 性能监控

```bash
# 查看资源使用情况
docker stats

# 查看容器日志
docker-compose logs -f --tail=100
```

## 安全建议

1. **更改默认密码**: 修改PostgreSQL、Nacos等服务的默认密码
2. **网络隔离**: 使用自定义网络隔离服务
3. **最小权限**: 容器内使用非root用户运行
4. **定期更新**: 定期更新基础镜像和依赖
5. **备份策略**: 定期备份数据卷

## 扩展部署

### 负载均衡

```yaml
services:
  nginx:
    image: nginx:alpine
    ports:
      - "80:80"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf
    depends_on:
      - tegola-pro
```

### 集群部署

使用Docker Swarm或Kubernetes进行集群部署，具体配置请参考相应的编排工具文档。
