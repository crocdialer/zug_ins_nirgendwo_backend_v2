package playlist

import "os"

// Movie groups information about a movie-file
type Movie struct {
	Path string `json:"path"`
}

// CreateMovieList recursively walks a directory and return a list of all movie files
func CreateMovieList(baseDir string) {
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		panic(err)
	}

}
