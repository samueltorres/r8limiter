package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	rl "github.com/envoyproxy/go-control-plane/envoy/api/v2/ratelimit"
	pb "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
	"google.golang.org/grpc"
)

func main() {

	var (
		grpcAddr = flag.String("grpc-addr", "10.152.183.198:8081", "gRPC listen address")
	)

	var conn *grpc.ClientConn
	conn, err := grpc.Dial(*grpcAddr, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %s", err)
	}
	defer conn.Close()
	c := pb.NewRateLimitServiceClient(conn)

	for {
		MakeCall(c)
	}
}

func MakeCall(c pb.RateLimitServiceClient) {
	defer func(begin time.Time) {
		fmt.Println("took > ", time.Since(begin))
	}(time.Now())

	resp, err := c.ShouldRateLimit(
		context.Background(),
		&pb.RateLimitRequest{
			Domain:     "envoy",
			HitsAddend: 1,
			Descriptors: []*rl.RateLimitDescriptor{
				{
					Entries: []*rl.RateLimitDescriptor_Entry{
						{
							Key:   "country",
							Value: "PT",
						},
						{
							Key:   "user_id",
							Value: "1234",
						},
					},
				},
			},
		})

	if err != nil {
		fmt.Println("error: ", err)
		return
	}

	fmt.Println(resp)
}
