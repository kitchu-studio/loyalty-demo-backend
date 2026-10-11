-- Every change to a customer's points is a row here; the balance is the sum.
-- Keeping a ledger instead of a balance column means history is always explainable.
CREATE TABLE point_ledger (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    delta      INT         NOT NULL CHECK (delta <> 0),
    reason     TEXT        NOT NULL CHECK (reason IN ('earn', 'redeem')),
    note       TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX point_ledger_user_idx ON point_ledger (user_id, id DESC);

-- One-time codes the staff hand out after a customer pays.
CREATE TABLE earn_codes (
    code        TEXT        PRIMARY KEY,
    amount_baht INT         NOT NULL CHECK (amount_baht > 0),
    points      INT         NOT NULL CHECK (points > 0),
    created_by  BIGINT      NOT NULL REFERENCES users (id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL,
    claimed_by  BIGINT      REFERENCES users (id),
    claimed_at  TIMESTAMPTZ
);

CREATE TABLE rewards (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    cost_points INT     NOT NULL CHECK (cost_points > 0),
    active      BOOLEAN NOT NULL DEFAULT true,
    sort_order  INT     NOT NULL DEFAULT 0
);

-- Name and cost are copied at redeem time so later menu changes do not rewrite history.
CREATE TABLE redemptions (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    reward_id   BIGINT      NOT NULL REFERENCES rewards (id),
    reward_name TEXT        NOT NULL,
    cost_points INT         NOT NULL,
    code        TEXT        NOT NULL UNIQUE,
    status      TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'used')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    used_at     TIMESTAMPTZ
);
CREATE INDEX redemptions_user_idx ON redemptions (user_id, id DESC);

-- Free-form shop information the owner edits in the app; the AI assistant answers from it.
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
