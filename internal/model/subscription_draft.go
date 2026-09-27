package model

import "time"

// SubscriptionDraft is a bounded preview, never an active subscription.
type SubscriptionDraft struct {
	ID              string `gorm:"primaryKey;size:64"`
	Revision        int
	Payload         string    `gorm:"type:text;not null"`
	ExpiresAt       time.Time `gorm:"index"`
	SubscriptionID  *uint
	ConfirmedChoice string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
