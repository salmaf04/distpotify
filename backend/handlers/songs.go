package handlers

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"distributed-systems-project/filters"
	"distributed-systems-project/models"
	"distributed-systems-project/structs"
	"distributed-systems-project/utils"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type SongHandler struct {
	DB *gorm.DB
}

func (h *SongHandler) InsertSong(c *fiber.Ctx, songInput *structs.SongInputModel) error {
	songInputCover := ""

	if songInput.Cover != nil {
		songInputCover = *songInput.Cover
	}

	song := models.Song{
		Title:    songInput.Title,
		Artist:   songInput.Artist,
		Album:    songInput.Album,
		Genre:    songInput.Genre,
		Duration: songInput.Duration,
		File:     songInput.File,
		Cover:    songInputCover,
	}

	song.ID = 0

	// Timeout para evitar cuelgues si la DB está lenta/caída
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := h.DB.WithContext(ctx).Create(&song)
	if result.Error != nil {
		log.Printf("InsertSong: error DB Create: %v", result.Error)
		return c.Status(500).JSON(fiber.Map{"error": result.Error.Error()})
	}

	return c.Status(201).JSON(song)
}

func (h *SongHandler) UploadSong(c *fiber.Ctx) error {
	log.Println("UploadSong: start")
	file, err := c.FormFile("file")

	if err != nil {
		log.Printf("UploadSong: no form file: %v", err)
		return c.Status(fiber.StatusBadRequest).SendString("No se recibió el archivo")
	}

	if strings.ToLower(filepath.Ext(file.Filename)) != ".mp3" {
		log.Printf("UploadSong: invalid extension for %s", file.Filename)
		return c.Status(fiber.StatusBadRequest).SendString("Solo se permiten archivos con extensión .mp3")
	}

	if file.Size > 20*1024*1024 {
		log.Printf("UploadSong: file too large: %d bytes", file.Size)
		return c.Status(fiber.StatusBadRequest).SendString("Archivo demasiado grande")
	}

	title := c.FormValue("title")
	artist := c.FormValue("artist")
	album := c.FormValue("album")
	genre := c.FormValue("genre")
	duration := c.FormValue("duration")

	log.Printf("UploadSong: received file=%s size=%d title=%s artist=%s", file.Filename, file.Size, title, artist)

	savedPath, err := utils.SaveFile(c, file, artist, title)
	if err != nil {
		log.Printf("UploadSong: save error: %v", err)
		return c.Status(fiber.StatusInternalServerError).SendString("Error al guardar el archivo")
	}
	log.Printf("UploadSong: saved to %s", savedPath)

	song_to_create := &structs.SongInputModel{
		Title:    title,
		Artist:   artist,
		Album:    album,
		Genre:    genre,
		Duration: duration,
		File:     savedPath,
	}

	value := h.InsertSong(c, song_to_create)
	log.Println("UploadSong: insert finished")
	return value
}

func (h *SongHandler) GetAllSongs(c *fiber.Ctx) error {
	var songs []models.Song
	result := h.DB.Find(&songs)
	if result.Error != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": result.Error.Error()})
	}

	var response []structs.SongResponse

	for _, song := range songs {
		// Construir ruta correcta para los archivos
		filePath := getStoragePath(song.File)
		coverPath := ""
		if song.Cover != "" {
			coverPath = getStoragePath(song.Cover)
		}

		fileData, err := os.ReadFile(filePath)
		fileBase64 := ""
		if err != nil {
			fmt.Printf("Error leyendo archivo %s: %v\n", filePath, err)
			fileBase64 = ""
		} else {
			fileBase64 = base64.StdEncoding.EncodeToString(fileData)
		}

		coverBase64 := ""
		if coverPath != "" {
			coverData, err := os.ReadFile(coverPath)
			if err != nil {
				fmt.Printf("Error leyendo cover %s: %v\n", coverPath, err)
				coverBase64 = ""
			} else {
				coverBase64 = base64.StdEncoding.EncodeToString(coverData)
			}
		}

		response = append(response, structs.SongResponse{
			ID:       song.ID,
			Title:    song.Title,
			Artist:   song.Artist,
			Album:    song.Album,
			Genre:    song.Genre,
			Duration: song.Duration,
			File:     fileBase64,
			Cover:    coverBase64,
		})
	}

	return c.JSON(response)
}

func getStoragePath(filename string) string {
	// Si ya es una ruta absoluta, usarla tal cual
	if filepath.IsAbs(filename) {
		return filename
	}

	// Si la ruta ya incluye "storage/", construir desde el directorio de trabajo
	if strings.Contains(filename, "storage/") || strings.Contains(filename, "storage\\") {
		return filepath.Join(".", filename)
	}

	// Construir ruta desde el directorio storage
	return filepath.Join("storage", "songs", filepath.Base(filename))
}

func (h *SongHandler) GetSongByID(c *fiber.Ctx) error {
	idParam := c.Params("id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid song ID"})
	}

	var song models.Song
	result := h.DB.First(&song, id)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "Song not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": result.Error.Error()})
	}

	return c.JSON(song)
}

func (h *SongHandler) GetSongsSearch(c *fiber.Ctx) error {
	query := c.Query("q")
	if query == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Query parameter 'q' is required"})
	}

	var songs []models.Song

	searchPattern := "%" + query + "%"

	result := h.DB.Where(
		"title ILIKE ? OR artist ILIKE ? OR album ILIKE ?",
		searchPattern,
		searchPattern,
		searchPattern,
	).Find(&songs)

	if result.Error != nil {
		return c.Status(500).JSON(fiber.Map{"error": result.Error.Error()})
	}

	return c.JSON(songs)
}

func (h *SongHandler) GetSongs(c *fiber.Ctx) error {
	var filters filters.SongFilters

	if err := c.QueryParser(&filters); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid query parameters"})
	}

	var songs []models.Song
	query := h.DB.Model(&models.Song{})

	query = applyFilters(query, filters)

	if filters.SortBy != "" {
		order := filters.SortBy
		if filters.Order == "desc" {
			order = order + " DESC"
		} else {
			order = order + " ASC"
		}
		query = query.Order(order)
	}

	if filters.Limit <= 0 {
		filters.Limit = 20
	}
	query = query.Limit(filters.Limit).Offset(filters.Offset)

	result := query.Find(&songs)
	if result.Error != nil {
		return c.Status(500).JSON(fiber.Map{"error": result.Error.Error()})
	}

	var total int64
	countQuery := h.DB.Model(&models.Song{})
	countQuery = applyFilters(countQuery, filters)
	countQuery.Count(&total)

	return c.JSON(fiber.Map{
		"songs":   songs,
		"total":   total,
		"limit":   filters.Limit,
		"offset":  filters.Offset,
		"filters": filters,
	})
}

func applyFilters(query *gorm.DB, filters filters.SongFilters) *gorm.DB {
	if filters.Artist != "" {
		query = query.Where("artist = ?", filters.Artist)
	}
	if filters.Genre != "" {
		query = query.Where("genre = ?", filters.Genre)
	}
	if filters.Album != "" {
		query = query.Where("album = ?", filters.Album)
	}
	if filters.Title != "" {
		query = query.Where("title = ?", filters.Title)
	}
	if filters.Year > 0 {
		query = query.Where("year = ?", filters.Year)
	}

	return query
}
