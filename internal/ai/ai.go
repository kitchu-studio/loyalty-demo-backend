// Package ai answers customer questions for a small shop. It calls the Claude
// Messages API when an API key is configured and otherwise falls back to simple
// built-in answers, so the demo still works with no key and no cost.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://api.anthropic.com"
	DefaultModel   = "claude-haiku-5-5"
	apiVersion     = "2023-06-01"
	maxReplyTokens = 400
)

var ErrNoAPIKey = errors.New("no API key configured")

// Message is one turn of the conversation. Role is "user" or "assistant".
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

// NewClient returns a client; an empty apiKey gives a client that reports Enabled() == false.
func NewClient(apiKey, model, baseURL string) *Client {
	if model == "" {
		model = DefaultModel
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		apiKey:  apiKey,
		model:   model,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Enabled() bool { return c.apiKey != "" }

type request struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system"`
	Messages  []Message `json:"messages"`
}

type response struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Reply sends the conversation to Claude and returns the assistant's text.
// The conversation must start with a user message and alternate roles.
func (c *Client) Reply(ctx context.Context, system string, msgs []Message) (string, error) {
	if !c.Enabled() {
		return "", ErrNoAPIKey
	}
	if len(msgs) == 0 || msgs[len(msgs)-1].Role != "user" {
		return "", errors.New("last message must be from the user")
	}

	body, err := json.Marshal(request{
		Model:     c.model,
		MaxTokens: maxReplyTokens,
		System:    system,
		Messages:  msgs,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	res, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var out response
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("claude api: status %d, unreadable body", res.StatusCode)
	}
	if res.StatusCode != http.StatusOK {
		msg := "unknown error"
		if out.Error != nil {
			msg = out.Error.Message
		}
		return "", fmt.Errorf("claude api: status %d: %s", res.StatusCode, msg)
	}

	var text strings.Builder
	for _, part := range out.Content {
		if part.Type == "text" {
			text.WriteString(part.Text)
		}
	}
	reply := strings.TrimSpace(text.String())
	if reply == "" {
		return "", errors.New("claude api: empty reply")
	}
	return reply, nil
}

// Reward is the part of a reward the assistant needs to know about.
type Reward struct {
	Name       string
	CostPoints int
}

// ShopContext is everything the assistant is allowed to know. It is rebuilt for
// every question so answers always reflect the shop's current information and
// the asking customer's current points.
type ShopContext struct {
	ShopName     string
	About        string // free text the shop owner maintains: hours, address, menu, promotions
	CustomerName string
	Points       int
	BahtPerPoint int
	Rewards      []Reward
}

// SystemPrompt builds the instructions for Claude from the shop context.
func SystemPrompt(sc ShopContext) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are the friendly assistant of the shop \"%s\", chatting with a customer inside the shop's loyalty app.\n", sc.ShopName)
	b.WriteString("Reply in the same language the customer uses (usually Thai), in a warm and short way: at most 3 sentences, plain text without markdown.\n")
	b.WriteString("Answer only from the shop information below. If the answer is not there, say you are not sure and suggest asking the staff at the counter. Never invent prices, hours or promotions.\n")
	b.WriteString("You cannot change points or redeem rewards yourself; tell the customer to use the Rewards tab of the app.\n\n")
	b.WriteString("Shop information (maintained by the owner):\n")
	if strings.TrimSpace(sc.About) == "" {
		b.WriteString("(none yet)\n")
	} else {
		b.WriteString(strings.TrimSpace(sc.About) + "\n")
	}
	fmt.Fprintf(&b, "\nPoints program: customers earn 1 point for every %d baht spent and get a code from the staff after paying.\n", sc.BahtPerPoint)
	b.WriteString("Rewards:\n")
	for _, r := range sc.Rewards {
		fmt.Fprintf(&b, "- %s: %d points\n", r.Name, r.CostPoints)
	}
	if sc.CustomerName != "" {
		fmt.Fprintf(&b, "\nThe customer is %s and currently has %d points.\n", sc.CustomerName, sc.Points)
	} else {
		fmt.Fprintf(&b, "\nThe customer currently has %d points.\n", sc.Points)
	}
	return b.String()
}

// Fallback answers the most common questions without calling any API. It is a
// small keyword matcher, good enough to demo the screen when no key is set.
func Fallback(sc ShopContext, question string) string {
	q := strings.ToLower(question)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(q, w) {
				return true
			}
		}
		return false
	}

	switch {
	case has("วิธีสะสม", "สะสม", "ได้แต้ม", "earn"):
		return fmt.Sprintf("ทุก %d บาทได้ 1 แต้ม หลังจ่ายเงินให้พนักงานออกโค้ด แล้วกรอกโค้ดในแอปได้เลยค่ะ", sc.BahtPerPoint)
	case has("แต้ม", "point", "คะแนน"):
		next := nextReward(sc)
		if next == nil {
			return fmt.Sprintf("ตอนนี้คุณมี %d แต้มค่ะ", sc.Points)
		}
		if sc.Points >= next.CostPoints {
			return fmt.Sprintf("ตอนนี้คุณมี %d แต้ม แลก \"%s\" ได้เลยที่แท็บของรางวัลค่ะ", sc.Points, next.Name)
		}
		return fmt.Sprintf("ตอนนี้คุณมี %d แต้ม อีก %d แต้มจะแลก \"%s\" ได้ค่ะ", sc.Points, next.CostPoints-sc.Points, next.Name)
	case has("แลก", "ของรางวัล", "reward", "redeem"):
		if len(sc.Rewards) == 0 {
			return "ตอนนี้ยังไม่มีของรางวัลให้แลกค่ะ"
		}
		parts := make([]string, 0, len(sc.Rewards))
		for _, r := range sc.Rewards {
			parts = append(parts, fmt.Sprintf("%s (%d แต้ม)", r.Name, r.CostPoints))
		}
		return "แลกได้ตอนนี้: " + strings.Join(parts, ", ") + " ค่ะ"
	case has("เปิด", "ปิด", "กี่โมง", "เวลา", "hour", "open"):
		if line := findLine(sc.About, "เปิด", "เวลา", "hour", "open"); line != "" {
			return line
		}
	case has("ที่อยู่", "อยู่ที่ไหน", "แผนที่", "address", "where"):
		if line := findLine(sc.About, "ที่อยู่", "address", "ถนน", "ซอย"); line != "" {
			return line
		}
	}
	if line := firstMatchingLine(sc.About, q); line != "" {
		return line
	}
	return "ขอโทษค่ะ ยังไม่แน่ใจเรื่องนี้ ลองถามพนักงานที่หน้าร้านได้เลยนะคะ (ตอนนี้เป็นโหมดตัวอย่าง ยังไม่ได้เชื่อมต่อ AI)"
}

// nextReward picks the reward worth mentioning: the priciest one the customer can
// already afford, or if none, the cheapest one they are working towards.
func nextReward(sc ShopContext) *Reward {
	var affordable, target *Reward
	for i := range sc.Rewards {
		r := &sc.Rewards[i]
		if r.CostPoints <= sc.Points {
			if affordable == nil || r.CostPoints > affordable.CostPoints {
				affordable = r
			}
		} else if target == nil || r.CostPoints < target.CostPoints {
			target = r
		}
	}
	if affordable != nil {
		return affordable
	}
	return target
}

func findLine(about string, keys ...string) string {
	for _, line := range strings.Split(about, "\n") {
		l := strings.ToLower(line)
		for _, k := range keys {
			if strings.Contains(l, k) {
				return strings.TrimSpace(line)
			}
		}
	}
	return ""
}

// firstMatchingLine looks for a line of the shop info that shares a run of at least
// 4 characters with the question. Thai has no spaces between words, so comparing
// fixed-size windows works better than splitting on whitespace.
func firstMatchingLine(about, question string) string {
	const window = 4
	q := []rune(strings.ReplaceAll(question, " ", ""))
	for _, line := range strings.Split(about, "\n") {
		l := strings.ToLower(line)
		for i := 0; i+window <= len(q); i++ {
			if strings.Contains(l, string(q[i:i+window])) {
				return strings.TrimSpace(line)
			}
		}
	}
	return ""
}

const (
	maxTurns         = 10
	MaxUserRunes     = 500
	maxAssistantRune = 1500
)

// CleanConversation prepares what the app sent for the Claude API: it keeps the last
// few turns, rejects bad roles, empty or over-long user messages, drops anything
// before the first user message, merges repeated roles (Claude requires alternating
// turns) and checks the conversation ends with the user.
func CleanConversation(in []Message) ([]Message, bool) {
	if len(in) > maxTurns {
		in = in[len(in)-maxTurns:]
	}
	out := make([]Message, 0, len(in))
	for _, m := range in {
		content := strings.TrimSpace(m.Content)
		if (m.Role != "user" && m.Role != "assistant") || content == "" {
			return nil, false
		}
		runes := []rune(content)
		switch {
		case m.Role == "user" && len(runes) > MaxUserRunes:
			return nil, false
		case m.Role == "assistant" && len(runes) > maxAssistantRune:
			content = string(runes[:maxAssistantRune])
		}
		if len(out) == 0 && m.Role != "user" {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Role == m.Role {
			out[n-1].Content += "\n" + content
			continue
		}
		out = append(out, Message{Role: m.Role, Content: content})
	}
	if len(out) == 0 || out[len(out)-1].Role != "user" {
		return nil, false
	}
	return out, true
}
