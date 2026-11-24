package controllers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"travo/backend/config"
	"travo/backend/middleware"
	"travo/backend/models"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestValidDriverPhone(t *testing.T) {
	for _, test := range []struct {
		phone string
		valid bool
	}{
		{"+237 650 123 456", true},
		{"650123456", true},
		{"", false},
		{"123", false},
		{"+1234567890123456", false},
		{"1234567<script>", false},
		{"123+4567", false},
	} {
		if got := validDriverPhone(test.phone); got != test.valid {
			t.Errorf("validDriverPhone(%q) = %v, want %v", test.phone, got, test.valid)
		}
	}
}

func TestDriverProfileAndBookingPhoneIntegration(t *testing.T) {
	dsn := os.Getenv("TRAVO_TEST_DSN")
	if dsn == "" {
		t.Skip("set TRAVO_TEST_DSN to an isolated test database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := db.AutoMigrate(&models.Country{}, &models.User{}, &models.Trip{}, &models.Booking{}); err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	oldDB := config.DB
	config.DB = tx
	defer func() { config.DB = oldDB }()

	country := models.Country{Name: "Cameroon", ISO2: "CM", IsActive: true}
	if err := tx.Create(&country).Error; err != nil {
		t.Fatal(err)
	}
	otherPhone := "+237 650 222 222"
	driver := models.User{FullName: "Driver", Role: "driver", Status: "active", PasswordHash: "unused", CountryID: &country.ID}
	passenger := models.User{FullName: "Passenger", Role: "passenger", Status: "active", PasswordHash: "unused"}
	other := models.User{FullName: "Other", Role: "passenger", Status: "active", PasswordHash: "unused", Phone: &otherPhone}
	for _, user := range []*models.User{&driver, &passenger, &other} {
		if err := tx.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}

	var role string
	var userID uint
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("role", role)
		c.Set("user_id", userID)
		c.Next()
	})
	router.PATCH("/driver/profile", middleware.RequireRoles("driver"), UpdateDriverProfile)
	router.POST("/trips", middleware.RequireRoles("driver"), CreateTrip)
	router.GET("/bookings", middleware.RequireRoles("passenger", "driver"), ListBookings)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp
	}

	role, userID = "driver", driver.ID
	tripBody := fmt.Sprintf(`{"origin":"Douala","destination":"Buea","departure_at":%q,"seats":2,"price_per_seat":1000}`, time.Now().Add(24*time.Hour).Format(time.RFC3339))
	if resp := call("POST", "/trips", tripBody); resp.Code != http.StatusForbidden {
		t.Fatalf("publishing without phone: %d %s", resp.Code, resp.Body)
	}
	if err := tx.SavePoint("before_duplicate").Error; err != nil {
		t.Fatal(err)
	}
	if resp := call("PATCH", "/driver/profile", `{"full_name":"Updated Driver","phone":"+237 650 222 222"}`); resp.Code != http.StatusConflict {
		t.Fatalf("duplicate phone: %d %s", resp.Code, resp.Body)
	}
	if err := tx.RollbackTo("before_duplicate").Error; err != nil {
		t.Fatal(err)
	}
	phone := "+237 650 111 111"
	if resp := call("PATCH", "/driver/profile", `{"full_name":"Updated Driver","phone":"+237 650 111 111"}`); resp.Code != http.StatusOK {
		t.Fatalf("update profile: %d %s", resp.Code, resp.Body)
	}
	if resp := call("POST", "/trips", tripBody); resp.Code != http.StatusCreated {
		t.Fatalf("publishing with phone: %d %s", resp.Code, resp.Body)
	}
	var trip models.Trip
	if err := tx.Where("driver_id = ?", driver.ID).First(&trip).Error; err != nil {
		t.Fatal(err)
	}
	booking := models.Booking{TripID: trip.ID, PassengerID: passenger.ID, Seats: 1, FareTotal: 1000, AmountCharged: 1031, Status: "pending_payment", PaymentStatus: "not_started"}
	if err := tx.Create(&booking).Error; err != nil {
		t.Fatal(err)
	}
	role, userID = "passenger", passenger.ID
	if resp := call("PATCH", "/driver/profile", `{"full_name":"Other","phone":"+237 650 111 111"}`); resp.Code != http.StatusForbidden {
		t.Fatalf("passenger updated profile: %d", resp.Code)
	}
	if resp := call("GET", "/bookings", ""); strings.Contains(resp.Body.String(), phone) {
		t.Fatalf("unpaid booking exposed phone: %s", resp.Body)
	}
	if err := tx.Model(&booking).Updates(map[string]any{"status": "confirmed", "payment_status": "SUCCESSFUL"}).Error; err != nil {
		t.Fatal(err)
	}
	if resp := call("GET", "/bookings", ""); resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), phone) || resp.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("paid booking missing phone or privacy header: %d %s", resp.Code, resp.Body)
	}
	userID = other.ID
	if resp := call("GET", "/bookings", ""); strings.Contains(resp.Body.String(), phone) {
		t.Fatalf("other passenger saw phone: %s", resp.Body)
	}
	role, userID = "driver", driver.ID
	if resp := call("GET", "/bookings", ""); strings.Contains(resp.Body.String(), phone) {
		t.Fatalf("driver booking view exposed phone: %s", resp.Body)
	}
	role, userID = "passenger", passenger.ID
	if err := tx.Model(&booking).Update("status", "cancelled").Error; err != nil {
		t.Fatal(err)
	}
	if resp := call("GET", "/bookings", ""); strings.Contains(resp.Body.String(), phone) {
		t.Fatalf("cancelled booking exposed phone: %s", resp.Body)
	}
}
