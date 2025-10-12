package structs

type SongInputModel struct {
	Title    string `json:"title" validate:"required"`
	Artist   string `json:"artist" validate:"required"`
	Album    string `json:"album"`
	Genre    string `json:"genre"`
	Duration string `json:"duration"`
	File     string `json:"file" validate:"required"`
	Cover    *string `json:"cover,omitempty"`
}