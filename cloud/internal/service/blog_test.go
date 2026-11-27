package service

import (
	"testing"
	"testing/fstest"
)

func TestBlogPostsNewestFirst(t *testing.T) {
	files := fstest.MapFS{
		"blog/old.md": {Data: []byte("---\ntitle: \"Old\"\ndescription: \"An old post.\"\ndate: \"2026-01-02\"\n---\n\nText.\n")},
		"blog/new.md": {Data: []byte("---\ntitle: \"New\"\ndescription: \"A new post.\"\ndate: \"2026-03-04\"\n---\n\n**Bold**\n")},
	}
	posts, err := BlogPosts(files)
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 2 || posts[0].Slug != "new" || posts[1].Slug != "old" {
		t.Fatalf("posts = %+v, want new then old", posts)
	}
	if posts[0].HTML != "<p><strong>Bold</strong></p>\n" {
		t.Errorf("html = %q", posts[0].HTML)
	}
	if _, ok := BlogPostBySlug(posts, "old"); !ok {
		t.Error("old not found")
	}
}

func TestBlogPostNeedsADate(t *testing.T) {
	files := fstest.MapFS{"blog/x.md": {Data: []byte("---\ntitle: \"X\"\ndescription: \"No date.\"\n---\n\nText.\n")}}
	if _, err := BlogPosts(files); err == nil {
		t.Fatal("a post without a date was taken")
	}
}
