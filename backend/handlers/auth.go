package handlers

import (
	"distributed-systems-project/models"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type RegisterInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type AuthHandler struct {
	DB *gorm.DB
}

var jwtSecret = []byte("super-secret-key-change-this")

func (h *AuthHandler) CreateUser(input RegisterInput) (*models.User, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	role := models.RoleUser
	if input.Role == "admin" {
		var count int64
		h.DB.Model(&models.User{}).Where("role = ?", models.RoleAdmin).Count(&count)
		if count > 0 {
			return nil, fiber.NewError(fiber.StatusForbidden, "Ya existe un administrador en el sistema")
		}
		role = models.RoleAdmin
	}

	user := models.User{
		Username: input.Username,
		Password: string(hashedPassword),
		Role:     role,
	}

	if err := h.DB.Create(&user).Error; err != nil {
		return nil, err
	}

	return &user, nil
}

func (h *AuthHandler) Register(c *fiber.Ctx) error {
	var input RegisterInput
	if err := c.BodyParser(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid input"})
	}

	user, err := h.CreateUser(input)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Could not create user"})
	}

	return c.Status(fiber.StatusCreated).JSON(user)
}

func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid input"})
	}

	var user models.User
	if err := h.DB.Where("username = ?", input.Username).First(&user).Error; err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid credentials"})
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.Password)); err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid credentials"})
	}

	existingSession := models.Session{}
	if err := h.DB.Where("user_id = ?", user.ID).First(&existingSession).Error; err == nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"error": "User already has an active session",
		})
	}

	// Generar sesión única
	sessionID := uuid.New().String()

	// Guardar sesión en BD
	session := models.Session{
		ID:           sessionID,
		UserID:       user.ID,
		IPAddress:    c.IP(),
		UserAgent:    c.Get("User-Agent"),
		LastActivity: time.Now(),
		ExpiresAt:    time.Now().Add(time.Hour * 72),
	}

	if err := h.DB.Create(&session).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Could not create session",
		})
	}

	// Crear cookie o devolver token de sesión
	c.Cookie(&fiber.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Expires:  time.Now().Add(time.Hour * 72),
		HTTPOnly: true,
		Secure:   false, // Solo HTTPS
		SameSite: "Strict",
	})

	// Limpiar datos sensibles
	userResponse := map[string]interface{}{
		"id":         user.ID,
		"username":   user.Username,
		"role":       user.Role,
		"created_at": user.CreatedAt,
	}

	return c.JSON(fiber.Map{
		"message": "Login successful",
		"user":    userResponse,
		"session": map[string]interface{}{
			"expires_at": session.ExpiresAt,
		},
	})
}
