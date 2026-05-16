package youtube

import (
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
)

var (
	APIURL     = getEnv("SHRUTI_API_URL", "https://api.shrutibots.site")
	APIKEY     = getEnv("SHRUTI_API_KEY", "YOUR_API_KEY")
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

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	return err == nil && info.Size() > 0
}

type VideoResult struct {
	Title       string `json:"title"`
	Duration    string `json:"duration"`
	ID          string `json:"id"`
	Link        string `json:"link"`
	Thumbnail   string
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

func (y *YouTubeAPI) Exists(link string, videoID bool) bool {
	if videoID {
		link = y.Base + link
	}
	return y.Regex.MatchString(link)
}

func (y *YouTubeAPI) Details(link string, videoID bool) (map[string]interface{}, error) {
	if videoID {
		link = y.Base + link
	}

	if strings.Contains(link, "&") {
		link = strings.Split(link, "&")[0]
	}

	api := fmt.Sprintf(
		"https://noembed.com/embed?url=%s",
		url.QueryEscape(link),
	)

	resp, err := http.Get(api)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (y *YouTubeAPI) Video(link string, videoID bool) (bool, string) {
	if videoID {
		link = y.Base + link
	}

	if strings.Contains(link, "&") {
		link = strings.Split(link, "&")[0]
	}

	file, err := DownloadVideo(link)
	if err != nil {
		return false, err.Error()
	}

	return true, file
}

func (y *YouTubeAPI) Download(
	link string,
	video bool,
	videoID bool,
) (string, bool) {

	if videoID {
		link = y.Base + link
	}

	var (
		file string
		err  error
	)

	if video {
		file, err = DownloadVideo(link)
	} else {
		file, err = DownloadSong(link)
	}

	if err != nil {
		return "", false
	}

	return file, true
}

var YouTube = NewYouTubeAPI()
