package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Telegram Bot API types.

// Update represents a Telegram update.
type Update struct {
	UpdateID int      `json:"update_id"`
	Message  *Message `json:"message,omitempty"`
}

// Message represents a Telegram message.
type Message struct {
	MessageID int    `json:"message_id"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text,omitempty"`
	Photo     []PhotoSize `json:"photo,omitempty"`
	Voice     *Voice      `json:"voice,omitempty"`
	Document  *Document   `json:"document,omitempty"`
}

// Chat represents a Telegram chat.
type Chat struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title,omitempty"`
}

// PhotoSize represents a photo size in Telegram.
type PhotoSize struct {
	FileID   string `json:"file_id"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	FileSize int    `json:"file_size,omitempty"`
}

// Voice represents a voice message.
type Voice struct {
	FileID   string `json:"file_id"`
	Duration int    `json:"duration"`
	MimeType string `json:"mime_type,omitempty"`
	FileSize int    `json:"file_size,omitempty"`
}

// Document represents a file sent as a document.
type Document struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	FileSize int    `json:"file_size,omitempty"`
}

// SentMessage is the result of sending a message.
type SentMessage struct {
	MessageID int  `json:"message_id"`
	Chat      Chat `json:"chat"`
}

// APIClient is the interface for Telegram Bot API operations.
type APIClient interface {
	GetUpdates(ctx context.Context, offset, timeout int) ([]Update, error)
	SendMessage(ctx context.Context, chatID int64, text string, replyTo int) (*SentMessage, error)
	EditMessage(ctx context.Context, chatID int64, msgID int, text string) error
	SendChatAction(ctx context.Context, chatID int64, action string) error
	GetFile(ctx context.Context, fileID string) ([]byte, error)
}

// httpAPIClient implements APIClient via net/http.
type httpAPIClient struct {
	token  string
	client *http.Client
	base   string
}

// NewAPIClient creates a Telegram API client.
func NewAPIClient(token string) APIClient {
	return &httpAPIClient{
		token:  token,
		client: &http.Client{Timeout: 60 * time.Second},
		base:   "https://api.telegram.org/bot" + token,
	}
}

type apiResponse struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Desc   string          `json:"description,omitempty"`
}

func (c *httpAPIClient) get(ctx context.Context, method string, params url.Values) (json.RawMessage, error) {
	u := c.base + "/" + method + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var ar apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return nil, fmt.Errorf("telegram: decode response: %w", err)
	}
	if !ar.OK {
		return nil, fmt.Errorf("telegram: %s", ar.Desc)
	}
	return ar.Result, nil
}

func (c *httpAPIClient) GetUpdates(ctx context.Context, offset, timeout int) ([]Update, error) {
	params := url.Values{
		"offset":  {strconv.Itoa(offset)},
		"timeout": {strconv.Itoa(timeout)},
	}
	data, err := c.get(ctx, "getUpdates", params)
	if err != nil {
		return nil, err
	}
	var updates []Update
	if err := json.Unmarshal(data, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

func (c *httpAPIClient) SendMessage(ctx context.Context, chatID int64, text string, replyTo int) (*SentMessage, error) {
	params := url.Values{
		"chat_id": {strconv.FormatInt(chatID, 10)},
		"text":    {text},
	}
	if replyTo > 0 {
		params.Set("reply_to_message_id", strconv.Itoa(replyTo))
	}
	data, err := c.get(ctx, "sendMessage", params)
	if err != nil {
		return nil, err
	}
	var msg SentMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (c *httpAPIClient) EditMessage(ctx context.Context, chatID int64, msgID int, text string) error {
	params := url.Values{
		"chat_id":    {strconv.FormatInt(chatID, 10)},
		"message_id": {strconv.Itoa(msgID)},
		"text":       {text},
	}
	_, err := c.get(ctx, "editMessageText", params)
	return err
}

func (c *httpAPIClient) SendChatAction(ctx context.Context, chatID int64, action string) error {
	params := url.Values{
		"chat_id": {strconv.FormatInt(chatID, 10)},
		"action":  {action},
	}
	_, err := c.get(ctx, "sendChatAction", params)
	return err
}

func (c *httpAPIClient) GetFile(ctx context.Context, fileID string) ([]byte, error) {
	// First get the file path.
	params := url.Values{"file_id": {fileID}}
	data, err := c.get(ctx, "getFile", params)
	if err != nil {
		return nil, err
	}

	var fileInfo struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(data, &fileInfo); err != nil {
		return nil, err
	}

	// Download the file.
	fileURL := "https://api.telegram.org/file/bot" + c.token + "/" + fileInfo.FilePath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}
