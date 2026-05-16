package platforms

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/amarnathcjd/gogram/telegram"

	state "main/internal/core/models"
)

const PlatformYouTube state.PlatformName = "youtube"

var (
	APIURL      = getEnv("SHRUTI_API_URL", "https://api.shrutibots.site")
	APIKEY      = getEnv("SHRUTI_API_KEY", "YOUR_API_KEY")
	DownloadDir = "downloads"
)

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func TimeToSeconds(t string) int {
	parts := strings.Split(t, ":")
	total := 0
	multiplier := 1

	for i := len(parts) - 1; i >= 0; i-- {
		var val int
		fmt.Sscanf(parts[i], "%d", &val)
		total += val * multiplier
		multiplier *= 60
	}

	return total
}

func DownloadSong(link string) (string, error) {
	videoID := extractVideoID(link)
	if videoID == "" {
		return "", fmt.Errorf("invalid video id")
	}

	os.MkdirAll(DownloadDir, os.ModePerm)

	filePath := filepath.Join(DownloadDir, videoID+".mp3")

	if fileExists(filePath) {
		return filePath, nil
	}

	err := downloadFile(videoID, "audio", filePath, 300*time.Second)
	if err != nil {
		os.Remove(filePath)
		return "", err
	}

	return filePath, nil
}

func DownloadVideo(link string) (string, error) {
	videoID := extractVideoID(link)
	if videoID == "" {
		return "", fmt.Errorf("invalid video id")
	}

	os.MkdirAll(DownloadDir, os.ModePerm)

	filePath := filepath.Join(DownloadDir, videoID+".mp4")

	if fileExists(filePath) {
		return filePath, nil
	}

	err := downloadFile(videoID, "video", filePath, 600*time.Second)
	if err != nil {
		os.Remove(filePath)
		return "", err
	}

	return filePath, nil
}

func downloadFile(videoID, fileType, filePath string, timeout time.Duration) error {
	client := &http.Client{
		Timeout: timeout,
	}

	reqURL := fmt.Sprintf(
		"%s/download?url=%s&type=%s&api_key=%s",
		APIURL,
		url.QueryEscape(videoID),
		fileType,
		url.QueryEscape(APIKEY),
	)

	resp, err := client.Get(reqURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("api returned status %d", resp.StatusCode)
	}

	out, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func extractVideoID(link string) string {
	if strings.Contains(link, "v=") {
		parts := strings.Split(link, "v=")
		if len(parts) > 1 {
			return strings.Split(parts[1], "&")[0]
		}
	}
	return link
}

type VideoResult struct {
	Title     string `json:"title"`
	Duration  string `json:"duration"`
	ID        string `json:"id"`
	Link      string `json:"link"`
	Thumbnail string
}

type YouTubeAPI struct {
	Base     string
	Regex    *regexp.Regexp
	ListBase string
}

func NewYouTubeAPI() *YouTubeAPI {
	return &YouTubeAPI{
		Base:     "https://www.youtube.com/watch?v=",
		Regex:    regexp.MustCompile(`(?:youtube\.com|youtu\.be)`),
		ListBase: "https://youtube.com/playlist?list=",
	}
}

// Name returns the platform identifier
func (y *YouTubeAPI) Name() state.PlatformName {
	return PlatformYouTube
}

// CanGetTracks returns true if the query is a YouTube URL or a plain search term
func (y *YouTubeAPI) CanGetTracks(query string) bool {
	return y.Regex.MatchString(query) || !strings.Contains(query, ".")
}

// GetTracks fetches track metadata for a YouTube URL or search query
func (y *YouTubeAPI) GetTracks(query string, video bool) ([]*state.Track, error) {
	// YouTube URL — get single track via noembed
	if y.Regex.MatchString(query) {
		if strings.Contains(query, "&") {
			query = strings.Split(query, "&")[0]
		}

		data, err := y.fetchNoembed(query)
		if err != nil {
			return nil, err
		}

		track := noembedToTrack(data, query, video)
		if track == nil {
			return nil, fmt.Errorf("failed to extract metadata for: %s", query)
		}
		return []*state.Track{track}, nil
	}

	// Plain search query — use Shruti API
	return y.searchTracks(query, video)
}

// searchTracks searches YouTube via Shruti API
func (y *YouTubeAPI) searchTracks(query string, video bool) ([]*state.Track, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	reqURL := fmt.Sprintf(
		"%s/search?query=%s&api_key=%s",
		APIURL,
		url.QueryEscape(query),
		url.QueryEscape(APIKEY),
	)

	resp, err := client.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("search api returned status %d", resp.StatusCode)
	}

	var results []VideoResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("failed to decode search results: %w", err)
	}

	tracks := make([]*state.Track, 0, len(results))
	for _, r := range results {
		tracks = append(tracks, &state.Track{
			ID:       r.ID,
			Title:    r.Title,
			Duration: TimeToSeconds(r.Duration),
			Artwork:  r.Thumbnail,
			URL:      y.Base + r.ID,
			Video:    video,
			Source:   PlatformYouTube,
		})
	}

	return tracks, nil
}

// VideoSearch searches for video tracks — used by Spotify and other platforms
func (y *YouTubeAPI) VideoSearch(query string) ([]*state.Track, error) {
	return y.searchTracks(query, true)
}

// CanDownload returns true only for YouTube source tracks
func (y *YouTubeAPI) CanDownload(source state.PlatformName) bool {
	return source == PlatformYouTube
}

// Download downloads a track's audio or video file
func (y *YouTubeAPI) Download(
	ctx context.Context,
	track *state.Track,
	statusMsg *telegram.NewMessage,
) (string, error) {
	if track.Video {
		return DownloadVideo(track.URL)
	}
	return DownloadSong(track.URL)
}

// Exists checks if a link is a valid YouTube URL
func (y *YouTubeAPI) Exists(link string, videoID bool) bool {
	if videoID {
		link = y.Base + link
	}
	return y.Regex.MatchString(link)
}

// Details fetches raw noembed metadata for a YouTube URL
func (y *YouTubeAPI) Details(link string, videoID bool) (map[string]interface{}, error) {
	if videoID {
		link = y.Base + link
	}
	if strings.Contains(link, "&") {
		link = strings.Split(link, "&")[0]
	}
	return y.fetchNoembed(link)
}

func (y *YouTubeAPI) fetchNoembed(link string) (map[string]interface{}, error) {
	api := fmt.Sprintf("https://noembed.com/embed?url=%s", url.QueryEscape(link))

	resp, err := http.Get(api)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data, nil
}

func noembedToTrack(data map[string]interface{}, link string, video bool) *state.Track {
	title, _ := data["title"].(string)
	thumbnail, _ := data["thumbnail_url"].(string)

	if title == "" {
		return nil
	}

	videoID := extractVideoID(link)

	return &state.Track{
		ID:      videoID,
		Title:   title,
		Artwork: thumbnail,
		URL:     link,
		Video:   video,
		Source:  PlatformYouTube,
	}
}

var (
	YouTube = NewYouTubeAPI()
	yt      = YouTube
)
