package playlist

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/crocdialer/zug_ins_nirgendwo_backend_v2/command"
)

// IconMap holds our icon-paths
var IconMap map[string]string

// playlists holds our playlists of movies
var playlists []*Playlist

var playlistMutex sync.RWMutex

var playlistFile = "playlists.json"

// GetPlaylists returns a slice of Playlists
func GetPlaylists() []*Playlist {
	playlistMutex.RLock()
	defer playlistMutex.RUnlock()
	return playlists
}

// SetPlaylists sets the
func SetPlaylists(p []*Playlist) {
	playlistMutex.Lock()
	defer playlistMutex.Unlock()
	playlists = append(playlists[:1], p...)
}

// Init will initialize the module state,
// meaning it scans for movies, icons and saved playlists
// and inits the IconMap and Playlists variables
func Init(baseDir string) {
	log.Println("scanning media directory:", baseDir)
	// TODO: init IconMap from json-file

	// clear slice
	playlists = playlists[:0]

	// start with "All Movies" playlist
	allMovies := &Playlist{Title: "All Movies", Movies: createMovieList(baseDir)}
	playlists = append(playlists, allMovies)

	// read user playlists from file
	jsonFile, err := os.Open(playlistFile)

	if err != nil {
		log.Println(err)
		return
	}
	defer jsonFile.Close()

	var loadedLists []*Playlist
	decoder := json.NewDecoder(jsonFile)
	decoder.Decode(&loadedLists)

	// append to playlists
	playlists = append(playlists, loadedLists...)

	log.Printf("found %d movies and %d playlists\n", len(allMovies.Movies), len(playlists))
}

// Save will save the module state to one or more json-config files
func Save(baseDir string) {
	jsonFile, err := os.Create(playlistFile)

	if err != nil {
		panic(err)
	}
	defer jsonFile.Close()

	// encode playlist as json
	enc := json.NewEncoder(jsonFile)
	enc.SetIndent("", "  ")
	enc.Encode(GetPlaylists()[1:])

	// jsonFile.Write(jsonData)
	// jsonFile.Close()
	log.Println("JSON data written to ", jsonFile.Name())
}

// Playlist groups information for a playlist of movies
type Playlist struct {
	Title  string   `json:"title"`
	Movies []*Movie `json:"movies"`
}

// Movie groups information about a movie-file
type Movie struct {
	Path     string `json:"path"`
	IconPath string `json:"icon"`
}

// PlaybackState groups information for the current playback state
type PlaybackState struct {
	PlaylistIndex int     `json:"playlist_index"`
	MovieIndex    int     `json:"movie_index"`
	Position      float64 `json:"position"`
	Duration      float64 `json:"duration"`
	Volume        float64 `json:"volume"`
	Rate          float64 `json:"rate"`
	Playing       bool    `json:"playing"`
}

// NewPlaybackState creates the default playbackstate
func NewPlaybackState() *PlaybackState {
	state := &PlaybackState{
		PlaylistIndex: -1,
		MovieIndex:    -1,
	}
	return state
}

// PlaybackStateUpdater is used to periodically update the playback-state
// and push the current state to a provided channel
type PlaybackStateUpdater struct {
	state      *PlaybackState
	Done       chan bool
	Address    string
	output     chan<- *PlaybackState
	timeOut    time.Duration
	ticker     *time.Ticker
	stateMutex sync.RWMutex
}

// NewPlaybackStateUpdater creates a new instance
func NewPlaybackStateUpdater(
	ip string,
	timeOut time.Duration,
	output chan<- *PlaybackState) *PlaybackStateUpdater {
	ret := &PlaybackStateUpdater{
		state:   NewPlaybackState(),
		Address: ip,
		timeOut: timeOut,
		output:  output,
		Done:    make(chan bool),
	}
	go ret.worker()
	return ret
}

// GetState returns the current state in a threadsafe way using a mutex
func (updater *PlaybackStateUpdater) GetState() PlaybackState {
	updater.stateMutex.RLock()
	defer updater.stateMutex.RUnlock()
	return *updater.state
}

// SetState sets the current state in a threadsafe way using a mutex
func (updater *PlaybackStateUpdater) SetState(state PlaybackState) {
	updater.stateMutex.Lock()
	defer updater.stateMutex.Unlock()
	*updater.state = state
}

func (updater *PlaybackStateUpdater) worker() {
	// defer log.Println("transmit done:", cmd)

	responseBuffer := make([]byte, 1<<11)
	requestStateCmd := &command.Command{Command: "playstate"}

	// start ticker
	updater.ticker = time.NewTicker(updater.timeOut)

	for {
		select {
		case <-updater.Done:
			updater.ticker.Stop()
			return
		case <-updater.ticker.C:
			ack := command.Send(requestStateCmd, updater.Address, responseBuffer)
			if ack.Success {
				updater.stateMutex.Lock()
				if err := json.Unmarshal([]byte(ack.Value), updater.state); err == nil {
					// state updated
					updater.output <- updater.state
				} else {
					log.Println("could not parse playbackstate")
				}
				updater.stateMutex.Unlock()
			}
		}
	}
}

// Playback sets a new playlist-index and optionally a new playlist
func (updater *PlaybackStateUpdater) Playback(movieIndex int, playlistIndex int) {
	var playlist []string

	if playlistIndex < 0 {
		playlistIndex = updater.state.PlaylistIndex
	}
	list := playlists[playlistIndex]

	for _, mov := range list.Movies {
		playlist = append(playlist, mov.Path)
	}
	command.Playback(updater.Address, movieIndex, playlist)

	// set playlist index, since mediaplayer will not be aware of it
	updater.stateMutex.Lock()
	defer updater.stateMutex.Unlock()
	updater.state.PlaylistIndex = playlistIndex
	updater.state.MovieIndex = movieIndex
}

// createMovieList recursively walks a directory and return a list of all movie files
func createMovieList(baseDir string) (movies []*Movie) {

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

	var files []string

	// walk the directory tree
	filepath.Walk(baseDir, func(path string, f os.FileInfo, _ error) error {
		if !f.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))

			if _, isMovie := movieExtensions[ext]; isMovie {
				// movies = append(movies, Movie{Path: path})
				files = append(files, path)
			}
		}
		return nil
	})
	sort.Strings(files)

	for _, f := range files {
		// log.Println(f)
		mov := &Movie{Path: f}
		if iconPath, ok := IconMap[f]; ok {
			mov.IconPath = iconPath
		}
		movies = append(movies, mov)
	}
	return movies
}
