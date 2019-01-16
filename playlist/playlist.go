package playlist

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Movie groups information about a movie-file
type Movie struct {
	Path     string `json:"path"`
	IconPath string `json:"icon"`
}

// CreateMovieList recursively walks a directory and return a list of all movie files
func CreateMovieList(baseDir string) (movies []Movie) {

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("could not create movielist:", r)
		}
	}()

	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		panic(err)
	}

	movieExtensions := map[string]bool{
		".mp4": true,
		".mov": true,
		".m4v": true,
		".mkv": true,
		".avi": true,
	}

	// walk the directory tree
	filepath.Walk(baseDir, func(path string, f os.FileInfo, _ error) error {
		if !f.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))

			if _, isMovie := movieExtensions[ext]; isMovie {
				movies = append(movies, Movie{Path: path})
			}
		}
		return nil
	})
	return movies
}
