package tweets

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// find is one post of the real tweets.json, by its id.
func find(t *testing.T, id string) Tweet {
	t.Helper()
	all, err := Load(os.DirFS("../../content"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tweet := range all {
		if tweet.ID == id {
			return tweet
		}
	}
	t.Fatalf("no tweet %s", id)
	return Tweet{}
}

func TestPartsTurnsEntitiesBack(t *testing.T) {
	text := "RAID &gt; backup? &lt;sigh&gt; &amp; restore"
	parts := Tweet{Text: text, Range: [2]int{0, len([]rune(text))}}.Parts()
	if len(parts) != 1 || parts[0].Text != "RAID > backup? <sigh> & restore" {
		t.Fatalf("got %q, want the entities turned back", parts)
	}
}

func TestPartsLinksMentionsAndLinks(t *testing.T) {
	parts := find(t, "1770803425074204694").Parts()
	mention := Part{Text: "@syntaxfm", Href: "https://x.com/syntaxfm"}
	link := Part{Text: "youtube.com/watch?v=Q1Y_g0…", Href: "https://www.youtube.com/watch?v=Q1Y_g0wMwww"}
	if !slices.Contains(parts, mention) {
		t.Errorf("no mention part in %q", parts)
	}
	if parts[len(parts)-1] != link {
		t.Errorf("last part %q, want %q", parts[len(parts)-1], link)
	}
	if !strings.HasPrefix(parts[0].Text, "I started a series over on ") {
		t.Errorf("first part %q", parts[0].Text)
	}
}

func TestPartsDropsThePhotoLink(t *testing.T) {
	parts := find(t, "1980245233339408596").Parts()
	for _, part := range parts {
		if strings.Contains(part.Text, "t.co") {
			t.Errorf("part %q still has a t.co link", part.Text)
		}
	}
	last := parts[len(parts)-1]
	if last.Href != "https://www.youtube.com/watch?v=-cEn_83zRFw&t=1306s" || last.Text != "youtube.com/watch?v=-cEn_8…" {
		t.Errorf("last part %q, want the YouTube link", last)
	}
}

func TestPartsStopsAtTheRange(t *testing.T) {
	parts := find(t, "2113587042185576853").Parts()
	if len(parts) != 1 || !strings.HasSuffix(parts[0].Text, "exploits and vulnerabilities") {
		t.Fatalf("got %q", parts)
	}

	// Truncated, with the photo link past the end.
	parts = find(t, "1997293892090183772").Parts()
	if len(parts) != 1 || !strings.HasSuffix(parts[0].Text, "a botnet was using my IP to") {
		t.Fatalf("got %q, want it cut before the photo link", parts)
	}
}

func TestCount(t *testing.T) {
	for n, want := range map[int]string{112: "112", 3737: "3.7K", 2000: "2K", 10388: "10K", 1_250_000: "1.2M"} {
		if got := Count(n); got != want {
			t.Errorf("Count(%d) = %q, want %q", n, got, want)
		}
	}
}
