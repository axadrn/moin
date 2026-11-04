package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// cleanupEvery is how often the agent frees disk besides after each deploy;
// buildCacheAge is how old build cache gets before it goes.
const (
	cleanupEvery  = 24 * time.Hour
	firstCleanup  = 10 * time.Minute
	buildCacheAge = "168h"
)

// imagesFile lists every image an app's releases ever named, so the agent
// can remove the pulled ones once no release needs them: Docker does not
// say which images moin pulled.
const imagesFile = "images"

// rememberImages adds the images the app's releases name to its images
// file, before prune removes old releases and with them the names.
func (a *apps) rememberImages(app string) {
	known := a.rememberedImages(app)
	for _, image := range a.releaseImages(app) {
		if !slices.Contains(known, image) {
			known = append(known, image)
		}
	}
	a.writeImages(app, known)
}

func (a *apps) rememberedImages(app string) []string {
	body, _ := os.ReadFile(filepath.Join(a.appDir(app), imagesFile))
	return strings.Fields(string(body))
}

func (a *apps) writeImages(app string, images []string) {
	slices.Sort(images)
	body := strings.Join(images, "\n")
	if body != "" {
		body += "\n"
	}
	_ = os.WriteFile(filepath.Join(a.appDir(app), imagesFile), []byte(body), 0o600)
}

// releaseImages are the images the releases on disk name, kept or not yet
// pruned, so a deploy in flight keeps its images.
func (a *apps) releaseImages(app string) []string {
	images := []string{}
	entries, _ := os.ReadDir(filepath.Join(a.appDir(app), "releases"))
	for _, entry := range entries {
		number, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		body, _ := os.ReadFile(filepath.Join(a.releaseDir(app, number), "compose.json"))
		for _, match := range imagePattern.FindAllStringSubmatch(string(body), -1) {
			if !slices.Contains(images, match[1]) {
				images = append(images, match[1])
			}
		}
	}
	return images
}

// unusedImages picks from the images apps remembered the ones to remove:
// no release on disk names them and no container, running or stopped,
// runs them. Names Docker does not know any more are dropped from the
// list. Anything moin never named is not a candidate.
func unusedImages(remembered, named, inContainers, present []string) (remove, keep []string) {
	named, inContainers, present = normalImages(named), normalImages(inContainers), normalImages(present)
	for _, image := range normalImages(remembered) {
		switch {
		case !slices.Contains(present, image):
			// Gone already: forget it.
		case slices.Contains(named, image) || slices.Contains(inContainers, image):
			keep = append(keep, image)
		default:
			remove = append(remove, image)
		}
	}
	return remove, keep
}

// normalImages writes image names the way docker image ls does: a name
// without a tag is :latest, and Docker Hub's prefixes are left out.
func normalImages(images []string) []string {
	normal := make([]string, 0, len(images))
	for _, image := range images {
		image = strings.TrimPrefix(image, "docker.io/")
		image = strings.TrimPrefix(image, "library/")
		last := image[strings.LastIndex(image, "/")+1:]
		if !strings.Contains(last, ":") && !strings.Contains(image, "@") {
			image += ":latest"
		}
		normal = append(normal, image)
	}
	return normal
}

// cleanup frees disk: the images moin pulled or built that no release and
// no container needs, dangling images, and build cache older than a week.
// It never touches volumes, nor images of containers, nor images moin never
// named except dangling ones. It logs what it freed.
func (a *apps) cleanup(ctx context.Context) {
	containers, err := a.output(ctx, "docker", "ps", "-a", "--format", "{{.Image}}")
	if err != nil {
		slog.Warn("cleanup: list containers", "error", err)
		return
	}
	inContainers := strings.Fields(containers)
	listed, err := a.output(ctx, "docker", "image", "ls", "--format", "{{.Repository}}:{{.Tag}}")
	if err != nil {
		slog.Warn("cleanup: list images", "error", err)
		return
	}
	present := strings.Fields(listed)
	var freed int64
	removed := 0
	entries, _ := os.ReadDir(a.dir)
	for _, entry := range entries {
		app := entry.Name()
		if _, err := os.Stat(filepath.Join(a.appDir(app), "releases")); err != nil {
			continue
		}
		a.rememberImages(app)
		remove, keep := unusedImages(a.rememberedImages(app), a.releaseImages(app), inContainers, present)
		for _, image := range remove {
			size := a.imageSize(ctx, image)
			if _, err := a.output(ctx, "docker", "image", "rm", image); err != nil {
				// Still needed after all, such as by another app: keep it.
				keep = append(keep, image)
				continue
			}
			freed += size
			removed++
		}
		a.writeImages(app, keep)
	}
	out, err := a.output(ctx, "docker", "image", "prune", "-f")
	if err == nil {
		freed += reclaimed(out)
	}
	out, err = a.output(ctx, "docker", "builder", "prune", "-f", "--filter", "until="+buildCacheAge)
	if err == nil {
		freed += reclaimed(out)
	}
	if freed > 0 || removed > 0 {
		slog.Info("cleanup: freed disk", "freed", sizeText(freed), "images", removed)
	}
}

func (a *apps) imageSize(ctx context.Context, image string) int64 {
	out, err := a.output(ctx, "docker", "image", "inspect", "-f", "{{.Size}}", image)
	if err != nil {
		return 0
	}
	size, _ := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	return size
}

// reclaimedPattern finds what docker image prune and docker builder prune
// say they freed: "Total reclaimed space: 1.2GB" or "Total:\t340MB".
var reclaimedPattern = regexp.MustCompile(`(?m)^Total(?: reclaimed space)?:\s*([0-9.]+)\s*([kKMGT]?i?B)\s*$`)

// reclaimed reads the space a prune freed, in bytes.
func reclaimed(out string) int64 {
	match := reclaimedPattern.FindStringSubmatch(out)
	if match == nil {
		return 0
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0
	}
	unit := strings.ToUpper(strings.TrimSuffix(strings.TrimSuffix(match[2], "B"), "i"))
	multiplier := map[string]float64{"": 1, "K": 1e3, "M": 1e6, "G": 1e9, "T": 1e12}[unit]
	return int64(value * multiplier)
}

// sizeText writes bytes as people read them: 2.1 GB.
func sizeText(bytes int64) string {
	switch {
	case bytes >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(bytes)/1e9)
	case bytes >= 1e6:
		return fmt.Sprintf("%.0f MB", float64(bytes)/1e6)
	default:
		return fmt.Sprintf("%d kB", bytes/1e3)
	}
}

// cleanups frees disk once a day, first a few minutes after the agent starts.
func (a *apps) cleanups(ctx context.Context) {
	timer := time.NewTimer(firstCleanup)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		a.cleanup(ctx)
		timer.Reset(cleanupEvery)
	}
}
