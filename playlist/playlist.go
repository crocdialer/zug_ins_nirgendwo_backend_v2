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
	// TODO: fetch icon paths here
	var icons map[string]string

	for _, f := range files {
		// log.Println(f)
		mov := Movie{Path: f}
		if iconPath, ok := icons[f]; ok {
			mov.IconPath = iconPath
		}
		movies = append(movies, mov)
	}
	return movies
}
