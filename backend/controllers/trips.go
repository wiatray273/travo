package controllers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"travo/backend/config"
	"travo/backend/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type tripView struct {
	ID             uint      `json:"id"`
	Origin         string    `json:"origin"`
	Destination    string    `json:"destination"`
	DepartureAt    time.Time `json:"departure_at"`
	Seats          int       `json:"seats"`
	AvailableSeats int       `json:"available_seats"`
	PricePerSeat   int64     `json:"price_per_seat"`
	Status         string    `json:"status"`
	DriverID       uint      `json:"driver_id"`
	DriverName     string    `json:"driver_name"`
}

type countryView struct {
	ID           uint   `json:"id"`
	Name         string `json:"name"`
	ISO2         string `json:"iso2"`
	ISO3         string `json:"iso3,omitempty"`
	CurrencyCode string `json:"currency_code,omitempty"`
	PhoneCode    string `json:"phone_code,omitempty"`
	IsActive     bool   `json:"is_active"`
}

func toCountryView(country models.Country) countryView {
	return countryView{
		ID: country.ID, Name: country.Name, ISO2: country.ISO2, ISO3: country.ISO3,
		CurrencyCode: country.CurrencyCode, PhoneCode: country.PhoneCode, IsActive: country.IsActive,
	}
}

func toTripView(trip models.Trip) tripView {
	return tripView{
		ID:             trip.ID,
		Origin:         trip.Origin,
		Destination:    trip.Destination,
		DepartureAt:    trip.DepartureAt,
		Seats:          trip.Seats,
		AvailableSeats: trip.AvailableSeats,
		PricePerSeat:   trip.PricePerSeat,
		Status:         trip.Status,
		DriverID:       trip.DriverID,
		DriverName:     trip.Driver.FullName,
	}
}

func ListCountries(c *gin.Context) {
	var countries []models.Country
	if err := config.DB.Select("id", "name", "iso2", "iso3", "currency_code", "phone_code", "is_active").
		Where("is_active = ?", true).Order("name").Find(&countries).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load countries"})
		return
	}
	result := make([]countryView, 0, len(countries))
	for _, country := range countries {
		result = append(result, toCountryView(country))
	}
	c.JSON(http.StatusOK, gin.H{"countries": result})
}

func ListTrips(c *gin.Context) {
	query := config.DB.Model(&models.Trip{}).
		Where("status = ? AND departure_at >= ? AND available_seats > 0", "published", time.Now()).
		Where("driver_id IN (SELECT id FROM users WHERE status = ?)", "active").
		Where("country_id IN (SELECT id FROM countries WHERE iso2 = ? AND is_active = ?)", "CM", true)

	if origin := strings.TrimSpace(c.Query("origin")); origin != "" {
		query = query.Where("LOWER(origin) = LOWER(?)", origin)
	}
	if destination := strings.TrimSpace(c.Query("destination")); destination != "" {
		query = query.Where("LOWER(destination) = LOWER(?)", destination)
	}
	if date := strings.TrimSpace(c.Query("date")); date != "" {
		cameroonTime := time.FixedZone("WAT", 60*60)
		parsed, err := time.ParseInLocation("2006-01-02", date, cameroonTime)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Travel date must use YYYY-MM-DD"})
			return
		}
		query = query.Where("departure_at >= ? AND departure_at < ?", parsed, parsed.AddDate(0, 0, 1))
	}

	var trips []models.Trip
	if err := query.Preload("Driver").Order("departure_at").Limit(100).Find(&trips).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load trips"})
		return
	}

	result := make([]tripView, 0, len(trips))
	for _, trip := range trips {
		result = append(result, toTripView(trip))
	}
	c.JSON(http.StatusOK, gin.H{"trips": result})
}

func GetTrip(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid trip"})
		return
	}
	var trip models.Trip
	err = config.DB.Where(
		"id = ? AND status = ? AND departure_at >= ? AND available_seats > 0 AND driver_id IN (SELECT id FROM users WHERE status = ?) AND country_id IN (SELECT id FROM countries WHERE iso2 = ? AND is_active = ?)",
		id, "published", time.Now(), "active", "CM", true,
	).Preload("Driver").First(&trip).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Trip is no longer available"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load trip"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"trip": toTripView(trip)})
}

type createTripInput struct {
	Origin       string `json:"origin" binding:"required"`
	Destination  string `json:"destination" binding:"required"`
	DepartureAt  string `json:"departure_at" binding:"required"`
	Seats        int    `json:"seats" binding:"required"`
	PricePerSeat int64  `json:"price_per_seat" binding:"required"`
}

func CreateTrip(c *gin.Context) {
	user, ok := getCurrentUser(c)
	if !ok {
		return
	}
	if user.Role != "driver" || user.Status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Your driver account must be verified before publishing trips"})
		return
	}
	if user.Phone == nil || !validDriverPhone(strings.TrimSpace(*user.Phone)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Add a valid phone number in your profile before publishing trips"})
		return
	}

	var input createTripInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Please provide all trip details"})
		return
	}

	input.Origin = strings.TrimSpace(input.Origin)
	input.Destination = strings.TrimSpace(input.Destination)
	departure, err := time.Parse(time.RFC3339, input.DepartureAt)
	if err != nil || !departure.After(time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Departure time must be a future date and time"})
		return
	}
	if input.Origin == "" || input.Destination == "" || strings.EqualFold(input.Origin, input.Destination) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Choose two different locations"})
		return
	}
	if input.Seats < 1 || input.Seats > 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Seat count must be between 1 and 8"})
		return
	}
	if input.PricePerSeat < 100 || input.PricePerSeat > 10000000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Price per seat must be between 100 and 10,000,000 XAF"})
		return
	}
	if user.CountryID == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Your account must be assigned to a country"})
		return
	}
	var country models.Country
	if err := config.DB.First(&country, *user.CountryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Your selected country is unavailable"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not verify your country"})
		}
		return
	}
	if !country.IsActive || country.ISO2 != "CM" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Trip publishing and mobile-money checkout are currently supported in Cameroon only"})
		return
	}

	trip := models.Trip{
		DriverID:       user.ID,
		CountryID:      *user.CountryID,
		Origin:         input.Origin,
		Destination:    input.Destination,
		DepartureAt:    departure,
		Seats:          input.Seats,
		AvailableSeats: input.Seats,
		PricePerSeat:   input.PricePerSeat,
		Status:         "published",
	}
	if err := config.DB.Create(&trip).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not publish trip"})
		return
	}
	trip.Driver = user
	c.JSON(http.StatusCreated, gin.H{"trip": toTripView(trip)})
}

func ListMyTrips(c *gin.Context) {
	userID := c.GetUint("user_id")
	var trips []models.Trip
	if err := config.DB.Where("driver_id = ?", userID).
		Preload("Driver").Order("departure_at DESC").Find(&trips).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load your trips"})
		return
	}
	result := make([]tripView, 0, len(trips))
	for _, trip := range trips {
		result = append(result, toTripView(trip))
	}
	c.JSON(http.StatusOK, gin.H{"trips": result})
}

func UpdateTripStatus(c *gin.Context) {
	userID := c.GetUint("user_id")
	tripID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid trip"})
		return
	}

	var input struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Status != "cancelled" && input.Status != "published" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Trip status must be published or cancelled"})
		return
	}
	if input.Status == "published" {
		user, ok := getCurrentUser(c)
		if !ok {
			return
		}
		if user.Role != "driver" || user.Status != "active" {
			c.JSON(http.StatusForbidden, gin.H{"error": "Your driver account must be verified before publishing trips"})
			return
		}
		if user.Phone == nil || !validDriverPhone(strings.TrimSpace(*user.Phone)) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Add a valid phone number in your profile before publishing trips"})
			return
		}
	}

	err = config.DB.Transaction(func(tx *gorm.DB) error {
		var trip models.Trip
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&trip, tripID).Error; err != nil {
			return err
		}
		if trip.DriverID != userID {
			return gorm.ErrRecordNotFound
		}
		if input.Status == "published" && (!trip.DepartureAt.After(time.Now()) || trip.AvailableSeats == 0) {
			return errTripNotPublishable
		}
		if input.Status == "cancelled" {
			var activeBookings int64
			if err := tx.Model(&models.Booking{}).
				Where("trip_id = ? AND status IN ?", trip.ID, []string{"pending_payment", "confirmed"}).
				Count(&activeBookings).Error; err != nil {
				return err
			}
			if activeBookings > 0 {
				return errTripHasBookings
			}
		}
		return tx.Model(&trip).Update("status", input.Status).Error
	})
	switch err {
	case nil:
		c.JSON(http.StatusOK, gin.H{"message": "Trip status updated"})
	case gorm.ErrRecordNotFound:
		c.JSON(http.StatusNotFound, gin.H{"error": "Trip not found"})
	case errTripNotPublishable:
		c.JSON(http.StatusConflict, gin.H{"error": "Past or fully booked trips cannot be published"})
	case errTripHasBookings:
		c.JSON(http.StatusConflict, gin.H{"error": "A trip with confirmed bookings cannot be cancelled"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not update trip"})
	}
}

var (
	errTripNotPublishable = errors.New("trip cannot be published")
	errTripHasBookings    = errors.New("trip has confirmed bookings")
)

func getCurrentUser(c *gin.Context) (models.User, bool) {
	var user models.User
	if err := config.DB.First(&user, c.GetUint("user_id")).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Your account is unavailable"})
		return models.User{}, false
	}
	return user, true
}
