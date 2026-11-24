package controllers

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"travo/backend/config"
	"travo/backend/models"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errDriverNotVerified = errors.New("driver documents are not approved")

func missingApprovedDriverDocuments(documents []models.IdentityDocument) []string {
	var nationalID, license bool
	for _, document := range documents {
		if document.Status != "approved" || document.BackStorageKey == "" {
			continue
		}
		switch document.DocumentType {
		case "national_id":
			nationalID = true
		case "driver_license":
			license = true
		}
	}
	var missing []string
	if !nationalID {
		missing = append(missing, "national ID")
	}
	if !license {
		missing = append(missing, "driver's license")
	}
	return missing
}

func hasApprovedDriverDocuments(documents []models.IdentityDocument) bool {
	return len(missingApprovedDriverDocuments(documents)) == 0
}

func driverVerificationStatus(status string, documents []models.IdentityDocument) string {
	if hasApprovedDriverDocuments(documents) {
		return "approved"
	}
	return status
}

func AdminOverview(c *gin.Context) {
	query := config.DB.Model(&models.User{})
	if countryID, scoped := adminCountryScope(c); scoped {
		query = query.Where("country_id = ?", countryID)
	}

	var users, drivers, passengers, trips, bookings int64
	if err := query.Count(&users).Error; err != nil {
		adminQueryError(c)
		return
	}
	driverQuery := config.DB.Model(&models.User{}).Where("role = ?", "driver")
	passengerQuery := config.DB.Model(&models.User{}).Where("role = ?", "passenger")
	tripQuery := config.DB.Model(&models.Trip{})
	bookingQuery := config.DB.Model(&models.Booking{})
	if countryID, scoped := adminCountryScope(c); scoped {
		driverQuery = driverQuery.Where("country_id = ?", countryID)
		passengerQuery = passengerQuery.Where("country_id = ?", countryID)
		tripQuery = tripQuery.Where("country_id = ?", countryID)
		bookingQuery = bookingQuery.Where("trip_id IN (SELECT id FROM trips WHERE country_id = ?)", countryID)
	}
	for _, count := range []struct {
		query *gorm.DB
		value *int64
	}{
		{driverQuery, &drivers},
		{passengerQuery, &passengers},
		{tripQuery, &trips},
		{bookingQuery, &bookings},
	} {
		if err := count.query.Count(count.value).Error; err != nil {
			adminQueryError(c)
			return
		}
	}

	commissionQuery := config.DB.Model(&models.Booking{}).
		Where("payment_status = ?", "SUCCESSFUL")
	if countryID, scoped := adminCountryScope(c); scoped {
		commissionQuery = commissionQuery.Where("trip_id IN (SELECT id FROM trips WHERE country_id = ?)", countryID)
	}
	var commission int64
	if err := commissionQuery.Select("COALESCE(SUM(commission_amount), 0)").Scan(&commission).Error; err != nil {
		adminQueryError(c)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"users": users, "drivers": drivers, "passengers": passengers,
		"trips": trips, "bookings": bookings, "commission_xaf": commission,
	})
}

type adminUserView struct {
	ID        uint      `json:"id"`
	FullName  string    `json:"full_name"`
	Email     *string   `json:"email"`
	Phone     *string   `json:"phone"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	CountryID *uint     `json:"country_id"`
	CreatedAt time.Time `json:"created_at"`
}

func ListAdminUsers(c *gin.Context) {
	query := config.DB.Model(&models.User{})
	if countryID, scoped := adminCountryScope(c); scoped {
		query = query.Where("country_id = ?", countryID)
	} else if requested := c.Query("country_id"); requested != "" {
		query = query.Where("country_id = ?", requested)
	}

	var users []models.User
	if err := query.Order("created_at DESC").Limit(300).Find(&users).Error; err != nil {
		adminQueryError(c)
		return
	}
	result := make([]adminUserView, 0, len(users))
	for _, user := range users {
		result = append(result, adminUserView{
			ID: user.ID, FullName: user.FullName, Email: user.Email,
			Phone: user.Phone, Role: user.Role, Status: user.Status,
			CountryID: user.CountryID, CreatedAt: user.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"users": result})
}

func UpdateUserStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user"})
		return
	}
	var input struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil ||
		input.Status != "active" && input.Status != "suspended" && input.Status != "disabled" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Status must be active, suspended, or disabled"})
		return
	}
	if uint(id) == c.GetUint("user_id") {
		c.JSON(http.StatusConflict, gin.H{"error": "You cannot change your own account status"})
		return
	}

	var user models.User
	query := config.DB.First(&user, id)
	if countryID, scoped := adminCountryScope(c); scoped {
		query = config.DB.Where("country_id = ?", countryID).First(&user, id)
	}
	if err := query.Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}
	if user.Role == "super_admin" || user.Role == "country_admin" && c.GetString("role") != "super_admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "You cannot change this administrator account"})
		return
	}
	verificationError := "Approve the driver's national ID and driver's license before activating the account"
	err = config.DB.Transaction(func(tx *gorm.DB) error {
		if input.Status == "active" && user.Role == "driver" {
			var profile models.DriverProfile
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", user.ID).First(&profile).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errDriverNotVerified
				}
				return err
			}
			if profile.VerificationStatus != "approved" {
				var documents []models.IdentityDocument
				if err := tx.Where("user_id = ?", user.ID).Find(&documents).Error; err != nil {
					return err
				}
				missing := missingApprovedDriverDocuments(documents)
				if len(missing) > 0 {
					verb := "is"
					if len(missing) > 1 {
						verb = "are"
					}
					verificationError = fmt.Sprintf("Approved two-sided %s %s required before activating the account", strings.Join(missing, " and "), verb)
					return errDriverNotVerified
				}
				if err := tx.Model(&profile).Update("verification_status", "approved").Error; err != nil {
					return err
				}
			}
		}
		return tx.Model(&user).Update("status", input.Status).Error
	})
	if errors.Is(err, errDriverNotVerified) {
		c.JSON(http.StatusConflict, gin.H{"error": verificationError})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not update user status"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "User status updated"})
}

type driverReview struct {
	UserID             uint                      `json:"user_id"`
	FullName           string                    `json:"full_name"`
	Email              *string                   `json:"email"`
	Phone              *string                   `json:"phone"`
	AccountStatus      string                    `json:"account_status"`
	VerificationStatus string                    `json:"verification_status"`
	Documents          []models.IdentityDocument `json:"documents"`
	Photo              *driverPhotoView          `json:"photo"`
}

func ListDriverReviews(c *gin.Context) {
	query := config.DB.Model(&models.User{}).Where("role = ?", "driver")
	if countryID, scoped := adminCountryScope(c); scoped {
		query = query.Where("country_id = ?", countryID)
	} else if countryID := c.Query("country_id"); countryID != "" {
		query = query.Where("country_id = ?", countryID)
	}
	var users []models.User
	if err := query.Order("created_at DESC").Limit(200).Find(&users).Error; err != nil {
		adminQueryError(c)
		return
	}
	result := make([]driverReview, 0, len(users))
	for _, user := range users {
		var profile models.DriverProfile
		if err := config.DB.Where("user_id = ?", user.ID).First(&profile).Error; err != nil {
			adminQueryError(c)
			return
		}
		var documents []models.IdentityDocument
		if err := config.DB.Where("user_id = ?", user.ID).Order("created_at DESC").Find(&documents).Error; err != nil {
			adminQueryError(c)
			return
		}
		var photo models.DriverPhoto
		photoErr := config.DB.Where("user_id = ?", user.ID).Order("id DESC").First(&photo).Error
		if photoErr != nil && !errors.Is(photoErr, gorm.ErrRecordNotFound) {
			adminQueryError(c)
			return
		}
		var photoView *driverPhotoView
		if photoErr == nil {
			view := toDriverPhotoView(photo)
			photoView = &view
		}
		result = append(result, driverReview{
			UserID: user.ID, FullName: user.FullName, Email: user.Email, Phone: user.Phone,
			AccountStatus: user.Status, VerificationStatus: driverVerificationStatus(profile.VerificationStatus, documents),
			Documents: documents, Photo: photoView,
		})
	}
	c.JSON(http.StatusOK, gin.H{"drivers": result})
}

func UploadDriverDocument(c *gin.Context) {
	user, ok := getCurrentUser(c)
	if !ok {
		return
	}
	if user.Role != "driver" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only drivers can upload identity documents"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 11<<20)
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Each document side must be 5 MB or smaller"})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Could not read document upload"})
		}
		return
	}
	defer func() {
		if err := c.Request.MultipartForm.RemoveAll(); err != nil {
			log.Printf("Could not remove temporary document upload: %v", err)
		}
	}()
	documentType := c.Request.FormValue("document_type")
	if documentType != "national_id" && documentType != "driver_license" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Document type must be national_id or driver_license"})
		return
	}
	front, err := readDocumentSide(c.Request.MultipartForm.File["front"], "Front")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	back, err := readDocumentSide(c.Request.MultipartForm.File["back"], "Back")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	directory := filepath.Join("private_uploads", "identity")
	if err := os.MkdirAll(directory, 0700); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not store document"})
		return
	}
	frontKey, err := storeDocumentSide(directory, front)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not store front of document"})
		return
	}
	backKey, err := storeDocumentSide(directory, back)
	if err != nil {
		removeStoredDocument(directory, frontKey)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not store back of document"})
		return
	}

	document := models.IdentityDocument{
		UserID: user.ID, DocumentType: documentType, StorageKey: frontKey,
		OriginalName: front.name, MIMEType: front.mimeType, FileSize: int64(len(front.data)),
		BackStorageKey: backKey, BackOriginalName: back.name,
		BackMIMEType: back.mimeType, BackFileSize: int64(len(back.data)), Status: "pending",
	}
	if err := config.DB.Create(&document).Error; err != nil {
		removeStoredDocument(directory, frontKey)
		removeStoredDocument(directory, backKey)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not save document record"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"document": document})
}

type documentSide struct {
	name      string
	mimeType  string
	extension string
	data      []byte
}

func readDocumentSide(headers []*multipart.FileHeader, side string) (documentSide, error) {
	if len(headers) != 1 {
		return documentSide{}, fmt.Errorf("%s: upload exactly one file", side)
	}
	header := headers[0]
	if header.Size > 5<<20 {
		return documentSide{}, fmt.Errorf("%s: file must be 5 MB or smaller", side)
	}
	file, err := header.Open()
	if err != nil {
		return documentSide{}, fmt.Errorf("%s: could not read uploaded file", side)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (5<<20)+1))
	if err != nil {
		return documentSide{}, fmt.Errorf("%s: could not read uploaded file", side)
	}
	if len(data) > 5<<20 {
		return documentSide{}, fmt.Errorf("%s: file must be 5 MB or smaller", side)
	}
	contentType := http.DetectContentType(data)
	extension, ok := allowedDocumentExtension(contentType)
	if !ok {
		return documentSide{}, fmt.Errorf("%s: file must be a PDF, JPEG, or PNG", side)
	}
	return documentSide{name: filepath.Base(header.Filename), mimeType: contentType, extension: extension, data: data}, nil
}

func storeDocumentSide(directory string, side documentSide) (string, error) {
	var randomName [16]byte
	if _, err := rand.Read(randomName[:]); err != nil {
		return "", err
	}
	storageName := hex.EncodeToString(randomName[:]) + side.extension
	storagePath := filepath.Join(directory, storageName)
	destination, err := os.OpenFile(storagePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	if _, err := destination.Write(side.data); err != nil {
		if closeErr := destination.Close(); closeErr != nil {
			log.Printf("Could not close document file: %v", closeErr)
		}
		removeStoredDocument(directory, storageName)
		return "", err
	}
	if err := destination.Close(); err != nil {
		removeStoredDocument(directory, storageName)
		return "", err
	}
	return storageName, nil
}

func removeStoredDocument(directory, key string) {
	if err := os.Remove(filepath.Join(directory, key)); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("Could not remove document after failed upload: %v", err)
	}
}

func allowedDocumentExtension(contentType string) (string, bool) {
	switch contentType {
	case "application/pdf":
		return ".pdf", true
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	default:
		return "", false
	}
}

func DownloadDriverDocument(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document"})
		return
	}
	var document models.IdentityDocument
	query := config.DB.Preload("User").First(&document, id)
	if countryID, scoped := adminCountryScope(c); scoped {
		query = config.DB.Joins("JOIN users ON users.id = identity_documents.user_id").
			Where("users.country_id = ?", countryID).Preload("User").First(&document, id)
	}
	if err := query.Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Document not found"})
		return
	}
	key, contentType, originalName := document.StorageKey, document.MIMEType, document.OriginalName
	switch c.DefaultQuery("side", "front") {
	case "front":
	case "back":
		key, contentType, originalName = document.BackStorageKey, document.BackMIMEType, document.BackOriginalName
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Side must be front or back"})
		return
	}
	if key == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Document side is unavailable"})
		return
	}
	storagePath := filepath.Join("private_uploads", "identity", filepath.Base(key))
	if _, err := os.Stat(storagePath); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Document file is unavailable"})
		return
	}
	c.Header("Content-Type", contentType)
	c.Header("X-Content-Type-Options", "nosniff")
	c.FileAttachment(storagePath, filepath.Base(originalName))
}

func ReviewDriverDocument(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document"})
		return
	}
	var input struct {
		Status          string `json:"status" binding:"required"`
		RejectionReason string `json:"rejection_reason"`
	}
	if err := c.ShouldBindJSON(&input); err != nil ||
		input.Status != "approved" && input.Status != "rejected" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Document status must be approved or rejected"})
		return
	}
	var document models.IdentityDocument
	query := config.DB.Preload("User").First(&document, id)
	if countryID, scoped := adminCountryScope(c); scoped {
		query = config.DB.Joins("JOIN users ON users.id = identity_documents.user_id").
			Where("users.country_id = ?", countryID).Preload("User").First(&document, id)
	}
	if err := query.Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Document not found"})
		return
	}
	if input.Status == "approved" && document.BackStorageKey == "" {
		c.JSON(http.StatusConflict, gin.H{"error": "Both sides must be uploaded before this document can be approved"})
		return
	}
	reviewerID := c.GetUint("user_id")
	now := time.Now()
	err = config.DB.Transaction(func(tx *gorm.DB) error {
		var profile models.DriverProfile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", document.UserID).First(&profile).Error; err != nil {
			return err
		}
		if err := tx.Model(&document).Updates(map[string]any{
			"status": input.Status, "rejection_reason": strings.TrimSpace(input.RejectionReason),
			"reviewed_by": reviewerID, "reviewed_at": now,
		}).Error; err != nil {
			return err
		}
		var documents []models.IdentityDocument
		if err := tx.Where("user_id = ?", document.UserID).Find(&documents).Error; err != nil {
			return err
		}
		verificationStatus := "pending"
		if input.Status == "rejected" {
			verificationStatus = "rejected"
		}
		return tx.Model(&profile).Update("verification_status", driverVerificationStatus(verificationStatus, documents)).Error
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not review document"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Document review saved"})
}

func CreateCountryAdmin(c *gin.Context) {
	var input struct {
		FullName  string `json:"full_name" binding:"required"`
		Email     string `json:"email" binding:"required"`
		Phone     string `json:"phone"`
		Password  string `json:"password" binding:"required"`
		CountryID uint   `json:"country_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provide the administrator's name, email, password, and country"})
		return
	}
	input.FullName = strings.TrimSpace(input.FullName)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Phone = strings.TrimSpace(input.Phone)
	if len(input.FullName) < 2 || len(input.Password) < 8 || len(input.Password) > 72 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name must have at least 2 characters and password must contain 8–72 characters"})
		return
	}
	var country models.Country
	if err := config.DB.Where("id = ? AND is_active = ?", input.CountryID, true).First(&country).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Selected country is unavailable"})
		return
	}
	var existing models.User
	if err := config.DB.Where("LOWER(email) = ?", input.Email).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Email is already registered"})
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		adminQueryError(c)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not secure password"})
		return
	}
	user := models.User{
		FullName: input.FullName, Email: &input.Email, PasswordHash: string(hash),
		Role: "country_admin", Status: "active", CountryID: &input.CountryID,
	}
	if input.Phone != "" {
		user.Phone = &input.Phone
	}
	if err := config.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create country administrator"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Country administrator created", "user": adminUserView{
		ID: user.ID, FullName: user.FullName, Email: user.Email, Phone: user.Phone,
		Role: user.Role, Status: user.Status, CountryID: user.CountryID, CreatedAt: user.CreatedAt,
	}})
}

func CreateCountry(c *gin.Context) {
	var input struct {
		Name         string `json:"name" binding:"required"`
		ISO2         string `json:"iso2" binding:"required"`
		ISO3         string `json:"iso3"`
		CurrencyCode string `json:"currency_code"`
		PhoneCode    string `json:"phone_code"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provide country name and ISO-2 code"})
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.ISO2 = strings.ToUpper(strings.TrimSpace(input.ISO2))
	input.ISO3 = strings.ToUpper(strings.TrimSpace(input.ISO3))
	if len(input.Name) < 2 || len(input.ISO2) != 2 || input.ISO3 != "" && len(input.ISO3) != 3 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Country name and ISO codes are invalid"})
		return
	}
	var existing models.Country
	if err := config.DB.Where("iso2 = ? OR (? <> '' AND iso3 = ?)", input.ISO2, input.ISO3, input.ISO3).
		First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "A country with those ISO codes already exists"})
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		adminQueryError(c)
		return
	}
	country := models.Country{
		Name: input.Name, ISO2: input.ISO2, ISO3: input.ISO3,
		CurrencyCode: strings.ToUpper(strings.TrimSpace(input.CurrencyCode)),
		PhoneCode:    strings.TrimSpace(input.PhoneCode), IsActive: true,
	}
	if err := config.DB.Create(&country).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create country"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"country": toCountryView(country)})
}

func UpdateCountryStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid country"})
		return
	}
	var input struct {
		IsActive bool `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provide an is_active value"})
		return
	}
	result := config.DB.Model(&models.Country{}).Where("id = ?", id).Update("is_active", input.IsActive)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not update country"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Country not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Country status updated"})
}

func ListAdminCountries(c *gin.Context) {
	var countries []models.Country
	if err := config.DB.Order("name").Find(&countries).Error; err != nil {
		adminQueryError(c)
		return
	}
	result := make([]countryView, 0, len(countries))
	for _, country := range countries {
		result = append(result, toCountryView(country))
	}
	c.JSON(http.StatusOK, gin.H{"countries": result})
}

func adminCountryScope(c *gin.Context) (uint, bool) {
	if c.GetString("role") != "country_admin" {
		return 0, false
	}
	return c.GetUint("country_id"), true
}

func adminQueryError(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load platform data"})
}
