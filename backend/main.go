package main

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"

	"travo/backend/config"
	"travo/backend/controllers"
	"travo/backend/middleware"
	"travo/backend/models"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func main() {
	_ = godotenv.Load()

	if len(os.Args) > 1 {
		if len(os.Args) == 2 && os.Args[1] == "reset-superadmin-password" {
			if err := resetSuperAdminPassword(); err != nil {
				log.Fatal("Superadmin password reset failed:", err)
			}
			log.Println("Superadmin password reset successfully")
			return
		}
		log.Fatal("Usage: go run . [reset-superadmin-password]")
	}

	if len(os.Getenv("JWT_SECRET")) < 32 {
		log.Fatal("JWT_SECRET must contain at least 32 characters")
	}
	if err := config.ConnectDB(); err != nil {
		log.Fatal(err)
	}

	if err := config.DB.AutoMigrate(
		&models.Country{},
		&models.Region{},
		&models.City{},
		&models.User{},
		&models.DriverProfile{},
		&models.PassengerProfile{},
		&models.IdentityDocument{},
		&models.DriverPhoto{},
		&models.Trip{},
		&models.Booking{},
	); err != nil {
		log.Fatal("Migration failed:", err)
	}

	var cameroon models.Country
	if err := config.DB.Where("iso2 = ?", "CM").FirstOrCreate(
		&cameroon,
		models.Country{
			Name: "Cameroon", ISO2: "CM", ISO3: "CMR",
			CurrencyCode: "XAF", PhoneCode: "+237", IsActive: true,
		},
	).Error; err != nil {
		log.Fatal("Country initialization failed:", err)
	}
	if err := seedSuperAdmin(); err != nil {
		log.Fatal("Superadmin setup failed:", err)
	}

	if os.Getenv("APP_ENV") != "development" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.Default()
	router.SetTrustedProxies(nil)
	router.Use(corsMiddleware())

	router.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{"application": "Travo", "status": "running"})
	})
	router.GET("/api/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"application": "Travo", "database": "connected", "status": "healthy"})
	})

	auth := router.Group("/api/auth")
	auth.POST("/register", controllers.Register)
	auth.POST("/login", controllers.Login)

	router.GET("/api/countries", controllers.ListCountries)
	router.GET("/api/trips", controllers.ListTrips)
	router.GET("/api/trips/:id", controllers.GetTrip)
	router.POST("/api/payments/fapshi/webhook", controllers.FapshiWebhook)

	api := router.Group("/api")
	api.Use(middleware.AuthRequired())
	api.GET("/auth/me", controllers.Me)
	api.POST("/trips", middleware.RequireRoles("driver"), controllers.CreateTrip)
	api.GET("/trips/mine", middleware.RequireRoles("driver"), controllers.ListMyTrips)
	api.PATCH("/trips/:id", middleware.RequireRoles("driver"), controllers.UpdateTripStatus)
	api.POST("/trips/:id/bookings", middleware.RequireRoles("passenger"), controllers.CreateBooking)
	api.GET("/bookings", middleware.RequireRoles("passenger", "driver"), controllers.ListBookings)
	api.POST("/bookings/:id/payment", middleware.RequireRoles("passenger"), controllers.StartBookingPayment)
	api.GET("/bookings/:id/payment-status", middleware.RequireRoles("passenger"), controllers.BookingPaymentStatus)
	api.DELETE("/bookings/:id", middleware.RequireRoles("passenger"), controllers.CancelBooking)
	api.GET("/driver/verification", middleware.RequireRoles("driver"), controllers.GetOwnDriverVerification)
	api.POST("/driver/documents", middleware.RequireRoles("driver"), controllers.UploadDriverDocument)
	api.PATCH("/driver/profile", middleware.RequireRoles("driver"), controllers.UpdateDriverProfile)
	api.GET("/driver/photo", middleware.RequireRoles("driver"), controllers.GetOwnDriverPhoto)
	api.POST("/driver/photo", middleware.RequireRoles("driver"), controllers.UploadDriverPhoto)
	api.GET("/drivers/:id/photo", controllers.DownloadDriverPhoto)

	admin := api.Group("/admin")
	admin.Use(middleware.RequireRoles("country_admin", "super_admin"))
	admin.GET("/overview", controllers.AdminOverview)
	admin.GET("/users", controllers.ListAdminUsers)
	admin.PATCH("/users/:id/status", controllers.UpdateUserStatus)
	admin.GET("/drivers", controllers.ListDriverReviews)
	admin.PATCH("/drivers/:id/photo-review", controllers.ReviewDriverPhoto)
	admin.GET("/documents/:id/file", controllers.DownloadDriverDocument)
	admin.PATCH("/documents/:id", controllers.ReviewDriverDocument)
	admin.POST("/country-admins", middleware.RequireRoles("super_admin"), controllers.CreateCountryAdmin)
	admin.GET("/countries", middleware.RequireRoles("super_admin"), controllers.ListAdminCountries)
	admin.POST("/countries", middleware.RequireRoles("super_admin"), controllers.CreateCountry)
	admin.PATCH("/countries/:id", middleware.RequireRoles("super_admin"), controllers.UpdateCountryStatus)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Println("Travo API starting on port", port)
	if err := router.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}

func seedSuperAdmin() error {
	name := strings.TrimSpace(os.Getenv("SUPERADMIN_NAME"))
	email := strings.ToLower(strings.TrimSpace(os.Getenv("SUPERADMIN_EMAIL")))
	password := os.Getenv("SUPERADMIN_PASSWORD")
	if name == "" && email == "" && password == "" {
		log.Println("Superadmin seed skipped; set SUPERADMIN_NAME, SUPERADMIN_EMAIL, and SUPERADMIN_PASSWORD to create one")
		return nil
	}
	if len(name) < 2 || email == "" || len(password) < 8 || len(password) > 72 {
		return fmt.Errorf("SUPERADMIN_NAME and SUPERADMIN_EMAIL are required; SUPERADMIN_PASSWORD must contain 8–72 characters")
	}

	var user models.User
	err := config.DB.Where("LOWER(email) = ?", email).First(&user).Error
	if err == nil {
		if user.Role != "super_admin" {
			return fmt.Errorf("SUPERADMIN_EMAIL is already assigned to a non-superadmin account")
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user = models.User{
		FullName: name, Email: &email, PasswordHash: string(hash),
		Role: "super_admin", Status: "active",
	}
	return config.DB.Create(&user).Error
}

func resetSuperAdminPassword() error {
	email := strings.ToLower(strings.TrimSpace(os.Getenv("SUPERADMIN_EMAIL")))
	password := os.Getenv("SUPERADMIN_PASSWORD")
	if email == "" {
		return errors.New("SUPERADMIN_EMAIL must identify the existing superadmin")
	}
	if len(password) < 8 || len(password) > 72 {
		return errors.New("SUPERADMIN_PASSWORD must contain 8–72 characters")
	}
	if err := config.ConnectDB(); err != nil {
		return err
	}

	var user models.User
	err := config.DB.Where("LOWER(email) = ? AND role = ?", email, "super_admin").
		First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("no superadmin account matches SUPERADMIN_EMAIL")
	}
	if err != nil {
		return err
	}
	if user.Status != "active" {
		return errors.New("superadmin account is not active; password reset does not change account status")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	result := config.DB.Model(&user).Update("password_hash", string(hash))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("superadmin password was not updated")
	}
	return nil
}

func corsMiddleware() gin.HandlerFunc {
	origins := map[string]struct{}{}
	configured := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if configured == "" {
		configured = "http://localhost:5173,http://127.0.0.1:5173,capacitor://localhost"
	}
	for _, origin := range strings.Split(configured, ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			origins[origin] = struct{}{}
		}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		_, explicitlyAllowed := origins[origin]
		loopbackDevelopmentOrigin := false
		if os.Getenv("APP_ENV") == "development" {
			parsed, err := url.Parse(origin)
			if err == nil && parsed.Scheme == "http" &&
				(parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "::1") {
				loopbackDevelopmentOrigin = true
			}
		}
		if explicitlyAllowed || loopbackDevelopmentOrigin {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		}
		if c.Request.Method == "OPTIONS" {
			if origin == "" {
				c.AbortWithStatus(204)
				return
			}
			if !explicitlyAllowed && !loopbackDevelopmentOrigin {
				c.AbortWithStatus(403)
				return
			}
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
