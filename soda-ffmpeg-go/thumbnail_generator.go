package main

import (
	"bytes"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"

	"os/exec"
	"path/filepath"
)

type Catbox struct {
	Client   *http.Client
	Userhash string
}

func executeFFmpeg(videoUrl string, filename string) (imgUrl string) {
	imagePath := "./thumbnails/" + filename + ".webp"

	cmd := exec.Command("ffmpeg", "-ss", "07:00", "-i", videoUrl, "-vf", "scale=800:-1", "-update", "1", "-frames:v", "1", "-map_metadata", "-1", imagePath)
	
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		log.Println(err)
		return ""
	}

	catbox := Catbox{
		Client: &http.Client{},
	}

	// upload image
	catboxImage, err := catbox.fileUpload(imagePath)
	if err != nil {
		log.Println(err)
		return ""
	}

	// remove image
	err = os.Remove(imagePath)
	if err != nil {
		log.Println(err)
	}

	return catboxImage
}

func (cat *Catbox) fileUpload(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	r, w := io.Pipe()
	m := multipart.NewWriter(w)

	go func() {
		defer w.Close()
		defer m.Close()

		m.WriteField("reqtype", "fileupload")
		m.WriteField("time", "1h")
		part, err := m.CreateFormFile("fileToUpload", filepath.Base(file.Name()))
		if err != nil {
			return
		}

		if _, err = io.Copy(part, file); err != nil {
			return
		}
	}()
	
	req, _ := http.NewRequest(http.MethodPost, "https://litterbox.catbox.moe/resources/internals/api.php", r)
	req.Header.Add("Content-Type", m.FormDataContentType())

	resp, err := cat.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}
