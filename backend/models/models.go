package models

import (
	"time"

	"gorm.io/gorm"
)

type Country struct {
	gorm.Model
	Name         string `gorm:"not null"`
	ISO2         string `gorm:"uniqueIndex;size:2;not null"`
	ISO3         string `gorm:"uniqueIndex;size:3"`
	CurrencyCode string `gorm:"size:3"`
	PhoneCode    string
	IsActive     bool `gorm:"default:false"`

	Regions []Region
}

type Region struct {
	gorm.Model
	Name      string `gorm:"not null"`
	CountryID uint   `gorm:"not null;uniqueIndex:idx_region_country"`
	Country   Country

	Cities []City
}

type City struct {
	gorm.Model
	Name      string `gorm:"not null"`
	RegionID  uint   `gorm:"not null;uniqueIndex:idx_city_region"`
	CountryID uint   `gorm:"not null"`

	Region Region
}

type User struct {
	gorm.Model

	FullName string `gorm:"not null" json:"full_name"`

	Email *string `gorm:"uniqueIndex" json:"email,omitempty"`

	Phone *string `gorm:"uniqueIndex" json:"phone,omitempty"`

	PasswordHash string `gorm:"not null" json:"-"`

	Role string `gorm:"not null;index" json:"role"`

	Status string `gorm:"not null;default:pending;index" json:"status"`

	CountryID *uint `json:"country_id"`

	EmailVerified bool `gorm:"default:false" json:"email_verified"`

	PhoneVerified bool `gorm:"default:false" json:"phone_verified"`
}

type DriverProfile struct {
	gorm.Model

	UserID uint `gorm:"uniqueIndex;not null" json:"user_id"`

	DateOfBirth *time.Time `json:"date_of_birth"`

	Gender string `json:"gender"`

	VerificationStatus string `gorm:"default:pending" json:"verification_status"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

type PassengerProfile struct {
	gorm.Model

	UserID uint `gorm:"uniqueIndex;not null" json:"user_id"`

	DateOfBirth *time.Time `json:"date_of_birth"`

	Gender string `json:"gender"`

	VerificationStatus string `gorm:"default:pending" json:"verification_status"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

type DriverPhoto struct {
	gorm.Model
	UserID           uint   `gorm:"not null;index"`
	StorageKey       string `gorm:"not null" json:"-"`
	MIMEType         string
	ConsentAt        time.Time `gorm:"not null"`
	ReviewStatus     string    `gorm:"not null;default:pending"`
	ReviewDocumentID *uint
	ReviewedBy       *uint
	ReviewedAt       *time.Time
	User             User `gorm:"foreignKey:UserID" json:"-"`
}

type IdentityDocument struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	UserID uint `gorm:"not null;index" json:"user_id"`

	DocumentType string `gorm:"not null" json:"document_type"`

	StorageKey string `gorm:"not null" json:"-"`

	OriginalName string `json:"original_name"`

	MIMEType string `json:"mime_type"`

	FileSize int64 `json:"file_size"`

	BackStorageKey string `json:"-"`

	BackOriginalName string `json:"back_original_name"`

	BackMIMEType string `json:"back_mime_type"`

	BackFileSize int64 `json:"back_file_size"`

	Status string `gorm:"default:pending" json:"status"`

	RejectionReason string `json:"rejection_reason,omitempty"`

	ReviewedBy *uint `json:"reviewed_by,omitempty"`

	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

type Trip struct {
	gorm.Model

	DriverID       uint      `gorm:"not null;index" json:"driver_id"`
	CountryID      uint      `gorm:"not null;index" json:"country_id"`
	Origin         string    `gorm:"not null" json:"origin"`
	Destination    string    `gorm:"not null" json:"destination"`
	DepartureAt    time.Time `gorm:"not null;index" json:"departure_at"`
	Seats          int       `gorm:"not null" json:"seats"`
	AvailableSeats int       `gorm:"not null" json:"available_seats"`
	PricePerSeat   int64     `gorm:"not null" json:"price_per_seat"`
	Status         string    `gorm:"not null;default:published;index" json:"status"`

	Driver User `gorm:"foreignKey:DriverID" json:"-"`
}

type Booking struct {
	gorm.Model

	TripID               uint    `gorm:"not null;index" json:"trip_id"`
	PassengerID          uint    `gorm:"not null;index" json:"passenger_id"`
	Seats                int     `gorm:"not null" json:"seats"`
	FareTotal            int64   `gorm:"not null" json:"fare_total"`
	CommissionAmount     int64   `gorm:"not null" json:"commission_amount"`
	ProviderFee          int64   `gorm:"not null;default:0" json:"provider_fee"`
	AmountCharged        int64   `gorm:"not null" json:"amount_charged"`
	ProviderRevenue      int64   `gorm:"not null;default:0" json:"provider_revenue"`
	PaymentTransactionID *string `gorm:"uniqueIndex" json:"-"`
	PaymentURL           string  `json:"-"`
	PaymentStatus        string  `gorm:"not null;default:not_started;index" json:"payment_status"`
	Status               string  `gorm:"not null;default:pending_payment;index" json:"status"`

	Trip      Trip `gorm:"foreignKey:TripID" json:"trip,omitempty"`
	Passenger User `gorm:"foreignKey:PassengerID" json:"-"`
}
