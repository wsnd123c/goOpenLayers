package myhttp

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-spatial/tegola/config"
	"github.com/go-spatial/tegola/internal/log"
)

type PostHandle struct {
	bounds          []float64
	totalTiles      int
	completed       int
	mutex           sync.Mutex
	lastBroadcast   int
	broadcastTicker *time.Ticker
	taskID          string // 添加taskID字段
}

var conf *config.Config

// SetConfig 设置全局配置
func SetConfig(c *config.Config) {
	conf = c // c 已经是指针，直接赋值
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
		bounds:        bounds,
		lastBroadcast: 0,
		taskID:        task_id,
	}
}

// 请求单个瓦片（让数据进入Redis缓存）
func requestTile(z, x, y int, taskID string, handle *PostHandle, wg *sync.WaitGroup, sem chan struct{}) {
	defer wg.Done()
	sem <- struct{}{}
	defer func() { <-sem }()

	client := &http.Client{
		Timeout: 30 * time.Second, // 增加超时时间到30秒，避免数据库查询超时
	}

	url := fmt.Sprintf("http://127.0.0.1:19089/maps/inference_database/%d/%d/%d.pbf?task_id=%s",
		z, x, y, taskID)
	resp, err := client.Get(url)
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

	_, err = io.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("读取响应失败: %v", err)
		handle.updateProgress()
		return
	}
	if conf != nil {
		log.Infof("conf:%v", *conf)
		log.Infof("Cache config: %v", conf.Cache)
	} else {
		log.Infof("conf is nil")
	}
	handle.updateProgress()
	log.Infof("成功请求瓦片 Z:%d X:%d Y:%d (已缓存到Redis) 进度: %d/%d",
		z, x, y, handle.completed, handle.totalTiles)
}

// 更新进度
func (h *PostHandle) updateProgress() {
	h.mutex.Lock()
	h.completed++
	shouldBroadcast := false

	// 只在完成数量变化较大时才广播（每10个瓦片或每5%进度）
	if h.totalTiles > 0 {
		progressDiff := h.completed - h.lastBroadcast
		percentageDiff := float64(progressDiff) / float64(h.totalTiles) * 100

		if progressDiff >= 10 || percentageDiff >= 5.0 || h.completed == h.totalTiles {
			shouldBroadcast = true
			h.lastBroadcast = h.completed
		}
	}
	h.mutex.Unlock()

	// 只在需要时广播进度更新
	if shouldBroadcast {
		BroadcastTaskProgress(h.taskID)
	}
}

// 获取进度
func (h *PostHandle) GetProgress() (completed, total int, percentage float64) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	if h.totalTiles > 0 {
		percentage = float64(h.completed) / float64(h.totalTiles) * 100
	}
	// 减少日志输出频率，只在Debug模式下输出
	// log.Infof("[GetProgress] 已完成 %d / %d (%.2f%%)", h.completed, h.totalTiles, percentage)
	return h.completed, h.totalTiles, percentage
}

// 请求瓦片（缓存到Redis）
func (h *PostHandle) FetchTiles(taskID string, zooms []int) {
	sem := make(chan struct{}, 15) // 增加并发数，与数据库连接池匹配
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
	h.lastBroadcast = 0

	// 异步发送初始进度广播，避免阻塞主流程
	go func() {
		time.Sleep(100 * time.Millisecond) // 稍微延迟，确保任务已经开始
		BroadcastTaskProgress(h.taskID)
	}()

	// 开始请求瓦片
	log.Infof("开始处理 %d 个缩放级别的瓦片", len(zooms))
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

	log.Infof("等待所有 %d 个瓦片请求完成...", h.totalTiles)
	wg.Wait()
	log.Infof("所有任务完成，总共处理 %d 个瓦片", h.totalTiles)

	// 发送最终进度广播
	BroadcastTaskProgress(h.taskID)

	// 任务完成后清理handle
	handleMutex.Lock()
	delete(currentHandles, h.taskID)
	handleMutex.Unlock()
	log.Infof("任务 %s 已清理", h.taskID)
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
