package server

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/go-spatial/geom/encoding/mvt"
	"github.com/go-spatial/tegola/atlas"
	"github.com/go-spatial/tegola/cache"
	"github.com/go-spatial/tegola/internal/log"
)

// TileCacheHandler implements a request cache for tiles on requests when the URLs
// have a /:z/:x/:y scheme suffix (i.e. /osm/1/3/4.pbf)
func TileCacheHandler(a *atlas.Atlas, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error

		// Debug: 记录请求信息
		//log.Infof("DEBUG缓存中间件: 处理请求 %s, 查询参数: %s", r.URL.Path, r.URL.RawQuery)

		// check if a cache backend exists
		cacher := a.GetCache()
		if cacher == nil {
			// nope. move on
			//log.Warnf("DEBUG缓存中间件: Atlas中没有缓存实例!")
			next.ServeHTTP(w, r)
			return
		}

		//log.Infof("DEBUG缓存中间件: 找到缓存实例，类型: %T", cacher)

		// ignore requests with query parameters (except task_id and isSlice for dynamic tables)
		// 如果有查询参数，但没有包含 task_id= 和 isSlice=，则跳过缓存
		hasTaskId := strings.Contains(r.URL.RawQuery, "task_id=")
		hasIsSlice := strings.Contains(r.URL.RawQuery, "isSlice=")
		if r.URL.RawQuery != "" && !(hasTaskId || hasIsSlice) {
			log.Infof("DEBUG缓存中间件: 跳过缓存，不支持的查询参数: %s", r.URL.RawQuery)
			next.ServeHTTP(w, r)
			return
		}

		//log.Infof("DEBUG缓存中间件: 查询参数检查通过，继续缓存处理")

		// parse our URI into a cache key structure (remove any configured URIPrefix + "maps/" )
		keyPath := strings.TrimPrefix(r.URL.Path, path.Join(URIPrefix, "maps"))

		key, err := cache.ParseKey(keyPath)
		if err != nil {
			log.Errorf("DEBUG缓存中间件: ParseKey错误: %v, keyPath: %s", err, keyPath)
			next.ServeHTTP(w, r)
			return
		}

		// include query parameters in cache key for dynamic content
		// 使用hash来避免文件系统特殊字符问题
		if r.URL.RawQuery != "" {
			// 生成查询参数的MD5 hash
			queryHash := fmt.Sprintf("%x", md5.Sum([]byte(r.URL.RawQuery)))
			key.MapName = key.MapName + "_" + queryHash
			log.Infof("DEBUG缓存中间件: 查询参数 '%s' 转换为hash: %s", r.URL.RawQuery, queryHash)
		}

		//log.Infof("DEBUG缓存中间件: 缓存key: %s", key.String())

		// use the URL path as the key
		//log.Infof("DEBUG缓存中间件: 开始检查缓存，key: %s", key.String())
		cachedTile, hit, err := cacher.Get(r.Context(), key)
		if err != nil {
			log.Errorf("cache middleware: error reading from cache: %v", err)
			next.ServeHTTP(w, r)
			return
		}

		//log.Infof("DEBUG缓存中间件: 缓存检查结果 - hit: %v, err: %v", hit, err)

		// cache miss
		if !hit {
			//log.Infof("DEBUG缓存中间件: 缓存未命中，准备创建新缓存")
			// buffer which will hold a copy of the response for writing to the cache
			var buff bytes.Buffer

			// overwrite our current responseWriter with a tileCacheResponseWriter
			w = newTileCacheResponseWriter(w, &buff)
			//log.Infof("DEBUG缓存中间件: 创建了tileCacheResponseWriter，缓冲区初始大小: %d", buff.Len())

			//log.Infof("DEBUG缓存中间件: 开始处理请求以生成瓦片数据")
			next.ServeHTTP(w, r)
			//log.Infof("DEBUG缓存中间件: 请求处理完成，缓冲区最终大小: %d", buff.Len())

			// check if our request context has been canceled
			if r.Context().Err() != nil {
				//log.Infof("DEBUG缓存中间件: 请求上下文已取消")
				return
			}

			// if nothing has been written to the buffer, don't write to the cache
			if buff.Len() == 0 {
				//log.Infof("DEBUG缓存中间件: 缓冲区为空，不写入缓存")
				return
			}

			//log.Infof("DEBUG缓存中间件: 准备写入缓存，key: %s, 数据大小: %d bytes", key.String(), buff.Len())
			if err := cacher.Set(r.Context(), key, buff.Bytes()); err != nil {
				log.Warnf("cache response writer err: %v", err)
			} else {
				//log.Infof("DEBUG缓存中间件: 成功写入缓存，key: %s", key.String())
			}
			return
		}

		log.Infof("DEBUG缓存中间件: 缓存命中，返回缓存数据，key: %s, 大小: %d bytes", key.String(), len(cachedTile))

		// mimetype for mapbox vector tiles
		w.Header().Add("Content-Type", mvt.MimeType)

		// communicate the cache is being used
		w.Header().Add("Tegola-Cache", "HIT")
		w.Header().Add("Content-Length", fmt.Sprintf("%d", len(cachedTile)))

		w.Write(cachedTile)
		return
	})
}

func newTileCacheResponseWriter(resp http.ResponseWriter, w io.Writer) http.ResponseWriter {
	//log.Infof("DEBUG缓存中间件: 正在创建tileCacheResponseWriter")
	tcw := &tileCacheResponseWriter{
		resp:  resp,
		multi: io.MultiWriter(w, resp),
	}
	//log.Infof("DEBUG缓存中间件: tileCacheResponseWriter创建完成")
	return tcw
}

// tileCacheResponseWriter wraps http.ResponseWriter (https://golang.org/pkg/net/http/#ResponseWriter)
// to additionally write the response to a cache when there is a cache MISS
type tileCacheResponseWriter struct {
	// status response code
	status int
	resp   http.ResponseWriter
	multi  io.Writer
}

func (w *tileCacheResponseWriter) Header() http.Header {
	log.Infof("DEBUG缓存ResponseWriter: Header()方法被调用")
	// communicate the cache is being used
	w.resp.Header().Set("Tegola-Cache", "MISS")

	return w.resp.Header()
}

func (w *tileCacheResponseWriter) Write(b []byte) (int, error) {
	// 如果没有设置状态码，默认为200 OK
	if w.status == 0 {
		w.status = http.StatusOK
	}

	// only write to the multi writer when myhttp response == StatusOK
	if w.status == http.StatusOK {
		//log.Infof("DEBUG缓存ResponseWriter: 状态OK，写入缓存缓冲区，数据大小: %d bytes", len(b))
		// write to our multi writer
		return w.multi.Write(b)
	}

	//log.Infof("DEBUG缓存ResponseWriter: 状态非OK (status: %d)，不写入缓存，直接返回响应", w.status)
	// write to the original response writer
	return w.resp.Write(b)
}

func (w *tileCacheResponseWriter) WriteHeader(i int) {
	w.status = i
	//log.Infof("DEBUG缓存ResponseWriter: 设置响应状态码: %d", i)

	w.resp.WriteHeader(i)
}
