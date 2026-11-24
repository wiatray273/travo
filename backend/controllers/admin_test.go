package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestDocumentSides(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, field := range []string{"front", "back"} {
		part, err := writer.CreateFormFile(field, field+".jpg")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte{0xff, 0xd8, 0xff, 0xdb, 0, 1, 2}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	defer req.MultipartForm.RemoveAll()

	dir := t.TempDir()
	keys := make(map[string]bool)
	for _, field := range []string{"front", "back"} {
		side, err := readDocumentSide(req.MultipartForm.File[field], field)
		if err != nil {
			t.Fatal(err)
		}
		key, err := storeDocumentSide(dir, side)
		if err != nil {
			t.Fatal(err)
		}
		if keys[key] {
			t.Fatal("document sides must have distinct storage keys")
		}
		keys[key] = true
		stored, err := os.ReadFile(filepath.Join(dir, key))
		if err != nil || !bytes.Equal(stored, side.data) {
			t.Fatalf("stored %s differs from upload: %v", field, err)
		}
	}
	if _, err := readDocumentSide(nil, "Back"); err == nil {
		t.Fatal("missing back was accepted")
	}
}

func TestDocumentSideRejectsInvalidAndOversizedFiles(t *testing.T) {
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"invalid", []byte("not an image")},
		{"oversized", bytes.Repeat([]byte("x"), (5<<20)+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("front", "front.jpg")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write(test.data); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			if err := req.ParseMultipartForm(1 << 20); err != nil {
				t.Fatal(err)
			}
			defer req.MultipartForm.RemoveAll()
			if _, err := readDocumentSide(req.MultipartForm.File["front"], "Front"); err == nil {
				t.Fatal("invalid upload was accepted")
			}
		})
	}
}

func TestIdentityDocumentJSON(t *testing.T) {
	doc := models.IdentityDocument{ID: 42, StorageKey: "private-front", BackStorageKey: "private-back", BackOriginalName: "back.jpg"}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result["id"] != float64(42) || result["back_original_name"] != "back.jpg" || strings.Contains(string(data), "private-") {
		t.Fatalf("unexpected document JSON: %s", data)
	}
}

func TestDocumentUploadReviewIntegration(t *testing.T) {
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
	if err := db.AutoMigrate(&models.Country{}, &models.User{}, &models.DriverProfile{}, &models.IdentityDocument{}); err != nil {
		t.Fatal(err)
	}
	oldDB := config.DB
	config.DB = db
	defer func() { config.DB = oldDB }()
	t.Chdir(t.TempDir())

	driver := models.User{FullName: "Test Driver", PasswordHash: "unused", Role: "driver", Status: "pending"}
	if err := db.Create(&driver).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.DriverProfile{UserID: driver.ID, VerificationStatus: "pending"}).Error; err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	userID := driver.ID
	role := "driver"
	router.Use(func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Set("role", role)
		c.Next()
	})
	router.POST("/documents", UploadDriverDocument)
	router.GET("/documents/:id/file", DownloadDriverDocument)
	router.PATCH("/documents/:id", ReviewDriverDocument)
	router.GET("/verification", middleware.RequireRoles("driver"), GetOwnDriverVerification)
	router.PATCH("/users/:id/status", middleware.RequireRoles("country_admin", "super_admin"), UpdateUserStatus)

	upload := func(documentType string, includeBack bool) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if err := writer.WriteField("document_type", documentType); err != nil {
			t.Fatal(err)
		}
		fields := []string{"front"}
		if includeBack {
			fields = append(fields, "back")
		}
		for _, field := range fields {
			part, err := writer.CreateFormFile(field, field+".jpg")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write([]byte{0xff, 0xd8, 0xff, 0xdb, 0, 1, 2}); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/documents", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp
	}
	if resp := upload("national_id", false); resp.Code != http.StatusBadRequest {
		t.Fatalf("missing back: %d %s", resp.Code, resp.Body)
	}
	verificationResponse := httptest.NewRecorder()
	router.ServeHTTP(verificationResponse, httptest.NewRequest(http.MethodGet, "/verification", nil))
	if verificationResponse.Code != http.StatusOK ||
		!strings.Contains(verificationResponse.Body.String(), `"verification_status":"pending"`) ||
		!strings.Contains(verificationResponse.Body.String(), `"documents":[]`) {
		t.Fatalf("initial verification: %d %s", verificationResponse.Code, verificationResponse.Body)
	}
	role, userID = "super_admin", driver.ID+10000
	activate := func() *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/users/%d/status", driver.ID), strings.NewReader(`{"status":"active"}`))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp
	}
	if resp := activate(); resp.Code != http.StatusConflict || !strings.Contains(resp.Body.String(), "national ID") {
		t.Fatalf("unverified activation: %d %s", resp.Code, resp.Body)
	}
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/verification", nil))
	if resp.Code != http.StatusForbidden {
		t.Fatalf("non-driver verification access: %d %s", resp.Code, resp.Body)
	}
	role, userID = "driver", driver.ID
	var count int64
	if err := db.Model(&models.IdentityDocument{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("missing back created a record: count=%d error=%v", count, err)
	}
	legacy := models.IdentityDocument{UserID: driver.ID, DocumentType: "national_id", StorageKey: "old.jpg", Status: "pending"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	legacyRequest := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/documents/%d", legacy.ID), strings.NewReader(`{"status":"approved"}`))
	legacyRequest.Header.Set("Content-Type", "application/json")
	legacyResponse := httptest.NewRecorder()
	router.ServeHTTP(legacyResponse, legacyRequest)
	if legacyResponse.Code != http.StatusConflict {
		t.Fatalf("legacy approval: %d %s", legacyResponse.Code, legacyResponse.Body)
	}
	for _, documentType := range []string{"national_id", "driver_license"} {
		resp := upload(documentType, true)
		if resp.Code != http.StatusCreated {
			t.Fatalf("upload: %d %s", resp.Code, resp.Body)
		}
		var result struct {
			Document models.IdentityDocument `json:"document"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil || result.Document.ID == 0 {
			t.Fatalf("invalid document response: %s (%v)", resp.Body, err)
		}
		for _, side := range []string{"front", "back"} {
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/documents/%d/file?side=%s", result.Document.ID, side), nil)
			got := httptest.NewRecorder()
			router.ServeHTTP(got, req)
			if got.Code != http.StatusOK || got.Header().Get("Content-Type") != "image/jpeg" {
				t.Fatalf("download %s: %d %s", side, got.Code, got.Body)
			}
		}
		role, userID = "super_admin", driver.ID+10000
		req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/documents/%d", result.Document.ID), strings.NewReader(`{"status":"approved"}`))
		req.Header.Set("Content-Type", "application/json")
		review := httptest.NewRecorder()
		router.ServeHTTP(review, req)
		if review.Code != http.StatusOK {
			t.Fatalf("review: %d %s", review.Code, review.Body)
		}
		role, userID = "driver", driver.ID
		verificationResponse = httptest.NewRecorder()
		router.ServeHTTP(verificationResponse, httptest.NewRequest(http.MethodGet, "/verification", nil))
		if verificationResponse.Code != http.StatusOK ||
			strings.Contains(verificationResponse.Body.String(), "private_uploads") {
			t.Fatalf("verification response: %d %s", verificationResponse.Code, verificationResponse.Body)
		}
		if documentType == "national_id" {
			var profile models.DriverProfile
			if err := db.Where("user_id = ?", driver.ID).First(&profile).Error; err != nil || profile.VerificationStatus != "pending" {
				t.Fatalf("verification after one document = %q, error = %v", profile.VerificationStatus, err)
			}
			role, userID = "super_admin", driver.ID+10000
			if resp := activate(); resp.Code != http.StatusConflict ||
				!strings.Contains(resp.Body.String(), "driver's license") ||
				strings.Contains(resp.Body.String(), "national ID") {
				t.Fatalf("missing license activation: %d %s", resp.Code, resp.Body)
			}
			role, userID = "driver", driver.ID
		}
	}
	var profile models.DriverProfile
	if err := db.Where("user_id = ?", driver.ID).First(&profile).Error; err != nil || profile.VerificationStatus != "approved" {
		t.Fatalf("verification status = %q, error = %v", profile.VerificationStatus, err)
	}
	if !strings.Contains(verificationResponse.Body.String(), `"verification_status":"approved"`) ||
		strings.Contains(verificationResponse.Body.String(), "storage_key") {
		t.Fatalf("approved verification response: %s", verificationResponse.Body)
	}
	role, userID = "super_admin", driver.ID+10000
	reject := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/documents/%d", legacy.ID), strings.NewReader(`{"status":"rejected","rejection_reason":"Upload both sides"}`))
	reject.Header.Set("Content-Type", "application/json")
	rejected := httptest.NewRecorder()
	router.ServeHTTP(rejected, reject)
	if rejected.Code != http.StatusOK {
		t.Fatalf("reject redundant legacy document: %d %s", rejected.Code, rejected.Body)
	}
	if err := db.Where("user_id = ?", driver.ID).First(&profile).Error; err != nil || profile.VerificationStatus != "approved" {
		t.Fatalf("rejected duplicate changed approved verification = %q, error = %v", profile.VerificationStatus, err)
	}
	if err := db.Model(&profile).Update("verification_status", "rejected").Error; err != nil {
		t.Fatal(err)
	}
	role, userID = "driver", driver.ID
	verificationResponse = httptest.NewRecorder()
	router.ServeHTTP(verificationResponse, httptest.NewRequest(http.MethodGet, "/verification", nil))
	if verificationResponse.Code != http.StatusOK ||
		!strings.Contains(verificationResponse.Body.String(), `"verification_status":"approved"`) ||
		!strings.Contains(verificationResponse.Body.String(), `"rejection_reason":"Upload both sides"`) {
		t.Fatalf("stale verification status: %d %s", verificationResponse.Code, verificationResponse.Body)
	}
	role, userID = "super_admin", driver.ID+10000
	if resp := activate(); resp.Code != http.StatusOK {
		t.Fatalf("verified activation: %d %s", resp.Code, resp.Body)
	}
	if err := db.First(&driver, driver.ID).Error; err != nil || driver.Status != "active" {
		t.Fatalf("driver activation status = %q, error = %v", driver.Status, err)
	}
	if err := db.Where("user_id = ?", driver.ID).First(&profile).Error; err != nil || profile.VerificationStatus != "approved" {
		t.Fatalf("activation did not repair verification = %q, error = %v", profile.VerificationStatus, err)
	}
}
