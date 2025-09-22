package myhttp

import (
	"net/http"
	"sync"

	"github.com/dimfeld/httptreemux"
	"github.com/gin-gonic/gin"
	"github.com/go-spatial/tegola/internal/log"
	"github.com/gorilla/websocket"
)

type ginHandler struct {
	engine *gin.Engine
}

var ginRouter *gin.Engine
var currentHandle *PostHandle
var handleMutex sync.Mutex
var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}
var wsClients = make(map[*websocket.Conn]bool)
var wsClientsMutex sync.Mutex

// 初始化Gin并挂载到主路由
func InitGin(mainRouter *httptreemux.TreeMux) {
	initGinEngine()
	registerGinRoutes()
	mountToMainRouter(mainRouter)
	log.Info("Gin integration initialized")
}
func (h *ginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.engine.ServeHTTP(w, r)
}

func registerGinRoutes() {
	ginRouter.POST("/sliceTiles", func(c *gin.Context) {
		var req struct {
			IsSlice bool      `json:"isSlice"`
			TaskID  string    `json:"task_id"`
			Bounds  []float64 `json:"bounds"`
			Minzoom int       `json:"minzoom"`
			Maxzoom int       `json:"maxzoom"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    500,
				"message": "error",
			})
			return
		}
		if len(req.Bounds) != 4 {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "bounds 必须是 [minLng, minLat, maxLng, maxLat]",
			})
			return
		}
		log.Infof("IsSlice=%v, TaskID=%s, Bounds=%v , Minzoom=%v ,Maxzoom=%v", req.IsSlice, req.TaskID, req.Bounds, req.Minzoom, req.Maxzoom)

		if !req.IsSlice {
			c.JSON(http.StatusOK, gin.H{
				"message": "停止切片",
				"code":    200,
				"isSlice": req.IsSlice,
			})
		} else {
			//处理边界范围
			zooms := HandleZoom([]int{req.Minzoom, req.Maxzoom})
			handle := NewHandle(req.Bounds, req.TaskID)

			// 保存当前处理的handle到全局变量
			handleMutex.Lock()
			currentHandle = handle
			handleMutex.Unlock()

			// 在goroutine中异步处理切片
			go func() {
				handle.FetchTiles(req.TaskID, zooms)
				log.Infof("任务 %s 切片完成", req.TaskID)
			}()

			c.JSON(http.StatusOK, gin.H{
				"message": "开始切片，请等待",
				"code":    200,
				"isSlice": req.IsSlice,
			})
		}
	})

	// WebSocket进度推送
	ginRouter.GET("/ws/progress", func(c *gin.Context) {
		conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Errorf("WebSocket升级失败: %v", err)
			return
		}
		defer conn.Close()

		// 注册客户端
		wsClientsMutex.Lock()
		wsClients[conn] = true
		wsClientsMutex.Unlock()

		log.Info("新的WebSocket客户端连接")

		// 发送当前进度
		sendCurrentProgress(conn)

		// 保持连接活跃
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				log.Infof("WebSocket客户端断开连接: %v", err)
				break
			}
		}

		// 移除客户端
		wsClientsMutex.Lock()
		delete(wsClients, conn)
		wsClientsMutex.Unlock()
	})

	v1 := ginRouter.Group("/v1")
	{
		v1.GET("/status", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"tegola":  "running",
				"version": "custom",
			})
		})
	}
}

// 发送当前进度给单个客户端
func sendCurrentProgress(conn *websocket.Conn) {
	handleMutex.Lock()
	handle := currentHandle
	handleMutex.Unlock()

	var message map[string]interface{}
	if handle == nil {
		message = map[string]interface{}{
			"type":       "progress",
			"code":       200,
			"message":    "无正在进行的任务",
			"completed":  0,
			"total":      0,
			"percentage": 0.0,
			"isRunning":  false,
		}
	} else {
		completed, total, percentage := handle.GetProgress()
		isRunning := completed < total
		message = map[string]interface{}{
			"type":       "progress",
			"code":       200,
			"message":    "获取进度成功",
			"completed":  completed,
			"total":      total,
			"percentage": percentage,
			"isRunning":  isRunning,
		}
	}

	if err := conn.WriteJSON(message); err != nil {
		log.Errorf("发送WebSocket消息失败: %v", err)
	}
}

// 广播进度更新给所有客户端
func BroadcastProgress() {
	handleMutex.Lock()
	handle := currentHandle
	handleMutex.Unlock()

	if handle == nil {
		return
	}

	completed, total, percentage := handle.GetProgress()
	isRunning := completed < total

	message := map[string]interface{}{
		"type":       "progress",
		"code":       200,
		"message":    "进度更新",
		"completed":  completed,
		"total":      total,
		"percentage": percentage,
		"isRunning":  isRunning,
	}

	wsClientsMutex.Lock()
	defer wsClientsMutex.Unlock()

	for conn := range wsClients {
		if err := conn.WriteJSON(message); err != nil {
			log.Errorf("发送WebSocket消息失败: %v", err)
			// 连接已断开，从列表中移除
			delete(wsClients, conn)
			conn.Close()
		}
	}
}

func mountToMainRouter(mainRouter *httptreemux.TreeMux) {
	// 去前缀
	stripped := http.StripPrefix("/api", ginRouter)
	adapter := func(w http.ResponseWriter, r *http.Request, params map[string]string) {
		stripped.ServeHTTP(w, r)
	}

	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete}
	for _, method := range methods {
		mainRouter.Handle(method, "/api/*path", adapter)
	}
}
