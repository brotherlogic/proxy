package main

import (
	"fmt"

	"golang.org/x/net/context"

	pbft "github.com/brotherlogic/frametracker/proto"
	pb "github.com/brotherlogic/location/proto"
)

// AddLocation adds the user location
func (s *Server) AddLocation(ctx context.Context, req *pb.AddLocationRequest) (*pb.AddLocationResponse, error) {
	s.Log(fmt.Sprintf("Received location for %v", req.Location.Name))
	conn, err := s.DialMaster("location")
	if err != nil {
		return &pb.AddLocationResponse{}, err
	}

	defer conn.Close()
	c := pb.NewLocationServiceClient(conn)
	lr, err := c.AddLocation(ctx, req)
	if err == nil {
		s.loccount++
	}
	return lr, err
}

// RecordStatus records status
func (s *Server) RecordStatus(ctx context.Context, req *pbft.StatusRequest) (*pbft.StatusResponse, error) {
	//s.Log(fmt.Sprintf("Received status %v", req))
	conn, err := s.FDialServer(ctx, "frametracker")
	if err != nil {
		return &pbft.StatusResponse{}, err
	}
	defer conn.Close()

	c := pbft.NewFrameTrackerServiceClient(conn)
	return c.RecordStatus(ctx, req)
}

// GetLocation gets the most recent user location
func (s *Server) GetLocation(ctx context.Context, req *pb.GetLocationRequest) (*pb.GetLocationResponse, error) {
	return &pb.GetLocationResponse{}, fmt.Errorf("Not implemented")
}
