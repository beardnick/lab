package main

import (
	"context"
	"log"
	"net"

	"google.golang.org/grpc"
)

type mockExampleService struct {
	UnimplementedExampleServiceServer
}

func (m *mockExampleService) SayHello(ctx context.Context, req *HelloRequest) (*HelloResponse, error) {
	// return nil, status.Errorf(codes.Unauthenticated, "test 403")
	return &HelloResponse{Message: "hi, " + req.Name + "!"}, nil
}

func main() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	RegisterExampleServiceServer(grpcServer, &mockExampleService{})

	log.Println("Starting mock gRPC server on port 50051...")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
