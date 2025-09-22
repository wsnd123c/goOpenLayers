package myhttp

import (
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/go-spatial/tegola/internal/log"
)

type PostHandle struct {
	bounds                 []float64
	xMin, yMin, xMax, yMax int32
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
	zoom := 14
	xMin, yMax := lngLatToTile(bounds[0], bounds[1], zoom)
	xMax, yMin := lngLatToTile(bounds[2], bounds[3], zoom)
	fmt.Print(xMin, yMin, xMax, yMin)
	log.Infof("xMax=%v, yMax=%v,xMin=%v,yMin=%v", xMax, yMax, xMin, yMin)
	return &PostHandle{
		bounds: bounds,
		xMin:   int32(xMin),
		yMin:   int32(yMin),
		xMax:   int32(xMax),
		yMax:   int32(yMax),
	}
}

// 下载单个瓦片
func downloadTile(z, x, y int, taskID string, wg *sync.WaitGroup, sem chan struct{}) {
	defer wg.Done()
	sem <- struct{}{}
	defer func() { <-sem }()

	url := fmt.Sprintf("http://127.0.0.1:19091/maps/inference_database/%d/%d/%d.pbf?task_id=%s&isSlice=true",
		z, x, y, taskID)

	resp, err := http.Get(url)
	if err != nil {
		log.Errorf("请求失败: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Errorf("请求错误: %v", resp.Status)
		return
	}

	data, _ := ioutil.ReadAll(resp.Body)
	filename := fmt.Sprintf("%d_%d_%d.pbf", z, x, y)
	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		log.Errorf("写文件失败: %v", err)
	}
	log.Infof("成功下载: %s", filename)
}

// 新增方法：并发调用服务，下载瓦片
func (h *PostHandle) FetchTiles(taskID string, zoom int) {
	sem := make(chan struct{}, 5) // 限制并发数
	var wg sync.WaitGroup

	for x := h.xMin; x <= h.xMax; x++ {
		for y := h.yMin; y <= h.yMax; y++ {
			wg.Add(1)
			go downloadTile(zoom, int(x), int(y), taskID, &wg, sem)
		}
	}

	wg.Wait()
	log.Infof("所有任务完成 ✅")
}
