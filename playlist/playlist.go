package playlist

import (
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bakape/thumbnailer"
	"github.com/crocdialer/zug_ins_nirgendwo_backend_v2/command"
)

// Playlist groups information for a playlist of movies
type Playlist struct {
	Title  string   `json:"title"`
	Movies []*Movie `json:"movies"`
}

// Movie groups information about a movie-file
type Movie struct {
	Path     string  `json:"path"`
	Duration float64 `json:"duration"`
	Delay    float64 `json:"delay"`
	IconPath string  `json:"icon"`
}

// PlaybackState groups information for the current playback state
type PlaybackState struct {
	Connected     bool    `json:"connected"`
	Path          string  `json:"path"`
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
		PlaylistIndex: 0,
		MovieIndex:    0,
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

// UpdateMovieSettings looks up the provided movie by path
// and updates its settings, if found
func UpdateMovieSettings(movie *Movie) {
	if movie != nil {
		movieMutex.Lock()

		if m, ok := movieMap[movie.Path]; ok {
			m.Delay = movie.Delay
			movieMap[movie.Path] = m
			// log.Println("movieSettings updated:", m)
		}
		movieMutex.Unlock()
		SetPlaylists(playlists[1:])
	}
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
			updater.stateMutex.Lock()

			if ack.Success {
				if err := json.Unmarshal([]byte(ack.Value), updater.state); err == nil {
					// state updated
					updater.state.Connected = true
				} else {
					// log.Println("could not parse playbackstate")
				}
			} else {
				// log.Println("player not reachable")
				updater.state.Connected = false
				updater.state.Path = ""
				updater.state.Position = 0
				updater.state.Duration = 0
				updater.state.Playing = false
			}
			updater.output <- updater.state
			updater.stateMutex.Unlock()
		}
	}
}

// Playback sets a new playlist-index and optionally a new playlist
func (updater *PlaybackStateUpdater) Playback(movieIndex int, playlistIndex int) {

	defer func() {
		if err := recover(); err != nil {
			log.Println(err)
		}
	}()

	var playlist []string
	var delays []float64

	if playlistIndex < 0 {
		playlistIndex = updater.state.PlaylistIndex
	}
	list := playlists[playlistIndex]

	// extract values from playlist
	for _, mov := range list.Movies {
		playlist = append(playlist, mov.Path)
		delays = append(delays, mov.Delay)
	}
	command.Playback(updater.Address, movieIndex, playlist, delays)

	// set playlist index, since mediaplayer will not be aware of it
	updater.stateMutex.Lock()
	defer updater.stateMutex.Unlock()
	updater.state.PlaylistIndex = playlistIndex
	updater.state.MovieIndex = movieIndex
}

// IconMap holds our icon-paths
var IconMap map[string]string

// playlists holds our playlists of movies
var playlists []*Playlist

var movieMap map[string]*Movie

var playlistMutex, thumbMutex, movieMutex sync.RWMutex

var playlistFile = "playlists.json"

var thumbsFile = "thumbs.json"

var movieDataFile = "movieData.json"

// GetPlaylists returns a slice of Playlists
func GetPlaylists() []*Playlist {
	playlistMutex.RLock()
	defer playlistMutex.RUnlock()
	return playlists
}

// SetPlaylists looks up items in the provided playlist slice by path
// and updates all secondary lists with pointers from the global movieMap, if found
func SetPlaylists(p []*Playlist) {

	var newLists []*Playlist
	movieMutex.Lock()
	defer movieMutex.Unlock()

	for _, list := range p {
		listCopy := &Playlist{Title: list.Title}

		for _, mov := range list.Movies {
			if movPtr, ok := movieMap[mov.Path]; ok {
				// movPtr.Delay = mov.Delay
				// movieMap[mov.Path] = movPtr
				listCopy.Movies = append(listCopy.Movies, movPtr)
			}
		}
		newLists = append(newLists, listCopy)
	}
	playlistMutex.Lock()
	defer playlistMutex.Unlock()

	if len(playlists) > 0 {
		// update "All Movies" playlist
		for i, mov := range playlists[0].Movies {
			if movPtr, ok := movieMap[mov.Path]; ok {
				playlists[0].Movies[i] = movPtr
			}
		}
	}
	playlists = append(playlists[:1], newLists...)
}

// Init will initialize the module state,
// meaning it scans for movies, icons and saved playlists
// and inits the IconMap and Playlists variables
func Init(baseDir string) {
	log.Println("(re-)init playlist module:", baseDir)

	if IconMap == nil {
		IconMap = make(map[string]string)
	}

	// read user playlists from file
	if iconFile, err := os.Open(thumbsFile); err == nil {
		thumbMutex.Lock()
		decoder := json.NewDecoder(iconFile)
		decoder.Decode(&IconMap)
		thumbMutex.Unlock()
		log.Println("icons loaded:", len(IconMap))
	}

	// clear slice
	playlists = playlists[:0]

	// start with "All Movies" playlist
	allMovies := &Playlist{Title: "All Movies", Movies: createMovieList(baseDir)}

	// lock playlist mutex
	playlistMutex.Lock()
	playlists = append(playlists, allMovies)
	playlistMutex.Unlock()

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
	SetPlaylists(loadedLists)
	log.Printf("found %d movies and %d playlists\n", len(allMovies.Movies), len(playlists))
}

// Save will save the module state to one or more json-config files
func Save(baseDir string) {
	if jsonFile, err := os.Create(playlistFile); err == nil {
		defer jsonFile.Close()
		// encode playlist as json
		enc := json.NewEncoder(jsonFile)
		enc.SetIndent("", "  ")
		enc.Encode(GetPlaylists()[1:])
		log.Println("playlist JSON written to ", jsonFile.Name())
	}

	if dataFile, err := os.Create(movieDataFile); err == nil {
		movieMutex.RLock()
		// encode playlist as json
		enc := json.NewEncoder(dataFile)
		// enc.SetIndent("", "  ")
		enc.Encode(movieMap)
		log.Println("movie-database written to ", dataFile.Name())
		movieMutex.RUnlock()
		dataFile.Close()
	}
}

// createMovieList recursively walks a directory and return a list of all movie files
func createMovieList(baseDir string) (movies []*Movie) {

	log.Println("scanning media directory:", baseDir)

	if movieMap == nil {
		movieMap = make(map[string]*Movie)

		// read user playlists from file
		if dataFile, err := os.Open(movieDataFile); err == nil {
			movieMutex.Lock()
			decoder := json.NewDecoder(dataFile)
			decoder.Decode(&movieMap)
			movieMutex.Unlock()
			log.Println("movie database loaded:", len(movieMap))
			dataFile.Close()
		}
	}

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
		// protect insertion into map with mutex
		movieMutex.Lock()
		var mov *Movie
		var ok bool

		if mov, ok = movieMap[f]; !ok {
			mov = &Movie{Path: f}
			movieMap[f] = mov
		}
		movieMutex.Unlock()

		if iconPath, ok := IconMap[f]; ok {
			mov.IconPath = iconPath
		}
		movies = append(movies, mov)
	}
	return movies
}

// GenerateThumbnails scans the availability of thumbs for all movies
// and genrates missing ones
func GenerateThumbnails(mediaDir, outDir string) {
	log.Println("GenerateThumbnails -> scanning for new movies")
	_ = createMovieList(mediaDir)

	dirtyThumbs := false

	defer func() {
		if err := recover(); err != nil {
			log.Println("recovered from", err)
		}
	}()

	thumbsDirRel := "/img/thumbs"
	thumbsDirAbs := filepath.Join(outDir, thumbsDirRel)

	// create directory, if necessary
	if dirErr := os.MkdirAll(thumbsDirAbs, os.ModePerm); dirErr != nil {
		log.Println("could not create directory:", thumbsDirAbs)
		return
	}

	movieMutex.RLock()
	for _, mov := range movieMap {

		// check for existing thumbnail
		if p, hasIcon := IconMap[mov.Path]; hasIcon {
			mov.IconPath = p
		} else {

			// we are about to change something
			dirtyThumbs = true

			// open movie-file
			if movieFile, err := os.Open(mov.Path); err == nil {

				context, contextErr := thumbnailer.NewFFContext(movieFile)

				if contextErr != nil {
					log.Println(contextErr)
					continue
				}
				// get duration
				// movieDur := context.Duration()

				log.Println("generating thumb for: ", movieFile.Name())

				if thumb, thumbErr := context.Thumbnail(); thumbErr == nil {
					imgRelPath := filepath.Join(thumbsDirRel, filepath.Base(movieFile.Name())+".jpg")
					imgAbsPath := filepath.Join(outDir, imgRelPath)

					if imgFile, err := os.Create(imgAbsPath); err == nil {

						img := image.NewRGBA(image.Rect(0, 0, int(thumb.Width), int(thumb.Height)))
						img.Pix = thumb.Data

						if encodeErr := jpeg.Encode(imgFile, img, nil); encodeErr != nil {
							log.Println(encodeErr)
						}
						imgFile.Close()
						log.Println("done ->", imgRelPath)

						mov.IconPath = imgRelPath

						thumbMutex.Lock()
						IconMap[mov.Path] = imgRelPath
						thumbMutex.Unlock()
					} else {
						log.Println("could not create file:", imgAbsPath)
					}

				} else {
					log.Println("error:", thumbErr)
				}
				context.Close()
				movieFile.Close()
			} else {
				log.Println("could not open movie file")
			}
		}
	}
	movieMutex.RUnlock()

	if dirtyThumbs {
		if iconsFile, err := os.Create(thumbsFile); err == nil {
			defer iconsFile.Close()

			// encode playlist as json
			thumbMutex.RLock()
			enc := json.NewEncoder(iconsFile)
			enc.SetIndent("", "  ")
			enc.Encode(IconMap)
			log.Println("icons JSON written to ", iconsFile.Name())
			thumbMutex.RUnlock()
		}

		// force re-init
		Init(mediaDir)
	}
}
