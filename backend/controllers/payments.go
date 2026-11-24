package controllers

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"travo/backend/config"
	"travo/backend/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const paymentCommissionPercent = 5

type fapshiPaymentRequest struct {
	Amount      int64  `json:"amount"`
	Email       string `json:"email,omitempty"`
	RedirectURL string `json:"redirectUrl,omitempty"`
	UserID      string `json:"userId"`
	ExternalID  string `json:"externalId"`
	Message     string `json:"message"`
}

type fapshiPaymentResponse struct {
	Link    string `json:"link"`
	TransID string `json:"transId"`
}

type fapshiTransaction struct {
	TransID    string `json:"transId"`
	Status     string `json:"status"`
	Amount     int64  `json:"amount"`
	Revenue    int64  `json:"revenue"`
	ExternalID string `json:"externalId"`
	TransType  string `json:"transType"`
}

func StartBookingPayment(c *gin.Context) {
	bookingID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid booking"})
		return
	}

	var booking models.Booking
	if err := config.DB.Preload("Passenger").First(&booking, bookingID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Booking not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load booking"})
		}
		return
	}
	if booking.PassengerID != c.GetUint("user_id") {
		c.JSON(http.StatusNotFound, gin.H{"error": "Booking not found"})
		return
	}

	var claimed models.Booking
	err = config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&claimed, bookingID).Error; err != nil {
			return err
		}
		if claimed.PassengerID != c.GetUint("user_id") {
			return gorm.ErrRecordNotFound
		}
		if claimed.Status != "pending_payment" || claimed.PaymentStatus != "not_started" {
			return errPaymentAlreadyStarted
		}
		return tx.Model(&claimed).Update("payment_status", "creating").Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Booking not found"})
		return
	}
	if errors.Is(err, errPaymentAlreadyStarted) {
		c.JSON(http.StatusConflict, gin.H{"error": "A payment has already started or this booking is no longer payable"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not start payment"})
		return
	}

	payment, err := createFapshiPayment(booking)
	if err != nil {
		if errors.Is(err, errPaymentOutcomeUnknown) {
			if updateErr := config.DB.Model(&claimed).Update("payment_status", "unknown").Error; updateErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":      "Payment initialization is uncertain and its state could not be saved. Do not retry; contact support with your booking ID.",
					"booking_id": booking.ID,
				})
				return
			}
			c.JSON(http.StatusBadGateway, gin.H{
				"error":      "Payment initialization could not be confirmed. Do not retry; contact support with your booking ID.",
				"booking_id": booking.ID,
			})
			return
		}
		if updateErr := config.DB.Model(&claimed).Update("payment_status", "not_started").Error; updateErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Checkout failed and booking payment state could not be reset"})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	result := config.DB.Model(&claimed).Updates(map[string]any{
		"payment_transaction_id": payment.TransID,
		"payment_url":            payment.Link,
		"payment_status":         "CREATED",
	})
	if result.Error != nil || result.RowsAffected != 1 {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Checkout was created but could not be saved. Contact support with booking ID " + strconv.FormatUint(uint64(booking.ID), 10),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"payment_url":                payment.Link,
		"amount_charged":             booking.AmountCharged,
		"fare":                       booking.FareTotal,
		"estimated_mobile_money_fee": booking.ProviderFee,
		"commission":                 booking.CommissionAmount,
	})
}

var (
	errPaymentAlreadyStarted = errors.New("payment already started")
	errPaymentOutcomeUnknown = errors.New("payment outcome unknown")
)

func createFapshiPayment(booking models.Booking) (fapshiPaymentResponse, error) {
	apiUser := strings.TrimSpace(os.Getenv("FAPSHI_API_USER"))
	apiKey := strings.TrimSpace(os.Getenv("FAPSHI_API_KEY"))
	if apiUser == "" || apiKey == "" {
		return fapshiPaymentResponse{}, errors.New("Mobile-money checkout is not configured. Please contact support.")
	}

	baseURL, err := fapshiBaseURL()
	if err != nil {
		return fapshiPaymentResponse{}, err
	}
	requestBody := fapshiPaymentRequest{
		Amount:     booking.AmountCharged,
		UserID:     strconv.FormatUint(uint64(booking.PassengerID), 10),
		ExternalID: strconv.FormatUint(uint64(booking.ID), 10),
		Message:    fmt.Sprintf("Travo ride booking %d", booking.ID),
	}
	if booking.Passenger.Email != nil {
		requestBody.Email = *booking.Passenger.Email
	}
	requestBody.RedirectURL = strings.TrimSpace(os.Getenv("FAPSHI_REDIRECT_URL"))
	body, err := json.Marshal(requestBody)
	if err != nil {
		return fapshiPaymentResponse{}, err
	}
	request, err := http.NewRequest(http.MethodPost, baseURL+"/initiate-pay", bytes.NewReader(body))
	if err != nil {
		return fapshiPaymentResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("apiuser", apiUser)
	request.Header.Set("apikey", apiKey)

	client := &http.Client{Timeout: 20 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return fapshiPaymentResponse{}, errPaymentOutcomeUnknown
	}
	defer response.Body.Close()
	responseBytes, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fapshiPaymentResponse{}, errPaymentOutcomeUnknown
	}
	if response.StatusCode != http.StatusOK {
		if response.StatusCode >= 500 {
			return fapshiPaymentResponse{}, errPaymentOutcomeUnknown
		}
		var providerError struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(responseBytes, &providerError)
		if providerError.Message == "" {
			providerError.Message = "Mobile-money provider rejected the checkout request"
		}
		return fapshiPaymentResponse{}, errors.New(providerError.Message)
	}
	var result fapshiPaymentResponse
	if err := json.Unmarshal(responseBytes, &result); err != nil ||
		result.TransID == "" || result.Link == "" {
		return fapshiPaymentResponse{}, errPaymentOutcomeUnknown
	}
	parsedLink, err := url.Parse(result.Link)
	if err != nil || parsedLink.Scheme != "https" || parsedLink.Host == "" {
		return fapshiPaymentResponse{}, errPaymentOutcomeUnknown
	}
	return result, nil
}

func fapshiBaseURL() (string, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("FAPSHI_MODE"))) {
	case "", "sandbox":
		return "https://sandbox.fapshi.com", nil
	case "live":
		return "https://live.fapshi.com", nil
	default:
		return "", errors.New("FAPSHI_MODE must be sandbox or live")
	}
}

func BookingPaymentStatus(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	bookingID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid booking"})
		return
	}
	var booking models.Booking
	if err := config.DB.First(&booking, bookingID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Booking not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load booking"})
		}
		return
	}
	if booking.PassengerID != c.GetUint("user_id") {
		c.JSON(http.StatusNotFound, gin.H{"error": "Booking not found"})
		return
	}
	if booking.PaymentTransactionID == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "This booking has no mobile-money transaction yet"})
		return
	}
	transaction, err := getFapshiTransaction(*booking.PaymentTransactionID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Could not verify the payment with the provider"})
		return
	}
	if err := applyFapshiTransaction(booking, transaction); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Provider payment details did not match this booking"})
		return
	}
	var refreshed models.Booking
	if err := config.DB.Preload("Trip").Preload("Trip.Driver").First(&refreshed, booking.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not refresh booking"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"booking": passengerBookingView(refreshed)})
}

func FapshiWebhook(c *gin.Context) {
	secret := os.Getenv("FAPSHI_WEBHOOK_SECRET")
	if secret == "" || subtle.ConstantTimeCompare(
		[]byte(c.GetHeader("x-wh-secret")),
		[]byte(secret),
	) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid webhook credentials"})
		return
	}
	var input struct {
		TransID string `json:"transId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payment notification"})
		return
	}
	var booking models.Booking
	if err := config.DB.Where("payment_transaction_id = ?", input.TransID).First(&booking).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Payment transaction not found"})
		return
	}
	transaction, err := getFapshiTransaction(input.TransID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Could not verify payment notification"})
		return
	}
	if err := applyFapshiTransaction(booking, transaction); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Payment details did not match this booking"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Payment notification processed"})
}

func getFapshiTransaction(transID string) (fapshiTransaction, error) {
	apiUser := strings.TrimSpace(os.Getenv("FAPSHI_API_USER"))
	apiKey := strings.TrimSpace(os.Getenv("FAPSHI_API_KEY"))
	if apiUser == "" || apiKey == "" {
		return fapshiTransaction{}, errors.New("Fapshi credentials are not configured")
	}
	baseURL, err := fapshiBaseURL()
	if err != nil {
		return fapshiTransaction{}, err
	}
	request, err := http.NewRequest(http.MethodGet, baseURL+"/payment-status/"+url.PathEscape(transID), nil)
	if err != nil {
		return fapshiTransaction{}, err
	}
	request.Header.Set("apiuser", apiUser)
	request.Header.Set("apikey", apiKey)
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		return fapshiTransaction{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fapshiTransaction{}, fmt.Errorf("provider returned status %d", response.StatusCode)
	}
	var transaction fapshiTransaction
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&transaction); err != nil {
		return fapshiTransaction{}, err
	}
	return transaction, nil
}

func applyFapshiTransaction(booking models.Booking, transaction fapshiTransaction) error {
	if booking.PaymentTransactionID == nil ||
		transaction.TransID != *booking.PaymentTransactionID ||
		transaction.ExternalID != strconv.FormatUint(uint64(booking.ID), 10) ||
		transaction.Amount != booking.AmountCharged ||
		transaction.TransType != "Collection" {
		return errors.New("provider transaction does not match booking")
	}
	status := strings.ToUpper(transaction.Status)
	if status != "CREATED" && status != "PENDING" && status != "SUCCESSFUL" &&
		status != "FAILED" && status != "EXPIRED" {
		return errors.New("unknown provider status")
	}
	if status == "SUCCESSFUL" && (transaction.Revenue < 0 || transaction.Revenue > transaction.Amount) {
		return errors.New("invalid provider revenue")
	}

	return config.DB.Transaction(func(tx *gorm.DB) error {
		var trip models.Trip
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&trip, booking.TripID).Error; err != nil {
			return err
		}
		if status == "SUCCESSFUL" && trip.Status != "published" {
			return errors.New("trip is no longer available for payment confirmation")
		}
		var current models.Booking
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, booking.ID).Error; err != nil {
			return err
		}
		if current.PaymentTransactionID == nil || *current.PaymentTransactionID != transaction.TransID {
			return errors.New("booking transaction changed")
		}
		if current.Status != "pending_payment" {
			return nil
		}
		updates := map[string]any{"payment_status": status}
		switch status {
		case "SUCCESSFUL":
			updates["status"] = "confirmed"
			updates["provider_revenue"] = transaction.Revenue
			updates["provider_fee"] = transaction.Amount - transaction.Revenue
		case "FAILED", "EXPIRED":
			updates["status"] = "cancelled"
			if err := tx.Model(&models.Trip{}).
				Where("id = ? AND status = ?", current.TripID, "published").
				Update("available_seats", gorm.Expr("available_seats + ?", current.Seats)).Error; err != nil {
				return err
			}
		}
		return tx.Model(&current).Updates(updates).Error
	})
}

func driverAmountDue(booking models.Booking) int64 {
	if booking.PaymentStatus != "SUCCESSFUL" {
		return 0
	}
	return booking.ProviderRevenue - booking.CommissionAmount
}
