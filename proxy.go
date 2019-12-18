package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/brotherlogic/goserver"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	pbft "github.com/brotherlogic/frametracker/proto"
	pbg "github.com/brotherlogic/goserver/proto"
	"github.com/brotherlogic/goserver/utils"
	pb "github.com/brotherlogic/location/proto"
)

//Server main server type
type Server struct {
	*goserver.GoServer
	loccount    int64
	githubcount int64
	githuberr   string
}

// Init builds the server
func Init() *Server {
	s := &Server{
		&goserver.GoServer{},
		0,
		0,
		"",
	}
	return s
}

// DoRegister does RPC registration
func (s *Server) DoRegister(server *grpc.Server) {
	pb.RegisterLocationServiceServer(server, s)
	pbft.RegisterFrameTrackerServiceServer(server, s)
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
		&pbg.State{Key: "githubs", Value: s.githubcount},
		&pbg.State{Key: "locations", Value: s.loccount},
		&pbg.State{Key: "github_error", Text: s.githuberr},
	}
}

func (s *Server) githubwebhook(w http.ResponseWriter, r *http.Request) {
	s.githubcount++
	entries, err := utils.ResolveV3("githubreceiver")

	ctx, cancel := utils.ManualContext("proxy", "proxy-github", time.Minute)
	defer cancel()

	if err != nil || len(entries) == 0 {
		s.RaiseIssue(ctx, "Unable to pass on web hook", fmt.Sprintf("%v %v", err, entries), false)
		s.Log(fmt.Sprintf("Unable to resolve githubcard: %v -> %v", err, entries))
		return
	}

	defer r.Body.Close()
	bodyd, err := ioutil.ReadAll(r.Body)
	if err != nil {
		s.Log(fmt.Sprintf("Cannot read body: %v", err))
	}

	for _, entry := range entries {

		req, err := http.NewRequest(r.Method, fmt.Sprintf("http://%v:%v/githubwebhook", entry.Ip, entry.Port-1), bytes.NewReader(bodyd))
		for name, value := range r.Header {
			req.Header.Set(name, value[0])
		}
		if err != nil {
			s.RaiseIssue(ctx, "Unable to pass on web hook", fmt.Sprintf("%v", err), false)
			s.Log(fmt.Sprintf("Unable to process request: %v", err))
			break
		}

		client := &http.Client{}
		resp, err := client.Do(req)

		// combined for GET/POST
		if err != nil {
			s.RaiseIssue(ctx, "Unable to pass on web hook", fmt.Sprintf("%v", err), false)
			s.Log(fmt.Sprintf("Error doing: %v", err))
		} else {
			for k, v := range resp.Header {
				w.Header().Set(k, v[0])
			}
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
			resp.Body.Close()
			return
		}
	}
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

	err := server.RegisterServerV2("proxy", true, false)
	if err != nil {
		return
	}

	// Handle web requests
	go server.serveUp(server.Registry.Port - 1)
	server.Serve()
}
