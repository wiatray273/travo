package middleware

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"travo/backend/config"
	"travo/backend/models"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

type TokenClaims struct {
	UserID uint   `json:"user_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")

		parts := strings.SplitN(header, " ", 2)

		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Authentication required",
			})
			return
		}

		secret := os.Getenv("JWT_SECRET")

		claims := &TokenClaims{}

		token, err := jwt.ParseWithClaims(
			parts[1],
			claims,
			func(token *jwt.Token) (interface{}, error) {
				if token.Method != jwt.SigningMethodHS256 {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(secret), nil
			},
			jwt.WithIssuer("travo"),
		)

		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid or expired session",
			})
			return
		}

		var user models.User
		if err := config.DB.First(&user, claims.UserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Your account is unavailable"})
			} else {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "Authentication service is unavailable"})
			}
			return
		}
		if user.Status == "suspended" || user.Status == "disabled" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Your account is unavailable",
			})
			return
		}

		c.Set("user_id", user.ID)
		c.Set("role", user.Role)
		if user.CountryID != nil {
			c.Set("country_id", *user.CountryID)
		}

		c.Next()
	}
}

func RequireRoles(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}

	return func(c *gin.Context) {
		if _, ok := allowed[c.GetString("role")]; !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "You do not have permission to access this resource",
			})
			return
		}
		c.Next()
	}
}
