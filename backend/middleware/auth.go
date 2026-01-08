package middleware

import (
	"distributed-systems-project/models"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

var jwtSecret = []byte("super-secret-key-change-this")

func Protected(db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Obtener el session_id de las cookies
		sessionID := c.Cookies("session_id")
		if sessionID == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "No se encontró la sesión",
			})
		}
		// Buscar la sesión en la base de datos
		var session models.Session
		if err := db.Where("id = ? AND expires_at > ?", sessionID, time.Now()).First(&session).Error; err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Sesión inválida o expirada",
			})
		}
		// Obtener el usuario
		var user models.User
		if err := db.First(&user, session.UserID).Error; err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Usuario no encontrado",
			})
		}
		// Actualizar la última actividad
		session.LastActivity = time.Now()
		db.Save(&session)
		// Establecer el usuario en el contexto
		c.Locals("user", user)
		return c.Next()
	}
}

func AdminOnly() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Obtener el usuario del contexto
		user, ok := c.Locals("user").(models.User)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "No se pudo obtener la información del usuario",
			})
		}
		// Verificar si es admin
		if user.Role != models.RoleAdmin {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "Acceso denegado: se requiere rol de administrador",
			})
		}
		return c.Next()
	}
}
