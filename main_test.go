package main

import (
	"encoding/json"
	"testing"
)

func TestCommandBoundary(t *testing.T) {
	commands := make(map[string]bool)
	for _, command := range newRootCommand().Commands() {
		commands[command.Name()] = true
	}
	for _, name := range []string{"inspect", "convert", "upload", "draft", "config", "version"} {
		if !commands[name] {
			t.Fatalf("publishing command %q is missing", name)
		}
	}
	if len(commands) != 6 {
		t.Fatalf("unexpected publishing command surface: %#v", commands)
	}
}

func TestCLIResponseContract(t *testing.T) {
	payload, err := json.Marshal(cliResponse{Success: true, SchemaVersion: "v1", Data: map[string]string{"status": "completed"}})
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatal(err)
	}
	if response["success"] != true || response["schema_version"] != "v1" {
		t.Fatalf("unexpected response envelope: %s", payload)
	}
}

func TestParseArticleAndImages(t *testing.T) {
	doc := parseArticle("---\ntitle: Example\nauthor: Author\n---\n# Body\n![local](./image.png)\n![remote](https://example.com/image.png)")
	if doc.Metadata.Title != "Example" || doc.Metadata.Author != "Author" {
		t.Fatalf("unexpected metadata: %#v", doc.Metadata)
	}
	images, err := parseImages(doc.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 2 || !images[0].Local || images[1].Local {
		t.Fatalf("unexpected images: %#v", images)
	}
}
