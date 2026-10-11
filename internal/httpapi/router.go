package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kitchu-studio/loyalty-demo-backend/internal/ai"
	"github.com/kitchu-studio/loyalty-demo-backend/internal/auth"
	"github.com/kitchu-studio/loyalty-demo-backend/internal/store"
)

type Server struct {
	store        *store.Store
	tokens       *auth.TokenManager
	assistant    *ai.Client
	limiter      *limiter
	shopName     string
	bahtPerPoint int
	adminEmails  []string
}

type Options struct {
	AppEnv       string
	ShopName     string
	BahtPerPoint int
	AdminEmails  []string
	Assistant    *ai.Client
}

func NewRouter(st *store.Store, tokens *auth.TokenManager, opt Options) *gin.Engine {
	if opt.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	s := &Server{
		store:        st,
		tokens:       tokens,
		assistant:    opt.Assistant,
		limiter:      newLimiter(),
		shopName:     opt.ShopName,
		bahtPerPoint: opt.BahtPerPoint,
		adminEmails:  opt.AdminEmails,
	}

	r := gin.New()
	r.Use(gin.Recovery(), requestLogger())

	r.GET("/healthz", s.health)

	v1 := r.Group("/api/v1")
	{
		a := v1.Group("/auth")
		a.POST("/register", s.register)
		a.POST("/login", s.login)
		a.POST("/refresh", s.refresh)

		// Public: anyone can see the shop and what the points can buy.
		v1.GET("/shop", s.shopInfo)
		v1.GET("/rewards", s.listRewards)

		authed := v1.Group("")
		authed.Use(s.requireAuth())
		authed.GET("/me", s.me)
		authed.GET("/points/history", s.pointsHistory)
		authed.POST("/points/claim", s.limit("claim", 10, time.Minute), s.claimPoints)
		authed.POST("/rewards/:id/redeem", s.redeemReward)
		authed.GET("/redemptions", s.listMyRedemptions)
		authed.POST("/chat", s.limit("chat", 12, time.Minute), s.chat)

		admin := authed.Group("/admin")
		admin.Use(s.requireAdmin())
		admin.GET("/summary", s.adminSummary)
		admin.POST("/earn-codes", s.adminCreateEarnCode)
		admin.GET("/redemptions/:code", s.adminLookupRedemption)
		admin.POST("/redemptions/:code/use", s.adminUseRedemption)
		admin.PUT("/shop-about", s.adminSetAbout)
	}
	return r
}

func (s *Server) health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
