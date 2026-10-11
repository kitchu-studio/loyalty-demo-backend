package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kitchu-studio/loyalty-demo-backend/internal/loyalty"
	"github.com/kitchu-studio/loyalty-demo-backend/internal/store"
)

const (
	earnCodeTTL      = 24 * time.Hour
	earnCodeLength   = 6
	voucherCodeTries = 5
	maxBillBaht      = 100000
	maxAboutRunes    = 2000
)

func parseID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		abort(c, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return id, true
}

func (s *Server) internalError(c *gin.Context, what string, err error) {
	slog.Error(what, "error", err)
	abort(c, http.StatusInternalServerError, "internal error")
}

// GET /api/v1/shop
func (s *Server) shopInfo(c *gin.Context) {
	about, err := s.store.GetSetting(c.Request.Context(), "about")
	if err != nil {
		s.internalError(c, "get about", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"name":           s.shopName,
		"about":          about,
		"baht_per_point": s.bahtPerPoint,
	})
}

// GET /api/v1/rewards
func (s *Server) listRewards(c *gin.Context) {
	rewards, err := s.store.ListRewards(c.Request.Context())
	if err != nil {
		s.internalError(c, "list rewards", err)
		return
	}
	c.JSON(http.StatusOK, rewards)
}

type meView struct {
	store.User
	Points int `json:"points"`
}

// GET /api/v1/me
func (s *Server) me(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetInt64(ctxUserID)
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		abort(c, http.StatusNotFound, "user not found")
		return
	}
	points, err := s.store.Balance(ctx, userID)
	if err != nil {
		s.internalError(c, "balance", err)
		return
	}
	c.JSON(http.StatusOK, meView{User: *user, Points: points})
}

// GET /api/v1/points/history
func (s *Server) pointsHistory(c *gin.Context) {
	entries, err := s.store.ListLedger(c.Request.Context(), c.GetInt64(ctxUserID), 50)
	if err != nil {
		s.internalError(c, "list ledger", err)
		return
	}
	c.JSON(http.StatusOK, entries)
}

type claimRequest struct {
	Code string `json:"code" binding:"required,max=32"`
}

// POST /api/v1/points/claim  -- the customer types the code the staff gave them.
func (s *Server) claimPoints(c *gin.Context) {
	var req claimRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "code is required")
		return
	}
	res, err := s.store.ClaimEarnCode(c.Request.Context(), c.GetInt64(ctxUserID), loyalty.NormalizeCode(req.Code))
	switch {
	case errors.Is(err, store.ErrCodeNotFound):
		abort(c, http.StatusNotFound, "code not found, please check and try again")
		return
	case errors.Is(err, store.ErrCodeUsed):
		abort(c, http.StatusConflict, "this code has already been used")
		return
	case errors.Is(err, store.ErrCodeExpired):
		abort(c, http.StatusGone, "this code has expired, ask the staff for a new one")
		return
	case err != nil:
		s.internalError(c, "claim code", err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// POST /api/v1/rewards/:id/redeem
func (s *Server) redeemReward(c *gin.Context) {
	rewardID, ok := parseID(c, "id")
	if !ok {
		return
	}
	userID := c.GetInt64(ctxUserID)

	for attempt := 0; attempt < voucherCodeTries; attempt++ {
		code, err := loyalty.NewCode(earnCodeLength)
		if err != nil {
			s.internalError(c, "new voucher code", err)
			return
		}
		red, balance, err := s.store.Redeem(c.Request.Context(), userID, rewardID, "V"+code)
		switch {
		case errors.Is(err, store.ErrCodeTaken):
			continue // extremely unlikely collision, try another code
		case errors.Is(err, store.ErrRewardNotFound):
			abort(c, http.StatusNotFound, "reward not found")
			return
		case errors.Is(err, store.ErrNotEnoughPoints):
			abort(c, http.StatusConflict, "not enough points")
			return
		case err != nil:
			s.internalError(c, "redeem", err)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"redemption": red, "balance": balance})
		return
	}
	s.internalError(c, "redeem", errors.New("could not generate a unique voucher code"))
}

// GET /api/v1/redemptions
func (s *Server) listMyRedemptions(c *gin.Context) {
	list, err := s.store.ListRedemptions(c.Request.Context(), c.GetInt64(ctxUserID))
	if err != nil {
		s.internalError(c, "list redemptions", err)
		return
	}
	c.JSON(http.StatusOK, list)
}

// ---- staff ----

type earnCodeRequest struct {
	AmountBaht int `json:"amount_baht" binding:"required,min=1,max=100000"`
}

// POST /api/v1/admin/earn-codes  -- staff types the bill total and gets a code for the customer.
func (s *Server) adminCreateEarnCode(c *gin.Context) {
	var req earnCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "amount_baht must be between 1 and "+strconv.Itoa(maxBillBaht))
		return
	}
	points := loyalty.PointsFor(req.AmountBaht, s.bahtPerPoint)
	if points < 1 {
		abort(c, http.StatusBadRequest, "bill is too small to earn a point (minimum "+strconv.Itoa(s.bahtPerPoint)+" baht)")
		return
	}

	staffID := c.GetInt64(ctxUserID)
	for attempt := 0; attempt < voucherCodeTries; attempt++ {
		code, err := loyalty.NewCode(earnCodeLength)
		if err != nil {
			s.internalError(c, "new earn code", err)
			return
		}
		ec, err := s.store.CreateEarnCode(c.Request.Context(), staffID, code, req.AmountBaht, points, time.Now().Add(earnCodeTTL))
		if errors.Is(err, store.ErrCodeTaken) {
			continue
		}
		if err != nil {
			s.internalError(c, "create earn code", err)
			return
		}
		c.JSON(http.StatusCreated, ec)
		return
	}
	s.internalError(c, "create earn code", errors.New("could not generate a unique code"))
}

// GET /api/v1/admin/summary
func (s *Server) adminSummary(c *gin.Context) {
	sum, err := s.store.Summary(c.Request.Context())
	if err != nil {
		s.internalError(c, "summary", err)
		return
	}
	c.JSON(http.StatusOK, sum)
}

// GET /api/v1/admin/redemptions/:code
func (s *Server) adminLookupRedemption(c *gin.Context) {
	red, err := s.store.GetRedemptionByCode(c.Request.Context(), loyalty.NormalizeCode(c.Param("code")))
	if errors.Is(err, store.ErrNotFound) {
		abort(c, http.StatusNotFound, "voucher not found")
		return
	}
	if err != nil {
		s.internalError(c, "lookup voucher", err)
		return
	}
	c.JSON(http.StatusOK, red)
}

// POST /api/v1/admin/redemptions/:code/use
func (s *Server) adminUseRedemption(c *gin.Context) {
	red, err := s.store.UseRedemption(c.Request.Context(), loyalty.NormalizeCode(c.Param("code")))
	switch {
	case errors.Is(err, store.ErrNotFound):
		abort(c, http.StatusNotFound, "voucher not found")
		return
	case errors.Is(err, store.ErrAlreadyUsed):
		abort(c, http.StatusConflict, "this voucher has already been used")
		return
	case err != nil:
		s.internalError(c, "use voucher", err)
		return
	}
	c.JSON(http.StatusOK, red)
}

type aboutRequest struct {
	About string `json:"about"`
}

// PUT /api/v1/admin/shop-about  -- the owner edits what the assistant knows about the shop.
func (s *Server) adminSetAbout(c *gin.Context) {
	var req aboutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "about is required")
		return
	}
	about := strings.TrimSpace(req.About)
	if len([]rune(about)) > maxAboutRunes {
		abort(c, http.StatusBadRequest, "about is too long (max "+strconv.Itoa(maxAboutRunes)+" characters)")
		return
	}
	if err := s.store.SetSetting(c.Request.Context(), "about", about); err != nil {
		s.internalError(c, "set about", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"about": about})
}
