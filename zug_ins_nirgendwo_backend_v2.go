package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"

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

func handleMovies(w http.ResponseWriter, r *http.Request) {

	// set content type
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	// create movielist
	movieList := playlist.CreateMovieList("/home/crocdialer/Movies")

	for _, mov := range movieList {
		log.Println(mov)
	}

	enc := json.NewEncoder(w)
	enc.Encode(movieList)
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
	muxRouter.HandleFunc("/movies", corsHandler(handleMovies)).Methods("GET", "OPTIONS")
	muxRouter.HandleFunc("/cmd", corsHandler(handleCommand)).Methods("POST", "OPTIONS")
	muxRouter.PathPrefix("/").Handler(fs)
	http.Handle("/", muxRouter)

	log.Println("server listening on port", listenPort, " -- serving files from", serveFilesPath)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", listenPort), nil))
}
