package uploader

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

const maxUploadSize = 10 << 20 // 10 MiB

func UploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Limit the entire request body.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	// Extract the uploaded file from multipart/form-data.
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Invalid upload", http.StatusBadRequest)
		return
	}
	defer file.Close()

	uploadDir := "./uploads"
	if err := os.MkdirAll(uploadDir, 0750); err != nil {
		http.Error(w, "Storage unavailable", http.StatusInternalServerError)
		return
	}

	// Generate a unique filename; don't trust the client's filename.
	dst, err := os.CreateTemp(uploadDir, "upload-*")
	if err != nil {
		http.Error(w, "Could not create file", http.StatusInternalServerError)
		return
	}

	dstPath := dst.Name()
	defer dst.Close()

	// Copy the uploaded contents to disk.
	if _, err := io.Copy(dst, file); err != nil {
		_ = os.Remove(dstPath)
		http.Error(w, "Could not save file", http.StatusInternalServerError)
		return
	}

	log.Printf("Uploaded %q (%d bytes) to %s",
		header.Filename, header.Size, dstPath)

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "Upload successful")
}
