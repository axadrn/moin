package handler

import (
	"encoding/xml"
	"net/http"
	"time"
)

// feedItem is one entry of an RSS feed.
type feedItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	PubDate     string `xml:"pubDate"`
	Description string `xml:"description"`
}

// writeFeed answers an RSS feed, for a reader or a Slack channel.
func writeFeed(w http.ResponseWriter, title, link, description string, items []feedItem) {
	type channel struct {
		Title       string     `xml:"title"`
		Link        string     `xml:"link"`
		Description string     `xml:"description"`
		Items       []feedItem `xml:"item"`
	}
	type rss struct {
		XMLName xml.Name `xml:"rss"`
		Version string   `xml:"version,attr"`
		Channel channel  `xml:"channel"`
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=600")
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(rss{Version: "2.0", Channel: channel{Title: title, Link: link, Description: description, Items: items}})
}

func feedDate(date time.Time) string {
	if date.IsZero() {
		date = time.Now()
	}
	return date.UTC().Format(time.RFC1123Z)
}
