package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brotherlogic/goserver"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	pbft "github.com/brotherlogic/frametracker/proto"
	pbg "github.com/brotherlogic/goserver/proto"
	"github.com/brotherlogic/goserver/utils"
	pb "github.com/brotherlogic/location/proto"
	ppb "github.com/brotherlogic/proxy/proto"
)

//Server main server type
type Server struct {
	*goserver.GoServer
	loccount    int64
	githubcount int64
	githuberr   string
	githubKey   string
}

// Init builds the server
func Init() *Server {
	s := &Server{
		&goserver.GoServer{},
		0,
		0,
		"",
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
		&pbg.State{Key: "yep", Value: int64(8)},
	}
}

var (
	hook = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "proxy_ghwhook",
		Help: "Push Size",
	}, []string{"error"})
)

func (s *Server) githubwebhook(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	bodyd, err := ioutil.ReadAll(r.Body)
	if err != nil {
		hook.With(prometheus.Labels{"error": "bodyread"}).Inc()
		s.Log(fmt.Sprintf("Cannot read body: %v", err))
	}

	//Validate the webhook before fannin
	mac := hmac.New(sha256.New, []byte(s.githubKey))
	mac.Write(bodyd)
	expectedMAC := mac.Sum(nil)
	signature := fmt.Sprintf("sha1=%v", string(expectedMAC))
	s.Log(fmt.Sprintf("Found signature %v vs %v", signature, r.Header.Get("X-Hub-Signature")))

	s.githubcount++
	ctx, cancel := utils.ManualContext("githubweb", "githubweb", time.Minute, true)
	entries, err := utils.LFFind(ctx, "githubreceiver")
	cancel()

	if err != nil || len(entries) == 0 {
		hook.With(prometheus.Labels{"error": "resolve"}).Inc()
		s.Log(fmt.Sprintf("Unable to resolve githubcard: %v -> %v", err, entries))

		return
	}

	// Fanout
	first := false
	for _, entry := range entries {
		elems := strings.Split(entry, ":")
		port, err := strconv.Atoi(elems[1])
		if err != nil {
			hook.With(prometheus.Labels{"error": "badread"}).Inc()
			s.Log(fmt.Sprintf("Bad read: %v -> %v (%v)", err, elems[1], entry))
			continue
		}

		req, err := http.NewRequest(r.Method, fmt.Sprintf("http://%v:%v/githubwebhook", elems[0], port-1), bytes.NewReader(bodyd))
		for name, value := range r.Header {
			req.Header.Set(name, value[0])
		}
		if err != nil {
			hook.With(prometheus.Labels{"error": "process"}).Inc()
			s.Log(fmt.Sprintf("Unable to process request: %v", err))
			continue
		}

		client := &http.Client{}
		resp, err := client.Do(req)

		// combined for GET/POST
		if err != nil {
			hook.With(prometheus.Labels{"error": "pass"}).Inc()
			s.RaiseIssue("Unable to pass on web hook [2]", fmt.Sprintf("%v", err))
			s.Log(fmt.Sprintf("Error doing: %v", err))
		} else {
			for k, v := range resp.Header {
				w.Header().Set(k, v[0])
			}
			if first {
				w.WriteHeader(resp.StatusCode)
				first = false
			}
			io.Copy(w, resp.Body)
			resp.Body.Close()
			s.Log(fmt.Sprintf("Written hook to %v", entry))
			continue
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
	server.DiskLog = true
	server.PrepServer()
	server.Register = server

	err := server.RegisterServerV2("proxy", true, true)
	if err != nil {
		return
	}

	ctx, cancel := utils.ManualContext("githubs", "githubs", time.Minute, true)
	m, _, err := server.Read(ctx, "/github.com/brotherlogic/github/secret", &ppb.GithubKey{})
	if err != nil {
		log.Fatalf("Error reading token: %v", err)
	}
	cancel()
	if len(m.(*ppb.GithubKey).GetKey()) == 0 {
		log.Fatalf("Error reading key: %v", m)
	}
	server.githubKey = m.(*ppb.GithubKey).GetKey()

	// Handle web requests
	go server.serveUp(server.Registry.Port - 1)
	server.Serve()
}
