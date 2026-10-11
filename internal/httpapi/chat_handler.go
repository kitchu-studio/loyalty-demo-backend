package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kitchu-studio/loyalty-demo-backend/internal/ai"
)

const (
	chatTimeout      = 25 * time.Second
	modeClaude       = "claude"
	modeDemo         = "demo"
	modeDemoFallback = "demo_fallback"
)

type chatRequest struct {
	Messages []ai.Message `json:"messages" binding:"required,min=1,max=40"`
}

// POST /api/v1/chat  -- the app sends the recent conversation; the server adds the
// shop's information and this customer's points, so the client cannot tamper with them.
func (s *Server) chat(c *gin.Context) {
	var req chatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "messages is required")
		return
	}
	msgs, ok := ai.CleanConversation(req.Messages)
	if !ok {
		abort(c, http.StatusBadRequest, "conversation must end with a user message of at most 500 characters")
		return
	}

	ctx := c.Request.Context()
	userID := c.GetInt64(ctxUserID)
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		abort(c, http.StatusUnauthorized, "user not found")
		return
	}
	points, err := s.store.Balance(ctx, userID)
	if err != nil {
		s.internalError(c, "balance", err)
		return
	}
	about, err := s.store.GetSetting(ctx, "about")
	if err != nil {
		s.internalError(c, "get about", err)
		return
	}
	rewards, err := s.store.ListRewards(ctx)
	if err != nil {
		s.internalError(c, "list rewards", err)
		return
	}

	sc := ai.ShopContext{
		ShopName:     s.shopName,
		About:        about,
		CustomerName: user.Name,
		Points:       points,
		BahtPerPoint: s.bahtPerPoint,
	}
	for _, r := range rewards {
		sc.Rewards = append(sc.Rewards, ai.Reward{Name: r.Name, CostPoints: r.CostPoints})
	}

	if s.assistant.Enabled() {
		callCtx, cancel := context.WithTimeout(ctx, chatTimeout)
		defer cancel()
		reply, err := s.assistant.Reply(callCtx, ai.SystemPrompt(sc), msgs)
		if err == nil {
			c.JSON(http.StatusOK, gin.H{"reply": reply, "mode": modeClaude})
			return
		}
		// Do not fail the screen because the AI provider had a bad moment.
		slog.Error("assistant call failed, using fallback", "error", err)
		c.JSON(http.StatusOK, gin.H{
			"reply": ai.Fallback(sc, msgs[len(msgs)-1].Content),
			"mode":  modeDemoFallback,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"reply": ai.Fallback(sc, msgs[len(msgs)-1].Content),
		"mode":  modeDemo,
	})
}
