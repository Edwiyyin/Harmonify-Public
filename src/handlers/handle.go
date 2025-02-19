package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"harmonify/src/api"
	"harmonify/src/calc"
)

var (
	HomeTemplate          *template.Template
	SearchResultsTemplate *template.Template
	LyricsTemplate        *template.Template
	PlaylistTemplate      *template.Template
    PlaylistLyricsTemplate *template.Template
	Playlist              []api.Song
	PlaylistFile          = "playlist.json"
)

func init() {
	loadPlaylistFromFile()
}

func loadPlaylistFromFile() {
	data, err := ioutil.ReadFile(PlaylistFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Error reading playlist file: %v", err)
		}
		return
	}

	if err := json.Unmarshal(data, &Playlist); err != nil {
		log.Printf("Error parsing playlist file: %v", err)
	}
}

func savePlaylistToFile() {
	data, err := json.MarshalIndent(Playlist, "", "  ")
	if err != nil {
		log.Printf("Error marshaling playlist: %v", err)
		return
	}

	if err := ioutil.WriteFile(PlaylistFile, data, 0644); err != nil {
		log.Printf("Error writing playlist file: %v", err)
	}
}

func HandleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if err := HomeTemplate.Execute(w, nil); err != nil {
		log.Printf("Error rendering home template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func HandleLyrics(w http.ResponseWriter, r *http.Request) {
    songTitle, _ := url.QueryUnescape(r.URL.Query().Get("title"))
    artist := r.URL.Query().Get("artist")
    songID := r.URL.Query().Get("id")
    query := r.URL.Query().Get("query") // Extract the search query
    page := r.URL.Query().Get("page")   // Extract the page number
    pageNum, _ := strconv.Atoi(page)    // Convert page to an integer
    actionMessage := r.URL.Query().Get("action")

    // If pageNum is 0 (e.g., page parameter is missing or invalid), default to page 1
    if pageNum == 0 {
        pageNum = 1
    }

    lyrics, err := api.FetchLyricsOvh(songTitle, artist)
    if err != nil {
        log.Printf("Lyrics fetch error: %v", err)
        lyrics = "Lyrics not available for this song"
    }

    previewURL, _ := api.SearchSpotifyMusicSource(songTitle, artist)
    spotifyURL := fmt.Sprintf("https://open.spotify.com/track/%s", songID)

    inPlaylist := false
    for _, song := range Playlist {
        if song.ID == songID {
            inPlaylist = true
            break
        }
    }

    data := struct {
        ID            string
        Title         string
        Artist        string
        Lyrics        string
        PreviewURL    string
        SpotifyURL    string
        InPlaylist    bool
        ActionMessage string
        Query         string
        Page          int
    }{
        ID:            songID,
        Title:         songTitle,
        Artist:        artist,
        Lyrics:        lyrics,
        PreviewURL:    previewURL,
        SpotifyURL:    spotifyURL,
        InPlaylist:    inPlaylist,
        ActionMessage: actionMessage,
        Query:         query,
        Page:          pageNum, // Pass the page number
    }

    if err := LyricsTemplate.Execute(w, data); err != nil {
        log.Printf("Error rendering lyrics template: %v", err)
        http.Error(w, "Error rendering lyrics", http.StatusInternalServerError)
        return
    }
}

func HandlePlaylist(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Playlist []api.Song
	}{
		Playlist: Playlist,
	}

	if err := PlaylistTemplate.Execute(w, data); err != nil {
		log.Printf("Error rendering playlist template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func HandleAddToPlaylist(w http.ResponseWriter, r *http.Request) {
    songId := r.URL.Query().Get("id")
    title := r.URL.Query().Get("title")
    artist := r.URL.Query().Get("artist")
    query := r.URL.Query().Get("query") // Extract the search query
    page := r.URL.Query().Get("page")   // Extract the page number

    // Check if the song is already in the playlist
    for _, existingSong := range Playlist {
        if strings.EqualFold(existingSong.ID, songId) {
            // Song already exists in the playlist
            http.Redirect(w, r, fmt.Sprintf("/search?query=%s&page=%s&action=already_exists", query, page), http.StatusSeeOther)
            return
        }
    }

    // Fetch song details from Spotify
    accessToken, err := api.GetSpotifyAccessToken()
    if err != nil {
        http.Error(w, "Failed to get access token", http.StatusInternalServerError)
        return
    }

    req, err := http.NewRequest("GET", fmt.Sprintf("https://api.spotify.com/v1/tracks/%s", songId), nil)
    if err != nil {
        http.Error(w, "Failed to create request", http.StatusInternalServerError)
        return
    }

    req.Header.Add("Authorization", "Bearer "+accessToken)
    req.Header.Add("Content-Type", "application/json")

    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        http.Error(w, "Failed to fetch song details", http.StatusInternalServerError)
        return
    }
    defer resp.Body.Close()

    var trackDetails struct {
        Name     string `json:"name"`
        Duration int    `json:"duration_ms"`
        Artists  []struct {
            Name string `json:"name"`
        } `json:"artists"`
        Album struct {
            Images []struct {
                URL string `json:"url"`
            } `json:"images"`
            ReleaseDate string `json:"release_date"`
        } `json:"album"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&trackDetails); err != nil {
        http.Error(w, "Failed to decode song details", http.StatusInternalServerError)
        return
    }

    // Create the song object
    fullSong := api.Song{
        ID:          songId,
        Title:       title,
        Artist:      artist,
        CoverURL:    "",
        ReleaseDate: time.Time{},
        Duration:    trackDetails.Duration,
    }

    if len(trackDetails.Album.Images) > 0 {
        fullSong.CoverURL = trackDetails.Album.Images[0].URL
    }

    if trackDetails.Album.ReleaseDate != "" {
        fullSong.ReleaseDate = api.FormatReleaseDate(trackDetails.Album.ReleaseDate)
    }

    // Add the song to the playlist
    Playlist = append(Playlist, fullSong)
    savePlaylistToFile()

    // Redirect back to the search page with the query and page parameters
    http.Redirect(w, r, fmt.Sprintf("/search?query=%s&page=%s&action=added", query, page), http.StatusSeeOther)
}

func HandleRemoveFromPlaylist(w http.ResponseWriter, r *http.Request) {
    songId := r.URL.Query().Get("id")

    for i, song := range Playlist {
        if song.ID == songId {
            // Remove the song from the playlist
            Playlist = append(Playlist[:i], Playlist[i+1:]...)
            savePlaylistToFile()

            // Redirect back to the playlist page with a success message
            http.Redirect(w, r, "/playlist?action=removed", http.StatusSeeOther)
            return
        }
    }

    // If the song was not found in the playlist, redirect with an error message
    http.Redirect(w, r, "/playlist?action=not_found", http.StatusSeeOther)
}

func HandlePlaylistLyrics(w http.ResponseWriter, r *http.Request) {
    songTitle, _ := url.QueryUnescape(r.URL.Query().Get("title"))
    artist := r.URL.Query().Get("artist")
    songID := r.URL.Query().Get("id")
    query := r.URL.Query().Get("query")
    page := r.URL.Query().Get("page")
    pageNum, _ := strconv.Atoi(page)
    if pageNum == 0 {
        pageNum = 1
    }

    // Fetch lyrics for the song
    lyrics, err := api.FetchLyricsOvh(songTitle, artist)
    if err != nil {
        log.Printf("Lyrics fetch error: %v", err)
        lyrics = "Lyrics not available for this song"
    }

    // Generate Spotify URL
    spotifyURL := fmt.Sprintf("https://open.spotify.com/track/%s", songID)

    // Check if the song is in the playlist
    inPlaylist := false
    for _, song := range Playlist {
        if song.ID == songID {
            inPlaylist = true;
            break;
        }
    }

    // Prepare data for the template
    data := struct {
        ID            string
        Title         string
        Artist        string
        Lyrics        string
        SpotifyURL    string // Add Spotify URL
        InPlaylist    bool
        Query         string
        Page          int
    }{
        ID:            songID,
        Title:         songTitle,
        Artist:        artist,
        Lyrics:        lyrics,
        SpotifyURL:    spotifyURL, // Pass Spotify URL to the template
        InPlaylist:    inPlaylist,
        Query:         query,
        Page:          pageNum,
    }

    // Render the playlist-lyrics template
    if err := PlaylistLyricsTemplate.Execute(w, data); err != nil {
        log.Printf("Error rendering playlist-lyrics template: %v", err)
        http.Error(w, "Error rendering playlist-lyrics", http.StatusInternalServerError)
        return
    }
}

func HandleGetLyricsText(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    lyrics := r.URL.Query().Get("lyrics")
    if lyrics == "" {
        http.Error(w, "No lyrics provided", http.StatusBadRequest)
        return
    }

    w.Header().Set("Content-Type", "text/plain")
    w.Write([]byte(lyrics))
}

func HandleSearch(w http.ResponseWriter, r *http.Request) {
    query := r.URL.Query().Get("query")
    page := r.URL.Query().Get("page")
    pageNum, _ := strconv.Atoi(page)
    if pageNum == 0 {
        pageNum = 1
    }

    songs, totalResults, err := api.SearchSpotifySongs(query, pageNum, api.SearchFilters{})
    if err != nil {
        log.Printf("Search error: %v", err)
        http.Error(w, "Error searching songs", http.StatusInternalServerError)
        return
    }

    totalPages := calc.CalculateTotalPages(totalResults)

    filters := api.SearchFilters{
        StartDate:   r.URL.Query().Get("startDate"),
        EndDate:     r.URL.Query().Get("endDate"),
        SortBy:      r.URL.Query().Get("sortBy"),
        SortOrder:   r.URL.Query().Get("sortOrder"),
        MinDuration: calc.ParseDuration(r.URL.Query().Get("minDuration")),
        MaxDuration: calc.ParseDuration(r.URL.Query().Get("maxDuration")),
    }

    songs, totalResults, err = api.SearchSpotifySongs(query, pageNum, filters)
    if err != nil {
        log.Printf("Search error: %v", err)
        http.Error(w, "Error searching songs", http.StatusInternalServerError)
        return
    }

    totalPages = (totalResults + 9) / 10 

    data := struct {
        Songs        []api.Song
        Query        string
        CurrentPage  int
        TotalPages   int
        TotalResults int
        Filters      api.SearchFilters
        DurationMinutes func(int) int
        DurationSeconds func(int) int
    }{
        Songs:        songs,
        Query:        query,
        CurrentPage:  pageNum,
        TotalPages:   totalPages,
        TotalResults: totalResults,
        Filters:      filters,
        DurationMinutes: calc.DurationMinutes,
        DurationSeconds: calc.DurationSeconds,
    }

    if err := SearchResultsTemplate.Execute(w, data); err != nil {
        log.Printf("Template execution error: %v", err)
        http.Error(w, "Error rendering results", http.StatusInternalServerError)
        return
    }
}