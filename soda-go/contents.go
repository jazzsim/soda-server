package main

import (
	"path/filepath"
	"strings"

	"github.com/gocolly/colly"
)

type Contents struct {
	Folders []string `json:"folders"`
	Files   []Files  `json:"files"`
}

type Files struct {
	Media    string `json:"media"`
	Filename string `json:"filename"`
}

type Thumbnail struct {
	Thumbnail string `json:"thumbnail"`
}

var mediaTypeExtensions = map[string][]string{
	"Image":    {"jpg", "jpeg", "png", "webp", "gif", "heif"},
	"Video":    {"mp4", "mkv", "avi", "mov", "wmv", "flv", "webm", "mov", "qt", "m4v", "ogg"},
	"Audio":    {"mp3", "wav", "aiff", "aa", "aax", "flac", "m4a"},
	"Document": {"pdf", "txt", "rtf", "xls", "ppt", "doc", "docx", "xlsx"},
	"Others":   {},
}

func (c *Contents) retrieveContents(collector *colly.Collector) {
	// On every a element which has href attribute call callback
	collector.OnHTML("a[href]", func(e *colly.HTMLElement) {
		// check if page is using table
		parent := e.DOM.ParentsFiltered("th")

		// ignore header links
		if parent.Text() == "" {
			var link string
			if strings.EqualFold(e.Text, "Parent Directory") {
				link = "../"
			} else {
				link = e.Attr("href")
			}
			c.getContents(link)
		}
	})
}

func (c *Contents) getContents(link string) {
	ext := strings.ToLower(filepath.Ext(link))

	if ext != "" {
		file := Files{}
		file.Filename = link
		file.sortMediaType(ext)
		c.Files = append(c.Files, file)
	} else {
		c.Folders = append(c.Folders, link)
	}
}

func (f *Files) sortMediaType(ext string) {
	for media, extensions := range mediaTypeExtensions {
		if contains(extensions, ext) {
			f.Media = media
			return
		}
	}
	f.Media = "Others"
}

func contains(slice []string, str string) bool {
	for _, s := range slice {
		if "."+s == str {
			return true
		}
	}
	return false
}