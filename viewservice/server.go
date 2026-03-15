package viewservice

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"

	"github.com/hollinsStuart/pb-kv-service/proto/viewpb"
)

type ViewServer struct {
	viewpb.UnimplementedViewServiceServer

	mu       sync.Mutex
	currView *viewpb.View
	lastPing map[string]time.Time

	// NEW: Tracks the view number the Primary has explicitly acknowledged
	primaryAck uint64
}

func (vs *ViewServer) Ping(ctx context.Context, args *viewpb.PingArgs) (*viewpb.PingReply, error) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	// 1. Update the liveness tracker
	vs.lastPing[args.Me] = time.Now()

	// 2. Handle Primary Acknowledgment
	// If the ping is from the Primary, and it knows the current view, record the ack.
	if args.Me == vs.currView.Primary && args.ViewNum == vs.currView.ViewNum {
		vs.primaryAck = args.ViewNum
	}

	// 3. Check if we are allowed to change the view
	// We can only change the view if it's the very first view (0),
	// or if the current Primary has acknowledged the current view.
	canChangeView := vs.currView.ViewNum == 0 || vs.primaryAck == vs.currView.ViewNum

	if canChangeView {
		if vs.currView.Primary == "" {
			// Case A: Bootstrapping - The first node to ping becomes Primary
			vs.currView.Primary = args.Me
			vs.currView.ViewNum++
			fmt.Printf("[ViewService] View %d: %s initialized as Primary\n", vs.currView.ViewNum, args.Me)

		} else if vs.currView.Backup == "" && args.Me != vs.currView.Primary {
			// Case B: Adding a Backup - A new node pings while we have a Primary
			vs.currView.Backup = args.Me
			vs.currView.ViewNum++
			fmt.Printf("[ViewService] View %d: %s initialized as Backup\n", vs.currView.ViewNum, args.Me)
		}
	}

	// 4. Handle a crashed node restarting
	// If a node restarts, it loses its memory and sends ViewNum = 0.
	// If the Primary restarts, it is effectively dead and we must treat it as a failure.
	if args.Me == vs.currView.Primary && args.ViewNum == 0 && canChangeView {
		fmt.Printf("[ViewService] Warning: Primary %s restarted!\n", args.Me)
		vs.promoteBackup()
	}

	// Always return the view we currently consider to be active
	return &viewpb.PingReply{
		View: vs.currView,
	}, nil
}

// promoteBackup is a helper function to handle failure transitions
// Note: It assumes the caller already holds the vs.mu lock!
func (vs *ViewServer) promoteBackup() {
	if vs.currView.Backup != "" {
		fmt.Printf("[ViewService] View %d: Promoting Backup %s to Primary\n", vs.currView.ViewNum+1, vs.currView.Backup)
		vs.currView.Primary = vs.currView.Backup
		vs.currView.Backup = ""
		vs.currView.ViewNum++
	} else {
		fmt.Printf("[ViewService] View %d: Primary died, no backup available to promote!\n", vs.currView.ViewNum)
		vs.currView.Primary = ""
		// We do not increment the view here, we wait for a new node to ping and become Primary
	}
}

// tick runs periodically to check for dead nodes.
func (vs *ViewServer) tick() {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	// 1. Define our timeout threshold (e.g., 3 seconds)
	timeout := 3 * time.Second
	now := time.Now()

	// 2. We can only process timeouts and change the view if the current view is acknowledged
	canChangeView := vs.currView.ViewNum == 0 || vs.primaryAck == vs.currView.ViewNum
	if !canChangeView {
		return
	}

	viewChanged := false

	// 3. Check if the Primary has timed out
	if vs.currView.Primary != "" {
		last, ok := vs.lastPing[vs.currView.Primary]
		if !ok || now.Sub(last) > timeout {
			fmt.Printf("[ViewService] 🚨 Timeout: Primary %s is dead!\n", vs.currView.Primary)
			vs.promoteBackup()
			viewChanged = true
		}
	}

	// 4. Check if the Backup has timed out (only if we didn't just promote it)
	if !viewChanged && vs.currView.Backup != "" {
		last, ok := vs.lastPing[vs.currView.Backup]
		if !ok || now.Sub(last) > timeout {
			fmt.Printf("[ViewService] 🚨 Timeout: Backup %s is dead!\n", vs.currView.Backup)
			vs.currView.Backup = ""
			vs.currView.ViewNum++ // We must increment the view so the Primary knows it lost its backup
		}
	}
}

func (vs *ViewServer) Get(ctx context.Context, args *viewpb.GetArgs) (*viewpb.GetReply, error) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	return &viewpb.GetReply{
		View: vs.currView,
	}, nil
}

func StartServer(port string) *grpc.Server {
	vs := &ViewServer{
		currView:   &viewpb.View{ViewNum: 0, Primary: "", Backup: ""},
		lastPing:   make(map[string]time.Time),
		primaryAck: 0,
	}

	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", port, err)
	}

	grpcServer := grpc.NewServer()
	viewpb.RegisterViewServiceServer(grpcServer, vs)

	// Start the gRPC server listener
	go func() {
		fmt.Printf("ViewService listening on %s\n", port)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("Failed to serve ViewService: %v", err)
		}
	}()

	// NEW: Start the background liveness monitor
	go func() {
		for {
			time.Sleep(1 * time.Second) // Check every 1 second
			vs.tick()
		}
	}()

	return grpcServer
}
