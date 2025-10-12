package filters

type SongFilters struct {
    Artist string `query:"artist"`
    Genre  string `query:"genre"`
    Album  string `query:"album"`
    Year   int    `query:"year"`
    Title  string `query:"title"`
    Limit  int    `query:"limit" default:"20"`
    Offset int    `query:"offset" default:"0"`
    SortBy string `query:"sort_by" default:"title"` 
    Order  string `query:"order" default:"asc"`   
}
