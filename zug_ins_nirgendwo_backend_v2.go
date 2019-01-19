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

	// ps := playlist.GetPlaylists()

	var ps []*playlist.Playlist

	// decode json-request
	decoder := json.NewDecoder(r.Body)
	decoder.Decode(&ps)

	// log.Println("handlePlaylistsPOST", "items:")
	// for _, item := range ps {
	// 	log.Println("title:", item.Title)
	// 	for _, mov := range item.Movies {
	// 		log.Println("movie:", mov.Path)
	// 		log.Println("icon:", mov.IconPath)
	// 	}
	// }

	// set state to hold the altered playlist slice
	playlist.SetPlaylists(ps)

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

	// encode json ACK and send as response
	enc := json.NewEncoder(w)
	enc.Encode(true)
}

// GET
func handleSave(w http.ResponseWriter, r *http.Request) {
	// set content type
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.Encode(true)

	// save playlist state
	playlist.Save(mediaDir)

	// save settings in mediaplayer
	command := &command.Command{Command: "save_settings"}
	queueWorker.Commands <- command
}

// GET
func handleLoad(w http.ResponseWriter, r *http.Request) {
	// set content type
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.Encode(true)

	// load (re-init) playlist state
	playlist.Init(mediaDir)

	// load settings in mediaplayer
	command := &command.Command{Command: "load_settings"}
	queueWorker.Commands <- command
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
	muxRouter.HandleFunc("/save", corsHandler(handleSave)).Methods("GET", "OPTIONS")
	muxRouter.HandleFunc("/load", corsHandler(handleLoad)).Methods("GET", "OPTIONS")

	muxRouter.HandleFunc("/cmd", corsHandler(handleCommand)).Methods("POST", "OPTIONS")
	muxRouter.PathPrefix("/").Handler(fs)
	http.Handle("/", muxRouter)

	// init playlist module
	playlist.Init(mediaDir)

	// kick off periodic playbackstate updates
	playStateUpdater = playlist.NewPlaybackStateUpdater(playerAddress, time.Second, sseServer.PlaybackQueue)

	// generate thumbnails in the background
	go playlist.GenerateThumbnails(serveFilesPath)

	log.Println("server listening on port", listenPort, " -- serving files from", serveFilesPath)
	log.Println("media_player @", playerAddress)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", listenPort), nil))
}
