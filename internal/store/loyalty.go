package store

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrCodeNotFound    = errors.New("code not found")
	ErrCodeUsed        = errors.New("code already used")
	ErrCodeExpired     = errors.New("code expired")
	ErrCodeTaken       = errors.New("code already exists")
	ErrNotEnoughPoints = errors.New("not enough points")
	ErrRewardNotFound  = errors.New("reward not found")
	ErrAlreadyUsed     = errors.New("voucher already used")
)

type LedgerEntry struct {
	ID        int64     `json:"id"`
	Delta     int       `json:"delta"`
	Reason    string    `json:"reason"` // "earn" or "redeem"
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

type EarnCode struct {
	Code       string    `json:"code"`
	AmountBaht int       `json:"amount_baht"`
	Points     int       `json:"points"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type Reward struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CostPoints  int    `json:"cost_points"`
}

type Redemption struct {
	ID           int64      `json:"id"`
	RewardName   string     `json:"reward_name"`
	CostPoints   int        `json:"cost_points"`
	Code         string     `json:"code"`
	Status       string     `json:"status"` // "active" or "used"
	CreatedAt    time.Time  `json:"created_at"`
	UsedAt       *time.Time `json:"used_at"`
	CustomerName string     `json:"customer_name,omitempty"` // only filled for staff lookups
}

type ClaimResult struct {
	Points  int `json:"points"`
	Balance int `json:"balance"`
}

type Summary struct {
	Members        int `json:"members"`
	PointsIssued   int `json:"points_issued"`
	ActiveVouchers int `json:"active_vouchers"`
	UsedVouchers   int `json:"used_vouchers"`
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

func (s *Store) Balance(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta), 0)::int FROM point_ledger WHERE user_id = $1`, userID).Scan(&n)
	return n, err
}

func (s *Store) ListLedger(ctx context.Context, userID int64, limit int) ([]LedgerEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, delta, reason, note, created_at
		FROM point_ledger WHERE user_id = $1 ORDER BY id DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LedgerEntry{}
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.ID, &e.Delta, &e.Reason, &e.Note, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CreateEarnCode stores a code the staff will hand to the customer. A duplicate code
// returns ErrCodeTaken so the caller can try another one.
func (s *Store) CreateEarnCode(ctx context.Context, staffID int64, code string, amountBaht, points int, expiresAt time.Time) (*EarnCode, error) {
	var c EarnCode
	err := s.pool.QueryRow(ctx, `
		INSERT INTO earn_codes (code, amount_baht, points, created_by, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING code, amount_baht, points, expires_at`,
		code, amountBaht, points, staffID, expiresAt).
		Scan(&c.Code, &c.AmountBaht, &c.Points, &c.ExpiresAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrCodeTaken
		}
		return nil, err
	}
	return &c, nil
}

// ClaimEarnCode turns a staff code into points for the customer, once.
func (s *Store) ClaimEarnCode(ctx context.Context, userID int64, code string) (*ClaimResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	var (
		amount, points int
		expiresAt      time.Time
		claimedBy      *int64
	)
	err = tx.QueryRow(ctx, `
		SELECT amount_baht, points, expires_at, claimed_by
		FROM earn_codes WHERE code = $1 FOR UPDATE`, code).
		Scan(&amount, &points, &expiresAt, &claimedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCodeNotFound
	}
	if err != nil {
		return nil, err
	}
	if claimedBy != nil {
		return nil, ErrCodeUsed
	}
	if time.Now().After(expiresAt) {
		return nil, ErrCodeExpired
	}

	if _, err := tx.Exec(ctx,
		`UPDATE earn_codes SET claimed_by = $2, claimed_at = now() WHERE code = $1`, code, userID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO point_ledger (user_id, delta, reason, note)
		VALUES ($1, $2, 'earn', $3)`, userID, points, "ยอดซื้อ "+itoa(amount)+" บาท"); err != nil {
		return nil, err
	}
	var balance int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta), 0)::int FROM point_ledger WHERE user_id = $1`, userID).Scan(&balance); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &ClaimResult{Points: points, Balance: balance}, nil
}

func (s *Store) ListRewards(ctx context.Context) ([]Reward, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, description, cost_points
		FROM rewards WHERE active ORDER BY sort_order, cost_points, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Reward{}
	for rows.Next() {
		var r Reward
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.CostPoints); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Redeem spends points on a reward and issues a voucher code the customer shows at the counter.
// It returns the new balance. A duplicate voucher code returns ErrCodeTaken.
func (s *Store) Redeem(ctx context.Context, userID, rewardID int64, voucherCode string) (*Redemption, int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	// Lock the customer's row so two simultaneous redemptions cannot spend the same points.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, userID); err != nil {
		return nil, 0, err
	}

	var (
		name string
		cost int
	)
	err = tx.QueryRow(ctx, `SELECT name, cost_points FROM rewards WHERE id = $1 AND active`, rewardID).Scan(&name, &cost)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, ErrRewardNotFound
	}
	if err != nil {
		return nil, 0, err
	}

	var balance int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta), 0)::int FROM point_ledger WHERE user_id = $1`, userID).Scan(&balance); err != nil {
		return nil, 0, err
	}
	if balance < cost {
		return nil, 0, ErrNotEnoughPoints
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO point_ledger (user_id, delta, reason, note)
		VALUES ($1, $2, 'redeem', $3)`, userID, -cost, "แลก "+name); err != nil {
		return nil, 0, err
	}

	r := Redemption{RewardName: name, CostPoints: cost, Code: voucherCode, Status: "active"}
	err = tx.QueryRow(ctx, `
		INSERT INTO redemptions (user_id, reward_id, reward_name, cost_points, code)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`, userID, rewardID, name, cost, voucherCode).Scan(&r.ID, &r.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, 0, ErrCodeTaken
		}
		return nil, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return &r, balance - cost, nil
}

func (s *Store) ListRedemptions(ctx context.Context, userID int64) ([]Redemption, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, reward_name, cost_points, code, status, created_at, used_at
		FROM redemptions WHERE user_id = $1 ORDER BY id DESC LIMIT 50`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Redemption{}
	for rows.Next() {
		var r Redemption
		if err := rows.Scan(&r.ID, &r.RewardName, &r.CostPoints, &r.Code, &r.Status, &r.CreatedAt, &r.UsedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRedemptionByCode lets staff see who a voucher belongs to before they hand over the reward.
func (s *Store) GetRedemptionByCode(ctx context.Context, code string) (*Redemption, error) {
	var r Redemption
	err := s.pool.QueryRow(ctx, `
		SELECT r.id, r.reward_name, r.cost_points, r.code, r.status, r.created_at, r.used_at,
		       COALESCE(NULLIF(u.name, ''), u.email)
		FROM redemptions r JOIN users u ON u.id = r.user_id
		WHERE r.code = $1`, code).
		Scan(&r.ID, &r.RewardName, &r.CostPoints, &r.Code, &r.Status, &r.CreatedAt, &r.UsedAt, &r.CustomerName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// UseRedemption marks a voucher as handed over. Using it twice returns ErrAlreadyUsed.
func (s *Store) UseRedemption(ctx context.Context, code string) (*Redemption, error) {
	var r Redemption
	err := s.pool.QueryRow(ctx, `
		WITH upd AS (
			UPDATE redemptions SET status = 'used', used_at = now()
			WHERE code = $1 AND status = 'active'
			RETURNING id, user_id, reward_name, cost_points, code, status, created_at, used_at
		)
		SELECT upd.id, upd.reward_name, upd.cost_points, upd.code, upd.status, upd.created_at, upd.used_at,
		       COALESCE(NULLIF(u.name, ''), u.email)
		FROM upd JOIN users u ON u.id = upd.user_id`, code).
		Scan(&r.ID, &r.RewardName, &r.CostPoints, &r.Code, &r.Status, &r.CreatedAt, &r.UsedAt, &r.CustomerName)
	if err == nil {
		return &r, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	// Nothing was updated: either the code does not exist or it was already used.
	if _, err := s.GetRedemptionByCode(ctx, code); err != nil {
		return nil, err
	}
	return nil, ErrAlreadyUsed
}

func (s *Store) Summary(ctx context.Context) (*Summary, error) {
	var m Summary
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM users WHERE NOT is_admin)::int,
			(SELECT COALESCE(SUM(delta), 0) FROM point_ledger WHERE delta > 0)::int,
			(SELECT count(*) FROM redemptions WHERE status = 'active')::int,
			(SELECT count(*) FROM redemptions WHERE status = 'used')::int`).
		Scan(&m.Members, &m.PointsIssued, &m.ActiveVouchers, &m.UsedVouchers)
	return &m, err
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, key, value)
	return err
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
