package webchat

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/LumabyteCo/aibutler/internal/notification"
	"nhooyr.io/websocket"
)

// NotificationStore is the durable queue used when a scheduled result
// cannot be broadcast live (no webchat tab open).
type NotificationStore interface {
	Pending(ctx context.Context, channel string, limit int) ([]notification.Notification, error)
	MarkDelivered(ctx context.Context, id int64) error
}

// SetNotificationStore wires connect-time delivery of queued messages.
func (a *Adapter) SetNotificationStore(s NotificationStore) {
	a.notifications = s
}

// deliverPending pushes queued notifications to a newly connected client,
// then marks them delivered. Serialized so two tabs opening together do not
// both consume the same rows.
func (a *Adapter) deliverPending(conn *websocket.Conn) {
	if a.notifications == nil {
		return
	}
	a.notifyMu.Lock()
	defer a.notifyMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pending, err := a.notifications.Pending(ctx, "webchat", 10)
	if err != nil {
		log.Printf("webchat: load pending notifications: %v", err)
		return
	}
	for _, n := range pending {
		frame := wsFrame{Type: "message", Text: n.Text}
		data, _ := json.Marshal(frame)
		if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
			log.Printf("webchat: deliver queued notification %d: %v", n.ID, err)
			return
		}
		if err := a.notifications.MarkDelivered(ctx, n.ID); err != nil {
			log.Printf("webchat: mark notification %d delivered: %v", n.ID, err)
			return
		}
	}
}
