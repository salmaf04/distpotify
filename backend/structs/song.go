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

type SongResponse struct {
    ID       uint   `json:"id"`
    Title    string `json:"title"`
    Artist   string `json:"artist"`
    Album    string `json:"album"`
    Genre    string `json:"genre"`
    Duration string `json:"duration"`
    File     string `json:"file"`   
    Cover    string `json:"cover"`  
}
