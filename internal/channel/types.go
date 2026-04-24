package channel

import "time"

// MessageType classifies the kind of content in a message.
type MessageType string

const (
	TypeText  MessageType = "text"
	TypeImage MessageType = "image"
	TypeAudio MessageType = "audio"
	TypeFile  MessageType = "file"
	TypeVoice MessageType = "voice"
)

// Envelope is the normalized message container that flows through the system.
type Envelope struct {
	ID          string
	Channel     string            // "webchat", "telegram", etc.
	AccountID   string
	ThreadID    string            // For threading (Slack threads, Telegram topics)
	Type        MessageType
	Text        string
	Attachments []Attachment
	Metadata    map[string]string
	Timestamp   time.Time
	ReplyTo     string
}

// Attachment represents a media file attached to a message.
type Attachment struct {
	Type     MessageType
	MimeType string
	Data     []byte
	URL      string
	Filename string
	Size     int64
}

// OutgoingMessage is the response sent back through a channel.
type OutgoingMessage struct {
	Text        string
	Attachments []Attachment
	ReplyTo     string
	Streaming   bool
	EditID      string // For streaming: message to edit
}

// EventType classifies system events.
type EventType string

const (
	EventWorking EventType = "working"
	EventDone    EventType = "done"
	EventError   EventType = "error"
	EventTyping  EventType = "typing"
)
