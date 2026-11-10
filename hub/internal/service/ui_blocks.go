package service

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/axadrn/moin/hub/internal/model"
)

// uiBlockGroups are the topics of the blocks, in their order. The first word
// of a file's name says its topic: saas-billing.html.
var uiBlockGroups = []struct{ prefix, title string }{
	{"dev", "Developer tools"},
	{"auth", "Sign in"},
	{"saas", "SaaS"},
	{"work", "Work"},
	{"booking", "Booking"},
	{"messages", "Messages"},
	{"content", "Content"},
	{"states", "States"},
}

var blockTitle = regexp.MustCompile(`<title>(.*?)</title>`)

// UIBlocks reads blocks/ in files: every block under its topic.
func UIBlocks(files fs.FS) ([]model.UIBlockGroup, error) {
	entries, err := fs.ReadDir(files, "blocks")
	if err != nil {
		return nil, fmt.Errorf("read the blocks: %w", err)
	}
	groups := make([]model.UIBlockGroup, 0, len(uiBlockGroups))
	for _, group := range uiBlockGroups {
		kept := model.UIBlockGroup{Title: group.title}
		for _, entry := range entries {
			slug, isBlock := strings.CutSuffix(entry.Name(), ".html")
			if !isBlock || !strings.HasPrefix(slug, group.prefix+"-") {
				continue
			}
			source, err := fs.ReadFile(files, "blocks/"+entry.Name())
			if err != nil {
				return nil, fmt.Errorf("read the block %s: %w", slug, err)
			}
			title := blockTitle.FindSubmatch(source)
			if title == nil {
				return nil, fmt.Errorf("the block %s has no title", slug)
			}
			kept.Blocks = append(kept.Blocks, model.UIBlock{
				Slug: slug, Title: string(title[1]),
				Code: strings.ReplaceAll(string(source), `"../moin.`, `"ui/moin.`),
			})
		}
		groups = append(groups, kept)
	}
	return groups, nil
}

// UIBlock is one block by its name.
func UIBlock(files fs.FS, slug string) (*model.UIBlock, error) {
	groups, err := UIBlocks(files)
	if err != nil {
		return nil, err
	}
	for _, group := range groups {
		for i := range group.Blocks {
			if group.Blocks[i].Slug == slug {
				return &group.Blocks[i], nil
			}
		}
	}
	return nil, fmt.Errorf("no block %s", slug)
}
