package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/brotherlogic/goserver"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	pbg "github.com/brotherlogic/goserver/proto"
	"github.com/brotherlogic/goserver/utils"
	pb "github.com/brotherlogic/location/proto"
)

//Server main server type
type Server struct {
	*goserver.GoServer
	loccount    int64
	githubcount int64
}

// Init builds the server
func Init() *Server {
	s := &Server{
		&goserver.GoServer{},
		0,
		0,
	}
	return s
}

// DoRegister does RPC registration
func (s *Server) DoRegister(server *grpc.Server) {
	pb.RegisterLocationServiceServer(server, s)
}

// ReportHealth alerts if we're not healthy
func (s *Server) ReportHealth() bool {
	return true
}

// Shutdown the server
func (s *Server) Shutdown(ctx context.Context) error {
	return nil
}

// Mote promotes/demotes this server
func (s *Server) Mote(ctx context.Context, master bool) error {
	return nil
}

// GetState gets the state of the server
func (s *Server) GetState() []*pbg.State {
	return []*pbg.State{
		&pbg.State{Key: "locations", Value: s.loccount},
		&pbg.State{Key: "githubs", Value: s.githubcount},
	}
}

func (s *Server) githubwebhook(w http.ResponseWriter, r *http.Request) {
	s.githubcount++
	entry, err := utils.GetMaster("githubcard")

	if err != nil {
		s.Log(fmt.Sprintf("Unable to resolve githubcard: %v", err))
		return
	}

	req, err := http.NewRequest(r.Method, fmt.Sprintf("http://%v:%v/githubwebhook", entry.Ip, entry.Port), r.Body)
	for name, value := range r.Header {
		req.Header.Set(name, value[0])
	}
	if err != nil {
		s.Log(fmt.Sprintf("Unable to process request: %v", err))
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	r.Body.Close()

	// combined for GET/POST
	if err != nil {
		s.Log(fmt.Sprintf("Error doing: %v", err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for k, v := range resp.Header {
		w.Header().Set(k, v[0])
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
	resp.Body.Close()
}

func (s *Server) serveUp(port int32) {
	http.HandleFunc("/githubwebhook", s.githubwebhook)
	err := http.ListenAndServe(fmt.Sprintf(":%v", port), nil)
	if err != nil {
		panic(err)
	}
}

func main() {
	var quiet = flag.Bool("quiet", false, "Show all output")
	flag.Parse()

	//Turn off logging
	if *quiet {
		log.SetFlags(0)
		log.SetOutput(ioutil.Discard)
	}
	server := Init()
	server.PrepServer()
	server.Register = server

	err := server.RegisterServer("proxy", true)
	if err != nil {
		log.Fatalf("Unable to register: %v", err)
	}

	// Handle web requests
	go server.serveUp(server.Registry.Port - 1)

	server.Log("Starting!")
	server.Serve()
}
