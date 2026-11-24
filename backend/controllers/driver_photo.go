package controllers

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"travo/backend/config"
	"travo/backend/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type driverPhotoView struct {
	ID               uint       `json:"id"`
	ReviewStatus     string     `json:"review_status"`
	ReviewDocumentID *uint      `json:"review_document_id,omitempty"`
	ReviewedAt       *time.Time `json:"reviewed_at,omitempty"`
}

func toDriverPhotoView(photo models.DriverPhoto) driverPhotoView {
	return driverPhotoView{ID: photo.ID, ReviewStatus: photo.ReviewStatus, ReviewDocumentID: photo.ReviewDocumentID, ReviewedAt: photo.ReviewedAt}
}

func latestDriverPhoto(userID uint) (models.DriverPhoto, error) {
	var photo models.DriverPhoto
	err := config.DB.Where("user_id = ?", userID).Order("id DESC").First(&photo).Error
	return photo, err
}

func GetOwnDriverPhoto(c *gin.Context) {
	photo, err := latestDriverPhoto(c.GetUint("user_id"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusOK, gin.H{"photo": nil})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load profile photo"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"photo": toDriverPhotoView(photo)})
}

func UploadDriverPhoto(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 6<<20)
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Photo must be 5 MB or smaller"})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Could not read photo upload"})
		}
		return
	}
	defer func() {
		if err := c.Request.MultipartForm.RemoveAll(); err != nil {
			// Gin's error logger records cleanup failures without exposing file paths to clients.
			c.Error(err)
		}
	}()
	if c.Request.FormValue("consent") != "true" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Consent is required to store and share the profile photo"})
		return
	}
	side, err := readDocumentSide(c.Request.MultipartForm.File["photo"], "Photo")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if side.mimeType != "image/jpeg" && side.mimeType != "image/png" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Photo must be a JPEG or PNG"})
		return
	}
	dimensions, _, err := image.DecodeConfig(bytes.NewReader(side.data))
	if err != nil || dimensions.Width <= 0 || dimensions.Height <= 0 ||
		int64(dimensions.Width)*int64(dimensions.Height) > 25_000_000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Photo must be a valid JPEG or PNG with at most 25 megapixels"})
		return
	}
	if _, _, err := image.Decode(bytes.NewReader(side.data)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Photo must be a complete JPEG or PNG image"})
		return
	}
	directory := filepath.Join("private_uploads", "driver_photos")
	if err := os.MkdirAll(directory, 0700); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not store profile photo"})
		return
	}
	key, err := storeDocumentSide(directory, side)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not store profile photo"})
		return
	}
	photo := models.DriverPhoto{
		UserID: c.GetUint("user_id"), StorageKey: key, MIMEType: side.mimeType,
		ConsentAt: time.Now().UTC(), ReviewStatus: "pending",
	}
	if err := config.DB.Create(&photo).Error; err != nil {
		removeStoredDocument(directory, key)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not save profile photo"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"photo": toDriverPhotoView(photo)})
}

func DownloadDriverPhoto(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid driver"})
		return
	}
	var driver models.User
	err = config.DB.Where("id = ? AND role = ?", id, "driver").First(&driver).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Photo not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load profile photo"})
		return
	}

	allowed := false
	switch c.GetString("role") {
	case "driver":
		allowed = driver.ID == c.GetUint("user_id")
	case "super_admin":
		allowed = true
	case "country_admin":
		allowed = driver.CountryID != nil && *driver.CountryID == c.GetUint("country_id")
	case "passenger":
		var count int64
		err := config.DB.Model(&models.Booking{}).
			Joins("JOIN trips ON trips.id = bookings.trip_id AND trips.deleted_at IS NULL").
			Where("bookings.passenger_id = ? AND trips.driver_id = ? AND bookings.status IN ?",
				c.GetUint("user_id"), driver.ID, []string{"pending_payment", "confirmed"}).Count(&count).Error
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not check photo access"})
			return
		}
		allowed = count > 0
	}
	if !allowed {
		c.JSON(http.StatusNotFound, gin.H{"error": "Photo not found"})
		return
	}
	photo, err := latestDriverPhoto(driver.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Photo not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load profile photo"})
		return
	}
	path := filepath.Join("private_uploads", "driver_photos", filepath.Base(photo.StorageKey))
	if _, err := os.Stat(path); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Photo file is unavailable"})
		return
	}
	c.Header("Content-Type", photo.MIMEType)
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.File(path)
}

func ReviewDriverPhoto(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid driver"})
		return
	}
	var input struct {
		PhotoID    uint   `json:"photo_id" binding:"required"`
		DocumentID uint   `json:"document_id" binding:"required"`
		Status     string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil ||
		(input.Status != "appears_match" && input.Status != "does_not_match" && input.Status != "unclear") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provide the photo, national ID, and manual review result"})
		return
	}
	var driver models.User
	query := config.DB.Where("id = ? AND role = ?", id, "driver")
	if countryID, scoped := adminCountryScope(c); scoped {
		query = query.Where("country_id = ?", countryID)
	}
	if err := query.First(&driver).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Driver not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load driver"})
		}
		return
	}
	var document models.IdentityDocument
	err = config.DB.Where("id = ? AND user_id = ? AND document_type = ?", input.DocumentID, driver.ID, "national_id").First(&document).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Choose a national ID belonging to this driver"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load identity document"})
		return
	}
	photo, err := latestDriverPhoto(driver.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && photo.ID != input.PhotoID {
		c.JSON(http.StatusConflict, gin.H{"error": "The photo changed; reload and review the current photo"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load profile photo"})
		return
	}
	if _, err := os.Stat(filepath.Join("private_uploads", "identity", filepath.Base(document.StorageKey))); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "The national ID file is unavailable for review"})
		return
	}
	if _, err := os.Stat(filepath.Join("private_uploads", "driver_photos", filepath.Base(photo.StorageKey))); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "The selfie file is unavailable for review"})
		return
	}
	now := time.Now().UTC()
	if err := config.DB.Model(&photo).Updates(map[string]any{
		"review_status": input.Status, "review_document_id": document.ID,
		"reviewed_by": c.GetUint("user_id"), "reviewed_at": now,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not save manual photo review"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Manual photo review saved"})
}
