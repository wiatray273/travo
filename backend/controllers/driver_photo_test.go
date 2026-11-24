package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"travo/backend/config"
	"travo/backend/middleware"
	"travo/backend/models"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestDriverPhotoIntegration(t *testing.T) {
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
	if err := db.AutoMigrate(&models.Country{}, &models.User{}, &models.DriverProfile{}, &models.IdentityDocument{}, &models.DriverPhoto{}, &models.Trip{}, &models.Booking{}); err != nil {
		t.Fatal(err)
	}
	oldDB := config.DB
	config.DB = db
	defer func() { config.DB = oldDB }()
	t.Chdir(t.TempDir())

	country := models.Country{Name: "Test country", ISO2: "XY", IsActive: true}
	if err := db.Create(&country).Error; err != nil {
		t.Fatal(err)
	}
	driver := models.User{FullName: "Photo Driver", PasswordHash: "unused", Role: "driver", Status: "active", CountryID: &country.ID}
	passenger := models.User{FullName: "Photo Passenger", PasswordHash: "unused", Role: "passenger", Status: "active"}
	for _, user := range []*models.User{&driver, &passenger} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	document := models.IdentityDocument{UserID: driver.ID, DocumentType: "national_id", StorageKey: "id.jpg", Status: "pending"}
	if err := db.Create(&document).Error; err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join("private_uploads", "identity"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("private_uploads", "identity", document.StorageKey), []byte("test document"), 0600); err != nil {
		t.Fatal(err)
	}

	var role string
	var userID, countryID uint
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("role", role)
		c.Set("user_id", userID)
		c.Set("country_id", countryID)
		c.Next()
	})
	router.POST("/photo", middleware.RequireRoles("driver"), UploadDriverPhoto)
	router.GET("/drivers/:id/photo", DownloadDriverPhoto)
	router.PATCH("/drivers/:id/review", middleware.RequireRoles("country_admin", "super_admin"), ReviewDriverPhoto)

	var validImage bytes.Buffer
	if err := jpeg.Encode(&validImage, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	upload := func(consent bool, data []byte) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if consent {
			if err := writer.WriteField("consent", "true"); err != nil {
				t.Fatal(err)
			}
		}
		part, err := writer.CreateFormFile("photo", "selfie.jpg")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/photo", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp
	}
	photoPath := fmt.Sprintf("/drivers/%d/photo", driver.ID)
	getPhoto := func() int {
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, photoPath, nil))
		if resp.Code == http.StatusOK && resp.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("private photo response is cacheable")
		}
		return resp.Code
	}
	review := func(photoID uint) int {
		body := fmt.Sprintf(`{"photo_id":%d,"document_id":%d,"status":"appears_match"}`, photoID, document.ID)
		req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/drivers/%d/review", driver.ID), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp.Code
	}

	role, userID = "driver", driver.ID
	if resp := upload(false, validImage.Bytes()); resp.Code != http.StatusBadRequest {
		t.Fatalf("upload without consent: %d", resp.Code)
	}
	if resp := upload(true, []byte{0xff, 0xd8, 0xff, 0xdb}); resp.Code != http.StatusBadRequest {
		t.Fatalf("corrupt photo upload: %d", resp.Code)
	}
	resp := upload(true, validImage.Bytes())
	if resp.Code != http.StatusCreated {
		t.Fatalf("upload with consent: %d %s", resp.Code, resp.Body)
	}
	var result struct {
		Photo driverPhotoView `json:"photo"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil || result.Photo.ID == 0 || result.Photo.ReviewStatus != "pending" {
		t.Fatalf("invalid photo response: %s (%v)", resp.Body, err)
	}
	if status := getPhoto(); status != http.StatusOK {
		t.Fatalf("driver photo access: %d", status)
	}
	userID = passenger.ID
	if status := getPhoto(); status != http.StatusNotFound {
		t.Fatalf("other driver photo access: %d", status)
	}
	role = "passenger"
	if status := getPhoto(); status != http.StatusNotFound {
		t.Fatalf("unbooked passenger access: %d", status)
	}

	trip := models.Trip{DriverID: driver.ID, CountryID: country.ID, Origin: "A", Destination: "B", Seats: 1, AvailableSeats: 1, PricePerSeat: 100}
	if err := db.Create(&trip).Error; err != nil {
		t.Fatal(err)
	}
	booking := models.Booking{TripID: trip.ID, PassengerID: passenger.ID, Seats: 1, FareTotal: 100, AmountCharged: 104, Status: "confirmed"}
	if err := db.Create(&booking).Error; err != nil {
		t.Fatal(err)
	}
	if status := getPhoto(); status != http.StatusOK {
		t.Fatalf("booked passenger access: %d", status)
	}
	if err := db.Model(&booking).Update("status", "cancelled").Error; err != nil {
		t.Fatal(err)
	}
	if status := getPhoto(); status != http.StatusNotFound {
		t.Fatalf("cancelled passenger access: %d", status)
	}

	role, userID, countryID = "country_admin", passenger.ID, country.ID+1
	if status := getPhoto(); status != http.StatusNotFound {
		t.Fatalf("out-of-country admin photo access: %d", status)
	}
	if status := review(result.Photo.ID); status != http.StatusNotFound {
		t.Fatalf("out-of-country admin review: %d", status)
	}
	countryID = country.ID
	if status := getPhoto(); status != http.StatusOK {
		t.Fatalf("country admin photo access: %d", status)
	}
	if status := review(result.Photo.ID); status != http.StatusOK {
		t.Fatalf("manual review: %d", status)
	}
	oldID := result.Photo.ID
	role, userID = "driver", driver.ID
	resp = upload(true, validImage.Bytes())
	if resp.Code != http.StatusCreated {
		t.Fatalf("replacement upload: %d %s", resp.Code, resp.Body)
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil || result.Photo.ID == oldID || result.Photo.ReviewStatus != "pending" {
		t.Fatalf("replacement response: %s (%v)", resp.Body, err)
	}
	role, userID = "country_admin", passenger.ID
	if status := review(oldID); status != http.StatusConflict {
		t.Fatalf("stale photo review: %d", status)
	}
	var oldPhoto models.DriverPhoto
	if err := db.First(&oldPhoto, oldID).Error; err != nil || oldPhoto.ReviewStatus != "appears_match" || oldPhoto.ReviewDocumentID == nil || *oldPhoto.ReviewDocumentID != document.ID || oldPhoto.ConsentAt.IsZero() {
		t.Fatalf("retained review or consent missing: %+v (%v)", oldPhoto, err)
	}
}
