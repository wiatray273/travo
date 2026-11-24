package controllers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"travo/backend/config"
)

func validDriverPhone(phone string) bool {
	digits := 0
	for i, r := range phone {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '+' && i == 0:
		case r == ' ' || r == '-' || r == '(' || r == ')':
		default:
			return false
		}
	}
	return len(phone) <= 24 && digits >= 7 && digits <= 15
}

func UpdateDriverProfile(c *gin.Context) {
	user, ok := getCurrentUser(c)
	if !ok {
		return
	}
	var input struct {
		FullName string `json:"full_name"`
		Phone    string `json:"phone"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provide your name and phone number"})
		return
	}
	input.FullName = strings.TrimSpace(input.FullName)
	input.Phone = strings.TrimSpace(input.Phone)
	if len(input.FullName) < 2 || len(input.FullName) > 120 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name must contain 2–120 characters"})
		return
	}
	if !validDriverPhone(input.Phone) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Enter a valid phone number with 7–15 digits"})
		return
	}
	updates := map[string]any{"full_name": input.FullName, "phone": input.Phone}
	if user.Phone == nil || *user.Phone != input.Phone {
		updates["phone_verified"] = false
	}
	if err := config.DB.Model(&user).Updates(updates).Error; err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			c.JSON(http.StatusConflict, gin.H{"error": "Phone number is already registered"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not update your profile"})
		}
		return
	}
	user.FullName = input.FullName
	user.Phone = &input.Phone
	c.JSON(http.StatusOK, gin.H{"user": accountView(user)})
}
