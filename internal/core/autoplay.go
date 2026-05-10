package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type PipedVideo struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

func GetAutoPlay(title string) (string, string, error) {

	api := fmt.Sprintf(
		"https://piped.video/api/v1/search?q=%s&filter=videos",
		url.QueryEscape(title),
	)

	resp, err := http.Get(api)
	if err != nil {
		return "", "", err
	}

	defer resp.Body.Close()

	var results []PipedVideo

	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return "", "", err
	}

	if len(results) == 0 {
		return "", "", fmt.Errorf("no results")
	}

	return results[0].Title,
		"https://youtube.com"+results[0].URL,
		nil
}
