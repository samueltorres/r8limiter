package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"sync"

	rl "github.com/envoyproxy/go-control-plane/envoy/api/v2/ratelimit"
	pb "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
	"google.golang.org/grpc"
)

func main() {
	var (
		grpcAddr  = flag.String("grpc-addr", "localhost:8081", "grpc address")
		httpAddr  = flag.String("http-addr", "localhost:8082", "http address")
		transport = flag.String("transport", "grpc", "type of transport (grpc/http)")
		routines  = flag.Int("routines", 20, "number of goroutines doing calls concurrently")
	)
	flag.Parse()

	cancel := make(chan struct{})
	wg := &sync.WaitGroup{}

	if *transport == "grpc" {
		wg.Add(1)
		go runGRPC(wg, cancel, *grpcAddr, *routines)
	} else if *transport == "http" {
		wg.Add(1)
		go runHTTP(wg, cancel, *httpAddr, *routines)
	}

	var input string
	fmt.Scanln(&input)
	close(cancel)
	wg.Wait()

	log.Printf("finished")
}

// ---------------- gRPC -----------------

func runGRPC(wg *sync.WaitGroup, cancel chan struct{}, addr string, routines int) {
	defer wg.Done()

	var conn *grpc.ClientConn
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %s", err)
	}
	defer conn.Close()
	client := pb.NewRateLimitServiceClient(conn)
	rwg := &sync.WaitGroup{}

	for i := 0; i < routines; i++ {
		rwg.Add(1)
		go grpcCaller(rwg, client, cancel)
	}
	rwg.Wait()
}

func grpcCaller(wg *sync.WaitGroup, client pb.RateLimitServiceClient, cancel chan struct{}) {
	for {
		select {
		case <-cancel:
			return
		default:
		}

		request := newRateLimitRequest()
		response, err := client.ShouldRateLimit(context.Background(), request)
		if err != nil {
			fmt.Printf("response error: %v", err)
		} else {
			fmt.Println(response)
		}
	}
}

// ---------------- HTTP -----------------

func runHTTP(wg *sync.WaitGroup, cancel chan struct{}, addr string, routines int) {
	defer wg.Done()

	client := &http.Client{}

	rwg := &sync.WaitGroup{}
	for i := 0; i < routines; i++ {
		rwg.Add(1)
		go httpCaller(rwg, client, addr, cancel)
	}
	rwg.Wait()
}

func httpCaller(wg *sync.WaitGroup, client *http.Client, addr string, cancel chan struct{}) {
	defer wg.Done()
	reqURL := "http://" + addr + "/rateLimit"

	for {
		select {
		case <-cancel:
			return
		default:
		}

		request := newRateLimitRequest()
		requestBody, err := json.Marshal(request)
		if err != nil {
			fmt.Printf("could not marshall http request body error: %v", err)
			continue
		}

		httpReq, err := http.NewRequest("POST", reqURL, bytes.NewBuffer(requestBody))
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(httpReq)
		if err != nil {
			fmt.Printf("http response error: %v", err)
			continue
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("http response error: %v", err)
			resp.Body.Close()
			continue
		}

		ratelimitResponse := &pb.RateLimitResponse{}
		err = json.Unmarshal(body, ratelimitResponse)
		if err != nil {
			fmt.Printf("could not unmarshall http response body error: %v", err)
		}

		fmt.Println(ratelimitResponse)
	}
}

// ---------------- Request Builder -----------------

func newRateLimitRequest() *pb.RateLimitRequest {
	tenant := rand.Intn(10000)

	return &pb.RateLimitRequest{
		Domain:     "kong",
		HitsAddend: 1,
		Descriptors: []*rl.RateLimitDescriptor{
			{
				Entries: []*rl.RateLimitDescriptor_Entry{
					{
						Key:   "tenant_id",
						Value: strconv.Itoa(tenant),
					},
				},
			},
		},
	}
}
