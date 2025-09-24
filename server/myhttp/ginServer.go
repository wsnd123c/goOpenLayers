package myhttp

import (
	"net/http"
	"sync"

	"github.com/dimfeld/httptreemux"
	"github.com/doquangtan/socketio/v4"
	"github.com/gin-gonic/gin"
	"github.com/go-spatial/tegola/internal/log"
)

type ginHandler struct {
	engine *gin.Engine
}

var ginRouter *gin.Engine
var currentHandles = make(map[string]*PostHandle) // 支持多个任务并发
var handleMutex sync.Mutex
var ioServer *socketio.Io
var socketClients = make(map[*socketio.Socket]bool)
var socketClientsMutex sync.Mutex

// 初始化Gin并挂载到主路由
func InitGin(mainRouter *httptreemux.TreeMux) {
	initGinEngine()
	initSocketIOServer()
	registerGinRoutes()
	mountToMainRouter(mainRouter)
	log.Info("Gin integration initialized")
}

func (h *ginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.engine.ServeHTTP(w, r)
}

// 初始化Socket.IO服务器
func initSocketIOServer() {
	// 创建新的Socket.IO v4服务器实例
	ioServer = socketio.New()

	// 监听连接事件
	ioServer.OnConnection(func(socket *socketio.Socket) {
		log.Infof("新的Socket.IO客户端连接: %s", socket.Id)

		// 注册客户端
		socketClientsMutex.Lock()
		socketClients[socket] = true
		clientCount := len(socketClients)
		socketClientsMutex.Unlock()

		log.Infof("当前连接的客户端数量: %d", clientCount)

		// 监听断开连接事件
		socket.On("disconnect", func(event *socketio.EventPayload) {
			log.Infof("Socket.IO客户端断开连接: %s", socket.Id)

			// 移除客户端
			socketClientsMutex.Lock()
			delete(socketClients, socket)
			socketClientsMutex.Unlock()
		})

		// 监听加入房间事件
		socket.On("join_task", func(event *socketio.EventPayload) {
			if len(event.Data) == 0 {
				log.Errorf("客户端 %s 加入房间失败：无数据", socket.Id)
				return
			}

			data, ok := event.Data[0].(map[string]interface{})
			if !ok {
				log.Errorf("客户端 %s 加入房间失败：数据格式错误", socket.Id)
				return
			}

			taskID, ok := data["task_id"].(string)
			if !ok || taskID == "" {
				log.Errorf("客户端 %s 加入房间失败：无效的task_id", socket.Id)
				return
			}

			roomName := "task_" + taskID
			socket.Join(roomName)
			log.Infof("客户端 %s 加入房间: %s", socket.Id, roomName)

			// 发送该任务的当前进度
			sendTaskProgress(socket, taskID)
		})

		// 监听进度请求事件
		socket.On("get_progress", func(event *socketio.EventPayload) {
			if len(event.Data) == 0 {
				log.Errorf("客户端 %s 请求进度失败：无数据", socket.Id)
				return
			}

			data, ok := event.Data[0].(map[string]interface{})
			if !ok {
				log.Errorf("客户端 %s 请求进度失败：数据格式错误", socket.Id)
				return
			}

			taskID, ok := data["task_id"].(string)
			if !ok || taskID == "" {
				log.Errorf("客户端 %s 请求进度失败：无效的task_id", socket.Id)
				return
			}

			log.Infof("客户端 %s 请求任务 %s 的进度更新", socket.Id, taskID)
			sendTaskProgress(socket, taskID)
		})
	})

	// v4 版本不需要显式调用 Serve()
}

func registerGinRoutes() {
	// 添加CORS中间件
	ginRouter.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
		c.Header("Access-Control-Allow-Credentials", "true")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

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

			// 保存当前处理的handle到对应的task_id
			handleMutex.Lock()
			currentHandles[req.TaskID] = handle
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

	// Socket.IO进度推送
	ginRouter.GET("/socket.io/*any", gin.WrapH(ioServer.HttpHandler()))
	ginRouter.POST("/socket.io/*any", gin.WrapH(ioServer.HttpHandler()))
	ginRouter.OPTIONS("/socket.io/*any", gin.WrapH(ioServer.HttpHandler()))

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

// 发送特定任务的进度给单个客户端
func sendTaskProgress(socket *socketio.Socket, taskID string) {
	handleMutex.Lock()
	handle, exists := currentHandles[taskID]
	handleMutex.Unlock()

	var message map[string]interface{}
	if !exists || handle == nil {
		message = map[string]interface{}{
			"type":       "progress",
			"code":       200,
			"message":    "任务不存在或已完成",
			"task_id":    taskID,
			"completed":  0,
			"total":      0,
			"percentage": 0.0,
			"isRunning":  false,
		}
		log.Infof("[sendTaskProgress] 任务 %s 不存在，发送默认进度给客户端 %s", taskID, socket.Id)

	} else {
		completed, total, percentage := handle.GetProgress()
		isRunning := completed < total
		message = map[string]interface{}{
			"type":       "progress",
			"code":       200,
			"message":    "获取进度成功",
			"task_id":    taskID,
			"completed":  completed,
			"total":      total,
			"percentage": percentage,
			"isRunning":  isRunning,
		}
		log.Infof("[sendTaskProgress] 发送任务 %s 进度给客户端 %s: 已完成 %d / %d (%.2f%%) isRunning=%v",
			taskID, socket.Id, completed, total, percentage, isRunning)
	}

	log.Infof("[sendTaskProgress] 准备发送消息给客户端 %s: %+v", socket.Id, message)
	socket.Emit("progress", message)
}

// 向特定任务的房间广播进度更新
func BroadcastTaskProgress(taskID string) {
	handleMutex.Lock()
	handle, exists := currentHandles[taskID]
	handleMutex.Unlock()

	if !exists || handle == nil {
		log.Infof("[BroadcastTaskProgress] 任务 %s 不存在，跳过广播", taskID)
		return
	}

	completed, total, percentage := handle.GetProgress()
	isRunning := completed < total

	message := map[string]interface{}{
		"type":       "progress",
		"code":       200,
		"message":    "进度更新",
		"task_id":    taskID,
		"completed":  completed,
		"total":      total,
		"percentage": percentage,
		"isRunning":  isRunning,
	}

	roomName := "task_" + taskID
	log.Infof("[BroadcastTaskProgress] 向房间 %s 广播进度: %d/%d (%.2f%%)",
		roomName, completed, total, percentage)

	// 向房间广播消息
	ioServer.To(roomName).Emit("progress", message)
}

// 兼容旧的广播函数（已废弃）
func BroadcastProgress() {
	// 这个函数已被 BroadcastTaskProgress 替代
	log.Infof("[BroadcastProgress] 该函数已废弃，请使用 BroadcastTaskProgress")
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
