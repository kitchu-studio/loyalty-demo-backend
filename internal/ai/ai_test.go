package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testContext() ShopContext {
	return ShopContext{
		ShopName:     "ร้านทดสอบ",
		About:        "เปิดทุกวัน 08:00-17:00\nที่อยู่ 12 ซอยสุขใจ\nมีที่จอดรถหน้าร้าน",
		CustomerName: "แนน",
		Points:       12,
		BahtPerPoint: 10,
		Rewards: []Reward{
			{Name: "เอสเปรสโซ่ฟรี", CostPoints: 30},
			{Name: "ส่วนลด 20 บาท", CostPoints: 10},
		},
	}
}

func TestReplySendsExpectedRequest(t *testing.T) {
	var gotKey, gotVersion string
	var gotBody request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"  สวัสดีค่ะ  "}]}`))
	}))
	defer srv.Close()

	c := NewClient("sk-test", "", srv.URL)
	got, err := c.Reply(context.Background(), "system text", []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "สวัสดีค่ะ" {
		t.Errorf("reply = %q", got)
	}
	if gotKey != "sk-test" || gotVersion != apiVersion {
		t.Errorf("headers: key=%q version=%q", gotKey, gotVersion)
	}
	if gotBody.Model != DefaultModel || gotBody.System != "system text" || gotBody.MaxTokens != maxReplyTokens {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestReplyErrors(t *testing.T) {
	if _, err := NewClient("", "", "").Reply(context.Background(), "s", []Message{{Role: "user", Content: "x"}}); err != ErrNoAPIKey {
		t.Errorf("want ErrNoAPIKey, got %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	}))
	defer srv.Close()
	c := NewClient("k", "m", srv.URL)
	_, err := c.Reply(context.Background(), "s", []Message{{Role: "user", Content: "x"}})
	if err == nil || !strings.Contains(err.Error(), "slow down") {
		t.Errorf("want API error message, got %v", err)
	}

	if _, err := c.Reply(context.Background(), "s", []Message{{Role: "assistant", Content: "x"}}); err == nil {
		t.Error("expected error when the last message is not from the user")
	}
}

func TestSystemPromptContainsFacts(t *testing.T) {
	p := SystemPrompt(testContext())
	for _, want := range []string{"ร้านทดสอบ", "08:00-17:00", "เอสเปรสโซ่ฟรี: 30 points", "12 points", "แนน", "every 10 baht"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
}

func TestFallback(t *testing.T) {
	sc := testContext()
	cases := []struct{ q, want string }{
		{"แต้มฉันเหลือเท่าไหร่", "12 แต้ม"},
		{"ร้านเปิดกี่โมง", "08:00-17:00"},
		{"ที่อยู่ร้านอยู่ไหน", "สุขใจ"},
		{"แลกอะไรได้บ้าง", "ส่วนลด 20 บาท (10 แต้ม)"},
		{"สะสมแต้มยังไง", "ทุก 10 บาท"},
		{"มีที่จอดรถไหม", "ที่จอดรถ"},
		{"ขายพิซซ่าไหม", "ไม่แน่ใจ"},
	}
	for _, c := range cases {
		if got := Fallback(sc, c.q); !strings.Contains(got, c.want) {
			t.Errorf("Fallback(%q) = %q, want it to contain %q", c.q, got, c.want)
		}
	}
}

func TestNextReward(t *testing.T) {
	sc := testContext()
	if r := nextReward(sc); r == nil || r.Name != "ส่วนลด 20 บาท" {
		t.Errorf("with 12 points want the affordable discount, got %+v", r)
	}
	sc.Points = 3
	if r := nextReward(sc); r == nil || r.CostPoints != 10 {
		t.Errorf("with 3 points want the cheapest target, got %+v", r)
	}
	sc.Rewards = nil
	if r := nextReward(sc); r != nil {
		t.Errorf("no rewards: want nil, got %+v", r)
	}
}

func TestCleanConversation(t *testing.T) {
	long := strings.Repeat("ก", MaxUserRunes+1)
	cases := []struct {
		name string
		in   []Message
		ok   bool
		want int // expected number of messages
	}{
		{"simple", []Message{{"user", "hi"}}, true, 1},
		{"drops leading assistant", []Message{{"assistant", "hello"}, {"user", "hi"}}, true, 1},
		{"merges repeated user turns", []Message{{"user", "a"}, {"user", "b"}}, true, 1},
		{"must end with user", []Message{{"user", "a"}, {"assistant", "b"}}, false, 0},
		{"bad role", []Message{{"system", "x"}, {"user", "a"}}, false, 0},
		{"empty content", []Message{{"user", "  "}}, false, 0},
		{"user message too long", []Message{{"user", long}}, false, 0},
		{"long assistant is truncated, not rejected", []Message{{"user", "a"}, {"assistant", strings.Repeat("ข", 3000)}, {"user", "b"}}, true, 3},
	}
	for _, c := range cases {
		got, ok := CleanConversation(c.in)
		if ok != c.ok || len(got) != c.want {
			t.Errorf("%s: ok=%v len=%d, want ok=%v len=%d", c.name, ok, len(got), c.ok, c.want)
		}
	}

	var many []Message
	for i := 0; i < 15; i++ {
		many = append(many, Message{"user", "q"}, Message{"assistant", "a"})
	}
	many = append(many, Message{"user", "last"})
	got, ok := CleanConversation(many)
	if !ok || len(got) > maxTurns || got[len(got)-1].Content != "last" {
		t.Errorf("long conversation: ok=%v len=%d", ok, len(got))
	}
}
