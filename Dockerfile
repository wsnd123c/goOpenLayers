# =========================
# 1) 构建 UI（生成 ui/dist）
# =========================
FROM node:20-alpine AS ui
WORKDIR /app/ui
# 仅复制包清单，利用缓存
COPY ui/package*.json ./
RUN npm ci
# 复制前端代码并构建
COPY ui/ .
RUN npm run build

# ==================================
# 2) 构建 Go 二进制（嵌入 ui/dist）
# ==================================
FROM golang:1.24.5-alpine3.22 AS build

ARG BUILDPKG="github.com/go-spatial/tegola/internal/build"
ARG VER="Version Not Set"
ARG BRANCH="not set"
ARG REVISION="not set"
ENV VERSION="${VER}"
ENV GIT_BRANCH="${BRANCH}"
ENV GIT_REVISION="${REVISION}"
ENV BUILD_PKG="${BUILDPKG}"

# CGO 依赖（与原注释一致）
RUN apk update && apk add --no-cache build-base git

# 准备源代码
RUN mkdir -p /go/src/github.com/go-spatial/tegola
WORKDIR /go/src/github.com/go-spatial/tegola
COPY . .

# 把 UI 产物塞回仓库路径，供 //go:embed 匹配
COPY --from=ui /app/ui/dist ./ui/dist

# 输出环境变量（可保留，也可删除）
RUN env

# 构建 tegola 可执行文件
WORKDIR /go/src/github.com/go-spatial/tegola/cmd/tegola
RUN go build -v \
  -ldflags "-w -X '${BUILD_PKG}.Version=${VERSION}' -X '${BUILD_PKG}.GitRevision=${GIT_REVISION}' -X '${BUILD_PKG}.GitBranch=${GIT_BRANCH}'" \
  -gcflags "-N -l" \
  -o /opt/tegola && chmod a+x /opt/tegola

# ===========================
# 3) 运行时最小化镜像
# ===========================
FROM alpine:3.18
RUN apk update && apk add --no-cache ca-certificates && rm -rf /var/cache/apk/*
COPY --from=build /opt/tegola /opt/
WORKDIR /opt
ENTRYPOINT ["/opt/tegola"]
