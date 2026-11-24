package controllers

import (
	"errors"
	"net/http"

	"travo/backend/config"
	"travo/backend/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type driverVerificationDocument struct {
	ID               uint   `json:"id"`
	DocumentType     string `json:"document_type"`
	OriginalName     string `json:"original_name"`
	BackOriginalName string `json:"back_original_name"`
	Status           string `json:"status"`
	RejectionReason  string `json:"rejection_reason,omitempty"`
}

func GetOwnDriverVerification(c *gin.Context) {
	userID := c.GetUint("user_id")

	var profile models.DriverProfile
	if err := config.DB.Where("user_id = ?", userID).First(&profile).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Driver verification profile not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load driver verification"})
		}
		return
	}

	var records []models.IdentityDocument
	if err := config.DB.Where("user_id = ?", userID).Order("created_at DESC").Find(&records).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load verification documents"})
		return
	}

	documents := make([]driverVerificationDocument, 0, len(records))
	for _, record := range records {
		documents = append(documents, driverVerificationDocument{
			ID: record.ID, DocumentType: record.DocumentType,
			OriginalName: record.OriginalName, BackOriginalName: record.BackOriginalName,
			Status: record.Status, RejectionReason: record.RejectionReason,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"verification_status": driverVerificationStatus(profile.VerificationStatus, records),
		"documents":           documents,
	})
}
