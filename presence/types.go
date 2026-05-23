package presence

type PresenceState string

const (
	StateOnline  PresenceState = "online"
	StateOffline PresenceState = "offline"
)

type TypingState string

const (
	TypingStart TypingState = "start"
	TypingStop  TypingState = "stop"
)

type PresenceEvent struct {
	UserID    string        `json:"user_id"`
	State     PresenceState `json:"state"`
	Timestamp int64         `json:"ts"`
	NodeID    string        `json:"node_id,omitempty"`
}

type TypingEvent struct {
	From      string      `json:"from"`
	To        []string    `json:"to"`
	State     TypingState `json:"state"`
	Timestamp int64       `json:"ts"`
	NodeID    string      `json:"node_id,omitempty"`
}
