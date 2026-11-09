// public-test-metrics-sink is a deliberately small OTLP receiver for the
// disposable public test stand. It acknowledges worker metrics and reports
// aggregate batch sizes without logging attributes or other payload data.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	collectormetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

type receiver struct {
	collectormetrics.UnimplementedMetricsServiceServer
}

func (receiver) Export(_ context.Context, request *collectormetrics.ExportMetricsServiceRequest) (*collectormetrics.ExportMetricsServiceResponse, error) {
	resources := len(request.GetResourceMetrics())
	metrics := 0
	for _, resource := range request.GetResourceMetrics() {
		for _, scope := range resource.GetScopeMetrics() {
			metrics += len(scope.GetMetrics())
		}
	}
	log.Printf("received OTLP metrics batch: resources=%d metrics=%d", resources, metrics)
	return &collectormetrics.ExportMetricsServiceResponse{}, nil
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--healthcheck" {
		if err := checkHealth(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) != 1 {
		log.Fatal("usage: public-test-metrics-sink [--healthcheck]")
	}
	listener, err := net.Listen("tcp", ":4317")
	if err != nil {
		log.Fatal(err)
	}
	server := grpc.NewServer()
	collectormetrics.RegisterMetricsServiceServer(server, receiver{})
	status := health.NewServer()
	status.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, status)
	go func() {
		if err := server.Serve(listener); err != nil {
			log.Printf("metrics server stopped: %v", err)
		}
	}()
	log.Print("public test OTLP metrics receiver listening on :4317")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	status.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	server.GracefulStop()
}

func checkHealth() (resultErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := grpc.NewClient("127.0.0.1:4317", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, conn.Close()) }()
	response, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		return err
	}
	if response.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("metrics receiver status: %s", response.GetStatus())
	}
	return nil
}
