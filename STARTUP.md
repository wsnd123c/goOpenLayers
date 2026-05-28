# 本地启动文档

## 启动命令

当前本地测试请使用新编译的二进制：

```powershell
.\bin\tegola-schema.exe serve --config config.toml --no-cache
```

说明：

- `config.toml` 中服务端口是 `:19091`。
- `--no-cache` 是必须建议项，因为当前配置里的缓存目录是 `D:/temp/tegola-cache`，本机没有 `D:` 盘。
- 根目录的 `.\tegola.exe` 是旧二进制，不包含本次新增的 `schema` 参数逻辑。

## 访问地址

本机直接访问：

```text
http://localhost:19091/
```

瓦片接口：

```text
http://localhost:19091/maps/vector/{schema}/{table}/{z}/{x}/{y}.pbf
```

如果前端通过 nginx 代理 `/tile/`：

```nginx
location ^~ /tile/ {
    proxy_pass http://127.0.0.1:19091/;
}
```

前端访问方式：

```text
/tile/maps/vector/${schema}/${item.task_id}/{z}/{x}/{y}.pbf
```

## 路由规则

统一使用路径里的 schema 和表名：

```text
/tile/maps/vector/{schema}/{table}/{z}/{x}/{y}.pbf
```

示例：

```text
/tile/maps/vector/public/task_123/12/3365/1552.pbf
```

后端实际会查询：

```text
public.task_123
```

## 依赖

服务依赖本机 PostgreSQL：

```text
127.0.0.1:5432/inference_database
```

配置在 `config.toml`：

```text
postgres://postgres:postgres@127.0.0.1:5432/inference_database?sslmode=disable
```

## 后台启动

如果想在 PowerShell 后台启动，并把日志写入 `logs`：

```powershell
New-Item -ItemType Directory -Force -Path .\logs | Out-Null
Start-Process -FilePath .\bin\tegola-schema.exe `
  -ArgumentList @('serve','--config','config.toml','--no-cache') `
  -WorkingDirectory (Get-Location) `
  -WindowStyle Hidden `
  -RedirectStandardOutput .\logs\tegola.out.log `
  -RedirectStandardError .\logs\tegola.err.log
```

检查端口：

```powershell
Get-NetTCPConnection -LocalPort 19091
```

停止服务：

```powershell
Get-Process tegola-schema -ErrorAction SilentlyContinue | Stop-Process
```
