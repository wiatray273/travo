package controllers

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"travo/backend/config"
	"travo/backend/models"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type RegisterInput struct {
	FullName  string `json:"full_name" binding:"required"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Password  string `json:"password" binding:"required"`
	Role      string `json:"role" binding:"required"`
	CountryID uint   `json:"country_id" binding:"required"`
}

type LoginInput struct {
	Identifier string `json:"identifier" binding:"required"`
	Password   string `json:"password" binding:"required"`
}

type TokenClaims struct {
	UserID uint   `json:"user_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

func generateToken(user models.User) (string, error) {
	secret := os.Getenv("JWT_SECRET")

	if len(secret) < 32 {
		return "", fmt.Errorf("JWT_SECRET must contain at least 32 characters")
	}

	claims := TokenClaims{
		UserID: user.ID,
		Role:   user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "travo",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString([]byte(secret))
}

func Register(c *gin.Context) {
	var input RegisterInput

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Please provide all required registration details"})
		return
	}

	input.FullName = strings.TrimSpace(input.FullName)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Phone = strings.TrimSpace(input.Phone)
	input.Role = strings.ToLower(strings.TrimSpace(input.Role))

	if len(input.FullName) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Please enter your full name"})
		return
	}

	if len(input.Password) < 8 || len(input.Password) > 72 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password must contain 8–72 characters"})
		return
	}

	if input.Email == "" && input.Phone == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email or phone number is required"})
		return
	}

	if input.Role != "driver" && input.Role != "passenger" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Public registration is only available for drivers and passengers"})
		return
	}

	var country models.Country

	if err := config.DB.First(&country, input.CountryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Selected country is unavailable"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not verify selected country"})
		}
		return
	}
	if !country.IsActive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Selected country is unavailable"})
		return
	}

	query := config.DB.Model(&models.User{})
	switch {
	case input.Email != "" && input.Phone != "":
		query = query.Where("LOWER(email) = ? OR phone = ?", input.Email, input.Phone)
	case input.Email != "":
		query = query.Where("LOWER(email) = ?", input.Email)
	default:
		query = query.Where("phone = ?", input.Phone)
	}

	var existing models.User
	err := query.First(&existing).Error
	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Email or phone number is already registered"})
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not check registration details"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword(
		[]byte(input.Password),
		bcrypt.DefaultCost,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not secure password"})
		return
	}

	status := "active"
	verificationStatus := "not_required"
	if input.Role == "driver" {
		status = "pending"
		verificationStatus = "pending"
	}

	user := models.User{
		FullName:     input.FullName,
		PasswordHash: string(hash),
		Role:         input.Role,
		Status:       status,
		CountryID:    &input.CountryID,
	}

	if input.Email != "" {
		user.Email = &input.Email
	}

	if input.Phone != "" {
		user.Phone = &input.Phone
	}

	var token string
	err = config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}

		var profileErr error
		if input.Role == "driver" {
			profileErr = tx.Create(&models.DriverProfile{
				UserID:             user.ID,
				VerificationStatus: verificationStatus,
			}).Error
		} else {
			profileErr = tx.Create(&models.PassengerProfile{
				UserID:             user.ID,
				VerificationStatus: verificationStatus,
			}).Error
		}
		if profileErr != nil {
			return profileErr
		}
		var tokenErr error
		token, tokenErr = generateToken(user)
		return tokenErr
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Registration failed"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": map[string]string{
			"driver":    "Registration successful. Your driver account is awaiting verification.",
			"passenger": "Registration successful. You can now book a ride.",
		}[input.Role],
		"token": token,
		"user": gin.H{
			"id":         user.ID,
			"full_name":  user.FullName,
			"email":      user.Email,
			"phone":      user.Phone,
			"role":       user.Role,
			"status":     user.Status,
			"country_id": user.CountryID,
		},
	})
}

func Login(c *gin.Context) {
	var input LoginInput

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email/phone and password are required"})
		return
	}

	identifier := strings.TrimSpace(input.Identifier)

	var user models.User

	err := config.DB.Where(
		"LOWER(email) = ? OR phone = ?",
		strings.ToLower(identifier),
		identifier,
	).First(&user).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid login credentials"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not verify login credentials"})
		return
	}

	if user.Status == "suspended" || user.Status == "disabled" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Your account has been suspended or disabled"})
		return
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(user.PasswordHash),
		[]byte(input.Password),
	); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid login credentials"})
		return
	}

	token, err := generateToken(user)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create login session"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Login successful",
		"token":   token,
		"user": gin.H{
			"id":         user.ID,
			"full_name":  user.FullName,
			"email":      user.Email,
			"phone":      user.Phone,
			"role":       user.Role,
			"status":     user.Status,
			"country_id": user.CountryID,
		},
	})
}

func Me(c *gin.Context) {
	userID := c.GetUint("user_id")

	var user models.User

	if err := config.DB.First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load user"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": accountView(user)})
}

func accountView(user models.User) gin.H {
	return gin.H{
		"id": user.ID, "full_name": user.FullName, "email": user.Email,
		"phone": user.Phone, "role": user.Role, "status": user.Status, "country_id": user.CountryID,
	}
}
