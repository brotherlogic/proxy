package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brotherlogic/goserver"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	pbft "github.com/brotherlogic/frametracker/proto"
	gbspb "github.com/brotherlogic/gobuildslave/proto"
	pbg "github.com/brotherlogic/goserver/proto"
	"github.com/brotherlogic/goserver/utils"
	pb "github.com/brotherlogic/location/proto"
	ppb "github.com/brotherlogic/proxy/proto"
)

// Server main server type
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
	return []*pbg.State{}
}

var (
	hook = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "proxy_ghwhook",
		Help: "Push Size",
	}, []string{"error"})
)

func (s *Server) githubwebhook(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := utils.ManualContext("githubweb-fanout", time.Minute)

	hook.With(prometheus.Labels{"error": "received"}).Inc()
	defer r.Body.Close()
	bodyd, err := ioutil.ReadAll(r.Body)
	if err != nil {
		hook.With(prometheus.Labels{"error": "bodyread"}).Inc()
		s.CtxLog(ctx, fmt.Sprintf("Cannot read body: %v", err))
	}

	//Validate the webhook before fannin
	mac := hmac.New(sha1.New, []byte(s.githubKey))
	mac.Write(bodyd)
	expectedMAC := mac.Sum(nil)
	signature := fmt.Sprintf("sha1=%x", string(expectedMAC))

	if signature != r.Header.Get("X-Hub-Signature") {
		s.CtxLog(ctx, fmt.Sprintf("%v = %v vs %v from %v", r.Header.Get("X-Hub-Signature"), len(signature), len(r.Header.Get("X-Hub-Signature")), s.githubKey))
		hook.With(prometheus.Labels{"error": "signature"}).Inc()
		s.RaiseIssue("Bad Signature", fmt.Sprintf("%v did not match the expected signature", string(bodyd)))
		return
	}

	s.githubcount++

	entries, err := s.FFind(ctx, "githubreceiver")
	cancel()

	if err != nil || len(entries) == 0 {
		hook.With(prometheus.Labels{"error": "resolve"}).Inc()
		s.CtxLog(ctx, fmt.Sprintf("Unable to resolve githubcard: %v -> %v", err, entries))

		return
	}

	// Fanout
	first := false
	s.CtxLog(ctx, fmt.Sprintf("FANNING OUT TO %v", entries))
	for _, entry := range entries {
		elems := strings.Split(entry, ":")
		port, err := strconv.Atoi(elems[1])
		if err != nil {
			hook.With(prometheus.Labels{"error": "badread"}).Inc()
			s.CtxLog(ctx, fmt.Sprintf("Bad read: %v -> %v (%v)", err, elems[1], entry))
			continue
		}

		req, err := http.NewRequest(r.Method, fmt.Sprintf("http://%v:%v/githubwebhook", elems[0], port-1), bytes.NewReader(bodyd))
		for name, value := range r.Header {
			req.Header.Set(name, value[0])
		}
		if err != nil {
			hook.With(prometheus.Labels{"error": "process"}).Inc()
			s.CtxLog(ctx, fmt.Sprintf("Unable to process request: %v", err))
			continue
		}

		client := &http.Client{}
		resp, err := client.Do(req)

		// combined for GET/POST
		if err != nil {
			hook.With(prometheus.Labels{"error": "pass"}).Inc()
			s.CtxLog(ctx, fmt.Sprintf("Error doing: %v", err))
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
			s.CtxLog(ctx, fmt.Sprintf("written hook to %v", entry))
			hook.With(prometheus.Labels{"error": "nil"}).Inc()
			continue
		}
	}
}

func (s *Server) shutdown(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := utils.ManualContext("proxy-shutdown", time.Minute*10)
	defer cancel()

	s.CtxLog(ctx, "shutting down the cluster")

	wg := &sync.WaitGroup{}
	for i := 1; i <= 8; i++ {
		wg.Add(1)
		go func(val int) {
			conn, err := utils.LFDialSpecificServer(ctx, "gobuildslave", fmt.Sprintf("clust%v", val))
			if err != nil {
				s.CtxLog(ctx, fmt.Sprintf("Cannot dial gbs: %v", err))
				return
			}
			gbsclient := gbspb.NewBuildSlaveClient(conn)
			_, err = gbsclient.FullShutdown(ctx, &gbspb.ShutdownRequest{})
			s.CtxLog(ctx, fmt.Sprintf("Shutdown clust%v -> %v", val, err))
		}(i)
	}
	wg.Wait()
	s.CtxLog(ctx, "Shutdown complete")
}

func (s *Server) serveUp(port int32) {
	http.HandleFunc("/githubwebhook", s.githubwebhook)
	http.HandleFunc("/shutdown", s.shutdown)
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
	server.PrepServer("proxy")
	server.Register = server

	err := server.RegisterServerV2(true)
	if err != nil {
		return
	}

	ctx, cancel := utils.ManualContext("githubs", time.Minute)
	m, _, err := server.Read(ctx, "/github.com/brotherlogic/github/secret", &ppb.GithubKey{})
	if err != nil {
		log.Fatalf("error reading token: %v", err)
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
