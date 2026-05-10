package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// PipedVideo ek search result represent karta hai
type PipedVideo struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// PipedResponse Piped API ka full response
type PipedResponse struct {
	Items []PipedVideo `json:"items"`
}

// GetAutoPlay current song ke title se related next song dhundta hai
func GetAutoPlay(title string) (string, string, error) {
	api := fmt.Sprintf(
		"https://piped.video/api/v1/search?q=%s&filter=videos",
		url.QueryEscape(title),
	)

	resp, err := http.Get(api)
	if err != nil {
		return "", "", fmt.Errorf("piped API error: %w", err)
	}
	defer resp.Body.Close()

	// ⚠️ Teri original code mein bug tha — Piped response
	// `{"items": [...]}` format mein aata hai, directly array nahi
	var result PipedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("decode error: %w", err)
	}

	if len(result.Items) == 0 {
		return "", "", fmt.Errorf("no results found for: %s", title)
	}

	// Pehla result lo (index 0 = same song hoga, isliye index 1 lo)
	next := result.Items[0]
	if len(result.Items) > 1 {
		next = result.Items[1]
	}

	return next.Title, "https://youtube.com" + next.URL, nil
}
