package myhttp

import (
	"net/http"

	"github.com/dimfeld/httptreemux"
	"github.com/gin-gonic/gin"
	"github.com/go-spatial/tegola/internal/log"
)

type ginHandler struct {
	engine *gin.Engine
}

var ginRouter *gin.Engine

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
			handle := NewHandle(req.Bounds, req.TaskID)
			handle.FetchTiles(req.TaskID, 14)
			c.JSON(http.StatusOK, gin.H{
				"message": "开始切片，请等待",
				"code":    200,
				"isSlice": req.IsSlice,
			})
		}
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
