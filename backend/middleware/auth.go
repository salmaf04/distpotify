package middleware

import (
	"distributed-systems-project/models"
	"log"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

var jwtSecret = []byte("super-secret-key-change-this")

func Protected(db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		log.Printf("HOLA MUNDO MIDDLEWasdasdARE")

		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Missing Authorization header"})
		}

		tokenString := strings.Replace(authHeader, "Bearer ", "", 1)
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})

		if err != nil || !token.Valid {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid or expired token"})
		}

		claims := token.Claims.(jwt.MapClaims)
		userID := uint(claims["user_id"].(float64))
		tokenSessionID, _ := claims["session_id"].(string)

		var user models.User

		if err := db.First(&user, userID).Error; err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "User not found"})
		}

		if user.SessionID != tokenSessionID {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Sesión inválida. Se ha iniciado sesión en otro dispositivo.",
			})
		}

		c.Locals("user", claims)
		return c.Next()
	}
}

func AdminOnly() fiber.Handler {
	return func(c *fiber.Ctx) error {
		user := c.Locals("user").(jwt.MapClaims)
		role := user["role"].(string)

		if models.Role(role) != models.RoleAdmin {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Admin access required"})
		}

		return c.Next()
	}
}
