package model

type Room struct {
	ID           string                 `json:"room_id"`
	Participants map[string]Participant `json:"participants"`
}

type Participant struct {
	ID string `json:"id"`
}

// Global announcement types that sent by server.
const (
	AnnouncementTypeJoined = "joined"
	AnnouncementTypeLeft   = "left"
)

type Announcement struct {
	DST     string `json:"dst"`
	SRC     string `json:"src"` // for inbound messages server re-assigns this based on websocket session
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

type Wire struct {
	RX chan Announcement
	TX chan Announcement
}

func NewWire() Wire {
	return Wire{
		RX: make(chan Announcement),
		TX: make(chan Announcement),
	}
}
