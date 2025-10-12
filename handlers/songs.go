package handlers

import (
    "strconv"

    "github.com/gofiber/fiber/v2"
    "gorm.io/gorm"
    "distributed-systems-project/models"
    "distributed-systems-project/filters"
)

type SongHandler struct {
    DB *gorm.DB
}

func (h *SongHandler) CreateSong(c *fiber.Ctx) error {
    var song models.Song
    
    if err := c.BodyParser(&song); err != nil {
        return c.Status(400).JSON(fiber.Map{"error": err.Error()})
    }

    result := h.DB.Create(&song)
    if result.Error != nil {
        return c.Status(500).JSON(fiber.Map{"error": result.Error.Error()})
    }

    return c.Status(201).JSON(song)
}

func (h *SongHandler) GetAllSongs(c *fiber.Ctx) error {
    var songs []models.Song
    result := h.DB.Find(&songs)
    if result.Error != nil {
        return c.Status(500).JSON(fiber.Map{"error": result.Error.Error()})
    }
    return c.JSON(songs)
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
        "songs": songs,
        "total": total,
        "limit": filters.Limit,
        "offset": filters.Offset,
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