package myhttp

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/go-spatial/tegola/internal/log"
)

type PostHandle struct {
	bounds     []float64
	totalTiles int
	completed  int
	mutex      sync.Mutex
}

func initGinEngine() {
	gin.SetMode(gin.ReleaseMode)
	ginRouter = gin.Default()
}
func deg2rad(deg float64) float64 {
	return deg * math.Pi / 180.0
}

// 将经纬度转换为指定缩放级别的瓦片坐标
func lngLatToTile(lng, lat float64, zoom int) (x, y int) {
	n := math.Pow(2.0, float64(zoom))
	x = int(math.Floor((lng + 180.0) / 360.0 * n))
	latRad := deg2rad(lat)
	y = int(math.Floor((1.0 - math.Log(math.Tan(latRad)+1.0/math.Cos(latRad))/math.Pi) / 2.0 * n))
	return x, y
}
func NewHandle(bounds []float64, task_id string) *PostHandle {
	return &PostHandle{
		bounds: bounds,
	}
}

// 请求单个瓦片（让数据进入Redis缓存）
func requestTile(z, x, y int, taskID string, handle *PostHandle, wg *sync.WaitGroup, sem chan struct{}) {
	defer wg.Done()
	sem <- struct{}{}
	defer func() { <-sem }()

	url := fmt.Sprintf("http://127.0.0.1:19091/maps/inference_database/%d/%d/%d.pbf?task_id=%s&isSlice=true",
		z, x, y, taskID)

	resp, err := http.Get(url)
	if err != nil {
		log.Errorf("请求失败: %v", err)
		handle.updateProgress()
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Errorf("请求错误 %s: %v", url, resp.Status)
		handle.updateProgress()
		return
	}

	// 只读取响应内容以确保请求完整，但不保存到文件
	// 这样数据会被Tegola处理并存入Redis缓存
	_, err = io.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("读取响应失败: %v", err)
		handle.updateProgress()
		return
	}

	handle.updateProgress()
	log.Infof("成功请求瓦片 Z:%d X:%d Y:%d (已缓存到Redis) 进度: %d/%d", z, x, y, handle.completed, handle.totalTiles)
}

// 更新进度
func (h *PostHandle) updateProgress() {
	h.mutex.Lock()
	h.completed++
	h.mutex.Unlock()

	// 广播进度更新
	BroadcastProgress()
}

// 获取进度
func (h *PostHandle) GetProgress() (completed, total int, percentage float64) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	if h.totalTiles > 0 {
		percentage = float64(h.completed) / float64(h.totalTiles) * 100
	}
	return h.completed, h.totalTiles, percentage
}

// 请求瓦片（缓存到Redis）
func (h *PostHandle) FetchTiles(taskID string, zooms []int) {
	sem := make(chan struct{}, 5) // 限制并发数
	var wg sync.WaitGroup

	// 先计算总瓦片数
	h.totalTiles = 0
	for _, zoom := range zooms {
		xMin, yMax := lngLatToTile(h.bounds[0], h.bounds[1], zoom)
		xMax, yMin := lngLatToTile(h.bounds[2], h.bounds[3], zoom)
		tileCount := (xMax - xMin + 1) * (yMax - yMin + 1)
		h.totalTiles += tileCount
		log.Infof("Zoom=%d: xMin=%d, yMin=%d, xMax=%d, yMax=%d, 瓦片数: %d", zoom, xMin, yMin, xMax, yMax, tileCount)
	}

	log.Infof("总瓦片数: %d", h.totalTiles)
	h.completed = 0

	// 开始请求瓦片
	for _, zoom := range zooms {
		xMin, yMax := lngLatToTile(h.bounds[0], h.bounds[1], zoom)
		xMax, yMin := lngLatToTile(h.bounds[2], h.bounds[3], zoom)

		for x := xMin; x <= xMax; x++ {
			for y := yMin; y <= yMax; y++ {
				wg.Add(1)
				go requestTile(zoom, x, y, taskID, h, &wg, sem)
			}
		}
	}

	wg.Wait()
	log.Infof("所有任务完成，总共处理 %d 个瓦片", h.totalTiles)
}

func HandleZoom(zoomRange []int) []int {
	if len(zoomRange) != 2 {
		return nil
	}
	min, max := zoomRange[0], zoomRange[1]

	if min > max {
		min, max = max, min
	}

	zooms := make([]int, 0, max-min+1)
	for z := min; z <= max; z++ {
		zooms = append(zooms, z)
	}
	return zooms
}
