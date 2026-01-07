package handler

import (
	"fgo-calc-backend/internal/config"
	"fgo-calc-backend/internal/repository"
	"fgo-calc-backend/internal/service"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

type Handler struct {
	repo    *repository.Repository
	service *service.CalculatorService
	cfg     *config.Config
}

func NewHandler(repo *repository.Repository, service *service.CalculatorService, cfg *config.Config) *Handler {
	return &Handler{repo: repo, service: service, cfg: cfg}
}

func (h *Handler) Register(r *gin.Engine) {
	r.NoRoute(func(c *gin.Context) {
		c.File("./static/index.html")
	})

	r.Static("/static", "./static")

	api := r.Group("/api")
	{
		api.GET("/data", h.GetData)
		api.POST("/filtertraits", h.FilterTraits)
		api.POST("/calculate", h.Calculate)
	}

	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"msg": "给我玩FGO"})
	})
}

func (h *Handler) GetData(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"servants":      h.repo.GetServants(),
		"craftEssences": h.repo.GetCraftEssences(),
		"traits":        h.repo.GetTraits(),
	})
}

func (h *Handler) FilterTraits(c *gin.Context) {
	traits := mapStr2Int(c.PostFormArray("traits"))
	results := h.service.FilterServants(traits, []int{}, []int{})
	c.JSON(http.StatusOK, results)
}

func (h *Handler) Calculate(c *gin.Context) {
	costLimit, _ := strconv.Atoi(c.PostForm("costlimit"))
	svtLimit, _ := strconv.Atoi(c.PostForm("svtlimit"))
	ceLimit, _ := strconv.Atoi(c.PostForm("celimit"))

	if ceLimit > h.cfg.MaxCeLimit {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("礼装数量不能超过%d个", h.cfg.MaxCeLimit)})
		return
	}
	if svtLimit > h.cfg.MaxSvtLimit {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("从者数量不能超过%d个", h.cfg.MaxSvtLimit)})
		return
	}
	if costLimit > h.cfg.MaxCost {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("总cost不能超过%d", h.cfg.MaxCost)})
		return
	}

	baseBond, _ := strconv.Atoi(c.PostForm("basebond"))
	supportLimit, _ := strconv.Atoi(c.PostForm("supportlimit"))
	if supportLimit < 0 {
		supportLimit = 0
	}
	if supportLimit > 2 {
		supportLimit = 2
	}

	server := c.PostForm("server")
	if server == "" {
		server = "CN"
	}
	enableEventBonus := c.PostForm("enable_event_bonus") == "true"
	selectedEvents := mapStr2Int(c.PostFormArray("selected_events"))

	results, duration := h.service.Optimize(
		costLimit,
		svtLimit,
		ceLimit,
		supportLimit,
		mapStr2Int(c.PostFormArray("allowtraits")),
		mapStr2Int(c.PostFormArray("includesvt")),
		c.PostFormArray("includesvtdiff"),
		mapStr2Int(c.PostFormArray("excludesvt")),
		mapStr2Int(c.PostFormArray("includece")),
		mapStr2Int(c.PostFormArray("excludece")),
		baseBond,
		server,
		enableEventBonus,
		selectedEvents,
	)

	c.JSON(http.StatusOK, gin.H{
		"teams":    results,
		"duration": duration.Milliseconds(),
	})
}

func mapStr2Int(data []string) []int {
	result := []int{}
	for _, d := range data {
		if v, err := strconv.Atoi(d); err == nil {
			result = append(result, v)
		}
	}
	return result
}

