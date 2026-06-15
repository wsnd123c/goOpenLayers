# WSL 本地启动文档

项目已经放在 WSL 的 Ubuntu 里，建议在 Ubuntu 终端内编译和启动，不再使用 Windows 的
`.exe` 二进制。

## 进入项目

在 PowerShell 里进入 WSL：

```powershell
wsl -d Ubuntu-24.04
```

然后在 Ubuntu 里进入项目目录：

```bash
cd ~/goOpenLayers
```

也可以从 PowerShell 直接执行 WSL 命令：

```powershell
wsl -d Ubuntu-24.04 -- bash -lc "cd ~/goOpenLayers && ./bin/tegola-schema serve --config config.toml --no-cache"
```

## 安装 Go

如果 Ubuntu 里还没有 `go` 命令，先安装 Go：

```bash
sudo apt update
sudo apt install -y golang-go
go version
```

本项目要求 Go 1.22 或更高版本。

## 编译

在 WSL 里编译 Linux 二进制：

```bash
cd ~/goOpenLayers
mkdir -p bin
go build -mod=vendor -o bin/tegola-schema ./cmd/tegola
```

说明：

- `bin/tegola-schema.exe`、根目录的 `tegola.exe` 和 `ok.exe` 都是 Windows 二进制，不适合作为 WSL 原生启动入口。
- WSL 里启动请使用新编译出来的 `bin/tegola-schema`。

## 启动命令

前台启动：

```bash
cd ~/goOpenLayers
./bin/tegola-schema serve --config config.toml --no-cache
```

说明：

- `config.toml` 中服务端口是 `:19091`。
- `--no-cache` 是建议项，因为当前配置里的缓存目录是 `D:/temp/tegola-cache`，这是 Windows 路径；在 WSL 内直接用会不稳定。
- 如果要启用文件缓存，请先把 `config.toml` 里的 `basepath` 改成 WSL 路径，例如 `/tmp/tegola-cache`。

## 访问地址

服务在 WSL 启动后，Windows 浏览器通常可以直接访问：

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

可选查询参数：

```text
?status=editing
?status=deleted
```

说明：

- `status` 是可选参数，不传时不过滤状态，兼容老项目。
- 传 `status` 时，只返回表里 `status` 字段等于该值的矢量。
- `status=editing` 用于前端实时编辑态瓦片，后端会跳过瓦片缓存，并返回 `Cache-Control: no-store`，避免浏览器复用旧瓦片。
- 目前常用值是 `editing` 和 `deleted`，后续如果新增其他状态，直接传对应值即可。

## 路由规则

统一使用路径里的 schema 和表名：

```text
/tile/maps/vector/{schema}/{table}/{z}/{x}/{y}.pbf
```

示例：

```text
/tile/maps/vector/public/task_123/12/3365/1552.pbf
/tile/maps/vector/public/task_123/12/3365/1552.pbf?status=editing
```

后端实际会查询：

```text
public.task_123
```

## 依赖

服务依赖 PostgreSQL，当前 `config.toml` 里的连接地址是：

```text
127.0.0.1:35432/rjxt
```

配置在 `config.toml`：

```text
postgres://postgres:postgres@127.0.0.1:35432/rjxt?sslmode=disable&pool_max_conns=20&pool_min_conns=5
```

如果 PostgreSQL 跑在 Windows 上，而 WSL 里连接 `127.0.0.1:35432` 失败，可以先取 Windows
宿主机 IP：

```bash
cat /etc/resolv.conf | grep nameserver
```

然后把 `config.toml` 里的 `127.0.0.1` 临时改成这个 IP 再启动。

## 后台启动

如果想在 WSL 后台启动，并把日志写入 `logs`：

```bash
cd ~/goOpenLayers
mkdir -p logs
nohup ./bin/tegola-schema serve --config config.toml --no-cache \
  > logs/tegola.out.log \
  2> logs/tegola.err.log &
```

检查端口：

```bash
ss -lntp | grep 19091
```

停止服务：

```bash
pkill -f "tegola-schema serve"
```

查看日志：

```bash
tail -f logs/tegola.out.log logs/tegola.err.log
```

## 日志格式

默认日志是适合本地排查的控制台格式，例如：

```text
2026-06-15 16:22:10 ERROR PostGIS(pgx): Query err="column \"g_clip\" does not exist code=42703" args=[editing] pid=3792640 time=28.080404ms
  sql: SELECT ...
```

如果要输出 JSON 格式，方便日志平台采集：

```bash
TEGOLA_LOG_FORMAT=json ./bin/tegola-schema serve --config config.toml --no-cache
```

默认不输出错误堆栈；需要排查代码调用栈时再开启：

```bash
TEGOLA_LOG_STACK=true ./bin/tegola-schema serve --config config.toml --no-cache
```
