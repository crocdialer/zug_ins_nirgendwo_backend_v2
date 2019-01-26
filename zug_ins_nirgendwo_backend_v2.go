package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/crocdialer/zug_ins_nirgendwo_backend_v2/command"
	"github.com/crocdialer/zug_ins_nirgendwo_backend_v2/playlist"
	"github.com/crocdialer/zug_ins_nirgendwo_backend_v2/sse"
	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/mux"
)

// http listen port
var listenPort = 8080

// player TCP-port
var playerAddress = "127.0.0.1:33333"

// static serve directory
var serveFilesPath = "./public"

// handle for SSE-Server
var sseServer *sse.Server

// next command id
var nextCommandID int32 = 1

// holds a command queue and does the processing
var queueWorker *command.QueueWorker

// playback info and updater
var playStateUpdater *playlist.PlaybackStateUpdater

// media base directory
var mediaDir = "/media/astrobase/Movies"

// interval to scan the movie-directory
var autoSaveMinInterval = time.Minute * 5

var saveChan chan bool

// GET
func handlePlaylistsGET(w http.ResponseWriter, r *http.Request) {
	// set content type
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.Encode(playlist.GetPlaylists())
}

// POST
func handlePlaylistsPOST(w http.ResponseWriter, r *http.Request) {
	// set content type
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	var ps []*playlist.Playlist

	// decode json-request
	decoder := json.NewDecoder(r.Body)
	decoder.Decode(&ps)

	// set state to hold the altered playlist slice
	playlist.SetPlaylists(ps)

	// signal that we need to save
	trySave()

	enc := json.NewEncoder(w)
	enc.Encode(ps)
}

// GET
func handlePlayStateGET(w http.ResponseWriter, r *http.Request) {
	// set content type
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.Encode(playStateUpdater.GetState())
}

// POST
func handleCommand(w http.ResponseWriter, r *http.Request) {
	// set content type
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	// decode json-request
	command := &command.Command{}
	decoder := json.NewDecoder(r.Body)
	decoder.Decode(command)

	// insert struct-type and CommandID
	command.CommandID = int(atomic.AddInt32(&nextCommandID, 1))
	log.Println("command:", command)
	queueWorker.Commands <- command

	// encode json ACK and send as response
	enc := json.NewEncoder(w)
	enc.Encode(true)
}

// POST
func handlePlayback(w http.ResponseWriter, r *http.Request) {
	// set content type
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	// decode json-request
	newState := &playlist.PlaybackState{}
	decoder := json.NewDecoder(r.Body)
	decoder.Decode(newState)

	// make it so!
	go playStateUpdater.Playback(newState.MovieIndex, newState.PlaylistIndex)

	// signal a changed that we need to save
	trySave()

	// encode json ACK and send as response
	enc := json.NewEncoder(w)
	enc.Encode(true)
}

// preflight OPTIONS
func corsHandler(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		// configure proper CORS
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method != "OPTIONS" {
			handler(w, r)
		}
	}
}

func commandQueueCollector(results <-chan *command.ACK) {
	for ack := range results {

		// send ack via SSE
		sseServer.ACKQueue <- ack
	}
}

func watchMediaDirectory(mediaDir string, doneChan chan bool) {

	log.Println("watching media-directory:", mediaDir)

	// creates a new file watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Println("ERROR", err)
	}
	defer watcher.Close()

	// watch our mediaDir
	if err := watcher.Add(mediaDir); err != nil {
		fmt.Println("ERROR", err)
	}
	for {
		select {
		case event := <-watcher.Events:
			// watch for events
			log.Printf("event in directory: %#v\n", event)
			log.Println("waiting 2 minutes for things to calm down ...")
			time.Sleep(time.Minute * 2)
			playlist.GenerateThumbnails(mediaDir, serveFilesPath)

		case err := <-watcher.Errors:
			// watch for errors
			fmt.Println("ERROR", err)
		}
	}
}

func trySave() {
	select {
	case saveChan <- true:
		// log.Println("saving")
	default:
		// log.Println("save blocked (debouncing)")
	}
}

func saveDeBounced(timeOut time.Duration) {
	for {
		// will block eventually
		<-saveChan

		// log.Println("saveDeBounced: ok")

		// save playlist state
		playlist.Save(mediaDir)

		// save settings in mediaplayer
		command := &command.Command{Command: "save_settings"}
		queueWorker.Commands <- command

		time.Sleep(timeOut)
	}
}

func main() {
	log.Println("welcome", os.Args[0])

	// get serve path
	if len(os.Args) > 1 {
		serveFilesPath = os.Args[1]
	}

	// get server port
	if len(os.Args) > 2 {
		if p, err := strconv.Atoi(os.Args[2]); err == nil {
			listenPort = p
		}
	}

	// get media directory
	if len(os.Args) > 3 {
		mediaDir = os.Args[3]
	}

	// get player address and port
	if len(os.Args) > 4 {
		playerAddress = os.Args[4]
	}

	saveChan = make(chan bool, 2)

	// start command processing
	queueWorker = command.NewQueueWorker(playerAddress)
	go commandQueueCollector(queueWorker.Results)

	// serve static files
	fs := http.FileServer(http.Dir(serveFilesPath))

	// serves eventstream
	sseServer = sse.NewServer()

	// create a gorilla mux-router
	muxRouter := mux.NewRouter()

	// http services
	muxRouter.Handle("/events", sseServer)

	// get/set playlist information
	muxRouter.HandleFunc("/playlists", corsHandler(handlePlaylistsGET)).Methods("GET", "OPTIONS")
	muxRouter.HandleFunc("/playlists", corsHandler(handlePlaylistsPOST)).Methods("POST", "OPTIONS")

	// set the current playback indices for movie/playlist
	muxRouter.HandleFunc("/playback", corsHandler(handlePlayback)).Methods("POST", "OPTIONS")

	muxRouter.HandleFunc("/playstate", corsHandler(handlePlayStateGET)).Methods("GET", "OPTIONS")

	muxRouter.HandleFunc("/cmd", corsHandler(handleCommand)).Methods("POST", "OPTIONS")
	muxRouter.PathPrefix("/").Handler(fs)
	http.Handle("/", muxRouter)

	// init playlist module
	playlist.Init(mediaDir)

	// initial thumb generation
	playlist.GenerateThumbnails(mediaDir, serveFilesPath)

	// kick off periodic playbackstate updates
	playStateUpdater = playlist.NewPlaybackStateUpdater(playerAddress, time.Second, sseServer.PlaybackQueue)

	// watch for changes in directory
	go watchMediaDirectory(mediaDir, nil)

	// debounced save-settings routine
	go saveDeBounced(autoSaveMinInterval)

	log.Println("server listening on port", listenPort, " -- serving files from", serveFilesPath)
	log.Println("media_player @", playerAddress)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", listenPort), nil))
}
