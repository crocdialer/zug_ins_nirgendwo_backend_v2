package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/crocdialer/zug_ins_nirgendwo_backend_v2/sse"
	"github.com/gorilla/mux"
)

// http listen port
var listenPort = 8080

// player TCP-port
var playerAddress = "127.0.0.1:33333"

// static serve directory
var serveFilesPath = "./public"

// IO channels
var serialInput, serialOutput chan []byte

// handle for SSE-Server
var sseServer *sse.Server

// next command id
var nextCommandID int32 = 1

// command queue
var commandQueue, commandsDone chan *Command

// Command realizes a simple RPC interface
type Command struct {
	CommandID int           `json:"id"`
	Command   string        `json:"cmd"`
	Arguments []interface{} `json:"arg"`
}

func (cmd *Command) String() string {
	str := cmd.Command

	for _, arg := range cmd.Arguments {
		str += " " + fmt.Sprintf("%v", arg)
	}
	return str
}

// CommandACK is used as simple ACK for received commands
type CommandACK struct {
	CommandID int           `json:"id"`
	Success   bool          `json:"success"`
	Value     []interface{} `json:"value"`
}

// Open will create a tcp-connection, wrapped in a bufio.ReadWriter
// func Open(addr string) (*bufio.ReadWriter, error) {
// 	log.Println("Dial " + addr)
// 	conn, err := net.Dial("tcp", addr)
// 	if err != nil {
// 		return nil, errors.Wrap(err, "Dialing "+addr+" failed")
// 	}
// 	return bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn)), nil
// }

// POST
func handleCommand(w http.ResponseWriter, r *http.Request) {

	// configure proper CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	// decode json-request
	command := &Command{}
	decoder := json.NewDecoder(r.Body)
	decoder.Decode(command)

	// insert struct-type and CommandID
	command.CommandID = int(atomic.AddInt32(&nextCommandID, 1))
	log.Println("command:", command)
	commandQueue <- command

	ack := CommandACK{CommandID: command.CommandID}

	// encode json ACK and send as response
	enc := json.NewEncoder(w)
	enc.Encode(ack)
}

// preflight OPTIONS
func corsHandler(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		} else {
			handler(w, r)
		}
	}
}

func commandQueueWorker(commands <-chan *Command, results chan<- *Command) {

	responseBuffer := make([]byte, 1<<12)

	for cmd := range commands {

		// tcp communication with kinskiPlayer here
		// con, err := Open(playerAddress)
		con, err := net.Dial("tcp", playerAddress)
		con.SetReadDeadline(time.Now().Add(time.Millisecond * 50))

		if err == nil {
			str := cmd.String() + "\n"

			// send to player
			// con.WriteString(str)
			con.Write([]byte(str))

			// try to read a response
			// response, readError := con.ReadString('\n')

			numBytes, readError := con.Read(responseBuffer)

			if readError != nil {
				log.Println(readError)
			} else {
				log.Println(string(responseBuffer[:numBytes]))
			}
			con.Close()
		}
	}
}

func commandQueueCollector() {
	for cmd := range commandsDone {

		// emit SSE update
		sseServer.Notifier <- []byte(cmd.String())
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

	commandQueue = make(chan *Command, 100)
	commandsDone = make(chan *Command, 100)

	// start command processing
	go commandQueueWorker(commandQueue, commandsDone)
	go commandQueueCollector()

	// serve static files
	fs := http.FileServer(http.Dir(serveFilesPath))

	// serves eventstream
	sseServer = sse.NewServer()

	// create a gorilla mux-router
	muxRouter := mux.NewRouter()

	// http services
	muxRouter.Handle("/events", sseServer)
	muxRouter.HandleFunc("/cmd", corsHandler(handleCommand)).Methods("POST", "OPTIONS")
	muxRouter.PathPrefix("/").Handler(fs)
	http.Handle("/", muxRouter)

	log.Println("server listening on port", listenPort, " -- serving files from", serveFilesPath)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", listenPort), nil))
}
