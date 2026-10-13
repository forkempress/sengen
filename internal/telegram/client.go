// Package telegram is a thin client + webhook dispatcher for the Telegram
// Bot API — no SDK, just the handful of endpoints this app needs.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Client talks to the Bot API for one tenant's bot (each tenant runs its own
// @BotFather bot, so a Client is constructed per-request from that tenant's
// stored token — see internal/store.TenantSettings).
type Client struct {
	BaseURL string // overridable in tests to point at a fake server
	Token   string
	HTTP    *http.Client
}

// NewClient builds a client against the real Bot API, unless
// TELEGRAM_API_BASE_URL is set (local testing against a fake server — never
// set this in production).
func NewClient(token string) *Client {
	base := "https://api.telegram.org"
	if override := os.Getenv("TELEGRAM_API_BASE_URL"); override != "" {
		base = override
	}
	return &Client{BaseURL: base, Token: token, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
}

func (c *Client) call(ctx context.Context, method string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", method, err)
	}
	url := fmt.Sprintf("%s/bot%s/%s", c.BaseURL, c.Token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("call %s: %w", method, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read %s response: %w", method, err)
	}
	var apiResp apiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}
	if !apiResp.OK {
		return fmt.Errorf("telegram %s failed: %s", method, apiResp.Description)
	}
	if out != nil && len(apiResp.Result) > 0 {
		if err := json.Unmarshal(apiResp.Result, out); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
	}
	return nil
}

func (c *Client) SendMessage(ctx context.Context, chatID int64, text string, replyMarkup any) error {
	payload := map[string]any{"chat_id": chatID, "text": text}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}
	return c.call(ctx, "sendMessage", payload, nil)
}

// SetWebhook registers this bot's webhook URL and secret with Telegram —
// called from the "Postavi webhook" button in Podešavanja once the owner
// has entered their @BotFather token.
func (c *Client) SetWebhook(ctx context.Context, url, secretToken string) error {
	payload := map[string]any{"url": url, "secret_token": secretToken}
	return c.call(ctx, "setWebhook", payload, nil)
}

func (c *Client) AnswerCallbackQuery(ctx context.Context, callbackID, text string) error {
	payload := map[string]any{"callback_query_id": callbackID, "text": text}
	return c.call(ctx, "answerCallbackQuery", payload, nil)
}

type tgFile struct {
	FilePath string `json:"file_path"`
}

// DownloadDocument resolves a file_id to its bytes via getFile + the file
// download endpoint — used for the admin CSV upload.
func (c *Client) DownloadDocument(ctx context.Context, fileID string) ([]byte, error) {
	var f tgFile
	if err := c.call(ctx, "getFile", map[string]any{"file_id": fileID}, &f); err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/file/bot%s/%s", c.BaseURL, c.Token, f.FilePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build file download request: %w", err)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download file: %w", err)
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
