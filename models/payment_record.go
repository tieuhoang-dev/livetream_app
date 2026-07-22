package models
import "time"

type Payment struct{
	ID            string    `gorm:"type:char(36);primary_key" json:"id"`
	OrderID       string    `gorm:"type:char(36);not null" json:"order_id"`
	PaymentMethod string    `gorm:"size:50;not null" json:"payment_method"`
	Amount        float64   `gorm:"type:decimal(10,2);not null" json:"amount"`
	Status        string    `gorm:"size:50;not null" json:"status"` 
	TransactionID string    `gorm:"size:255" json:"transaction_id"`  
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	Order Order `gorm:"foreignKey:OrderID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"order"`

}