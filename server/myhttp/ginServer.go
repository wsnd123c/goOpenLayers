package myhttp

import (
	"net/http"
	"sync"

	"github.com/dimfeld/httptreemux"
	"github.com/gin-gonic/gin"
	"github.com/go-spatial/tegola/internal/log"
	socketio "github.com/googollee/go-socket.io"
	"github.com/googollee/go-socket.io/engineio"
	"github.com/googollee/go-socket.io/engineio/transport"
	"github.com/googollee/go-socket.io/engineio/transport/polling"
	"github.com/googollee/go-socket.io/engineio/transport/websocket"
)

type ginHandler struct {
	engine *gin.Engine
}

var ginRouter *gin.Engine
var currentHandle *PostHandle
var handleMutex sync.Mutex
var socketServer *socketio.Server
var socketClients = make(map[socketio.Conn]bool)
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
	// 配置Socket.IO服务器选项
	opts := &engineio.Options{
		Transports: []transport.Transport{
			&polling.Transport{
				CheckOrigin: func(r *http.Request) bool {
					return true // 允许跨域
				},
			},
			&websocket.Transport{
				CheckOrigin: func(r *http.Request) bool {
					return true // 允许跨域
				},
			},
		},
	}

	socketServer = socketio.NewServer(opts)

	// 监听连接事件
	socketServer.OnConnect("/", func(s socketio.Conn) error {
		log.Info("新的Socket.IO客户端连接: ", s.ID())

		// 注册客户端
		socketClientsMutex.Lock()
		socketClients[s] = true
		socketClientsMutex.Unlock()

		// 发送当前进度
		sendCurrentProgress(s)

		return nil
	})

	// 监听断开连接事件
	socketServer.OnDisconnect("/", func(s socketio.Conn, reason string) {
		log.Infof("Socket.IO客户端断开连接: %s, 原因: %s", s.ID(), reason)

		// 移除客户端
		socketClientsMutex.Lock()
		delete(socketClients, s)
		socketClientsMutex.Unlock()
	})

	// 监听进度请求事件
	socketServer.OnEvent("/", "get_progress", func(s socketio.Conn, msg string) {
		log.Info("客户端请求进度更新: ", s.ID())
		sendCurrentProgress(s)
	})

	go func() {
		if err := socketServer.Serve(); err != nil {
			log.Errorf("Socket.IO服务器启动失败: %v", err)
		}
	}()
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

	// Socket.IO进度推送
	ginRouter.GET("/socket.io/*any", gin.WrapH(socketServer))
	ginRouter.POST("/socket.io/*any", gin.WrapH(socketServer))

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
func sendCurrentProgress(conn socketio.Conn) {
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

	conn.Emit("progress", message)
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

	socketClientsMutex.Lock()
	defer socketClientsMutex.Unlock()

	for conn := range socketClients {
		conn.Emit("progress", message)
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
