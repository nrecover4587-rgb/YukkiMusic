/*
 * ● YukkiMusic
 * ○ A high-performance engine for streaming music in Telegram voicechats.
 *
 * Copyright (C) 2026 TheTeamVivek
 *
 * This program is free software: you can redistribute it and/or modify it under the
 * terms of the GNU General Public License as published by the Free Software Foundation,
 * either version 3 of the License, or (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful, but WITHOUT ANY
 * WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A
 * PARTICULAR PURPOSE. See the GNU General Public License for more details.
 *
 * Repository: https://github.com/TheTeamVivek/YukkiMusic
 */

package platforms

import (
	"context"
	"encoding/json"
	"errors"
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
	"main/internal/utils"
)

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

var (
	youtubeLinkRegex = regexp.MustCompile(
		`(?i)^(?:https?:\/\/)?(?:www\.|m\.|music\.)?(?:youtube\.com|youtu\.be)\/\S+`,
	)
	videoIDRe1 = regexp.MustCompile(
		`(?i)(?:youtube\.com/(?:watch\?v=|embed/|shorts/|live/)|youtu\.be/)([A-Za-z0-9_-]{11})`,
	)
	videoIDRe2    = regexp.MustCompile(`(?:v=|\/)([0-9A-Za-z_-]{11})`)
	playlistIDRe1 = regexp.MustCompile(
		`(?i)(?:youtube\.com|music\.youtube\.com).*(?:\?|&)list=([A-Za-z0-9_-]+)`,
	)
	playlistIDRe2 = regexp.MustCompile(`list=([0-9A-Za-z_-]+)`)
	youtubeCache  = utils.NewCache[string, []*state.Track](1 * time.Hour)
)

const PlatformYouTube state.PlatformName = "YouTube"

type YouTubePlatform struct {
	name   state.PlatformName
	client *http.Client
}

var yt = &YouTubePlatform{
	name:   PlatformYouTube,
	client: &http.Client{Timeout: 30 * time.Second},
}

func init() {
	Register(90, yt)
}

func (p *YouTubePlatform) Name() state.PlatformName {
	return p.name
}

func (p *YouTubePlatform) CanGetTracks(link string) bool {
	return youtubeLinkRegex.MatchString(link)
}

func (p *YouTubePlatform) GetTracks(input string, video bool) ([]*state.Track, error) {
	query := strings.TrimSpace(input)
	if query == "" {
		return nil, errors.New("empty query")
	}

	var (
		tracks []*state.Track
		err    error
	)

	if !youtubeLinkRegex.MatchString(query) {
		tracks, err = p.VideoSearch(query, false)
	} else {
		playlistID := p.extractPlaylistID(query)
		videoID := p.extractVideoID(query)

		switch {
		case playlistID != "" && videoID != "":
			tracks, err = p.handleCombined(query, videoID)
		case playlistID != "":
			tracks, err = p.handlePlaylist(query)
		default:
			tracks, err = p.handleTrackURL(query)
		}
	}

	if err != nil {
		return nil, err
	}
	if len(tracks) == 0 {
		return nil, errors.New("no tracks found")
	}

	return updateCached(tracks, video), nil
}

func (p *YouTubePlatform) handlePlaylist(rawURL string) ([]*state.Track, error) {
	cacheKey := "playlist:" + strings.ToLower(rawURL)
	if cached, ok := youtubeCache.Get(cacheKey); ok {
		return cached, nil
	}

	playlistID := p.extractPlaylistID(rawURL)
	if playlistID == "" {
		return nil, errors.New("invalid playlist url")
	}

	tracks, err := p.fetchPlaylistFromAPI(playlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch playlist: %w", err)
	}

	if len(tracks) > 0 {
		youtubeCache.Set(cacheKey, tracks)
	}

	return tracks, nil
}

func (p *YouTubePlatform) handleCombined(rawURL, videoID string) ([]*state.Track, error) {
	vTracks, vErr := p.handleTrackURL(rawURL)
	pTracks, pErr := p.handlePlaylist(rawURL)

	if vErr == nil && pErr == nil && len(vTracks) > 0 {
		vid := vTracks[0].ID
		finalTracks := []*state.Track{vTracks[0]}
		for _, t := range pTracks {
			if t.ID != vid {
				finalTracks = append(finalTracks, t)
			}
		}
		return finalTracks, nil
	}

	if vErr == nil {
		return vTracks, nil
	}
	if pErr == nil {
		return pTracks, nil
	}

	return nil, fmt.Errorf("failed to fetch video (%v) and playlist (%v)", vErr, pErr)
}

func (p *YouTubePlatform) handleTrackURL(rawURL string) ([]*state.Track, error) {
	videoID := p.extractVideoID(rawURL)
	if videoID == "" {
		return nil, errors.New("invalid video url")
	}

	if cached, ok := youtubeCache.Get("track:" + videoID); ok && len(cached) > 0 {
		return cached, nil
	}

	track, err := p.fetchVideoFromAPI(videoID)
	if err != nil {
		return nil, err
	}

	youtubeCache.Set("track:"+videoID, []*state.Track{track})
	return []*state.Track{track}, nil
}

func (p *YouTubePlatform) CanDownload(source state.PlatformName) bool {
	return source == PlatformYouTube
}

func (p *YouTubePlatform) Download(
	ctx context.Context,
	track *state.Track,
	statusMsg *telegram.NewMessage,
) (string, error) {
	if track.Video {
		return p.downloadVideo(track.URL)
	}
	return p.downloadAudio(track.URL)
}

// VideoSearch searches YouTube — variadic singleOpt matches original signature
func (p *YouTubePlatform) VideoSearch(query string, singleOpt ...bool) ([]*state.Track, error) {
	single := false
	if len(singleOpt) > 0 && singleOpt[0] {
		single = true
	}

	cacheKey := "search:" + strings.TrimSpace(strings.ToLower(query))
	if arr, ok := youtubeCache.Get(cacheKey); ok {
		if single && len(arr) > 0 {
			return []*state.Track{arr[0]}, nil
		}
		if !single && len(arr) > 1 {
			return arr, nil
		}
	}

	tracks, err := p.searchFromAPI(query)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	if len(tracks) == 0 {
		return nil, errors.New("no tracks found")
	}

	youtubeCache.Set(cacheKey, tracks)

	if single {
		return []*state.Track{tracks[0]}, nil
	}

	return tracks, nil
}

// --- Shruti API calls ---

type shrutiSearchResult struct {
	Title     string `json:"title"`
	Duration  string `json:"duration"`
	ID        string `json:"id"`
	Link      string `json:"link"`
	Thumbnail string `json:"thumbnail"`
}

type shrutiVideoResult struct {
	Title     string `json:"title"`
	Duration  string `json:"duration"`
	ID        string `json:"id"`
	Thumbnail string `json:"thumbnail"`
}

type shrutiPlaylistResult struct {
	Videos []shrutiSearchResult `json:"videos"`
}

func (p *YouTubePlatform) searchFromAPI(query string) ([]*state.Track, error) {
	reqURL := fmt.Sprintf(
		"%s/search?query=%s&api_key=%s",
		APIURL,
		url.QueryEscape(query),
		url.QueryEscape(APIKEY),
	)

	var results []shrutiSearchResult
	if err := p.getJSON(reqURL, &results); err != nil {
		return nil, err
	}

	tracks := make([]*state.Track, 0, len(results))
	for _, r := range results {
		tracks = append(tracks, &state.Track{
			ID:       r.ID,
			Title:    r.Title,
			Duration: parseDuration(r.Duration),
			Artwork:  r.Thumbnail,
			URL:      "https://www.youtube.com/watch?v=" + r.ID,
			Source:   PlatformYouTube,
		})
	}

	return tracks, nil
}

func (p *YouTubePlatform) fetchVideoFromAPI(videoID string) (*state.Track, error) {
	reqURL := fmt.Sprintf(
		"%s/video?id=%s&api_key=%s",
		APIURL,
		url.QueryEscape(videoID),
		url.QueryEscape(APIKEY),
	)

	var result shrutiVideoResult
	if err := p.getJSON(reqURL, &result); err != nil {
		return nil, err
	}

	return &state.Track{
		ID:       result.ID,
		Title:    result.Title,
		Duration: parseDuration(result.Duration),
		Artwork:  result.Thumbnail,
		URL:      "https://www.youtube.com/watch?v=" + result.ID,
		Source:   PlatformYouTube,
	}, nil
}

func (p *YouTubePlatform) fetchPlaylistFromAPI(playlistID string) ([]*state.Track, error) {
	reqURL := fmt.Sprintf(
		"%s/playlist?id=%s&api_key=%s",
		APIURL,
		url.QueryEscape(playlistID),
		url.QueryEscape(APIKEY),
	)

	var result shrutiPlaylistResult
	if err := p.getJSON(reqURL, &result); err != nil {
		return nil, err
	}

	tracks := make([]*state.Track, 0, len(result.Videos))
	for _, r := range result.Videos {
		tracks = append(tracks, &state.Track{
			ID:       r.ID,
			Title:    r.Title,
			Duration: parseDuration(r.Duration),
			Artwork:  r.Thumbnail,
			URL:      "https://www.youtube.com/watch?v=" + r.ID,
			Source:   PlatformYouTube,
		})
	}

	return tracks, nil
}

func (p *YouTubePlatform) downloadAudio(link string) (string, error) {
	videoID := p.extractVideoID(link)
	if videoID == "" {
		videoID = link
	}

	os.MkdirAll(DownloadDir, os.ModePerm)
	filePath := filepath.Join(DownloadDir, videoID+".mp3")

	if fileExists(filePath) {
		return filePath, nil
	}

	if err := p.downloadFromAPI(videoID, "audio", filePath, 300*time.Second); err != nil {
		os.Remove(filePath)
		return "", err
	}

	return filePath, nil
}

func (p *YouTubePlatform) downloadVideo(link string) (string, error) {
	videoID := p.extractVideoID(link)
	if videoID == "" {
		videoID = link
	}

	os.MkdirAll(DownloadDir, os.ModePerm)
	filePath := filepath.Join(DownloadDir, videoID+".mp4")

	if fileExists(filePath) {
		return filePath, nil
	}

	if err := p.downloadFromAPI(videoID, "video", filePath, 600*time.Second); err != nil {
		os.Remove(filePath)
		return "", err
	}

	return filePath, nil
}

func (p *YouTubePlatform) downloadFromAPI(videoID, fileType, filePath string, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}

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

func (p *YouTubePlatform) getJSON(reqURL string, target any) error {
	resp, err := p.client.Get(reqURL)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("api returned status %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

// --- Helpers ---

func (p *YouTubePlatform) extractPlaylistID(input string) string {
	if m := playlistIDRe1.FindStringSubmatch(input); len(m) > 1 {
		return m[1]
	}
	if m := playlistIDRe2.FindStringSubmatch(input); len(m) > 1 {
		return m[1]
	}
	return ""
}

func (p *YouTubePlatform) extractVideoID(u string) string {
	if m := videoIDRe1.FindStringSubmatch(u); len(m) > 1 {
		return m[1]
	}
	if m := videoIDRe2.FindStringSubmatch(u); len(m) > 1 {
		return m[1]
	}
	return ""
}

func updateCached(tracks []*state.Track, video bool) []*state.Track {
	out := make([]*state.Track, 0, len(tracks))
	for _, t := range tracks {
		if t == nil {
			continue
		}
		tc := *t
		tc.Video = video
		out = append(out, &tc)
	}
	return out
}

func parseDuration(s string) int {
	parts := strings.Split(s, ":")
	total := 0
	mult := 1
	for i := len(parts) - 1; i >= 0; i-- {
		n := atoi(parts[i])
		total += n * mult
		mult *= 60
	}
	return total
}

func atoi(s string) int {
	var n int
	for _, r := range s {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
		}
	}
	return n
}
