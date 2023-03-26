package entities

import "time"

type Statistics struct {
	ID                int64     `json:"id"`
	ImageGenerationID int64     `json:"image_generation_id"`
	MemberID          string    `json:"member_id"`
	TimeMs            int64     `json:"time_ms"`
	CreatedAt         time.Time `json:"created_at"`
}

type StatsByMember struct {
	MemberID string `json:"member_id"`
	Count    int64  `json:"count"`
	TimeMs   int64  `json:"time_ms"`
}
