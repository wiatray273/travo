package controllers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"travo/backend/config"
	"travo/backend/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type bookingView struct {
	ID               uint      `json:"id"`
	TripID           uint      `json:"trip_id"`
	PassengerName    string    `json:"passenger_name,omitempty"`
	DriverName       string    `json:"driver_name"`
	DriverID         uint      `json:"driver_id"`
	DriverPhone      *string   `json:"driver_phone,omitempty"`
	Origin           string    `json:"origin"`
	Destination      string    `json:"destination"`
	DepartureAt      time.Time `json:"departure_at"`
	Seats            int       `json:"seats"`
	PricePerSeat     int64     `json:"price_per_seat"`
	FareTotal        int64     `json:"fare_total"`
	CommissionAmount int64     `json:"commission_amount"`
	ProviderFee      int64     `json:"provider_fee"`
	AmountCharged    int64     `json:"amount_charged"`
	ProviderRevenue  int64     `json:"provider_revenue"`
	DriverAmountDue  int64     `json:"driver_amount_due"`
	PaymentStatus    string    `json:"payment_status"`
	Status           string    `json:"status"`
	PaymentURL       string    `json:"payment_url,omitempty"`
}

func passengerBookingView(booking models.Booking) bookingView {
	view := toBookingView(booking)
	if booking.Status == "confirmed" && booking.PaymentStatus == "SUCCESSFUL" {
		view.DriverPhone = booking.Trip.Driver.Phone
	}
	return view
}

func toBookingView(booking models.Booking) bookingView {
	return bookingView{
		ID:               booking.ID,
		TripID:           booking.TripID,
		PassengerName:    booking.Passenger.FullName,
		DriverName:       booking.Trip.Driver.FullName,
		DriverID:         booking.Trip.DriverID,
		Origin:           booking.Trip.Origin,
		Destination:      booking.Trip.Destination,
		DepartureAt:      booking.Trip.DepartureAt,
		Seats:            booking.Seats,
		PricePerSeat:     booking.Trip.PricePerSeat,
		FareTotal:        booking.FareTotal,
		CommissionAmount: booking.CommissionAmount,
		ProviderFee:      booking.ProviderFee,
		AmountCharged:    booking.AmountCharged,
		ProviderRevenue:  booking.ProviderRevenue,
		DriverAmountDue:  driverAmountDue(booking),
		PaymentStatus:    booking.PaymentStatus,
		Status:           booking.Status,
		PaymentURL:       booking.PaymentURL,
	}
}

func CreateBooking(c *gin.Context) {
	user, ok := getCurrentUser(c)
	if !ok {
		return
	}
	if user.Role != "passenger" || user.Status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only active passenger accounts can book trips"})
		return
	}

	tripID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid trip"})
		return
	}
	var input struct {
		Seats int `json:"seats" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Seats < 1 || input.Seats > 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Choose between 1 and 8 seats"})
		return
	}

	var booking models.Booking
	err = config.DB.Transaction(func(tx *gorm.DB) error {
		var trip models.Trip
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&trip, tripID).Error; err != nil {
			return err
		}
		if trip.Status != "published" || !trip.DepartureAt.After(time.Now()) {
			return errTripUnavailable
		}
		if trip.DriverID == user.ID {
			return errOwnTrip
		}
		var country models.Country
		if err := tx.First(&country, trip.CountryID).Error; err != nil {
			return err
		}
		if country.ISO2 != "CM" || !country.IsActive {
			return errPaymentCountryUnsupported
		}
		if user.CountryID == nil || trip.CountryID != *user.CountryID {
			return errDifferentCountry
		}
		var existingBookings int64
		if err := tx.Model(&models.Booking{}).
			Where("trip_id = ? AND passenger_id = ? AND status IN ?", trip.ID, user.ID, []string{"pending_payment", "confirmed"}).
			Count(&existingBookings).Error; err != nil {
			return err
		}
		if existingBookings > 0 {
			return errExistingBooking
		}
		if input.Seats > trip.AvailableSeats {
			return errNotEnoughSeats
		}

		fareTotal := trip.PricePerSeat * int64(input.Seats)
		commission, amountCharged, providerFee := calculateBookingAmounts(fareTotal)
		booking = models.Booking{
			TripID:           trip.ID,
			PassengerID:      user.ID,
			Seats:            input.Seats,
			FareTotal:        fareTotal,
			CommissionAmount: commission,
			ProviderFee:      providerFee,
			AmountCharged:    amountCharged,
			PaymentStatus:    "not_started",
			Status:           "pending_payment",
		}
		if err := tx.Create(&booking).Error; err != nil {
			return err
		}
		return tx.Model(&trip).Update("available_seats", trip.AvailableSeats-input.Seats).Error
	})

	switch {
	case err == nil:
		if err := config.DB.Preload("Trip").Preload("Trip.Driver").First(&booking, booking.ID).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Seat was reserved, but booking details could not be loaded"})
			return
		}
		c.JSON(http.StatusCreated, gin.H{
			"booking": toBookingView(booking),
			"message": "Seat reserved. Continue to mobile-money checkout to confirm your booking.",
		})
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "Trip not found"})
	case errors.Is(err, errTripUnavailable):
		c.JSON(http.StatusConflict, gin.H{"error": "This trip is no longer available"})
	case errors.Is(err, errOwnTrip):
		c.JSON(http.StatusForbidden, gin.H{"error": "You cannot book your own trip"})
	case errors.Is(err, errDifferentCountry):
		c.JSON(http.StatusForbidden, gin.H{"error": "You can only book trips in your account country"})
	case errors.Is(err, errExistingBooking):
		c.JSON(http.StatusConflict, gin.H{"error": "You already have an active booking for this trip"})
	case errors.Is(err, errPaymentCountryUnsupported):
		c.JSON(http.StatusForbidden, gin.H{"error": "Mobile-money booking is currently supported in Cameroon only"})
	case errors.Is(err, errNotEnoughSeats):
		c.JSON(http.StatusConflict, gin.H{"error": "Not enough seats are available"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not reserve seats"})
	}
}

var (
	errTripUnavailable           = errors.New("trip unavailable")
	errOwnTrip                   = errors.New("cannot book own trip")
	errDifferentCountry          = errors.New("trip belongs to another country")
	errNotEnoughSeats            = errors.New("not enough seats")
	errPaymentCountryUnsupported = errors.New("mobile-money checkout does not support this country")
	errExistingBooking           = errors.New("passenger already booked this trip")
)

func calculateBookingAmounts(fareTotal int64) (commission, amountCharged, providerFee int64) {
	commission = (fareTotal*paymentCommissionPercent + 99) / 100
	amountCharged = (fareTotal*100 + 96) / 97
	providerFee = amountCharged - fareTotal
	return
}

func ListBookings(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	userID := c.GetUint("user_id")
	role := c.GetString("role")
	query := config.DB.Model(&models.Booking{})
	if role == "passenger" {
		query = query.Where("passenger_id = ?", userID)
	} else if role == "driver" {
		query = query.Where("trip_id IN (SELECT id FROM trips WHERE driver_id = ?)", userID)
	} else {
		c.JSON(http.StatusForbidden, gin.H{"error": "You do not have permission to view bookings"})
		return
	}

	var bookings []models.Booking
	if err := query.Preload("Trip").Preload("Trip.Driver").Preload("Passenger").
		Order("created_at DESC").Find(&bookings).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load bookings"})
		return
	}
	result := make([]bookingView, 0, len(bookings))
	for _, booking := range bookings {
		view := toBookingView(booking)
		if role == "passenger" {
			view = passengerBookingView(booking)
		} else {
			view.PaymentURL = ""
		}
		result = append(result, view)
	}
	c.JSON(http.StatusOK, gin.H{"bookings": result})
}

func CancelBooking(c *gin.Context) {
	bookingID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid booking"})
		return
	}
	userID := c.GetUint("user_id")
	err = config.DB.Transaction(func(tx *gorm.DB) error {
		var booking models.Booking
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&booking, bookingID).Error; err != nil {
			return err
		}
		if booking.PassengerID != userID {
			return gorm.ErrRecordNotFound
		}
		if booking.Status != "pending_payment" || booking.PaymentStatus != "not_started" {
			return errBookingCannotCancel
		}
		if err := tx.Model(&booking).Updates(map[string]any{"status": "cancelled", "payment_status": "cancelled"}).Error; err != nil {
			return err
		}
		return tx.Model(&models.Trip{}).Where("id = ? AND status = ?", booking.TripID, "published").
			Update("available_seats", gorm.Expr("available_seats + ?", booking.Seats)).Error
	})
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{"message": "Booking cancelled"})
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "Booking not found"})
	case errors.Is(err, errBookingCannotCancel):
		c.JSON(http.StatusConflict, gin.H{"error": "A booking with a started payment or confirmed payment cannot be cancelled online"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not cancel booking"})
	}
}

var errBookingCannotCancel = errors.New("booking cannot be cancelled")
