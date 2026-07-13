package models

import "time"

type User struct {
	ID                    string     `gorm:"type:char(36);primary_key" json:"id"`
	Username              string     `gorm:"size:255;unique;not null" json:"username"`
	Fullname              string     `gorm:"size:255;not null" json:"fullname"`
	Email                 string     `gorm:"size:255;unique;not null" json:"email"`
	PasswordHash          string     `gorm:"size:255;not null" json:"password_hash"`
	Avatar                string     `gorm:"size:255" json:"avatar"`
	Role                  string     `gorm:"size:50;not null" json:"role"`
	IsVerified            bool       `gorm:"not null" json:"is_verified"`
	VerificationCodeHash  string     `gorm:"size:255" json:"verification_code_hash"`
	VerificationExpiresAt *time.Time `json:"verification_expires_at"`
	Status                string     `gorm:"size:50;not null" json:"status"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`

	//relationships
	Wallet []Wallet `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"wallets"`
}

var RegisterInput struct {
	Username string `json:"username" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}
