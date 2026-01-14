package handlers

import (
	"distributed-systems-project/models"
	"fmt"
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
	DB               *gorm.DB
	OnSessionCreated func(session models.Session)
}

var jwtSecret = []byte("super-secret-key-change-this")

func (h *AuthHandler) CreateUser(input RegisterInput) (*models.User, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	role := models.RoleUser

	if input.Role == "admin" {
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

func (h *AuthHandler) Login(c *fiber.Ctx) (*models.User, *models.Session, error) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := c.BodyParser(&input); err != nil {
		return nil, nil, err
	}

	var user models.User
	if err := h.DB.Where("username = ?", input.Username).First(&user).Error; err != nil {
		return nil, nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.Password)); err != nil {
		return nil, nil, err
	}

	existingSession := models.Session{}
	if err := h.DB.Where("user_id = ?", user.ID).First(&existingSession).Error; err == nil {
		return nil, nil, fmt.Errorf("user already has an active session")
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
		return nil, nil, err
	}

	// Crear cookie (esto lo manejará el handler principal)
	// La cookie se creará en el Server.loginHandler

	return &user, &session, nil
}
