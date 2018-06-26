package main

import (
	"fmt"
	"strconv"

	"github.com/brotherlogic/goserver/utils"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	pb "github.com/brotherlogic/location/proto"
)

// AddLocation adds the user location
func (s *Server) AddLocation(ctx context.Context, req *pb.AddLocationRequest) (*pb.AddLocationResponse, error) {
	ip, port, err := utils.Resolve("location")
	if err != nil {
		return &pb.AddLocationResponse{}, err
	}

	conn, err := grpc.Dial(ip+":"+strconv.Itoa(int(port)), grpc.WithInsecure())
	if err != nil {
		return &pb.AddLocationResponse{}, err
	}

	defer conn.Close()
	c := pb.NewLocationServiceClient(conn)
	return c.AddLocation(ctx, req)
}

// GetLocation gets the most recent user location
func (s *Server) GetLocation(ctx context.Context, req *pb.GetLocationRequest) (*pb.GetLocationResponse, error) {
	return &pb.GetLocationResponse{}, fmt.Errorf("Not implemented")
}
